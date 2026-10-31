package compile

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/starttest"
)

func TestActionBlocksUnsupported(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule a = {}`,
		`rule a = "x" => {1}`,
		`var x int
rule a = "x" => {x:=1; x<10; 2}`,
		`rule a = ("x" => {1})`,
		`rule a = #operators { operand "x" => {1}; level { infix left "+" => {1} } }`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, input := range []*grammar.Grammar{g, jsonActionBlockGrammar(t, g)} {
			if _, err := compileStatic(input, Options{}, nil); err == nil || !strings.Contains(err.Error(), "action statement blocks are not implemented") {
				t.Fatalf("%s: %v", src, err)
			}
		}
	}
	for _, src := range []string{`var x int
rule a = {x:=1} {x<10} "x"`, "type A = {}\nrule a = \"x\" => A{}", `rule a = "x" => ...`} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSinglePredicateBlockAST(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Predicate{Term: &grammar.Block{Statements: []grammar.Term{&grammar.BoolLit{Value: true}}}}}}}}
	starttest.MarkAll(g)
	if _, err := compileStatic(jsonActionBlockGrammar(t, g), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Rules()[0].Expr.(*grammar.Predicate).Term.(*grammar.Block); !ok {
		t.Fatal("compile mutated source AST")
	}
}
