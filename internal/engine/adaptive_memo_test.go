package engine

import "testing"

// Seed long-lived estimator states without allocating billions of input positions.
func TestAdaptiveMemoCounters(t *testing.T) {
	const maxCalls = int32(1<<31 - 1)
	for _, sparse := range []bool{false, true} {
		for _, tc := range []struct {
			name                   string
			calls, repeats         int32
			repeat                 bool
			wantCalls, wantRepeats int32
			eager                  bool
		}{
			{"below", 15, 0, true, 16, 1, false},
			{"above", 14, 0, true, 15, 1, true},
			{"after", 16, 0, true, 17, 1, false},
			{"scaled boundary", 1<<31 - 2, 1<<27 - 1, true, maxCalls, 1 << 27, true},
			{"large exact", 1<<30 - 1, 1<<26 - 1, true, 1 << 30, 1 << 26, false},
			{"large above", 1<<30 - 2, 1<<26 - 1, true, 1<<30 - 1, 1 << 26, true},
			{"before rescale", maxCalls - 1, 1, false, maxCalls, 1, false},
			{"rescale odd unique", maxCalls, 1, false, 1 << 30, 0, false},
			{"rescale odd repeat", maxCalls, 3, true, 1 << 30, 2, false},
			{"rescale unique", maxCalls, 0, false, 1 << 30, 0, false},
			{"rescale repeat", maxCalls, 0, true, 1 << 30, 1, false},
			{"rescale below", maxCalls, 1<<27 - 3, true, 1 << 30, 1<<26 - 1, false},
			{"rescale exact", maxCalls, 1<<27 - 1, true, 1 << 30, 1 << 26, false},
			{"rescale above", maxCalls, 1 << 27, true, 1 << 30, 1<<26 + 1, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := memoTable{stride: 1, sparse: sparse}
				if tc.repeat {
					if sparseMemo && sparse {
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
					t.Fatalf("sparse=%v: counters=%+v, want (%d,%d,%v)", sparse, c, tc.wantCalls, tc.wantRepeats, tc.eager)
				}
				if tc.eager {
					if m.firstCall(0, 0) || m.calls[0] != c {
						t.Fatal("eager mode resumed counting")
					}
				}
			})
		}
		m := memoTable{stride: 1, sparse: sparse}
		m.calls = []seenCalls{{calls: maxCalls}}
		if !m.firstCall(0, 0) || m.firstCall(0, 0) || m.calls[0].eager {
			t.Fatal("fresh then repeated calls became eager after rescaling")
		}
		m.reset()
		if len(m.calls) != 0 {
			t.Fatal("reset retained adaptive counters")
		}
		m.stride, m.sparse = 1, sparse
		if !m.firstCall(0, 0) || m.calls[0].eager {
			t.Fatal("reset retained adaptive state or seen bits")
		}
	}
}

// A long unique-call pass isolates ordinary bookkeeping and also crosses the
// rescaling boundary from a seeded state; no enormous bitset is necessary.
func BenchmarkAdaptiveMemoUniqueCalls(b *testing.B) {
	const positions = 1 << 20
	for _, sparse := range []bool{false, true} {
		for _, seed := range []int32{0, 1<<31 - 1025} {
			name := "dense/ordinary"
			if sparse {
				name = "sparse/ordinary"
			}
			if seed != 0 {
				name += "/rescale"
			}
			b.Run(name, func(b *testing.B) {
				m := memoTable{stride: 1, sparse: sparse}
				m.calls = []seenCalls{{}}
				for pos := 0; pos < positions; pos++ {
					m.firstCall(pos, 0)
				}
				m.reset()
				m.stride, m.sparse = 1, sparse
				m.calls = []seenCalls{{calls: seed}}
				pos := 0
				b.ReportAllocs()
				for b.Loop() {
					if pos == positions {
						m.reset()
						m.stride, m.sparse = 1, sparse
						m.calls = []seenCalls{{calls: seed}}
						pos = 0
					}
					if !m.firstCall(pos, 0) {
						b.Fatal("unique call became repeated")
					}
					pos++
				}
			})
		}
	}
}
