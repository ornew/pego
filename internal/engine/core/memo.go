package core

import "slices"

// MemoTable is the memo table ((rule, position, binding level) → result). It keeps a linked
// list of entries per position. Positions are offsets from the start of the input; entries at
// positions before base have been discarded (stream parsing). Compared with a Go map, it avoids
// the cost of hashing entries and growing the table, and it can be pruned in position order.
type MemoTable struct {
	Base  int
	Slots []*MemoEntry // list of entries at position base+i (base+i-GapLen from the gap on)
	slab  []MemoEntry  // area for allocating entries in bulk
	// Chunks are the areas slab came from, kept by reset for later parses; used counts those in use.
	Chunks [][]MemoEntry
	used   int
	// Free links retired entries through next. Retiring clears their result
	// references; reuse bounds slab growth in streams and edited Documents.
	Free *MemoEntry
	// Document edits splice positions in and out at the gap, slots[gap:gap+GapLen] (in Document
	// tables, where base is 0): moving it costs only the distance to the next edit.
	Gap, GapLen int
	// Seen is a bit set of the (position, rule) pairs called once (parser.firstCall), with stride
	// bits per position. It is used only by whole-input parses, so base is 0.
	Seen   []uint64
	Stride int
	Sparse bool // selected once from rule count and input length
	// Pages allocate bits only for a rule's visited 1,024-position regions.
	// Small grammars and inputs keep the dense bitset to avoid directory overhead.
	Pages    []SeenRulePages
	BitPages Arena[[16]uint64]
	// Calls counts, per rule number, the calls and the repeated calls at a position; once repeats
	// are frequent, the rule is memoized on the first call (eager).
	Calls []SeenCalls
}

// SeenRulePages caches the last visited page beside its directory. Most calls
// of a rule stay within one region, avoiding a second dependent pointer lookup.
type SeenRulePages struct {
	Pages []*[16]uint64
	Last  *[16]uint64
	at    int
}

type SeenCalls struct {
	calls, repeats int32
	Eager          bool
}

// firstCall records a call of rule number r (rule.Seen) at position pos and reports whether its
// memoization is deferred: it is the first call there, and repeated calls of the rule have been
// rare so far.
func (t *MemoTable) firstCall(pos, r int) bool {
	if t.Calls == nil {
		t.Calls = make([]SeenCalls, t.Stride)
	}
	c := &t.Calls[r]
	if c.Eager {
		return false
	}
	// Discount old observations before the compact counters can wrap. Eager
	// mode is sticky; this estimator only chooses when to enter it.
	calls := uint32(c.calls) + 1
	if calls&(1<<31) != 0 {
		calls >>= 1
		c.repeats >>= 1
	}
	c.calls = int32(calls)
	var first bool
	if SparseMemo && t.Sparse {
		page := t.cachedSeenPage(pos, r)
		if page == nil {
			page = t.sparsePage(pos, r)
		}
		first = markSeenPage(page, pos)
	} else {
		first = t.MarkSeen(pos, r)
	}
	if first {
		return true
	}
	// Deferring costs an extra evaluation for each position where the rule is called again, and
	// saves a memo entry for each position where it is not. Evaluations cost several entries.
	c.repeats++
	if c.repeats > c.calls>>4 {
		c.Eager = true
	}
	return false
}

// MarkSeen records a call of rule number r at position pos and reports whether it is the first.
func (t *MemoTable) MarkSeen(pos, r int) bool {
	i := pos*t.Stride + r
	w, b := i>>6, uint64(1)<<(i&63)
	if w >= len(t.Seen) {
		t.Seen = append(t.Seen, make([]uint64, max(w+1-len(t.Seen), len(t.Seen), 64))...)
	}
	if t.Seen[w]&b != 0 {
		return false
	}
	t.Seen[w] |= b
	return true
}

// MarkSparseSeen records only visited per-rule regions, with page allocation
// batched to keep the directory savings from adding one allocation per page.
func (t *MemoTable) MarkSparseSeen(pos, r int) bool {
	page := t.cachedSeenPage(pos, r)
	if page == nil {
		page = t.sparsePage(pos, r)
	}
	return markSeenPage(page, pos)
}

// cachedSeenPage and markSeenPage inline into firstCall. Keeping allocation
// separate avoids another function call when a rule remains in its last page.
func (t *MemoTable) cachedSeenPage(pos, r int) *[16]uint64 {
	if r < len(t.Pages) {
		dir := &t.Pages[r]
		if dir.at == pos>>10 {
			return dir.Last
		}
	}
	return nil
}

func markSeenPage(page *[16]uint64, pos int) bool {
	w, b := (pos>>6)&15, uint64(1)<<(pos&63)
	first := page[w]&b == 0
	page[w] |= b
	return first
}

