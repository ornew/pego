package engine

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// Compare the ordered records with the original linear algorithm, including
// stack aliases, nested scopes, silence and retained memo/recovery copies.
// This file also runs unchanged inside generated plain and typed packages.
func TestExpectationRecords(t *testing.T) {
	p := &parser{}
	var expected []expID
	base, far := 0, 0
	type mark struct {
		actual    expMark
		base, far int
	}
	var stack []mark
	type saved struct{ actual, expected []expID }
	var kept []saved
	rng := rand.New(rand.NewPCG(12, 91))
	linear := func(pos int, id expID) {
		if p.silent > 0 || pos < far {
			return
		}
		if pos > far {
			far = pos
			expected = expected[:base]
		}
		if !slices.Contains(expected[base:], id) {
			expected = append(expected, id)
		}
	}
	record := func(pos int, id expID) { p.expect(pos, id); linear(pos, id) }
	check := func(step int) {
		t.Helper()
		if p.expBase != base || p.farthest != far || !slices.Equal(p.exp, expected) {
			t.Fatalf("step %d: got base=%d far=%d %v; want base=%d far=%d %v", step, p.expBase, p.farthest, p.exp, base, far, expected)
		}
		for _, k := range kept {
			if !slices.Equal(k.actual, k.expected) {
				t.Fatal("retained record changed")
			}
		}
	}
	// More IDs than filter bits guarantee collisions without depending on the
	// hash function. Plain and message IDs must remain distinct and ordered.
	for id := expID(0); id < 160; id++ {
		record(0, id)
		record(0, id|msgBit)
	}
	for id := expID(159); id >= 0; id-- {
		record(0, id|msgBit)
		record(0, id)
	}
	check(-1)
	for step := 0; step < 12000; step++ {
		switch rng.IntN(10) {
		case 0, 1, 2, 3:
			id := expID(rng.IntN(180))
			if rng.IntN(2) == 0 {
				id |= msgBit
			}
			record(far+rng.IntN(3)-1, id)
		case 4:
			if len(stack) < 16 {
				pos := rng.IntN(30)
				stack = append(stack, mark{p.isolate(pos), base, far})
				base, far = len(expected), pos
			}
		case 5, 6:
			if len(stack) > 0 {
				m := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				inner := slices.Clone(expected[base:])
				innerFar := far
				gotFar, gotInner := p.unisolate(m.actual)
				if gotFar != innerFar || !slices.Equal(gotInner, inner) {
					t.Fatal("inner record differs")
				}
				expected = expected[:base]
				base, far = m.base, m.far
				if step%2 == 0 {
					// gotInner aliases the stack which mergeExpected appends to.
					p.mergeExpected(gotFar, gotInner)
					for _, id := range inner {
						linear(innerFar, id)
					}
				}
			}
		case 7:
			p.silent++
			record(far+20, expID(rng.IntN(180)))
			p.silent--
		case 8:
			if len(kept) < 40 {
				kept = append(kept, saved{p.keep(p.exp[base:]), slices.Clone(expected[base:])})
			}
		case 9:
			// Farther failures reset only the current record, leaving parents.
			record(far+1, expID(step%180))
		}
		check(step)
	}
}
