package lint

import (
	"fmt"
	"testing"

	"github.com/ornew/zeroparse/internal/syntax"
)

func TestNullableRepetitionProofs(t *testing.T) {
	for _, minimum := range []int{0, 1, 2, 3} {
		for _, body := range []string{`_`, `_ #repeat(0,1)`, `("a" / _)`} {
			g, err := syntax.Parse(fmt.Sprintf("rule main = %s #repeat(%d, )", body, minimum))
			if err != nil {
				t.Fatal(err)
			}
			a := newAnalysis(g)
			e := g.Rules()[0].Expr
			want := minimum <= 1
			if a.isEngineNullable(e) != want || a.nullableAt(e, false) != want || a.nullableAt(e, true) != want || a.succeeds(e) != want {
				t.Errorf("%s min=%d: nullable=%v/%v/%v succeeds=%v, want %v", body, minimum, a.isEngineNullable(e), a.nullableAt(e, false), a.nullableAt(e, true), a.succeeds(e), want)
			}
		}
	}
	// The old proof called this lookahead useless because an unknown-length
	// successful inner match was repeated until the minimum. Empty text now
	// stops after the first zero-consumption iteration, so the lookahead works.
	g, err := syntax.Parse(`rule main = ![^a] #repeat(0, 1) #repeat(2)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range runUnchecked(g, "main", options{}) {
		if finding.Check == CheckUselessLookahead {
			t.Fatalf("unsound lookahead proof: %v", finding)
		}
	}
}
