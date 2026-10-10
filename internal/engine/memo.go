package engine

import (
	"errors"
	"slices"
	"strconv"
)

// memoTable is the memo table ((rule, position, binding level) → result). It keeps a linked
// list of entries per position. Positions are offsets from the start of the input; entries at
// positions before base have been discarded (stream parsing). Compared with a Go map, it avoids
// the cost of hashing entries and growing the table, and it can be pruned in position order.
type memoTable struct {
	base  int
	slots []*memoEntry // list of entries at position base+i (base+i-gapLen from the gap on)
	slab  []memoEntry  // area for allocating entries in bulk
	// chunks are the areas slab came from, kept by reset for later parses; used counts those in use.
	chunks [][]memoEntry
	used   int
	// free links retired entries through next. Retiring clears their result
	// references; reuse bounds slab growth in streams and edited Documents.
	free *memoEntry
	// Document edits splice positions in and out at the gap, slots[gap:gap+gapLen] (in Document
	// tables, where base is 0): moving it costs only the distance to the next edit.
	gap, gapLen int
	// seen is a bit set of the (position, rule) pairs called once (parser.firstCall), with stride
	// bits per position. It is used only by whole-input parses, so base is 0.
	seen   []uint64
	stride int
	// pages allocate bits only for a rule's visited 1,024-position regions.
	// Small grammars keep the dense bitset to avoid directory overhead.
	pages    []seenRulePages
	bitPages arena[[16]uint64]
	// calls counts, per rule number, the calls and the repeated calls at a position; once repeats
	// are frequent, the rule is memoized on the first call (eager).
	calls []seenCalls
}

// seenRulePages caches the last visited page beside its directory. Most calls
// of a rule stay within one region, avoiding a second dependent pointer lookup.
type seenRulePages struct {
	pages []*[16]uint64
	last  *[16]uint64
	at    int
}

type seenCalls struct {
	calls, repeats int32
	eager          bool
}

// firstCall records a call of rule number r (rule.seen) at position pos and reports whether its
// memoization is deferred: it is the first call there, and repeated calls of the rule have been
// rare so far.
func (t *memoTable) firstCall(pos, r int) bool {
	if t.calls == nil {
		t.calls = make([]seenCalls, t.stride)
	}
	c := &t.calls[r]
	if c.eager {
		return false
	}
	c.calls++
	var first bool
	if sparseMemo && t.stride > 64 {
		first = t.markSparseSeen(pos, r)
	} else {
		first = t.markSeen(pos, r)
	}
	if first {
		return true
	}
	// Deferring costs an extra evaluation for each position where the rule is called again, and
	// saves a memo entry for each position where it is not. Evaluations cost several entries.
	c.repeats++
	if c.repeats*16 > c.calls {
		c.eager = true
	}
	return false
}

// markSeen records a call of rule number r at position pos and reports whether it is the first.
func (t *memoTable) markSeen(pos, r int) bool {

	i := pos*t.stride + r
	w, b := i>>6, uint64(1)<<(i&63)
	if strconv.IntSize == 32 {
		w, b = seenBit(pos, r, t.stride)
	}
	if w >= len(t.seen) {
		if strconv.IntSize == 32 {
			seen := make([]uint64, seenCapacity(len(t.seen), w))
			copy(seen, t.seen)
			t.seen = seen
		} else {
			t.seen = append(t.seen, make([]uint64, max(w+1-len(t.seen), len(t.seen), 64))...)
		}
	}
	if t.seen[w]&b != 0 {
		return false
	}
	t.seen[w] |= b
	return true
}

// markSparseSeen records only visited per-rule regions, with page allocation
// batched to keep the directory savings from adding one allocation per page.
func (t *memoTable) markSparseSeen(pos, r int) bool {
	if len(t.pages) != t.stride {
		t.pages = make([]seenRulePages, t.stride)
	}
	dir := &t.pages[r]
	at := pos >> 10
	page := dir.last
	if page == nil || dir.at != at {
		pages := dir.pages
		if at >= len(pages) {
			if at < cap(pages) {
				pages = pages[:at+1]
			} else {
				grown := make([]*[16]uint64, at+1, max(at+1, 2*cap(pages), 4))
				copy(grown, pages)
				pages = grown
			}
			dir.pages = pages
		}
		page = pages[at]
		if page == nil {
			page = t.bitPages.alloc()
			pages[at] = page
		}
		dir.last, dir.at = page, at
	}
	w, b := (pos>>6)&15, uint64(1)<<(pos&63)
	if page[w]&b != 0 {
		return false
	}
	page[w] |= b
	return true
}

