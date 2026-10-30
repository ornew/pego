package compile

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestIndexTypeErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = xs:"a"+ => $xs["0"]`, "index must be int, got string"},
		{`rule main = x:"a" => $x[0]`, "cannot index node[string]"},
		{`rule main = "a" => (1)[0]`, "cannot index int"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// A node indexes its children.
	for _, src := range []string{
		"rule main = x:item => $x[0]\nrule item = a:\"a\" b:\"b\" => list($a, $b)",
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	// The element type of a list is the type of an indexed value.
	for _, tc := range []struct {
		field string
		ok    bool
	}{{"node[string]", true}, {"*node[string]", true}, {"int", false}} {
		g, err := parseTest("type Out = { First " + tc.field + " }\nrule main = xs:\"a\"+ => Out{First: $xs[0]}")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); (err == nil) != tc.ok {
			t.Errorf("field %s: %v", tc.field, err)
		}
	}
}

// Indexing is written directly after the indexed value; with a space before it, "[" begins a
// character class.
func TestIndexSyntax(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = xs:item+ => list($xs, $xs)[1][0]
rule item = [a-z]`)
	if err != nil {
		t.Fatal(err)
	}
	r := g.Rules()[0]
	outer, ok := r.Action.(*grammar.Binary)
	if !ok || outer.Op != "[]" {
		t.Fatalf("action %T", r.Action)
	}
	if inner, ok := outer.L.(*grammar.Binary); !ok || inner.Op != "[]" {
		t.Fatalf("inner %T", outer.L)
	}
	if got := grammar.Format(g); !strings.Contains(got, "list($xs, $xs)[1][0]") {
		t.Fatalf("formatted:\n%s", got)
	}
	// Parentheses that the source does not keep are added where "[" would not index.
	for _, x := range []grammar.Term{&grammar.IntLit{Value: 1}, &grammar.StringLit{Value: "a"}, &grammar.New{Type: "T"}} {
		formatted := grammar.Format(&grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Any{}, Action: &grammar.Binary{Op: "[]", L: x, R: &grammar.IntLit{Value: 0}}}}}})
		if !strings.Contains(formatted, ")[0]") {
			t.Errorf("formatted %T index: %s", x, formatted)
		}
	}
	if _, err := parseTest(`rule main = item[a-z]
rule item = "a"`); err == nil || !strings.Contains(err.Error(), "write a space before it") {
		t.Fatalf("adjacent character class: %v", err)
	}
	for _, src := range []string{
		"rule main = item [a-z]\nrule item = \"a\"",
		"rule main = (item) [a-z]\nrule item = \"a\"",
		"rule main = [a-z][0-9]",
		"rule main = \"a\"[0-9]",
	} {
		if _, err := parseTest(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}
