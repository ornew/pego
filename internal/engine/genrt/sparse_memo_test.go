package genrt

import "testing"

func TestSparseMemoBoundariesAndReset(t *testing.T) {
	memo := memoTable{}
	for _, stride := range []int{63, 64, 65, 128, 175, 65, 64} {
		memo.stride = stride
		positions := []int{0, 63, 64, 1023, 1024, 1025, 65535}
		for _, pos := range positions {
			for _, r := range []int{0, stride - 1} {
				mark := memo.markSeen
				if stride > 64 {
					mark = memo.markSparseSeen
				}
				if !mark(pos, r) || mark(pos, r) {
					t.Fatalf("stride=%d pos=%d rule=%d: first/repeated mismatch", stride, pos, r)
				}
			}
		}
		for i := len(positions) - 1; i >= 0; i-- {
			mark := memo.markSeen
			if stride > 64 {
				mark = memo.markSparseSeen
			}
			if mark(positions[i], stride-1) {
				t.Fatal("returning to an earlier page lost its bit")
			}
		}
		memo.resetSeen()
		for _, dir := range memo.pages {
			if dir.last != nil {
				t.Fatal("cached page pointer survived reset")
			}
		}
		if memo.bitPages.n != 0 {
			t.Fatal("allocated page count survived reset")
		}
	}
}

func TestSparseMemoPoolReuse(t *testing.T) {
	for _, typed := range []bool{false, true} {
		p := &parser{memo: memoTable{stride: 65}}
		r := &rule{seen: 64}
		p.pos = 1024
		if !p.firstCall(r) || p.firstCall(r) {
			t.Fatal("first/repeated mismatch")
		}
		if typed {
			q := &tparser{parser: p}
			q.recycle()
		} else {
			p.release()
			for _, dir := range p.memo.pages {
				if dir.last != nil {
					t.Fatal("released parser retains cached page")
				}
				for _, page := range dir.pages {
					if page != nil {
						t.Fatal("released parser retains page pointers")
					}
				}
			}
			for _, c := range p.memo.calls {
				if c != (seenCalls{}) {
					t.Fatal("released parser retains eager decisions")
				}
			}
			continue
		}
		p.memo.stride, p.pos = 65, 1024
		if !p.firstCall(r) {
			t.Fatal("page bit or eager decision survived pool reset")
		}
	}
}

func TestSparseMemoScratchBounds(t *testing.T) {
	const limit = 1 << 20
	dense := &tparser{parser: &parser{memo: memoTable{seen: make([]uint64, 0, limit+1)}}}
	dense.recycle()
	if dense.memo.seen != nil {
		t.Fatal("typed pool retained oversized dense seen scratch")
	}
	for _, memo := range []memoTable{
		{pages: make([]seenRulePages, 0, limit/5+1)},
		{pages: make([]seenRulePages, 0, limit/5), bitPages: seenPageArena{chunks: []*[64][16]uint64{new([64][16]uint64)}}},
		{pages: []seenRulePages{{pages: make([]*[16]uint64, 0, limit+1)}}},
	} {
		memo.resetSeen()
		if memo.pages != nil || memo.bitPages.chunks != nil {
			t.Fatal("oversized sparse scratch retained")
		}
	}
}

// Dropping an oversized dense buffer must preserve the plain parser's
// existing policy of discarding its complete memo scratch table.
func TestSparseMemoDenseReleasePolicy(t *testing.T) {
	p := &parser{memo: memoTable{slots: make([]*memoEntry, 1), seen: make([]uint64, 0, (1<<20)+1)}}
	p.release()
	if p.memo.slots != nil || p.memo.seen != nil {
		t.Fatal("oversized dense table changed the plain release policy")
	}
}