// seenBit addresses a bit without requiring the intermediate bit number to
// fit a native int. The 64-bit path keeps its original arithmetic; on 32-bit
// hosts the word index can fit even when position*stride exceeds MaxInt.
func seenBit(pos, r, stride int) (int, uint64) {
	if strconv.IntSize == 32 {
		i := uint64(pos)*uint64(stride) + uint64(r)
		w := i >> 6
		if w >= uint64(^uint(0))/8 {
			panic(fatal{errors.New("deferred memoization bitset size overflows uintptr")})
		}
		return int(w), uint64(1) << (i & 63)
	}
	i := pos*stride + r
	return i >> 6, uint64(1) << (i & 63)
}

// seenCapacity bounds both the requested word and geometric growth. The
// caller uses exact make/copy on 32-bit hosts so append cannot overallocate.
func seenCapacity(used, word int) int {
	return max(word+1, min(2*used, int(^uint(0)/8)), 64)
}

func newMemoTable() *memoTable { return &memoTable{} }

// get returns the entry for key k.
func (t *memoTable) get(k memoKey) (*memoEntry, bool) {
	i := k.pos - t.base
	if i >= t.gap {
		i += t.gapLen
	}
	if i < 0 || i >= len(t.slots) {
		return nil, false
	}
	for e := t.slots[i]; e != nil; e = e.next {
		if e.rule == int32(k.rule) && e.min == int32(k.min) && slices.Equal(e.env, k.env) {
			return e, true
		}
	}
	return nil, false
}

// put sets the entry for key k to e (replacing any existing entry for the same key).
func (t *memoTable) put(k memoKey, e *memoEntry) {
	i := k.pos - t.base
	if i < 0 {
		return // discarded position
	}
	if i >= t.gap {
		i += t.gapLen
	}
	if i >= len(t.slots) {
		if i < cap(t.slots) {
			t.slots = t.slots[:i+1]
		} else {
			grown := make([]*memoEntry, i+1, max(2*cap(t.slots), i+1, 64))
			copy(grown, t.slots)
			t.slots = grown
		}
	}
	e.rule, e.min, e.pos, e.env = int32(k.rule), int32(k.min), k.pos, k.env
	link := &t.slots[i]
	for x := *link; x != nil; link, x = &x.next, x.next {
		if x.rule == e.rule && x.min == e.min && slices.Equal(x.env, e.env) {
			e.next = x.next
			*link = e
			if x != e {
				// A replaced growth seed is no longer growState.best: the
				// caller installs its new best before replacing the key.
				t.retire(x)
			}
			return
		}
	}
	e.next = t.slots[i]
	t.slots[i] = e
}

// len returns the number of entries.
func (t *memoTable) len() int {
	n := 0
	t.each(func(*memoEntry) { n++ })
	return n
}

// alloc returns a new entry.
func (t *memoTable) alloc() *memoEntry {
	if e := t.free; e != nil {
		t.free = e.next
		e.next = nil
		return e
	}
	if len(t.slab) == 0 {
		if t.used < len(t.chunks) {
			t.slab = t.chunks[t.used]
		} else {
			t.slab = make([]memoEntry, 256)
			t.chunks = append(t.chunks, t.slab)
		}
		t.used++
	}
	e := &t.slab[0]
	t.slab = t.slab[1:]
	return e
}

// retire releases all result references and makes an unlinked entry reusable.
// It must not be called on a seed still held by an active growState.
func (t *memoTable) retire(e *memoEntry) {
	*e = memoEntry{next: t.free}
	t.free = e
}

