package sample

import (
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestNullableRepetitionMatcher(t *testing.T) {
	for _, minimum := range []int{0, 1, 2, 3} {
		e := &grammar.Repeat{Expr: &grammar.Top{}, Min: minimum, Max: -1}
		m := &matcher{}
		got, end := m.run(e, nil, 0, true)
		want := matched
		if minimum > 1 {
			want = failed
		}
		if got != want || end != 0 {
			t.Errorf("minimum %d: match=%v end=%d, want %v/0", minimum, got, end, want)
		}
		if got := (&info{}).alwaysMatches(e); got != (minimum <= 1) {
			t.Errorf("minimum %d: alwaysMatches=%v", minimum, got)
		}
	}
}
