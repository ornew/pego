package lint

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestNullableRepetitionProofs(t *testing.T) {
	for _, minimum := range []int{0, 1, 2, 3} {
		for _, body := range []string{`_`, `_{0,1}`, `("a" / _)`} {
			g, err := syntax.Parse(fmt.Sprintf("def main=(%s){%d,}", body, minimum))
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
	g, err := syntax.Parse(`def main=!((?^a){0,1}){2}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range Run(g, "main", Options{}) {
		if finding.Check == CheckUselessLookahead {
			t.Fatalf("unsound lookahead proof: %v", finding)
		}
	}
}
