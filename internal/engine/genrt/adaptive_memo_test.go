package genrt

import "testing"

// TestAdaptiveMemoCounters seeds large counters directly so the overflow edge can
// be covered without billions of parser calls or a huge seen bitmap.
func TestAdaptiveMemoCounters(t *testing.T) {
	const maxCalls = int32(1<<31 - 1)
	for _, mode := range []string{"dense", "sparse"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name                   string
				calls, repeats         int32
				repeat                 bool
				wantCalls, wantRepeats int32
				eager                  bool
			}{
				{"below threshold", 15, 0, true, 16, 1, false},
				{"above threshold", 14, 0, true, 15, 1, true},
				{"ordinary next call", 16, 0, true, 17, 1, false},
				{"threshold before discount", 1<<31 - 2, 1<<27 - 1, true, maxCalls, 1 << 27, true},
				{"large exact threshold", 1<<30 - 1, 1<<26 - 1, true, 1 << 30, 1 << 26, false},
				{"large above threshold", 1<<30 - 2, 1<<26 - 1, true, 1<<30 - 1, 1 << 26, true},
				{"before rescale", maxCalls - 1, 1, false, maxCalls, 1, false},
				{"rescale odd unique", maxCalls, 1, false, 1 << 30, 0, false},
				{"rescale odd repeat", maxCalls, 3, true, 1 << 30, 2, false},
				{"rescale unique", maxCalls, 0, false, 1 << 30, 0, false},
				{"rescale repeat", maxCalls, 0, true, 1 << 30, 1, false},
				{"rescale below threshold", maxCalls, 1<<27 - 3, true, 1 << 30, 1<<26 - 1, false},
				{"rescale exact threshold", maxCalls, 1<<27 - 1, true, 1 << 30, 1 << 26, false},
				{"rescale above threshold", maxCalls, 1 << 27, true, 1 << 30, 1<<26 + 1, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					p := Parser{memo: memoTable{stride: 1, sparse: mode == "sparse"}}
					r := &Rule{Seen: 0}
					if tc.repeat {
						if p.memo.sparse {
							p.memo.markSparseSeen(0, 0)
						} else {
							p.memo.seen = []uint64{1}
						}
					}
					p.memo.calls = []seenCalls{{calls: tc.calls, repeats: tc.repeats}}
					if got := p.FirstCall(r); got != !tc.repeat {
						t.Fatalf("first=%v, want %v", got, !tc.repeat)
					}
					c := p.memo.calls[0]
					if c.calls != tc.wantCalls || c.repeats != tc.wantRepeats || c.eager != tc.eager {
						t.Fatalf("counters=%+v, want (%d,%d,%v)", c, tc.wantCalls, tc.wantRepeats, tc.eager)
					}
					if tc.eager && (p.FirstCall(r) || p.memo.calls[0] != c) {
						t.Fatal("eager mode resumed counting")
					}
				})
			}
		})
	}
}
