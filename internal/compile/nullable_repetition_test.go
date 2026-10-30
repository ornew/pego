package compile

import (
	"fmt"
	"testing"
)

func TestNullableRepetitionAnalysis(t *testing.T) {
	t.Parallel()
	for _, minimum := range []int{0, 1, 2, 3} {
		g, err := parseTest(fmt.Sprintf("rule main = _ #repeat(%d, )", minimum))
		if err != nil {
			t.Fatal(err)
		}
		if got := analyze(g.Rules()).nullable["main"]; got != (minimum <= 1) {
			t.Errorf("minimum %d: nullable=%v", minimum, got)
		}
		g, err = parseTest(fmt.Sprintf("rule main = _ #repeat(%d, ) r / \"a\"\nrule r = main", minimum))
		if err != nil {
			t.Fatal(err)
		}
		if got := analyze(g.Rules()).cyclic["main"]; got != (minimum <= 1) {
			t.Errorf("minimum %d: left-call cycle=%v", minimum, got)
		}
	}
}