// reset empties the table for another parse, keeping its memory, and reports whether that was
// worth keeping (it is not when it is very large).
func (t *memoTable) reset() bool {
	if cap(t.slots) > maxScratch || len(t.chunks)*256 > maxScratch || len(t.seen) > maxScratch || cap(t.pages) > maxScratch/5 || len(t.bitPages.chunks) > maxScratch/(64*16) || cap(t.bitPages.chunks) > maxScratch {
		return false
	}
	// Bound directory backing arrays and the complete stable page chunks.
	words := 5*cap(t.pages) + len(t.bitPages.chunks)*64*16 + cap(t.bitPages.chunks)
	if words > maxScratch {
		return false
	}
	for _, dir := range t.pages {
		if cap(dir.pages) > maxScratch-words {
			return false
		}
		words += cap(dir.pages)
	}
	clear(t.slots)
	for _, c := range t.chunks[:t.used] {
		clear(c)
	}
	clear(t.seen)
	for i := range t.pages {
		dir := &t.pages[i]
		clear(dir.pages)
		dir.last, dir.at = nil, 0
	}
	t.bitPages.reset(0)
	*t = memoTable{slots: t.slots[:0], chunks: t.chunks, seen: t.seen[:0], pages: t.pages, bitPages: t.bitPages}
	return true
}

// prune discards the entries at positions before pos.
func (t *memoTable) prune(pos int) {
	k := pos - t.base
	if k <= 0 {
		return
	}
	for _, head := range t.slots[:min(k, len(t.slots))] {
		for e := head; e != nil; {
			next := e.next
			// The start rule may still be growing across a commit. Its
			// stack-owned best must survive even after its slot is gone.
			if !e.growing {
				t.retire(e)
			}
			e = next
		}
	}
	if k >= len(t.slots) {
		clear(t.slots)
		t.slots = t.slots[:0]
	} else {
		// Compact the remaining part to the front so no references to discarded entries remain.
		n := copy(t.slots, t.slots[k:])
		clear(t.slots[n:])
		t.slots = t.slots[:n]
	}
	t.base = pos
}

// splice adjusts the table to a Document edit that replaced the positions [start, end) with
// delta more positions. Entries elsewhere move with their positions, and advance is applied to
// them when they are next looked up; the entries at the positions [start, end] are decided now:
// advance reports whether each is still valid (one at start kept as it was, or one at end shifted
// by delta). Only tables without pruning (base 0) are spliced.
func (t *memoTable) splice(start, end, delta int, advance func(e *memoEntry) bool) {
	// Detach the entries at [start, end] and keep the valid ones.
	var stay *memoEntry
	for pos := start; pos <= end; pos++ {
		i := pos
		if i >= t.gap {
			i += t.gapLen
		}
		if i >= len(t.slots) {
			break
		}
		for e := t.slots[i]; e != nil; {
			next := e.next
			if advance(e) {
				e.next, stay = stay, e
			} else {
				t.retire(e)
			}
			e = next
		}
		t.slots[i] = nil
	}
	// Move the gap to start, take [start, end) into it, and give the new positions from it.
	if end+t.gapLen >= len(t.slots) { // room for position end, where an entry kept at start may go
		t.slots = append(t.slots, make([]*memoEntry, end+t.gapLen+1-len(t.slots))...)
	}
	switch {
	case t.gap > start:
		copy(t.slots[start+t.gapLen:t.gap+t.gapLen], t.slots[start:t.gap])
		clear(t.slots[start:min(t.gap, start+t.gapLen)])
	case t.gap < start:
		copy(t.slots[t.gap:start], t.slots[t.gap+t.gapLen:start+t.gapLen])
		clear(t.slots[max(start, t.gap+t.gapLen) : start+t.gapLen])
	}
	t.gap = start
	t.gapLen += end - start
	n := end - start + delta
	if n > t.gapLen {
		// Widen the gap, with room for later insertions.
		more := n - t.gapLen + max(1024, len(t.slots)/16)
		grown := make([]*memoEntry, len(t.slots)+more)
		copy(grown, t.slots[:t.gap])
		copy(grown[t.gap+t.gapLen+more:], t.slots[t.gap+t.gapLen:])
		t.slots, t.gapLen = grown, t.gapLen+more
	}
	t.gap += n
	t.gapLen -= n
	for e := stay; e != nil; {
		next := e.next
		i := e.pos
		if i >= t.gap {
			i += t.gapLen
		}
		e.next = t.slots[i]
		t.slots[i] = e
		e = next
	}
}

// each passes every entry to f in position order.
func (t *memoTable) each(f func(e *memoEntry)) {
	for _, head := range t.slots {
		for e := head; e != nil; {
			next := e.next
			f(e)
			e = next
		}
	}
}
