package apitest_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/grammar"
)

func TestCompileSource(t *testing.T) {
	p, err := compileTestSource(`type Pair = { Key node[string]; Value node[string] }
rule main = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic => Pair{Key: $k, Value: $v}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	n, err := p.Parse("abc=12")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.String(); got != `Pair{Key: "abc", Value: "12"}` {
		t.Errorf("got %s", got)
	}
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Key":{"start":0,"end":3,"value":"abc"}`) {
		t.Errorf("json %s", data)
	}

	_, err = p.Parse("abc=")
	var se *zp.SyntaxError
	if !errors.As(err, &se) || se.Line != 1 || se.Col != 5 {
		t.Errorf("got %v", err)
	}
}

func TestCompileAST(t *testing.T) {
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{
		&grammar.RuleDef{Attrs: []*grammar.Attribute{{Name: "start"}}, Name: "main", Expr: &grammar.Seq{Items: []grammar.Expr{
			&grammar.Literal{Value: "a"}, &grammar.Literal{Value: "b"},
		}}},
	}}}
	p, err := zp.Compile(g, "main", zp.WithSourceInfo())
	if err != nil {
		t.Fatal(err)
	}
	n, err := p.Parse("ab")
	if err != nil {
		t.Fatal(err)
	}
	if n.Start() != 0 || n.End() != 2 || n.Rule() != "main" {
		t.Errorf("got %+v", n)
	}
}

func TestCompileErrors(t *testing.T) {
	if _, err := compileTestSource(`def main = (`, "main"); err == nil {
		t.Error("expected a syntax error")
	}
	if _, err := compileTestSource(`rule main = x`, "main"); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
}

