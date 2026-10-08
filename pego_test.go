package pego_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

func TestCompileSource(t *testing.T) {
	p, err := pego.CompileSource(`
type Pair struct { Key Match, Value Match }
def main = k:@(?a-z)+ "=" v:@(?0-9)+ -> new Pair{Key: $k, Value: $v}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	n, err := p.Parse("abc=12")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.String(); got != `(Pair Key="abc" Value="12")` {
		t.Errorf("got %s", got)
	}
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Key":{"type":"Match","start":0,"end":3,"text":"abc"}`) {
		t.Errorf("json %s", data)
	}

	_, err = p.Parse("abc=")
	var se *pego.SyntaxError
	if !errors.As(err, &se) || se.Line != 1 || se.Col != 5 {
		t.Errorf("got %v", err)
	}
}

func TestCompileAST(t *testing.T) {
	g := &grammar.Grammar{Statements: []grammar.Statement{
		&grammar.RuleDef{Name: "main", Expr: &grammar.Seq{Items: []grammar.Expr{
			&grammar.Literal{Value: "a"}, &grammar.Literal{Value: "b"},
		}}},
	}}
	p, err := pego.Compile(g, "main")
	if err != nil {
		t.Fatal(err)
	}
	n, err := p.Parse("ab")
	if err != nil {
		t.Fatal(err)
	}
	if n.Start != 0 || n.End != 2 || n.Rule() != "main" {
		t.Errorf("got %+v", n)
	}
}

func TestCompileErrors(t *testing.T) {
	if _, err := pego.CompileSource(`def main = (`, "main"); err == nil {
		t.Error("expected a syntax error")
	}
	if _, err := pego.CompileSource(`def main = x`, "main"); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
}

func TestParseStream(t *testing.T) {
	p, err := pego.CompileSource(`def main = (@(?a-z)+ -"\n")* #stream`, "main")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	err = p.ParseStream(strings.NewReader("ab\ncd\n"), func(n *pego.Node) error {
		got = append(got, n.String())
		return nil
	})
	if err != nil || strings.Join(got, " ") != `(Seq "ab") (Seq "cd")` {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestDocument(t *testing.T) {
	p, err := pego.CompileSource(`def main = line*
def line = @(?a-z)+ -"\n"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.NewDocument("ab\ncd\nef\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal(err)
	}
	if err := doc.Edit(3, 5, "xyz"); err != nil {
		t.Fatal(err)
	}
	n, err := doc.Parse()
	if err != nil || n.String() != `[(Seq "ab")@line (Seq "xyz")@line (Seq "ef")@line]@main` || doc.Text() != "ab\nxyz\nef\n" {
		t.Errorf("got %v, %v", n, err)
	}
	if st := doc.Stats(); st.Reused == 0 {
		t.Errorf("stats %+v", st)
	}
}

func TestMarshalAndLoadParser(t *testing.T) {
	p, err := pego.CompileSource(`
type Pair struct { Key Match, Value Match }
def main = k:key "=" v:@(?0-9)+ -> new Pair{Key: $k, Value: $v}
def key = @(?a-z)+`, "main")
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !pego.IsCompiled(data) || pego.IsCompiled([]byte("def main = x")) {
		t.Error("IsCompiled")
	}
	loaded, err := pego.LoadParser(data)
	if err != nil {
		t.Fatal(err)
	}
	n, err := loaded.Parse("abc=12")
	if err != nil || n.String() != `(Pair Key="abc"@key Value="12")` {
		t.Errorf("got %v, %v", n, err)
	}
	key, err := loaded.WithStart("key")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := key.Parse("xyz"); err != nil || n.String() != `"xyz"@key` {
		t.Errorf("got %v, %v", n, err)
	}
	if _, err := loaded.WithStart("nope"); err == nil {
		t.Error("expected an error")
	}
	if got := grammar.Format(loaded.Grammar()); !strings.Contains(got, "def key = @(?a-z)+") {
		t.Errorf("grammar %s", got)
	}
	if _, err := pego.LoadParser([]byte("garbage")); err == nil {
		t.Error("expected an error")
	}
}

func TestUnits(t *testing.T) {
	p, err := pego.CompileSource(`def main = "é" x:.`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for unit, want := range map[pego.Unit]int32{pego.CodePoints: 1, pego.Bytes: 2} {
		n, err := p.Parse("éa", pego.WithUnit(unit))
		if err != nil || n.Field("x").(*pego.Node).Start != want {
			t.Errorf("%v: %v %v", unit, n, err)
		}
	}
	doc, err := p.NewDocument("éa", pego.WithUnit(pego.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Edit(2, 3, "b"); err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.Field("x").(*pego.Node).Text != "b" {
		t.Errorf("%v %v", n, err)
	}
}

func TestBackends(t *testing.T) {
	p, err := pego.CompileSource(`type Pair struct { Key Match, Value Match }
def main = k:@(?a-z)+ "=" v:@(?0-9)+ -> new Pair{Key: $k, Value: $v}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []pego.Backend{pego.DefaultBackend, pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		n, err := p.Parse("abc=12", pego.WithBackend(b))
		if err != nil || n.String() != `(Pair Key="abc" Value="12")` {
			t.Errorf("%v: %v %v", b, n, err)
		}
	}
	// A parser saved without the AST parses with the bytecode backend.
	data, err := p.Marshal(pego.WithoutAST())
	if err != nil {
		t.Fatal(err)
	}
	full, _ := p.MarshalBinary()
	if len(data) >= len(full) {
		t.Errorf("without AST %d bytes, with AST %d bytes", len(data), len(full))
	}
	bare, err := pego.LoadParser(data)
	if err != nil {
		t.Fatal(err)
	}
	if bare.Grammar() != nil {
		t.Error("grammar of a parser saved without AST")
	}
	if n, err := bare.Parse("abc=12"); err != nil || n.String() != `(Pair Key="abc" Value="12")` {
		t.Errorf("bare: %v %v", n, err)
	}
	if _, err := bare.Parse("abc=12", pego.WithBackend(pego.Closure)); err == nil {
		t.Error("closure backend without AST")
	}
	if _, err := bare.NewDocument("a=1", pego.WithBackend(pego.Closure)); err == nil {
		t.Error("document with closure backend without AST")
	}
	doc, err := bare.NewDocument("a=1")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.String() != `(Pair Key="a" Value="1")` {
		t.Errorf("document: %v %v", n, err)
	}
}

func TestRecognizeOnly(t *testing.T) {
	p, err := pego.CompileSource(`type Pair struct { Key Match, Value Match }
def main = k:@(?a-z)+ "=" v:@(?0-9)+ -> new Pair{Key: $k, Value: $v}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		if n, err := p.Parse("abc=12", pego.RecognizeOnly(), pego.WithBackend(b)); n != nil || err != nil {
			t.Errorf("%v: %v %v", b, n, err)
		}
		_, want := p.Parse("abc=x")
		if n, err := p.Parse("abc=x", pego.RecognizeOnly(), pego.WithBackend(b)); n != nil || err == nil || err.Error() != want.Error() {
			t.Errorf("%v: %v %v, want %v", b, n, err, want)
		}
	}
	if _, err := p.NewDocument("a=1", pego.RecognizeOnly()); err == nil {
		t.Error("document with RecognizeOnly")
	}
}
