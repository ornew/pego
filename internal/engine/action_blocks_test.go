package engine

import (
	"testing"

	"github.com/ornew/pego/grammar"
)

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

func TestContextualOperatorsBeforeLiteralBrace(t *testing.T) {
	t.Parallel()
	check(t, "rule operators = \"x\"\nrule main = operators \"{\"", ok("x{", `("x"@operators "{")@main`))
}
