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
		memo.resetSeen(1 << 20)
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
		p := &parser{memo: memoTable{stride: 65, sparse: true}}
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
		p.memo.stride, p.memo.sparse, p.pos = 65, true, 1024
		if !p.firstCall(r) {
			t.Fatal("page bit or eager decision survived pool reset")
		}
	}
}

func TestSparseMemoScratchBounds(t *testing.T) {
	const limit = 1 << 20
	dense := &tparser{parser: &parser{n: 4 << 20, memo: memoTable{seen: make([]uint64, 0, limit+1)}}}
	dense.recycle()
	if dense.memo.seen != nil {
		t.Fatal("typed pool retained oversized dense seen scratch")
	}
	for _, memo := range []memoTable{
		{pages: make([]seenRulePages, 0, limit/5+1)},
		{pages: make([]seenRulePages, 0, limit/5), bitPages: seenPageArena{chunks: []*[64][16]uint64{new([64][16]uint64)}}},
		{pages: []seenRulePages{{pages: make([]*[16]uint64, 0, limit+1)}}},
	} {
		memo.resetSeen(limit)
		if memo.pages != nil || memo.bitPages.chunks != nil {
			t.Fatal("oversized sparse scratch retained")
		}
	}
}

func TestSparseMemoTypedRetentionAndTrimming(t *testing.T) {
	const limit = 2 << 20
	for _, extra := range []int{0, 1} {
		// Count the whole chunk and its directory, including unused capacity.
		chunk := new([64][16]uint64)
		chunk[0][0] = 1
		directory := make([]*[16]uint64, 1, limit-5*65-64*16-1+extra)
		directory[0] = &chunk[0]
		pages := make([]seenRulePages, 65)
		pages[0] = seenRulePages{pages: directory, last: directory[0], at: 1}
		p := &tparser{parser: &parser{n: limit, memo: memoTable{
			pages:    pages,
			bitPages: seenPageArena{chunks: []*[64][16]uint64{chunk}, n: 1},
			calls:    []seenCalls{{calls: 1, repeats: 1, eager: true}},
		}}}
		p.recycle()
		if extra == 1 {
			if p.memo.pages != nil || p.memo.bitPages.chunks != nil {
				t.Fatal("typed pool retained sparse backing above its input budget")
			}
			continue
		}
		if len(p.memo.pages) != 65 || len(p.memo.bitPages.chunks) != 1 {
			t.Fatal("typed pool discarded sparse backing at its input budget")
		}
		dir := p.memo.pages[0]
		if dir.last != nil || dir.at != 0 || dir.pages[0] != nil || chunk[0][0] != 0 || p.memo.bitPages.n != 0 || p.memo.calls[0] != (seenCalls{}) {
			t.Fatal("retained sparse scratch preserved first-call state")
		}
		p.n, p.memo.stride, p.memo.sparse, p.pos = limit, 65, true, 0
		if !p.firstCall(&rule{seen: 0}) || p.firstCall(&rule{seen: 0}) {
			t.Fatal("retained sparse scratch changed first/repeated decisions")
		}
		if p.memo.bitPages.chunks[0] != chunk || p.memo.pages[0].pages[0] != &chunk[0] {
			t.Fatal("retained sparse scratch was not reused")
		}
		p.n = 16 // A small parse trims a cache left by a larger parse.
		p.recycle()
		if p.memo.pages != nil || p.memo.bitPages.chunks != nil {
			t.Fatal("small typed parse retained the preceding large sparse cache")
		}
		p.n, p.memo.stride, p.memo.sparse, p.pos = limit, 65, true, 0
		if !p.firstCall(&rule{seen: 0}) || p.firstCall(&rule{seen: 0}) {
			t.Fatal("trimmed sparse scratch changed first/repeated decisions")
		}
	}
}

func TestSparseMemoTypedAbsoluteRetentionBound(t *testing.T) {
	const limit = 4 << 20
	p := &tparser{parser: &parser{n: int(^uint(0) >> 1), memo: memoTable{
		pages: []seenRulePages{{pages: make([]*[16]uint64, 0, limit)}},
	}}}
	p.recycle()
	if p.memo.pages != nil || p.memo.bitPages.chunks != nil {
		t.Fatal("maximum-sized input bypassed the absolute sparse retention bound")
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
