package compile

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestTopBottomTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		// A _ value is assignable only to _.
		{"type T = { A _ }\ntype U = { B node[string] }\nrule main = x:\"a\" => U{B: T{A: $x}.A}", "_"},
		// No value has type _|_.
		{"type T = { A _|_ }\nrule main = \"a\"", "field A of T has type _|_, which has no values"},
		{"type B ~ []_|_\nrule main B = \"a\"", "rule main has type _|_, which has no values"},
		{"type B ~ *_|_\nrule main = x:\"a\" => $x.(B)", "the cast has type _|_, which has no values"},
		{"rule main = \"a\" => []_|_{}", "the list element has type _|_, which has no values"},
		{"type T = { A int & string }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A node[string] & []node[string] }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A []node[string] & []int }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A *[]node[string] & []int }\nrule main = \"a\"", "field A of T has type _|_"},
		// A meet is the type assignable to both: here node[string], which an int is not.
		{"type T = { A (node[string] | int) & node[_] }\nrule main = \"a\" => T{A: 1}", "cannot use int as node[string]"},
		// Nodes meet in their values, and tuples element by element.
		{"type T = { A node[string | []node[string]] & node[string | int] }\nrule main = \"a\" => T{A: 1}", "cannot use int as node[string]"},
		{"type T = { A (node[string] | int, node[string]) & (node[string] | bool, node[string]) }\nrule main = \"a\" => T{A: 1}", "cannot use int as (node[string], node[string])"},
		{"type T = { A (node[string], int) & (node[string], bool) }\nrule main = \"a\"", "field A of T has type _|_"},
		{"type T = { A (node[string],) & (node[string], node[string]) }\nrule main = \"a\"", "field A of T has type _|_"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	for _, src := range []string{
		// A rule may produce a value of type _.
		"type T = { A _ }\nrule main = x:\"a\" => T{A: $x}.A",
		"rule main _ = \"a\"",
		"type T = { A _ | node[string] }\nrule main = \"a\" => T{A: 1}",
		"type T = { A _|_ | node[string] }\nrule main = x:\"a\" => T{A: $x}",
		"type T = { A node[string] & _ }\nrule main = x:\"a\" => T{A: $x}\nrule other = x:\"a\" => T{A: $x}.A",
		"type T = { A _ }\nrule main = x:\"a\" => T{A: $x}.A.(node[string] | node[_])",
		"type B ~ _|_\nrule main = \"a\"", // an alias is not a location
		// Meets look inside lists: []Seq is assignable to both.
		"type T = { A [](node[string] | node[_]) & [](node[_] | node[_]) }\nrule main = x:(\"a\" \"b\")* => T{A: $x.Value}",
		"type T = { A *node[string] & *node[_] }\nrule main = \"a\" => T{A: nil}",
		"type T = { A *[](node[string] | node[_]) & [](node[_] | node[_]) }\nrule main = x:(\"a\" \"b\")* => T{A: $x.Value}",
		"type T = { A ([]node[string] | int) & []node[_] }\nrule main = x:\"a\"* => T{A: $x.Value}",
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestTopBottomReportedOnce(t *testing.T) {
	t.Parallel()
	g, err := parseTest("type T = { A int & string }\nrule main = x:\"a\" => T{A: $x}")
	if err != nil {
		t.Fatal(err)
	}
	_, err = compileStatic(g, Options{}, nil)
	if err == nil || strings.Count(err.Error(), "_|_") != 1 || strings.Contains(err.Error(), "never") {
		t.Fatalf("got %v, want one _|_ error", err)
	}
	g, err = parseTest("rule main = x:\"a\" => [](int & string){$x}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = compileStatic(g, Options{}, nil); err == nil || strings.Count(err.Error(), "_|_") != 1 {
		t.Fatalf("got %v, want one _|_ error", err)
	}
	// An empty meet (only an AST can hold one) is _.
	g, err = parseTest("type T = { A node[string] }\nrule main = x:\"a\" => T{A: $x}")
	if err != nil {
		t.Fatal(err)
	}
	g.Types()[0].(*grammar.TypeDef).Struct().Fields[0].Type = &grammar.MeetType{}
	if _, err := compileStatic(g, Options{}, nil); err != nil {
		t.Fatal(err)
	}
}

// A location of type _|_ is an error: no value is assignable to it.
func TestTopBottomLocationsAreRejected(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{"rule main = x:\"a\" => $x.(_|_)", "the cast has type _|_"},
		{"rule main = x:\"a\" => $x.(int & string)", "the cast has type _|_"},
		{"type T = { A _|_ }\nrule main = x:\"a\" => T{A: $x}", "field A of T has type _|_"},
		{"type T = { A int & string }\nrule main = x:\"a\" => T{A: $x}", "field A of T has type _|_"},
		{"type B ~ _|_\nrule main B = \"a\"", "rule main has type _|_"},
		{"rule main = x:\"a\" => []_|_{$x}", "the list element has type _|_"},
		{"rule main = x:\"a\" => [](int & string){$x}", "the list element has type _|_"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// Other type errors are reported too.
	g, err := parseTest("type T = { A node[string] }\nrule main = \"a\" => T{A: 1}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), "cannot use int") {
		t.Fatalf("got %v, want a type error", err)
	}
}
