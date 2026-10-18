package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
	"strings"
	"testing"
)

func TestActionBlocksUnsupported(t *testing.T) {
	for _, src := range []string{
		`rule a = {}`,
		`rule a = "x" => {1}`,
		`rule a = "x" => {x:=1; x<10; 2}`,
		`rule a = ("x" => {1})`,
		`rule a = operators { operand "x" => {1}; level { infix left "+" => {1} } }`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range []*grammar.Grammar{g, jsonActionBlockGrammar(t, g)} {
			if _, err := Compile(input, Options{}); err == nil || !strings.Contains(err.Error(), "action statement blocks are not implemented") {
				t.Fatalf("%s: %v", src, err)
			}
		}
	}
	for _, src := range []string{`rule a = {x:=1} {x<10} "x"`, "type A = {}\nrule a = \"x\" => A{}", `rule a = "x" => ...`} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); err != nil {
			t.Fatal(err)
		}
	}
}

func jsonActionBlockGrammar(t *testing.T, g *grammar.Grammar) *grammar.Grammar {
	t.Helper()
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	result, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSinglePredicateBlockAST(t *testing.T) {
	g := &grammar.Grammar{Statements: []grammar.Statement{&grammar.RuleDef{Name: "main", Expr: &grammar.Predicate{Term: &grammar.Block{Statements: []grammar.Term{&grammar.BoolLit{Value: true}}}}}}}
	if _, err := Compile(jsonActionBlockGrammar(t, g), Options{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Rules()[0].Expr.(*grammar.Predicate).Term.(*grammar.Block); !ok {
		t.Fatal("compile mutated source AST")
	}
}

func TestContextualOperatorsBeforeLiteralBrace(t *testing.T) {
	check(t, "rule operators = \"x\"\nrule main = operators \"{\"", ok("x{", `("x"@operators "{")@main`))
}