func TestParseStream(t *testing.T) {
	p, err := compileTestSource(`rule main = ([a-z]+ #atomic -"\n")* #stream`, "main")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	err = p.ParseStream(strings.NewReader("ab\ncd\n"), func(n *zp.Node) error {
		got = append(got, n.String())
		return nil
	})
	if err != nil || strings.Join(got, " ") != `("ab") ("cd")` {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestDocument(t *testing.T) {
	p, err := compileTestSource(`rule main = line*
rule line = [a-z]+ #atomic -"\n"`, "main", zp.WithSourceInfo())
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
	if err != nil || n.String() != `[("ab")@line ("xyz")@line ("ef")@line]@main` || doc.Text() != "ab\nxyz\nef\n" {
		t.Errorf("got %v, %v", n, err)
	}
	if st := doc.Stats(); st.Reused == 0 {
		t.Errorf("stats %+v", st)
	}
}

func TestMarshalAndLoadParser(t *testing.T) {
	p, err := compileTestSource(`type Pair = { Key node[string]; Value node[string] }
rule main = k:key "=" v:[0-9]+ #atomic => Pair{Key: $k, Value: $v}
rule key = [a-z]+ #atomic`, "main", zp.WithSourceInfo())
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !zp.IsCompiled(data) || zp.IsCompiled([]byte("rule main = x")) {
		t.Error("IsCompiled")
	}
	loaded, err := zp.LoadParser(data)
	if err != nil {
		t.Fatal(err)
	}
	n, err := loaded.Parse("abc=12")
	if err != nil || n.String() != `Pair{Key: "abc"@key, Value: "12"}` {
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
	// The grammar stays with the source: a compiled grammar holds only bytecode.
	if loaded.Grammar() != nil {
		t.Errorf("grammar of a loaded parser %s", grammar.Format(loaded.Grammar()))
	}
	if got := grammar.Format(p.Grammar()); !strings.Contains(got, "rule key = [a-z]+ #atomic") {
		t.Errorf("grammar %s", got)
	}
	if mode, ok := loaded.CompiledMode(); !ok || mode != zp.CompiledParse {
		t.Errorf("mode of MarshalBinary: %v %v", mode, ok)
	}
	if _, ok := p.CompiledMode(); ok {
		t.Error("mode of a parser compiled from source")
	}
	if _, err := zp.LoadParser([]byte("garbage")); err == nil {
		t.Error("expected an error")
	}
}

// TestCompiledModes checks that each mode of a compiled grammar serves its own operations, refuses
// the others with the mode they need, is saved again in its own mode only, and is named by
// ParseCompiledMode.
func TestCompiledModes(t *testing.T) {
	p, err := compileTestSource(`rule main = pair* #stream $$
rule pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic => list($k, $v)`, "main")
	if err != nil {
		t.Fatal(err)
	}
	modes := []zp.CompiledMode{zp.CompiledParse, zp.CompiledRecognize, zp.CompiledDocument}
	for _, mode := range modes {
		if got, err := zp.ParseCompiledMode(mode.String()); err != nil || got != mode {
			t.Errorf("ParseCompiledMode(%q) = %v, %v", mode.String(), got, err)
		}
	}
	if _, err := zp.ParseCompiledMode("ast"); err == nil {
		t.Error("ParseCompiledMode accepted an unknown mode")
	}
	parse := func(q *zp.Parser) error { _, err := q.Parse("a=1"); return err }
	stream := func(q *zp.Parser) error {
		return q.ParseStream(strings.NewReader("a=1"), func(*zp.Node) error { return nil })
	}
	prefix := func(q *zp.Parser) error { _, err := q.ParsePrefix("main", "a=1"); return err }
	iterate := func(opts ...zp.ParseOption) func(q *zp.Parser) error {
		return func(q *zp.Parser) error {
			it, err := q.Iterate(strings.NewReader("a=1b=2"), opts...)
			if err != nil {
				return err
			}
			defer it.Close()
			n := 0
			for {
				_, ok, err := it.Next()
				if err != nil {
					return err
				}
				if !ok {
					break
				}
				n++
			}
			if n != 2 {
				return fmt.Errorf("iterate: %d elements, want 2", n)
			}
			return nil
		}
	}
	recognize := func(q *zp.Parser) error { _, err := q.Parse("a=1", zp.RecognizeOnly()); return err }
	trace := func(q *zp.Parser) error {
		_, err := q.Parse("a=1", zp.WithTrace(func(zp.TraceEvent) {}))
		return err
	}
	profile := func(q *zp.Parser) error {
		_, err := q.Parse("a=1", zp.WithProfile(&zp.Profile{}))
		return err
	}
	document := func(q *zp.Parser) error {
		doc, err := q.NewDocument("a=1")
		if err == nil {
			_, err = doc.Parse()
		}
		return err
	}
	ops := []struct {
		name string
		run  func(*zp.Parser) error
		need zp.CompiledMode
	}{
		{"parse", parse, zp.CompiledParse},
		{"stream", stream, zp.CompiledParse},
		{"prefix", prefix, zp.CompiledParse},
		{"iterate", iterate(), zp.CompiledParse},
		{"traced iterate", iterate(zp.WithTrace(func(zp.TraceEvent) {})), zp.CompiledDocument},
		{"recognize", recognize, zp.CompiledRecognize},
		{"trace", trace, zp.CompiledDocument},
		{"profile", profile, zp.CompiledDocument},
		{"document", document, zp.CompiledDocument},
	}
	for _, mode := range modes {
		data, err := p.Marshal(mode)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := zp.LoadParser(data)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := loaded.CompiledMode(); !ok || got != mode {
			t.Errorf("%s: loaded mode %v %v", mode, got, ok)
		}
		if again, err := loaded.Marshal(mode); err != nil || !bytes.Equal(again, data) {
			t.Errorf("%s: saved again differently: %v", mode, err)
		}
		for _, other := range modes {
			if other == mode {
				continue
			}
			if _, err := loaded.Marshal(other); err == nil || !strings.Contains(err.Error(), "saved only in that mode") {
				t.Errorf("%s file saved in %s mode: %v", mode, other, err)
			}
		}
		for _, op := range ops {
			err := op.run(loaded)
			if op.need == mode {
				if err != nil {
					t.Errorf("%s file, %s: %v", mode, op.name, err)
				}
				continue
			}
			if err == nil || !strings.Contains(err.Error(), "the compiled grammar is in "+mode.String()+" mode") ||
				!strings.Contains(err.Error(), " "+op.need.String()+" mode: compile it") || !strings.Contains(err.Error(), "-mode "+op.need.String()) {
				t.Errorf("%s file, %s: %v", mode, op.name, err)
			}
		}
		// Traced or profiled recognition needs the grammar source in every mode.
		for _, opt := range []zp.ParseOption{zp.WithTrace(func(zp.TraceEvent) {}), zp.WithProfile(&zp.Profile{})} {
			if _, err := loaded.Parse("a=1", zp.RecognizeOnly(), opt); err == nil || !strings.Contains(err.Error(), "needs the grammar source") {
				t.Errorf("%s file, traced recognition: %v", mode, err)
			}
		}
	}
	if _, err := p.Marshal(zp.CompiledMode(9)); err == nil {
		t.Error("saved in an unknown mode")
	}
}

func TestUnits(t *testing.T) {
	p, err := compileTestSource(`rule main = "é" x:.`, "main", zp.WithSourceInfo())
	if err != nil {
		t.Fatal(err)
	}
	for unit, want := range map[zp.PositionUnit]int{zp.CodePoints: 1, zp.Bytes: 2} {
		n, err := p.Parse("éa", zp.WithUnit(unit))
		if err != nil || n.Field("x").(*zp.Node).Start() != want {
			t.Errorf("%v: %v %v", unit, n, err)
		}
	}
	doc, err := p.NewDocument("éa", zp.WithUnit(zp.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Edit(2, 3, "b"); err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.Field("x").(*zp.Node).Text() != "b" {
		t.Errorf("%v %v", n, err)
	}
}

func TestBackends(t *testing.T) {
	p, err := compileTestSource(`type Pair = { Key node[string]; Value node[string] }
rule main = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic => Pair{Key: $k, Value: $v}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []zp.Backend{zp.DefaultBackend, zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		n, err := p.Parse("abc=12", zp.WithBackend(b))
		if err != nil || n.String() != `Pair{Key: "abc", Value: "12"}` {
			t.Errorf("%v: %v %v", b, n, err)
		}
	}
	// A compiled grammar runs on the bytecode backends, each mode serving its own operations.
	load := func(mode zp.CompiledMode) *zp.Parser {
		t.Helper()
		data, err := p.Marshal(mode)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := zp.LoadParser(data)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Grammar() != nil {
			t.Errorf("grammar of a parser loaded in %s mode", mode)
		}
		return loaded
	}
	parsing := load(zp.CompiledParse)
	if n, err := parsing.Parse("abc=12"); err != nil || n.String() != `Pair{Key: "abc", Value: "12"}` {
		t.Errorf("parse mode: %v %v", n, err)
	}
	if _, err := parsing.Parse("abc=12", zp.WithBackend(zp.Closure)); err == nil || !strings.Contains(err.Error(), "closure backend needs the grammar source") {
		t.Errorf("closure backend on a compiled grammar: %v", err)
	}
	recognizing := load(zp.CompiledRecognize)
	for _, backend := range []zp.Backend{zp.Bytecode, zp.BytecodeIterative} {
		if n, err := recognizing.Parse("abc=12", zp.WithBackend(backend), zp.RecognizeOnly()); n != nil || err != nil {
			t.Errorf("recognition on %v: %v %v", backend, n, err)
		}
	}
	documents := load(zp.CompiledDocument)
	if _, err := documents.NewDocument("a=1", zp.WithBackend(zp.Closure)); err == nil || !strings.Contains(err.Error(), "closure backend needs the grammar source") {
		t.Errorf("document with the closure backend on a compiled grammar: %v", err)
	}
	doc, err := documents.NewDocument("a=1")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.String() != `Pair{Key: "a", Value: "1"}` {
		t.Errorf("document: %v %v", n, err)
	}
}

func TestRecognizeOnly(t *testing.T) {
	p, err := compileTestSource(`type Pair = { Key node[string]; Value node[string] }
rule main = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic => Pair{Key: $k, Value: $v}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		if n, err := p.Parse("abc=12", zp.RecognizeOnly(), zp.WithBackend(b)); n != nil || err != nil {
			t.Errorf("%v: %v %v", b, n, err)
		}
		_, want := p.Parse("abc=x")
		if n, err := p.Parse("abc=x", zp.RecognizeOnly(), zp.WithBackend(b)); n != nil || err == nil || err.Error() != want.Error() {
			t.Errorf("%v: %v %v, want %v", b, n, err, want)
		}
	}
	if _, err := p.NewDocument("a=1", zp.RecognizeOnly()); err == nil {
		t.Error("document with RecognizeOnly")
	}
}
