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
	if c.repeats*8 > c.calls {
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

// Decisions of a splice callback
const (
	dropEntry  = iota
	keepEntry  // keep at its position
	shiftEntry // move by the splice's delta
)

// splice adjusts the table to an edit that replaced the positions [start, end) with delta more
// positions (a Document edit). keep decides each entry's fate; an entry it shifts must lie at or
// after end. The chains are updated in place instead of being rebuilt. Only tables without
// pruning (base 0) are spliced.
func (t *memoTable) splice(start, end, delta int, keep func(e *memoEntry) int) {
	for i := range t.slots {
		link := &t.slots[i]
		for e := *link; e != nil; e = e.next {
			switch keep(e) {
			case keepEntry:
			case shiftEntry:
				e.pos += delta
			default:
				*link = e.next
				continue
			}
			link = &e.next
		}
	}
	if delta == 0 || len(t.slots) <= start {
		return
	}
	if len(t.slots) <= end {
		t.slots = append(t.slots, make([]*memoEntry, end+1-len(t.slots))...)
	}
	// Detach the entries that stay in place within [start, end] (at end, only an insertion keeps
	// any), move the chains from end on by delta, and put the detached entries back.
	var stay *memoEntry
	for i := start; i <= end; i++ {
		link := &t.slots[i]
		for e := *link; e != nil; {
			next := e.next
			if e.pos == i {
				*link = next
				e.next, stay = stay, e
			} else {
				link = &e.next
			}
			e = next
		}
	}
	n := len(t.slots)
	if delta > 0 {
		t.slots = slices.Grow(t.slots, delta)[:n+delta]
		copy(t.slots[end+delta:], t.slots[end:n])
		clear(t.slots[end : end+delta])
	} else {
		copy(t.slots[end+delta:], t.slots[end:n])
		clear(t.slots[n+delta:])
		t.slots = t.slots[:n+delta]
	}
	for e := stay; e != nil; {
		next := e.next
		e.next = t.slots[e.pos]
		t.slots[e.pos] = e
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
