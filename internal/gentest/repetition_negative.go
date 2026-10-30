package gentest

import (
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/compiletest"
)

// GrammarWithNegativeMaximum parses src and sets the maximum of its repetitions to maximum, which the syntax cannot write.
func GrammarWithNegativeMaximum(t testing.TB, src string, maximum int) *grammar.Grammar {
	t.Helper()
	g, err := compiletest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Rules() {
		compile.WalkExpr(r.Expr, func(e grammar.Expr) {
			if rep, ok := e.(*grammar.Repeat); ok {
				rep.Max = maximum
			}
		})
	}
	return g
}
