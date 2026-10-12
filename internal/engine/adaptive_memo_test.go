package engine

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
					m := memoTable{stride: 1, sparse: mode == "sparse"}
					if tc.repeat {
						if sparseMemo && m.sparse {
							m.markSparseSeen(0, 0)
						} else {
							m.markSeen(0, 0)
						}
					}
					m.calls = []seenCalls{{calls: tc.calls, repeats: tc.repeats}}
					if got := m.firstCall(0, 0); got != !tc.repeat {
						t.Fatalf("first=%v, want %v", got, !tc.repeat)
					}
					c := m.calls[0]
					if c.calls != tc.wantCalls || c.repeats != tc.wantRepeats || c.eager != tc.eager {
						t.Fatalf("counters=%+v, want (%d,%d,%v)", c, tc.wantCalls, tc.wantRepeats, tc.eager)
					}
					if tc.eager && (m.firstCall(0, 0) || m.calls[0] != c) {
						t.Fatal("eager mode resumed counting")
					}
				})
			}

			m := memoTable{stride: 1, sparse: mode == "sparse", calls: []seenCalls{{calls: maxCalls}}}
			if !m.firstCall(0, 0) || m.firstCall(0, 0) || m.calls[0].eager {
				t.Fatal("fresh then repeated calls became eager after rescaling")
			}
			if !m.reset() {
				t.Fatal("reset unexpectedly discarded a small memo table")
			}
			m.stride, m.sparse = 1, mode == "sparse"
			if !m.firstCall(0, 0) || m.calls[0].eager {
				t.Fatal("reset retained adaptive state or seen bits")
			}
		})
	}
}
