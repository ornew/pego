package syntax

import (
	"github.com/ornew/pego/grammar"
	"strings"
	"testing"
)

func TestActionBlocksSource(t *testing.T) {
	for _, src := range []string{
		`rule a = {indent:=0} {indent<10; indent>10} => {foo; A{}}`,
		"rule a = (b => {x:=1; x<10; B{}}) => {\n x := 1; x < 10\n A{}\n}",
		"rule a = {\n // assignment\n x := 1\n /* condition */ x < 10\n} => {\n // result\n A{}\n}",
		`rule a = {} => {}`,
	} {
		t.Run(src, func(t *testing.T) {
			g, err := Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			if err := grammar.Validate(g); err != nil {
				t.Fatal(err)
			}
			formatted := grammar.Format(g)
			g2, err := Parse(formatted)
			if err != nil {
				t.Fatalf("%v\n%s", err, formatted)
			}
			if next := grammar.Format(g2); next != formatted {
				t.Fatalf("unstable format:\n%s\n---\n%s", formatted, next)
			}
			if len(g.AllComments()) != len(g2.AllComments()) {
				t.Fatalf("lost comments: %d -> %d", len(g.AllComments()), len(g2.AllComments()))
			}
			raw, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := grammar.UnmarshalJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := grammar.Validate(decoded); err != nil {
				t.Fatal(err)
			}
			if _, ok := g.Rules()[0].Action.(*grammar.Block); !ok {
				t.Fatal("action block not retained")
			}
		})
	}
}

func TestStatementNewlines(t *testing.T) {
	for _, src := range []string{
		"rule a =\n b", "rule a = b\n c", "rule a = b\n => 1",
		"rule a = b =>\n 1", "rule a = operators {\n operand a\n b\n}",
		"rule a = operators {\n operand a\n => 1\n}",
		"rule a = {x :=\n 1}", "rule a = {x\n + 1}",
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("accepted free newline: %q", src)
		}
	}
	for _, src := range []string{
		"rule a = (\n b\n c\n)",
		"  rule a = b\n\trule b = c", "rule a = b; rule b = c",
		"rule a = b => (\n 1 +\n 2\n)",
		"rule a = operators {\n operand (\n a\n b\n) => (\n 1\n)\n level { infix left (\n \"+\" / \"-\"\n) }\n}",
		"rule a = {x := (\n 1 +\n 2\n)}",
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("rejected grouped source %q: %v", src, err)
		}
	}
}

func TestGroupedMultilineTypeExpressions(t *testing.T) {
	for _, src := range []string{
		"type Alias = A | B",
		"type Alias = (\n A\n | B\n)",
		"type Holder struct {\n Value (\n  A\n  | B\n )\n}",
		"rule main (\n A\n | B\n) = \"x\"",
	} {
		g, err := Parse(src)
		if err != nil {
			t.Errorf("rejected type expression %q: %v", src, err)
			continue
		}
		formatted := grammar.Format(g)
		if _, err := Parse(formatted); err != nil {
			t.Errorf("formatted type expression %q is invalid: %v", formatted, err)
		}
	}
	for _, src := range []string{
		"type Alias = A\n | B",
		"type Holder struct {\n Value A\n | B\n}",
		"rule main A\n | B = \"x\"",
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("accepted ungrouped multiline type expression %q", src)
		}
	}
}

func TestUnnamedOperatorsLevels(t *testing.T) {
	src := `rule main = level(expr, named)
rule expr = operators {
 operand "x"
 level { infix left "+" }
 level named { infix left "*" }
 level { prefix "-" }
}`
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := grammar.Validate(g); err != nil {
		t.Fatal(err)
	}
	levels := g.Rules()[1].Expr.(*grammar.OperatorsRule).Levels
	if len(levels) != 3 || levels[0].Name != "" || levels[1].Name != "named" || levels[2].Name != "" {
		t.Fatalf("unexpected level names: %+v", levels)
	}
	formatted := grammar.Format(g)
	if _, err := Parse(formatted); err != nil {
		t.Fatalf("formatted levels are invalid: %v\n%s", err, formatted)
	}
}

func TestContextualRuleIdentifiers(t *testing.T) {
	for word := range keywords {
		src := "rule " + word + " = \"x\"\nrule main = " + word
		if _, err := Parse(src); err != nil {
			t.Errorf("%s: %v", word, err)
		}
	}
	g, err := Parse(`rule main = level(expr, additive)`)
	if err != nil {
		t.Fatal(err)
	}
	ref := g.Rules()[0].Expr.(*grammar.Ref)
	if ref.Name != "expr" || ref.Level != "additive" || ref.Pos.Col != 19 {
		t.Fatalf("%+v", ref)
	}
	if got := grammar.FormatExpr(ref); got != "level(expr, additive)" {
		t.Fatal(got)
	}
	if _, err := Parse(`rule main = expr(additive)`); err == nil || !strings.Contains(err.Error(), "level(") {
		t.Fatalf("old call accepted: %v", err)
	}
}

func TestMalformedExpressionsReturnErrors(t *testing.T) {
	for _, src := range []string{"(", "{x :=}", "{x;;}", "expr(old)", "level(expr)", "(?x)"} {
		if _, err := ParseExpr(src); err == nil {
			t.Errorf("accepted malformed expression: %s", src)
		}
	}
}

func TestOperatorsGroupedComments(t *testing.T) {
	src := `rule main = operators { skip (/* skip */ " "*); operand (/* operand */ "x"); level { infix left (/* operator */ "+") } }`
	for i := 0; i < 2; i++ {
		g, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		comments := g.AllComments()
		if len(comments) != 3 {
			t.Fatalf("all comments: %+v", comments)
		}
		seen := map[string]bool{}
		for _, c := range comments {
			seen[c.Text] = true
		}
		for _, text := range []string{"/* skip */", "/* operand */", "/* operator */"} {
			if !seen[text] {
				t.Fatalf("missing %s", text)
			}
		}
		src = grammar.Format(g)
	}
}

func TestContextualOperatorsLiteralBrace(t *testing.T) {
	src := "rule operators = \"x\"\nrule main = operators \"{\""
	for i := 0; i < 2; i++ {
		g, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		src = grammar.Format(g)
	}
}

func TestAttributeGroupedComments(t *testing.T) {
	src := `rule main = "x" @future((/* positional */ "x"), value=(/* keyword */ "y"))`
	for i := 0; i < 2; i++ {
		g, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		comments := g.AllComments()
		if len(comments) != 2 {
			t.Fatalf("all comments: %+v", comments)
		}
		seen := map[string]bool{}
		for _, c := range comments {
			seen[c.Text] = true
		}
		for _, text := range []string{"/* positional */", "/* keyword */"} {
			if !seen[text] {
				t.Fatalf("missing %s", text)
			}
		}
		src = grammar.Format(g)
	}
}
