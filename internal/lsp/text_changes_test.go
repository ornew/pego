package lsp

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// TestApplyChangesMatchesSequential compares applyChanges with applying each change in turn, on
// random texts with every line terminator and surrogate pairs. Most notifications move up the
// text, as multi-cursor edits do; some include ranges outside the text, reversed ranges and full
// replacements.
func TestApplyChangesMatchesSequential(t *testing.T) {
	pieces := []string{"a", "bc", "\n", "\r", "\r\n", "😀", "é", "\n\r"}
	random := func(r *rand.Rand, n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(pieces[r.IntN(len(pieces))])
		}
		return b.String()
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 20000 {
		text := random(r, r.IntN(24))
		lines := strings.Count(text, "\n") + strings.Count(text, "\r") + 1
		line := lines + 1
		var changes []contentChange
		for range 1 + r.IntN(6) {
			if r.IntN(20) == 0 {
				changes = append(changes, contentChange{Text: random(r, r.IntN(6))})
				continue
			}
			if r.IntN(4) == 0 {
				line = r.IntN(lines+3) - 1 // any line, out of order
			} else {
				line -= r.IntN(3) // moving up, sometimes on the same line
			}
			start := Position{line - r.IntN(2), r.IntN(6) - 1}
			end := Position{line, r.IntN(6) - 1}
			if r.IntN(8) == 0 {
				start, end = end, start
			}
			changes = append(changes, contentChange{Range: &Range{start, end}, Text: random(r, r.IntN(3))})
		}
		want := text
		for _, c := range changes {
			want = applyChange(want, c)
		}
		if got := applyChanges(text, changes); got != want {
			t.Fatalf("case %d: text %q changes %s: got %q, want %q", i, text, describeChanges(changes), got, want)
		}
	}
}

func describeChanges(changes []contentChange) string {
	var b strings.Builder
	for _, c := range changes {
		if c.Range == nil {
			fmt.Fprintf(&b, "[all %q]", c.Text)
		} else {
			fmt.Fprintf(&b, "[%v-%v %q]", c.Range.Start, c.Range.End, c.Text)
		}
	}
	return b.String()
}
