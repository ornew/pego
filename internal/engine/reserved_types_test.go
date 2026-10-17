package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// Tuple types and type arguments are reserved syntax: they parse, format and round-trip through
// JSON, but grammars using them do not compile, with or without type checking.
func TestReservedTypeSyntax(t *testing.T) {
	src := "type A ~ () | (B,) | (B, *C) | node[B] | []node[(B, C), D]\ntype B terminal\ntype C terminal\ntype D terminal\nrule main = \"a\""
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	if !strings.Contains(formatted, "type A ~ () | (B,) | (B, *C) | node[B] | []node[(B, C), D]") {
		t.Fatalf("formatted:\n%s", formatted)
	}
	u := g.Types()[0].Spec.(*grammar.AliasSpec).Type.(*grammar.UnionType)
	for i, n := range []int{0, 1, 2} {
		if tt, ok := u.Types[i].(*grammar.TupleType); !ok || len(tt.Types) != n {
			t.Fatalf("tuple %d: %#v", i, u.Types[i])
		}
	}
	if a, ok := u.Types[3].(*grammar.TypeApp); !ok || a.Name != "node" || len(a.Args) != 1 {
		t.Fatalf("type application %#v", u.Types[3])
	}
	l, ok := u.Types[4].(*grammar.ListType)
	if a, ok2 := l.Elem.(*grammar.TypeApp); !ok || !ok2 || len(a.Args) != 2 {
		t.Fatalf("list of type application %#v", u.Types[4])
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	back, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	// JSON keeps the tree but not the layout.
	if got := grammar.Format(back); !strings.HasPrefix(got, "type A ~ () | (B,) | (B, *C) | node[B] | []node[(B, C), D]\n") {
		t.Fatalf("JSON round trip:\n%s", got)
	}

	for _, tc := range []struct{ src, want string }{
		{"type A ~ (B, C)\ntype B terminal\ntype C terminal\nrule main = \"a\"", "1:10: tuple types are not supported yet"},
		{"rule main () = \"a\"", "1:11: tuple types are not supported yet"},
		{"type T = { F (int,) }\nrule main = \"a\"", "1:14: tuple types are not supported yet"},
		{"rule main = x:\"a\" => $x.(node[Match])", "type arguments of node are not supported yet"},
		{"rule main = \"a\" => [](Match, Seq){}", "tuple types are not supported yet"},
		{"type B terminal\nrule main B[Match] = \"a\"", "type B takes no type arguments"},
		{"rule main int[Match] = \"a\"", "type int takes no type arguments"},
		{"rule main _[Match] = \"a\"", "type _ takes no type arguments"},
		// An undefined name is rejected without type checking too.
		{"type A ~ Nope[int]\nrule main = \"a\"", "undefined type Nope"},
		{"type T = { F Nope[int] }\nrule main = \"a\"", "undefined type Nope"},
		{"rule main Nope[int] = \"a\"", "undefined type Nope"},
	} {
		for _, opts := range []Options{{}, {NoTypeCheck: true}} {
			g, err := syntax.Parse(tc.src)
			if err != nil {
				t.Fatalf("%q: %v", tc.src, err)
			}
			_, err = Compile(g, opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%q (NoTypeCheck %v): got %v, want %q", tc.src, opts.NoTypeCheck, err, tc.want)
			} else if strings.Count(err.Error(), "not supported") > 1 {
				t.Errorf("%q (NoTypeCheck %v): reported more than once: %v", tc.src, opts.NoTypeCheck, err)
			}
		}
	}
	// An undefined name is reported as such, not as one that takes no type arguments.
	g, err = syntax.Parse("type A ~ Nope[int]\nrule main A = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, Options{}); err == nil || !strings.Contains(err.Error(), "undefined type Nope") || strings.Contains(err.Error(), "takes no type arguments") {
		t.Fatalf("got %v", err)
	}
	// A type used from several places, also by the checks of separate actions, is reported once.
	for _, src := range []string{
		"type A ~ node[Match]\nrule main A = \"a\"\nrule other A = \"b\"",
		"type A ~ (Match,)\nrule main = x:\"a\" {$x.(A) == $x} => $x.(A)",
	} {
		for _, opts := range []Options{{}, {NoTypeCheck: true}} {
			g, err := syntax.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(g, opts); err == nil || strings.Count(err.Error(), "not supported") != 1 {
				t.Fatalf("%q (NoTypeCheck %v): got %v", src, opts.NoTypeCheck, err)
			}
		}
	}
}
