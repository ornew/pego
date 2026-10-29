package engine

import (
	"slices"
	"sort"
)

// advanceEntry counts edit replay only during parsing. Document.Edit continues
// to use the uninstrumented helper, independently of invocation controls.
func (p *parser) advanceEntry(e *memoEntry, edits []docEdit) bool {
	if !workControlsEnabled || p.control == nil {
		return advanceEntry(e, edits)
	}
	if e.growing || e.provisional && e.vgen != uint32(len(edits)) {
		return false
	}
	from, examined, shift, shifted := e.from, e.examined, 0, e.shifted
	for _, ed := range edits[e.vgen:] {
		p.stepWork()
		switch {
		case examined <= ed.start:
		case from >= ed.end && !e.positional && len(e.errs) == 0:
			from += ed.delta
			examined += ed.delta
			shift += ed.delta
			shifted = shifted || ed.delta != 0
		default:
			return false
		}
	}
	e.pos += shift
	e.end += shift
	e.from, e.examined = from, examined
	e.far += shift
	e.shift += int32(shift)
	e.shifted = shifted
	e.vgen = uint32(len(edits))
	return true
}

func (p *parser) runSearch(elems []runElem, start, at int) int {
	if !workControlsEnabled || p.control == nil {
		return start + sort.Search(len(elems)-start, func(i int) bool { return elems[start+i].start >= at })
	}
	lo, hi := start, len(elems)
	for lo < hi {
		p.stepWork()
		mid := int(uint(lo+hi) >> 1)
		if elems[mid].start < at {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (p *parser) runAfter(elems []runElem, end int) bool {
	if !workControlsEnabled || p.control == nil {
		return after(elems, end)
	}
	for _, el := range elems {
		p.stepWork()
		if el.from < end {
			return false
		}
	}
	return true
}

// growWork bounds the explicit copy of existing values when capacity must
// grow. Allocation and runtime zeroing remain nonpreemptible intervals.
func growWork[T any](p *parser, values []T, extra int) []T {
	if !workControlsEnabled || p.control == nil {
		return slices.Grow(values, extra)
	}
	if extra < 0 {
		return slices.Grow(values, extra) // reject negative growth before spare-capacity checks
	}
	if extra <= cap(values)-len(values) {
		return values
	}
	maxInt := int(^uint(0) >> 1)
	if extra > maxInt-len(values) {
		return slices.Grow(values, extra) // preserve the library's invalid-size panic
	}
	capacity := len(values) + extra
	if cap(values) <= maxInt/2 {
		// Keep repeated single-element additions amortized; allocating only
		// the exact new length would repeatedly copy the accumulated prefix.
		capacity = max(capacity, 2*cap(values))
	}
	out := make([]T, len(values), capacity)
	copyWork(p, out, values)
	return out
}

// Successful movement cleanup is parse work; fatal Document cleanup remains
// unbudgeted. Retain the ordinary map-clear path when controls are absent.
func (p *parser) clearMovementWork() {
	if !workControlsEnabled || p.control == nil {
		if len(p.moved) > 1024 {
			p.moved = nil
		} else {
			clear(p.moved)
		}
		clear(p.shifts)
		return
	}
	if len(p.moved) > 1024 {
		p.moved = nil
	} else {
		for key := range p.moved {
			p.stepWork()
			delete(p.moved, key)
		}
	}
	for key := range p.shifts {
		p.stepWork()
		delete(p.shifts, key)
	}
}

// advanceEntry applies to the memo entry e the edits it does not account for yet (those after the
// first e.vgen of edits), and reports whether it is still valid. For each edit [start, end), in
// order, an entry that examined only input before it is kept as is, one that examined only input
// after it is shifted (unless its result depends on positions or contains recovered errors, whose
// messages contain positions), and any other is invalid.
// The entry is changed only if it is still valid: one that is not may be looked up again (when a
// parse is aborted before the entry is replaced).
func advanceEntry(e *memoEntry, edits []docEdit) bool {
	if e.growing || e.provisional && e.vgen != uint32(len(edits)) {
		return false
	}
	from, examined, shift, shifted := e.from, e.examined, 0, e.shifted
	for _, ed := range edits[e.vgen:] {
		switch {
		case examined <= ed.start:
		case from >= ed.end && !e.positional && len(e.errs) == 0:
			from += ed.delta
			examined += ed.delta
			shift += ed.delta
			shifted = shifted || ed.delta != 0 // an edit that keeps the length moves nothing
		default:
			return false
		}
	}
	e.pos += shift
	e.end += shift
	e.from, e.examined = from, examined
	e.far += shift
	e.shift += int32(shift)
	e.shifted = shifted
	e.vgen = uint32(len(edits))
	return true
}
