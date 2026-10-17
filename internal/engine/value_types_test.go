package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// The value types of rules follow the examples and decisions of Record 060.
func TestValueTypes(t *testing.T) {
	for _, tc := range []struct{ src, rule, want string }{
		// The examples of the record.
		{`rule a = "x"`, "a", `node[string]`},
		{`rule a = ("x" "y"*) #atomic`, "a", `node[string]`},
		{"rule a = b c d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, c, d)]`},
		{"rule a = b+\nrule b = \"b\"", "a", `node[[]b]`},
		{`rule a = "a"?`, "a", `*node[string]`},
		{"rule a = b? c\nrule b = \"b\"\nrule c = \"c\"", "a", `node[(*b, c)]`},
		{"type Foo = {}\nrule a = _ => Foo{}\nrule b = a \"a\"*", "a", `Foo`},
		{"type Foo = {}\nrule a = _ => Foo{}\nrule b = a \"a\"*", "b", `node[(a, node[[]node[string]])]`},
		// Call-only rules are transparent.
		{"rule a = b\nrule b = \"b\"", "a", `b`},
		// Sequences are always tuples, and groups flatten.
		{"rule a = -ws b $$\nrule b = \"b\"\nrule ws = \" \"*", "a", `node[(b,)]`},
		{"rule a = b (c d)\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, c, d)]`},
		{"rule a = (b c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, c, d)]`},
		// Valueless bodies produce the unit node.
		{`rule stub = _`, "stub", `node[()]`},
		{`rule a = &"a" !"b"`, "a", `node[()]`},
		// Captures index the tuple; a capture in a choice shares its element.
		{"rule a = x:b \"=\" y:c\nrule b = \"b\"\nrule c = \"c\"", "a", `node[(b, node[string], c), {"x": 0, "y": 2}]`},
		{"rule a = (x:b / y:c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b | c, d), {"x": 0, "y": 0}]`},
		{`rule a = x:"x"`, "a", `node[(node[string],), {"x": 0}]`},
		// A repetition element with captures is a node of its tuple.
		{`rule a = x:(y:.)*`, "a", `node[(node[[]node[(node[string],), {"y": 0}]],), {"x": 0}]`},
		// A captured sequence is one element; captures inside it have longer paths.
		{"rule a = x:(y:b c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(node[(b, c), {"y": 0}], d), {"x": 0, "y": 0.0}]`},
		// An optional sequence is an optional node of its tuple.
		{"rule a = (b c)? d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(*node[(b, c)], d)]`},
		// A choice with a valueless alternative is optional.
		{`rule a = "a" / &"b"`, "a", `*node[string]`},
		// Recovery is an explicit union; what recovers is one element.
		{`rule a = "a" #missing`, "a", `node[string] | error`},
		{"rule a = b (c d) #recover(skip=[^;]*) \";\"\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, node[(c, d)] | error, node[string])]`},
		// A capture in alternatives of different shapes has a path for each.
		{"rule a = (y:b c / y:d) e\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"\nrule e = \"e\"", "a", `node[(node[(b, c), {"y": 0}] | d, e), {"y": 0.0 | 0}]`},
		// T <= *T, and a valueless alternative makes the choice optional.
		{`rule a = ("a"? / "b") "c"`, "a", `node[(*node[string], node[string])]`},
		{`rule a = ("a" / -"b" -"c") "d"`, "a", `node[(*node[string], node[string])]`},
		{`rule a = (("a")?)? "d"`, "a", `node[(*node[string], node[string])]`},
		// Operator lines produce tuple nodes.
		{"rule e = operators {\n    operand n\n    level { infix left \"+\" }\n    level { prefix \"-\" }\n}\nrule n = [0-9]+", "e",
			`n | node[(e, node[string], e), {"lhs": 0, "op": 1, "rhs": 2}] | node[(node[string], e), {"op": 0, "rhs": 1}]`},
		// Interim: rule actions keep the current checker's types until actions are checked in the
		// value model, where $x here is node[[]node[string]].
		{`rule a = x:"1"* => $x`, "a", `[]node[string]`},
	} {
		g, err := syntax.Parse(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		p, err := Compile(g, Options{})
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if got := p.valueTypes()[tc.rule].String(); got != tc.want {
			t.Errorf("%q: rule %s\ngot  %s\nwant %s", tc.src, tc.rule, got, tc.want)
		}
	}
}

// Every rule of the repository grammars has a value type.
func TestValueTypesOfRepositoryGrammars(t *testing.T) {
	var paths []string
	for _, pattern := range []string{"../../examples/*/*.pego", "../../parsers/*/*.pego", "../../spec/*.pego"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, m...)
	}
	if len(paths) < 10 {
		t.Fatalf("found only %d grammars", len(paths))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		g, err := syntax.Parse(string(data))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		p, err := Compile(g, Options{})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, rd := range g.Rules() {
			if v, ok := p.valueTypes()[rd.Name]; !ok || v == nil || v == tyBottom {
				t.Errorf("%s: rule %s has value type %v", path, rd.Name, v)
			}
		}
	}
}

// A capture under ? is optional in the enclosing node only, not in the node it captures.
func TestValueTypesOptionalCaptures(t *testing.T) {
	g, err := syntax.Parse("rule a = (x:b c)? d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	outer := p.valueTypes()["a"].(nodeTy)
	if len(outer.caps) != 1 || outer.caps[0].t.String() != "*b" {
		t.Fatalf("outer captures %+v", outer.caps)
	}
	inner := outer.value.(tupleTy).elems[0].(optTy).elem.(nodeTy)
	if len(inner.caps) != 1 || inner.caps[0].t.String() != "b" {
		t.Fatalf("inner captures %+v", inner.caps)
	}
}
