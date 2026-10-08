package engine

import "slices"

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
	// Document edits splice positions in and out at the gap, slots[gap:gap+gapLen] (in Document
	// tables, where base is 0): moving it costs only the distance to the next edit.
	gap, gapLen int
	// seen is a bit set of the (position, rule) pairs called once (parser.firstCall), with stride
	// bits per position. It is used only by whole-input parses, so base is 0.
	seen   []uint64
	stride int
	// calls counts, per rule number, the calls and the repeated calls at a position; once repeats
	// are frequent, the rule is memoized on the first call (eager).
	calls []seenCalls
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
	if t.markSeen(pos, r) {
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
	if w >= len(t.seen) {
		t.seen = append(t.seen, make([]uint64, max(w+1-len(t.seen), len(t.seen), 64))...)
	}
	if t.seen[w]&b != 0 {
		return false
	}
	t.seen[w] |= b
	return true
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

// reset empties the table for another parse, keeping its memory, and reports whether that was
// worth keeping (it is not when it is very large).
func (t *memoTable) reset() bool {
	if cap(t.slots) > maxScratch || len(t.chunks)*256 > maxScratch || len(t.seen) > maxScratch {
		return false
	}
	clear(t.slots)
	for _, c := range t.chunks[:t.used] {
		clear(c)
	}
	clear(t.seen)
	*t = memoTable{slots: t.slots[:0], chunks: t.chunks, seen: t.seen[:0]}
	return true
}

// prune discards the entries at positions before pos.
func (t *memoTable) prune(pos int) {
	k := pos - t.base
	if k <= 0 {
		return
	}
	if k >= len(t.slots) {
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
