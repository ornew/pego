package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestDeclarationAttributesParse(t *testing.T) {
	const src = "rule a = \"x\"\n\n@start(default) @foo\n@bar\nrule b = a\n"
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	rules := g.Rules()
	if len(rules[0].Attrs) != 0 || rules[0].AttrBreaks != nil || rules[0].KeywordBreak != nil {
		t.Errorf("a rule without attributes has %v", rules[0].Attrs)
	}
	b := rules[1]
	var names []string
	for _, a := range b.Attrs {
		if a.Pragma {
			t.Errorf("@%s is a pragma", a.Name)
		}
		names = append(names, a.Name)
	}
	if got := strings.Join(names, " "); got != "start foo bar" {
		t.Fatalf("attributes %q", got)
	}
	if ref, ok := b.Attrs[0].Args[0].(*grammar.Ref); !ok || ref.Name != "default" || len(b.Attrs[0].Args) != 1 {
		t.Errorf("@start(default) arguments %#v", b.Attrs[0].Args)
	}
	// The declaration starts at its first attribute; its diagnostics point at "rule".
	if b.Pos != (grammar.Pos{Line: 3, Col: 1}) || b.Diagnostic() != (grammar.Pos{Line: 5, Col: 1}) {
		t.Errorf("pos %v diagnostic %v", b.Pos, b.Diagnostic())
	}
	if b.Attrs[2].Pos != (grammar.Pos{Line: 4, Col: 1}) {
		t.Errorf("@bar at %v", b.Attrs[2].Pos)
	}
	if len(b.AttrBreaks) != 3 || b.AttrBreaks[1] != nil || b.AttrBreaks[2] == nil || b.KeywordBreak == nil {
		t.Errorf("breaks %v keyword %v", b.AttrBreaks, b.KeywordBreak)
	}
}

func TestDeclarationAttributesErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"@start rule a = \"x\"\n", "1:8: expected a newline between the declaration attributes and 'rule'"},
		{"@start /* c */ rule a = \"x\"\n", "1:16: expected a newline between the declaration attributes and 'rule'"},
		{"@start @foo rule a = \"x\"\n", "1:13: expected a newline between the declaration attributes and 'rule'"},
		{"@start\ntype T = node[string]\n", "2:1: only rule declarations take attributes, found 'type'"},
		{"@start\nvar v = 1\nrule a = \"x\"\n", "2:1: only rule declarations take attributes, found 'var'"},
		{"@start\n", "2:1: expected 'rule' after the declaration attributes, found end of file"},
		{"@start\nfoo\n", "2:1: expected 'rule' after the declaration attributes, found 'foo'"},
		{"@ start\nrule a = \"x\"\n", "1:3: attribute name must immediately follow '@'"},
	} {
		_, err := Parse(tc.src)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q\n got %v\nwant %s", tc.src, err, tc.want)
		}
	}
}

// After an error, parsing resumes at the attributes of the next rule, so the
// rule keeps them.
func TestDeclarationAttributesRecovery(t *testing.T) {
	g, errs := ParsePartial("rule a = (\n\n@start @foo(1)\n@bar\nrule b = \"y\"\n")
	if len(errs) == 0 {
		t.Fatal("no error")
	}
	rules := g.Rules()
	last := rules[len(rules)-1]
	if last.Name != "b" || len(last.Attrs) != 3 {
		t.Fatalf("recovered %s with %d attributes; errors %v", last.Name, len(last.Attrs), errs)
	}
	// An "@" that starts a line of a rule body is no declaration.
	g, errs = ParsePartial("rule a = (\n@foo \"x\"\nrule b = \"y\"\n")
	if len(errs) == 0 {
		t.Fatal("no error")
	}
	if rules := g.Rules(); rules[len(rules)-1].Name != "b" || len(rules[len(rules)-1].Attrs) != 0 {
		t.Errorf("recovered %v", rules[len(rules)-1])
	}
}

func TestDeclarationAttributesFormat(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"one", "@start\nrule a = \"x\"\n", "@start\nrule a = \"x\"\n"},
		{"several on a line", "@start(default)   @foo\nrule a = \"x\"\n", "@start(default) @foo\nrule a = \"x\"\n"},
		{
			"comments around and between",
			"rule z = \"q\"\n// lead\n@start(default) @foo // trail\n// between\n@bar /* in */ @baz\n\n// before rule\nrule a = z // after\n",
			"rule z = \"q\"\n// lead\n@start(default) @foo // trail\n// between\n@bar /* in */ @baz\n\n// before rule\nrule a = z // after\n",
		},
		{
			"arguments over lines",
			"@start(\n// c\ndefault,\n)\nrule a = \"x\"\n",
			"@start(\n    // c\n    default,\n)\nrule a = \"x\"\n",
		},
		{"comment in the header", "@start\nrule /* c */ a = \"x\"\n", "@start\nrule /* c */ a = \"x\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			got := grammar.Format(g)
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
			if problem := checkRandomFormat(tc.src, g); problem != "" {
				t.Error(problem)
			}
		})
	}
}

// A grammar built without source layout writes each attribute on the line of
// the one before, and "rule" on a line of its own.
func TestDeclarationAttributesFormatWithoutLayout(t *testing.T) {
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{
		Attrs: []*grammar.Attribute{{Name: "start", Args: []any{&grammar.Ref{Name: "default"}}}, {Name: "foo"}},
		Name:  "a",
		Expr:  &grammar.Literal{Value: "x"},
	}}}}
	if got, want := grammar.Format(g), "@start(default) @foo\nrule a = \"x\"\n"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if err := grammar.Validate(g); err != nil {
		t.Error(err)
	}
	g.Rules()[0].Attrs[1].Name = "1x"
	if err := grammar.Validate(g); err == nil || !strings.Contains(err.Error(), "attrs[1]") {
		t.Errorf("an invalid attribute name: %v", err)
	}
}
