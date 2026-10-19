package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// A tuple literal is written like a tuple type: () is the unit, (x,) has one element, and one
// expression in parentheses without a comma is a group.
func TestTupleLiteralParseAndFormat(t *testing.T) {
	const src = `rule unit = "x" => ()
rule one = "x" => (1,)
rule group = "x" => (1)
rule two = "x" => (1, "a")
rule trailing = "x" => (1, "a",)
rule spaced = "x" => ( 1 , ( 2 , nil ) )
rule index = "x" => (1, 2)[0]
rule call = "x" => list((1, 2), ())
rule multiline = "x" => (
    1,
    2,
)
`
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]grammar.Term{}
	for _, d := range g.File.Decls {
		if r, ok := d.(*grammar.RuleDef); ok {
			actions[r.Name] = r.Action
		}
	}
	for name, want := range map[string]int{"unit": 0, "one": 1, "two": 2, "trailing": 2, "spaced": 2} {
		tup, ok := actions[name].(*grammar.TupleLit)
		if !ok {
			t.Fatalf("%s: action is %T, want *grammar.TupleLit", name, actions[name])
		}
		if tup.Elems == nil || len(tup.Elems) != want {
			t.Errorf("%s: %d elements (nil %v), want %d", name, len(tup.Elems), tup.Elems == nil, want)
		}
	}
	if _, ok := actions["group"].(*grammar.TermGroup); !ok {
		t.Errorf("group: action is %T, want *grammar.TermGroup", actions["group"])
	}
	if inner, ok := actions["spaced"].(*grammar.TupleLit).Elems[1].(*grammar.TupleLit); !ok || len(inner.Elems) != 2 {
		t.Errorf("nested tuple lost: %#v", actions["spaced"])
	}
	if b, ok := actions["index"].(*grammar.Binary); !ok || b.Op != "[]" {
		t.Errorf("index: action is %T", actions["index"])
	} else if _, ok := b.L.(*grammar.TupleLit); !ok {
		t.Errorf("index operand is %T, want *grammar.TupleLit", b.L)
	}

	formatted := grammar.Format(g)
	for _, want := range []string{
		`=> ()`, `=> (1,)`, `=> (1)`, `=> (1, "a")`, `=> (1, (2, nil))`, `=> (1, 2)[0]`, `list((1, 2), ())`,
		"=> (\n    1, 2\n)",
	} {
		if !strings.Contains(formatted, want) {
			t.Errorf("formatted grammar lacks %q:\n%s", want, formatted)
		}
	}
	again, err := Parse(formatted)
	if err != nil {
		t.Fatalf("formatted source does not parse: %v\n%s", err, formatted)
	}
	if out := grammar.Format(again); out != formatted {
		t.Fatalf("format is not stable:\n%s\n%s", formatted, out)
	}
}

func TestTupleLiteralComments(t *testing.T) {
	const src = `rule main = "x" => (
    // first
    1, /* second */ 2,
    // last
)
`
	out := format(t, src)
	for _, want := range []string{"// first", "/* second */", "// last"} {
		if !strings.Contains(out, want) {
			t.Errorf("comment %q lost:\n%s", want, out)
		}
	}
	if again := format(t, out); again != out {
		t.Fatalf("format is not stable:\n%s\n%s", out, again)
	}
}

func TestTupleLiteralErrors(t *testing.T) {
	for _, src := range []string{
		`rule main = "x" => (,)`,
		`rule main = "x" => (1,, 2)`,
		`rule main = "x" => (1, 2`,
		`rule main = "x" => (1,`,
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("accepted %s", src)
		}
	}
}
