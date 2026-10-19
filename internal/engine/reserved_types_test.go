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
	src := "type A ~ () | (B,) | (B, *C) | node[B] | []node[(B, C), D]\ntype B = { Text string }\ntype C = { Text string }\ntype D = { Text string }\nrule main = \"a\""
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
		{"type A ~ (B, C)\ntype B = { Text string }\ntype C = { Text string }\nrule main = \"a\"", "1:10: tuple types are not supported yet"},
		{"rule main () = \"a\"", "1:11: tuple types are not supported yet"},
		{"type T = { F (int,) }\nrule main = \"a\"", "1:14: tuple types are not supported yet"},
		{"rule main = x:\"a\" => $x.(node[node[string]])", "type arguments of node other than node[string] are not supported yet"},
		{"rule main = x:\"a\" => $x.(node[string, int])", "type arguments of node other than node[string] are not supported yet"},
		{"rule main = \"a\" => [](node[string], node){}", "tuple types are not supported yet"},
		{"type B = { Text string }\nrule main B[node[string]] = \"a\"", "type B takes no type arguments"},
		{"rule main int[node[string]] = \"a\"", "type int takes no type arguments"},
		{"rule main _[node[string]] = \"a\"", "type _ takes no type arguments"},
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
		"type A ~ node[node[string]]\nrule main A = \"a\"\nrule other A = \"b\"",
		"type A ~ (node[string],)\nrule main = x:\"a\" {$x.(A) == $x} => $x.(A)",
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

// node[string] is the type of a string node, in fields, rules and casts, and is checked like one.
func TestNodeStringType(t *testing.T) {
	for _, src := range []string{
		"type P = { K node[string] }\nrule main P = k:[a-z]+ #atomic => P{K: $k}",
		"rule main node[string] = \"a\"",
		"rule main = x:\"a\" => $x.(node[string])",
		"type W = { V int }\ntype T ~ node[string] | W\nrule main T = \"a\"",
	} {
		for _, opts := range []Options{{}, {NoTypeCheck: true}} {
			g, err := syntax.Parse(src)
			if err != nil {
				t.Fatalf("%q: %v", src, err)
			}
			p, err := Compile(g, opts)
			if err != nil {
				t.Fatalf("%q (NoTypeCheck %v): %v", src, opts.NoTypeCheck, err)
			}
			if _, err := p.Parse("main", "a"); err != nil {
				t.Fatalf("%q (NoTypeCheck %v): parse: %v", src, opts.NoTypeCheck, err)
			}
		}
	}
	// A value that is not a string node does not fit: statically, and in a cast at run time.
	for _, tc := range []struct{ src, want string }{
		{"type P = { K node[string] }\nrule main P = k:(\"a\" \"b\") => P{K: $k}", "cannot use"},
		{"rule main node[string] = \"a\" \"b\"", "not assignable"},
	} {
		g, err := syntax.Parse(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	g, err := syntax.Parse("rule main = k:(\"a\" \"b\") => $k.(node).(node[string])")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Parse("main", "ab"); err == nil {
		t.Fatal("a tuple node was accepted as node[string] at run time")
	}
}

// Unnamed struct types are reserved syntax too, formatted like struct definitions.
func TestReservedUnnamedStructType(t *testing.T) {
	src := "type A ~ {} | { X int; Y *A } | []{\n  // the name\n  Name string\n  Inner { V int }\n}\ntype T = {\n  F {}\n  G (int, { H int })\n}\nrule main = \"a\""
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "type A ~ {} | { X int; Y *A } | []{\n    // the name\n    Name  string\n    Inner { V int }\n}\ntype T = {\n    F {}\n    G (int, { H int })\n}\nrule main = \"a\"\n"
	formatted := grammar.Format(g)
	if formatted != want {
		t.Fatalf("formatted:\n%s\nwant\n%s", formatted, want)
	}
	again, err := syntax.Parse(formatted)
	if err != nil || grammar.Format(again) != formatted {
		t.Fatalf("formatting is not idempotent: %v", err)
	}
	u := g.Types()[0].Spec.(*grammar.AliasSpec).Type.(*grammar.UnionType)
	if s, ok := u.Types[1].(*grammar.StructType); !ok || len(s.Fields) != 2 || s.Fields[1].Name != "Y" {
		t.Fatalf("struct type %#v", u.Types[1])
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	back, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := grammar.Format(back); !strings.HasPrefix(got, "type A ~ {} | { X int; Y *A } | []{ Name string; Inner { V int } }\n") {
		t.Fatalf("JSON round trip:\n%s", got)
	}

	for _, tc := range []struct{ src, pos string }{
		{"type A ~ { X int }\nrule main = \"a\"", "1:10"},
		{"type T = { F { G int } }\nrule main = \"a\"", "1:14"},
		{"rule main {} = \"a\"", "1:11"},
		{"rule main = x:\"a\" => $x.({ Y node[string] })", "1:26"},
		{"rule main = \"a\" => []{ A node[string] }{}", "1:22"},
		// A list constructor of an unsupported type adds no error of its own, also where the
		// type was reported elsewhere.
		{"type A ~ { A node[string] }\nrule main = x:\"a\" => []A{$x}\nrule b = x:\"a\" => []A{$x}", "1:10"},
		{"type A ~ { A node[string] }\nrule main = x:\"a\" {[]A{$x} == nil} => []A{$x}", "1:10"},
	} {
		for _, opts := range []Options{{}, {NoTypeCheck: true}} {
			g, err := syntax.Parse(tc.src)
			if err != nil {
				t.Fatalf("%q: %v", tc.src, err)
			}
			_, err = Compile(g, opts)
			want := tc.pos + ": unnamed struct types are not supported yet"
			if err == nil || !strings.Contains(err.Error(), want) || strings.Count(err.Error(), "\n") > 0 {
				t.Errorf("%q (NoTypeCheck %v): got %v, want only %q", tc.src, opts.NoTypeCheck, err, want)
			}
		}
	}
}
