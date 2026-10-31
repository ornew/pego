package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

// A positional constructor of one argument is the source form zp fix rewrites; parseLegacy
// reads it as the text struct constructor it means.
func TestTypedConstructorsParseAndFormat(t *testing.T) {
	const src = `type Foo = { Name string; Children []Foo }
type Int = { Text string }
type Pair = { Left []Foo; Right []Foo }
rule empty = "x" => []Foo{}
rule nested = "x" => [][]Foo{[]Foo{Foo{}}}
rule pointers = "x" => []*Foo{nil}
rule terminal = "x" => Int{"0"}
rule terminal_empty = "x" => Int{}
rule fields = "x" => Pair{Left: []Foo{Foo{}}, Right: []Foo{Foo{}}}
rule grouped = "x" => (
    [][]Foo{[]Foo{Foo{}}}
)
`
	g, forms, err := parseLegacy(src)
	if err != nil {
		t.Fatal(err)
	}
	rules := make(map[string]*grammar.RuleDef)
	for _, stmt := range g.File.Decls {
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
	terminal, ok := rules["terminal"].Action.(*grammar.New)
	if !ok || terminal.Type != "Int" || len(terminal.Fields) != 1 || terminal.Fields[0].Name != "Text" || !forms.positional[terminal] {
		t.Fatalf("positional constructor: %#v", rules["terminal"].Action)
	}
	emptyTerminal, ok := rules["terminal_empty"].Action.(*grammar.New)
	if !ok || emptyTerminal.Type != "Int" || len(emptyTerminal.Fields) != 0 || forms.positional[emptyTerminal] {
		t.Fatalf("empty constructor source was not preserved: %#v", rules["terminal_empty"].Action)
	}
	// A positional constructor of two arguments was never a terminal constructor: it keeps
	// both arguments and is not one that Fix rewrites.
	g2, forms2, err := parseLegacy("type Int = { Text string }\nrule r = \"x\" => Int{\"a\", \"b\"}")
	if err != nil {
		t.Fatal(err)
	}
	if n := g2.File.Decls[1].(*grammar.RuleDef).Action.(*grammar.New); len(n.Fields) != 2 || forms2.positional[n] {
		t.Fatalf("positional constructor of two arguments: %#v", n)
	}
	if _, ok := rules["fields"].Action.(*grammar.New); !ok {
		t.Fatalf("named-field constructor became %T", rules["fields"].Action)
	}

	formatted := grammar.Format(g)
	for _, want := range []string{
		"type Pair = { Left []Foo; Right []Foo }",
		"Pair{Left: []Foo{Foo{}}, Right: []Foo{Foo{}}}",
		"Int{Text: \"0\"}",
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

// Fix rewrites the terminal type and the one-argument constructor of a grammar that also has a
// constructor of two arguments, which it leaves for the compiler to report.
func TestFixKeepsPositionalConstructorOfTwoArguments(t *testing.T) {
	const src = "type N terminal\nrule n N = [a-z]\nrule m N = x:n => N{text($x)}\nrule m2 N = x:n => N{\"a\", \"b\"}\n"
	got, err := Fix(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type N = { Text string }", "N{Text: $x.Text}", `N{"a", "b"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("Fix output lacks %q:\n%s", want, got)
		}
	}
}