// sparsePage handles directory growth and page allocation off the hot path.
func (t *MemoTable) sparsePage(pos, r int) *[16]uint64 {
	if len(t.Pages) != t.Stride {
		t.Pages = make([]SeenRulePages, t.Stride)
	}
	dir := &t.Pages[r]
	at := pos >> 10
	page := dir.Last
	if page == nil || dir.at != at {
		pages := dir.Pages
		if at >= len(pages) {
			if at < cap(pages) {
				pages = pages[:at+1]
			} else {
				grown := make([]*[16]uint64, at+1, max(at+1, 2*cap(pages), 4))
				copy(grown, pages)
				pages = grown
			}
			dir.Pages = pages
		}
		page = pages[at]
		if page == nil {
			page = t.BitPages.Alloc()
			pages[at] = page
		}
		dir.Last, dir.at = page, at
	}
	return page
}

func NewMemoTable() *MemoTable { return &MemoTable{} }

// Get returns the entry for key k.
func (t *MemoTable) Get(k MemoKey) (*MemoEntry, bool) {
	i := k.Pos - t.Base
	if i >= t.Gap {
		i += t.GapLen
	}
	if i < 0 || i >= len(t.Slots) {
		return nil, false
	}
	for e := t.Slots[i]; e != nil; e = e.Next {
		if e.Rule == int32(k.Rule) && e.Min == int32(k.Min) && e.missingSilent == k.MissingSilent && slices.Equal(e.Env, k.Env) {
			return e, true
		}
	}
	return nil, false
}

// growing returns the growing result of a left-recursion leader with the rule, position, level
// and lookahead kind of k, whatever variable environment it was stored with: while a leader
// grows at a position, every call of it there reads that result (see CallBegin).
func (t *MemoTable) growing(k MemoKey) (*MemoEntry, bool) {
	i := k.Pos - t.Base
	if i >= t.Gap {
		i += t.GapLen
	}
	if i < 0 || i >= len(t.Slots) {
		return nil, false
	}
	for e := t.Slots[i]; e != nil; e = e.Next {
		if e.Growing && e.Rule == int32(k.Rule) && e.Min == int32(k.Min) && e.missingSilent == k.MissingSilent {
			return e, true
		}
	}
	return nil, false
}

// Put sets the entry for key k to e (replacing any existing entry for the same key).
func (t *MemoTable) Put(k MemoKey, e *MemoEntry) {
	i := k.Pos - t.Base
	if i < 0 {
		return // discarded position
	}
	if i >= t.Gap {
		i += t.GapLen
	}
	if i >= len(t.Slots) {
		if i < cap(t.Slots) {
			t.Slots = t.Slots[:i+1]
		} else {
			grown := make([]*MemoEntry, i+1, max(2*cap(t.Slots), i+1, 64))
			copy(grown, t.Slots)
			t.Slots = grown
		}
	}
	e.Rule, e.Min, e.Pos, e.Env = int32(k.Rule), int32(k.Min), k.Pos, k.Env
	e.missingSilent = k.MissingSilent
	link := &t.Slots[i]
	for x := *link; x != nil; link, x = &x.Next, x.Next {
		if x.Rule == e.Rule && x.Min == e.Min && x.missingSilent == e.missingSilent && slices.Equal(x.Env, e.Env) {
			e.Next = x.Next
			*link = e
			if x != e {
				// A replaced growth seed is no longer GrowState.best: the
				// caller installs its new best before replacing the key.
				t.Retire(x)
			}
			return
		}
	}
	e.Next = t.Slots[i]
	t.Slots[i] = e
}

// Len returns the number of entries.
func (t *MemoTable) Len() int {
	n := 0
	t.Each(func(*MemoEntry) { n++ })
	return n
}

// Alloc returns a new entry.
func (t *MemoTable) Alloc() *MemoEntry {
	if e := t.Free; e != nil {
		t.Free = e.Next
		e.Next = nil
		return e
	}
	if len(t.slab) == 0 {
		if t.used < len(t.Chunks) {
			t.slab = t.Chunks[t.used]
		} else {
			t.slab = make([]MemoEntry, 256)
			t.Chunks = append(t.Chunks, t.slab)
		}
		t.used++
	}
	e := &t.slab[0]
	t.slab = t.slab[1:]
	return e
}

// Retire releases all result references and makes an unlinked entry reusable.
// It must not be called on a seed still held by an active GrowState.
func (t *MemoTable) Retire(e *MemoEntry) {
	*e = MemoEntry{Next: t.Free}
	t.Free = e
}

