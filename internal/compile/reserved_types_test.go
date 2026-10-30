package compile

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// Tuple types and type arguments parse, format and round-trip through JSON. Tuple types and node[T]
// compile; other type arguments are errors.
func TestReservedTypeSyntax(t *testing.T) {
	t.Parallel()
	src := "type A ~ () | (B,) | (B, *C) | node[B] | []node[(B, C), D]\ntype B = { Text string }\ntype C = { Text string }\ntype D = { Text string }\nrule main = \"a\""
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	if !strings.Contains(formatted, "type A ~ () | (B,) | (B, *C) | node[B] | []node[(B, C), D]") {
		t.Fatalf("formatted:\n%s", formatted)
	}
	u := g.Types()[0].(*grammar.TypeAlias).Type.(*grammar.JoinType)
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

	for _, src := range []string{
		"type A ~ (B, C)\ntype B = { Text string }\ntype C = { Text string }\nrule main = \"a\"",
		"type T = { F (int,) }\nrule main = \"a\"",
		"rule main = \"a\" => [](node[string], node[_]){}",
		"type A ~ node[node[string]] | node[int]\nrule main = \"a\"",
	} {
		for _, opts := range []Options{{}} {
			g, err := parseTest(src)
			if err != nil {
				t.Fatalf("%q: %v", src, err)
			}
			if _, err := compileStatic(g, opts, nil); err != nil {
				t.Errorf("%q: %v", src, err)
			}
		}
	}
	// A value that does not fit a tuple or node[T] type is a type error.
	for _, tc := range []struct{ src, want string }{
		{"rule main () = \"a\"", "1:1: rule main produces node[string], which is not assignable to ()"},
		{"rule main = x:\"a\" => $x.(node[node[string]])", "1:25: cannot cast node[string] to node[node[string]]"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}

	for _, tc := range []struct{ src, want string }{
		{"rule main = x:\"a\" => $x.(node[string, int])", "1:26: node takes one type argument, got 2"},
		{"type B = { Text string }\nrule main B[node[string]] = \"a\"", "type B takes no type arguments"},
		{"rule main int[node[string]] = \"a\"", "type int takes no type arguments"},
		{"rule main _[node[string]] = \"a\"", "type _ takes no type arguments"},
		// An undefined name is rejected.
		{"type A ~ Nope[int]\nrule main = \"a\"", "undefined type Nope"},
		{"type T = { F Nope[int] }\nrule main = \"a\"", "undefined type Nope"},
		{"rule main Nope[int] = \"a\"", "undefined type Nope"},
	} {
		for _, opts := range []Options{{}} {
			g, err := parseTest(tc.src)
			if err != nil {
				t.Fatalf("%q: %v", tc.src, err)
			}
			_, err = compileStatic(g, opts, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
			} else if strings.Count(err.Error(), "\n") > 0 {
				t.Errorf("%q: reported more than once: %v", tc.src, err)
			}
		}
	}
	// An undefined name is reported as such, not as one that takes no type arguments.
	g, err = parseTest("type A ~ Nope[int]\nrule main A = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), "undefined type Nope") || strings.Contains(err.Error(), "takes no type arguments") {
		t.Fatalf("got %v", err)
	}
	// A type used from several places, also by the checks of separate actions, is reported once.
	for _, src := range []string{
		"type A ~ node[string, int]\nrule main A = \"a\"\nrule other A = \"b\"",
		"type A ~ node[string, int]\nrule main = x:\"a\" {$x.(A) == $x} => $x.(A)",
	} {
		for _, opts := range []Options{{}} {
			g, err := parseTest(src)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := compileStatic(g, opts, nil); err == nil || strings.Count(err.Error(), "node takes one type argument") != 1 {
				t.Fatalf("%q: got %v", src, err)
			}
		}
	}
}

// Unnamed struct types are reserved syntax too, formatted like struct definitions.
func TestReservedUnnamedStructType(t *testing.T) {
	t.Parallel()
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
	u := g.Types()[0].(*grammar.TypeAlias).Type.(*grammar.JoinType)
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
	// JSON carries the comment of the field, which takes the struct to lines.
	// Without layout, the declarations are separated by blank lines.
	wantJSON := strings.Replace(strings.Replace(want, "}\ntype T", "}\n\ntype T", 1), "}\nrule main", "}\n\nrule main", 1)
	if got := grammar.Format(back); got != wantJSON {
		t.Fatalf("JSON round trip:\n%s", got)
	}
	// Without comments the struct type stays on one line.
	plain, err := parseTest("type A ~ []{ Name string; Inner { V int } }\nrule main = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	data, err = grammar.MarshalJSON(plain)
	if err != nil {
		t.Fatal(err)
	}
	back, err = grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := grammar.Format(back); !strings.HasPrefix(got, "type A ~ []{ Name string; Inner { V int } }\n") {
		t.Fatalf("JSON round trip without comments:\n%s", got)
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
		for _, opts := range []Options{{}} {
			g, err := parseTest(tc.src)
			if err != nil {
				t.Fatalf("%q: %v", tc.src, err)
			}
			_, err = compileStatic(g, opts, nil)
			want := tc.pos + ": unnamed struct types are not supported yet"
			if err == nil || !strings.Contains(err.Error(), want) || strings.Count(err.Error(), "\n") > 0 {
				t.Errorf("%q: got %v, want only %q", tc.src, err, want)
			}
		}
	}
}
