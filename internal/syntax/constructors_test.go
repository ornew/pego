package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestTypedConstructorsParseAndFormat(t *testing.T) {
	const src = `type Foo = { Name string; Children []Foo }
type Int terminal
type Pair = { Left []Foo; Right []Foo }
rule empty = "x" => []Foo{}
rule nested = "x" => [][]Foo{[]Foo{Foo{}}}
rule pointers = "x" => []*Foo{nil}
rule terminal = "x" => Int{"0"}
rule terminal_empty = "x" => Int{}
rule terminal_args = "x" => Int{"a", "b"}
rule fields = "x" => Pair{Left: []Foo{Foo{}}, Right: []Foo{Foo{}}}
rule grouped = "x" => (
    [][]Foo{[]Foo{Foo{}}}
)
`
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	rules := make(map[string]*grammar.RuleDef)
	for _, stmt := range g.Statements {
		if r, ok := stmt.(*grammar.RuleDef); ok {
			rules[r.Name] = r
		}
	}
	list := func(name string) *grammar.ListConstructor {
		t.Helper()
		n, ok := rules[name].Action.(*grammar.ListConstructor)
		if !ok {
			t.Fatalf("%s action is %T, want *grammar.ListConstructor", name, rules[name].Action)
		}
		return n
	}
	if got := list("empty"); got.Elem.(*grammar.TypeRef).Name != "Foo" || got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("empty list constructor lost its element type or empty items: %#v", got)
	}
	nested, ok := list("nested").Elem.(*grammar.ListType)
	if !ok || nested.Elem.(*grammar.TypeRef).Name != "Foo" {
		t.Fatalf("nested list element type: %#v", list("nested").Elem)
	}
	inner, ok := list("nested").Items[0].(*grammar.ListConstructor)
	if !ok || inner.Elem.(*grammar.TypeRef).Name != "Foo" || len(inner.Items) != 1 {
		t.Fatalf("nested list items: %#v", list("nested").Items)
	}
	if _, ok := list("pointers").Elem.(*grammar.OptionalType); !ok {
		t.Fatalf("pointer list element type: %#v", list("pointers").Elem)
	}
	if _, ok := list("pointers").Items[0].(*grammar.NilLit); !ok {
		t.Fatalf("pointer list nil item: %#v", list("pointers").Items[0])
	}
	terminal, ok := rules["terminal"].Action.(*grammar.TerminalConstructor)
	if !ok || terminal.Type != "Int" || len(terminal.Args) != 1 {
		t.Fatalf("terminal constructor: %#v", rules["terminal"].Action)
	}
	emptyTerminal, ok := rules["terminal_empty"].Action.(*grammar.New)
	if !ok || emptyTerminal.Type != "Int" || len(emptyTerminal.Fields) != 0 {
		t.Fatalf("empty constructor source was not preserved: %#v", rules["terminal_empty"].Action)
	}
	many, ok := rules["terminal_args"].Action.(*grammar.TerminalConstructor)
	if !ok || many.Type != "Int" || len(many.Args) != 2 {
		t.Fatalf("positional constructor arguments were not retained: %#v", rules["terminal_args"].Action)
	}
	if _, ok := rules["fields"].Action.(*grammar.New); !ok {
		t.Fatalf("named-field constructor became %T", rules["fields"].Action)
	}

	formatted := grammar.Format(g)
	for _, want := range []string{
		"type Pair = { Left []Foo; Right []Foo }",
		"Pair{Left: []Foo{Foo{}}, Right: []Foo{Foo{}}}",
		"Int{\"a\", \"b\"}",
		"rule grouped = \"x\" => (\n",
	} {
		if !strings.Contains(formatted, want) {
			t.Errorf("formatted grammar lacks %q:\n%s", want, formatted)
		}
	}
	if again, err := Parse(formatted); err != nil {
		t.Fatalf("formatted source does not parse: %v\n%s", err, formatted)
	} else if out := grammar.Format(again); out != formatted {
		t.Fatalf("format is not stable:\n%s\n%s", formatted, out)
	}
}