// Reset empties the table for another parse, keeping its memory, and reports whether that was
// worth keeping (it is not when it is very large).
func (t *MemoTable) Reset() bool {
	if cap(t.Slots) > MaxScratch || len(t.Chunks)*256 > MaxScratch || len(t.Seen) > MaxScratch || cap(t.Pages) > MaxScratch/5 || len(t.BitPages.Chunks) > MaxScratch/(64*16) || cap(t.BitPages.Chunks) > MaxScratch {
		return false
	}
	// Bound directory backing arrays and the complete stable page chunks.
	words := 5*cap(t.Pages) + len(t.BitPages.Chunks)*64*16 + cap(t.BitPages.Chunks)
	if words > MaxScratch {
		return false
	}
	for _, dir := range t.Pages {
		if cap(dir.Pages) > MaxScratch-words {
			return false
		}
		words += cap(dir.Pages)
	}
	clear(t.Slots)
	for _, c := range t.Chunks[:t.used] {
		clear(c)
	}
	clear(t.Seen)
	for i := range t.Pages {
		dir := &t.Pages[i]
		clear(dir.Pages)
		dir.Last, dir.at = nil, 0
	}
	t.BitPages.reset(0)
	*t = MemoTable{Slots: t.Slots[:0], Chunks: t.Chunks, Seen: t.Seen[:0], Pages: t.Pages, BitPages: t.BitPages}
	return true
}

// Prune discards the entries at positions before pos.
func (t *MemoTable) Prune(pos int) {
	k := pos - t.Base
	if k <= 0 {
		return
	}
	for _, head := range t.Slots[:min(k, len(t.Slots))] {
		for e := head; e != nil; {
			next := e.Next
			// The start rule may still be growing across a commit. Its
			// stack-owned best must survive even after its slot is gone.
			if !e.Growing {
				t.Retire(e)
			}
			e = next
		}
	}
	if k >= len(t.Slots) {
		clear(t.Slots)
		t.Slots = t.Slots[:0]
	} else {
		// Compact the remaining part to the front so no references to discarded entries remain.
		n := copy(t.Slots, t.Slots[k:])
		clear(t.Slots[n:])
		t.Slots = t.Slots[:n]
	}
	t.Base = pos
}

// Splice adjusts the table to a Document edit that replaced the positions [start, end) with
// delta more positions. Entries elsewhere move with their positions, and advance is applied to
// them when they are next looked up; the entries at the positions [start, end] are decided now:
// advance reports whether each is still valid (one at start kept as it was, or one at end shifted
// by delta). Only tables without pruning (base 0) are spliced.
func (t *MemoTable) Splice(start, end, delta int, advance func(e *MemoEntry) bool) {
	// Detach the entries at [start, end] and keep the valid ones.
	var stay *MemoEntry
	for pos := start; pos <= end; pos++ {
		i := pos
		if i >= t.Gap {
			i += t.GapLen
		}
		if i >= len(t.Slots) {
			break
		}
		for e := t.Slots[i]; e != nil; {
			next := e.Next
			if advance(e) {
				e.Next, stay = stay, e
			} else {
				t.Retire(e)
			}
			e = next
		}
		t.Slots[i] = nil
	}
	// Move the gap to start, take [start, end) into it, and give the new positions from it.
	if end+t.GapLen >= len(t.Slots) { // room for position end, where an entry kept at start may go
		t.Slots = append(t.Slots, make([]*MemoEntry, end+t.GapLen+1-len(t.Slots))...)
	}
	switch {
	case t.Gap > start:
		copy(t.Slots[start+t.GapLen:t.Gap+t.GapLen], t.Slots[start:t.Gap])
		clear(t.Slots[start:min(t.Gap, start+t.GapLen)])
	case t.Gap < start:
		copy(t.Slots[t.Gap:start], t.Slots[t.Gap+t.GapLen:start+t.GapLen])
		clear(t.Slots[max(start, t.Gap+t.GapLen) : start+t.GapLen])
	}
	t.Gap = start
	t.GapLen += end - start
	n := end - start + delta
	if n > t.GapLen {
		// Widen the gap, with room for later insertions.
		more := n - t.GapLen + max(1024, len(t.Slots)/16)
		grown := make([]*MemoEntry, len(t.Slots)+more)
		copy(grown, t.Slots[:t.Gap])
		copy(grown[t.Gap+t.GapLen+more:], t.Slots[t.Gap+t.GapLen:])
		t.Slots, t.GapLen = grown, t.GapLen+more
	}
	t.Gap += n
	t.GapLen -= n
	for e := stay; e != nil; {
		next := e.Next
		i := e.Pos
		if i >= t.Gap {
			i += t.GapLen
		}
		e.Next = t.Slots[i]
		t.Slots[i] = e
		e = next
	}
}

// RemoveProvisional removes from the table the entries of es that are still provisional and in
// the table: es may also hold entries that were replaced, and retired or allocated again, since.
func (t *MemoTable) RemoveProvisional(es []*MemoEntry) {
	for _, e := range es {
		if !e.Provisional {
			continue
		}
		i := e.Pos - t.Base
		if i >= t.Gap {
			i += t.GapLen
		}
		if i < 0 || i >= len(t.Slots) {
			continue
		}
		for link := &t.Slots[i]; *link != nil; link = &(*link).Next {
			if *link == e {
				*link = e.Next
				t.Retire(e)
				break
			}
		}
	}
}

// Each passes every entry to f in position order.
func (t *MemoTable) Each(f func(e *MemoEntry)) {
	for _, head := range t.Slots {
		for e := head; e != nil; {
			next := e.Next
			f(e)
			e = next
		}
	}
}
