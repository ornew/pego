package compile

import (
	"strings"
	"testing"
)

func TestTypedNodeConstructorErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ action, message string }{
		{`[]Int{nil}`, "cannot use"},
		{`[]Int{Other{}}`, "cannot use"},
		{`[]Int{x}`, "cannot use int as Int in list constructor"},
		{`[][]Int{list(x)}`, "cannot use []int as []Int in list constructor"},
		{`[]Int{$d}`, "cannot use _ as Int in list constructor: the type of the value is not known; convert it with a cast, as in value.(Int)"},
		{`[][]Int{list($d)}`, "in list constructor: the type of the value is not known; convert it with a cast, as in value.([]Int)"},
		// The retired positional forms are read as the text struct constructors they mean.
		{`Int{1}`, "cannot use int as string in field Text of Int"},
		{`Int{nil}`, "cannot use nil as string in field Text of Int"},
		{`Int{"a", "b"}`, "duplicate field Text"},
		{`Other{"x"}`, "Other has no field Text"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			g, err := parseLegacyTest(`type Int terminal
type Other = {}
var x int
rule main = {x := 1} d:dyn "x" => ` + tc.action + `
rule dyn _ = _ => 1`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = compileStatic(g, Options{}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v; want %q", err, tc.message)
			}
		})
	}
	// Lists may hold ints, strings and bools.
	for _, action := range []string{`[]_{}`, `[][]_{}`, `[]int{}`, `[][]string{}`, `[]int{1, 2}`, `[]*bool{nil, true}`} {
		g, err := parseLegacyTest("var x int\nrule main = {x := 1} \"x\" => " + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
	// A terminal constructor needed exactly one positional argument; a text struct is an ordinary
	// struct, whose constructor may leave Text unset or name it.
	for _, action := range []string{`Int{}`, `Int{Text: "0"}`} {
		g, err := parseLegacyTest("type Int terminal\nrule main = \"x\" => " + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
}

func TestConstructedTerminalEvaluationErrors(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`type Int terminal
var value int
rule main = {value := 1} "x" => Int{value}`,
		`type Int terminal
var value int
rule main = {value := 1} {Int{value} != nil} "x" / "x"`,
	} {
		g, err := parseLegacyTest(source)
		if err != nil {
			t.Fatal(err)
		}
		// The checker rejects these constructors when the grammar is compiled.
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), "cannot use int as string in field Text of Int") {
			t.Fatalf("compilation: %v", err)
		}
	}
}

// The text struct counterpart: a field of the wrong type is a compile error.
func TestConstructedTextStructEvaluationErrors(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`type Int = { Text string }
var value int
rule main = {value := 1} "x" => Int{Text: value}`,
		`type Int = { Text string }
var value int
rule main = {value := 1} {Int{Text: value} != nil} "x" / "x"`,
	} {
		// The checker rejects these constructors when the grammar is compiled.
		if got := compileMessage(t, source); !strings.Contains(got, "cannot use int as string in field Text of Int") {
			t.Fatalf("compilation: %q", got)
		}
	}
}

func TestNodeConstructorShapes(t *testing.T) {
	t.Parallel()
	const header = "type Int terminal\ntype Other = {}\nrule main = \"x\" => "
	for _, action := range []string{`[]any{}`, `Int{"a", "b"}`, `Other{"a"}`} {
		g, err := parseLegacyTest(header + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil {
			t.Fatalf("accepted unsupported constructor shape: %s", action)
		}
	}
	for _, action := range []string{`[]int{}`, `[][]string{}`} {
		if g, err := parseLegacyTest(header + action); err != nil {
			t.Fatal(err)
		} else if _, err := compileStatic(g, Options{}, nil); err != nil {
			t.Errorf("rejected a list of values %s: %v", action, err)
		}
	}
	if g, err := parseLegacyTest(header + `Int{}`); err != nil {
		t.Fatal(err)
	} else if _, err := compileStatic(g, Options{}, nil); err != nil {
		t.Fatalf("Int{} is a text struct with no field set: %v", err)
	}
	// Values of the wrong type are rejected when the grammar is compiled, in actions and predicates.
	for _, action := range []string{`[]Int{nil}`, `[]Int{Other{}}`, `[]Int{1}`, `Int{1}`} {
		g, err := parseLegacyTest(header + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil {
			t.Errorf("%s was accepted", action)
		}
	}
	for _, action := range []string{`[]Int{1}`, `Int{1}`} {
		g, err := parseLegacyTest("type Int terminal\nrule main = {" + action + " != nil} \"x\" / \"x\"")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil {
			t.Errorf("predicate %s was accepted", action)
		}
	}
}
