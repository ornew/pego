package engine

import "slices"

// memoTable is the memo table ((rule, position, binding level) → result). It keeps a linked
// list of entries per position. Positions are offsets from the start of the input; entries at
// positions before base have been discarded (stream parsing). Compared with a Go map, it avoids
// the cost of hashing entries and growing the table, and it can be pruned in position order.
type memoTable struct {
	base  int
	slots []*memoEntry // list of entries at position base+i
	slab  []memoEntry  // area for allocating entries in bulk
}

func newMemoTable() *memoTable { return &memoTable{} }

// get returns the entry for key k.
func (t *memoTable) get(k memoKey) (*memoEntry, bool) {
	i := k.pos - t.base
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
		t.slab = make([]memoEntry, 256)
	}
	e := &t.slab[0]
	t.slab = t.slab[1:]
	return e
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
