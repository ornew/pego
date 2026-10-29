package core

import (
	"slices"
	"sort"
)

// AdvanceEntry counts edit replay only during parsing. Document.Edit continues
// to use the uninstrumented helper, independently of invocation controls.
func (p *Parser) AdvanceEntry(e *MemoEntry, edits []DocEdit) bool {
	if !WorkControlsEnabled || p.Control == nil {
		return AdvanceEntry(e, edits)
	}
	if e.Growing || e.Provisional && e.Vgen != uint32(len(edits)) {
		return false
	}
	from, examined, shift, shifted := e.From, e.Examined, 0, e.shifted
	for _, ed := range edits[e.Vgen:] {
		p.StepWork()
		switch {
		case examined <= ed.Start:
		case from >= ed.End && !e.Positional && len(e.Errs) == 0:
			from += ed.Delta
			examined += ed.Delta
			shift += ed.Delta
			shifted = shifted || ed.Delta != 0
		default:
			return false
		}
	}
	e.Pos += shift
	e.End += shift
	e.From, e.Examined = from, examined
	e.Far += shift
	e.shift += int32(shift)
	e.shifted = shifted
	e.Vgen = uint32(len(edits))
	return true
}

func (p *Parser) RunSearch(elems []RunElem, start, at int) int {
	if !WorkControlsEnabled || p.Control == nil {
		return start + sort.Search(len(elems)-start, func(i int) bool { return elems[start+i].Start >= at })
	}
	lo, hi := start, len(elems)
	for lo < hi {
		p.StepWork()
		mid := int(uint(lo+hi) >> 1)
		if elems[mid].Start < at {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (p *Parser) RunAfter(elems []RunElem, end int) bool {
	if !WorkControlsEnabled || p.Control == nil {
		return after(elems, end)
	}
	for _, el := range elems {
		p.StepWork()
		if el.From < end {
			return false
		}
	}
	return true
}

// GrowWork bounds the explicit copy of existing values when capacity must
// grow. Allocation and runtime zeroing remain nonpreemptible intervals.
func GrowWork[T any](p *Parser, values []T, extra int) []T {
	if !WorkControlsEnabled || p.Control == nil {
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
	CopyWork(p, out, values)
	return out
}

// Successful movement cleanup is parse work; fatal Document cleanup remains
// unbudgeted. Retain the ordinary map-clear path when controls are absent.
func (p *Parser) ClearMovementWork() {
	if !WorkControlsEnabled || p.Control == nil {
		if len(p.Moved) > 1024 {
			p.Moved = nil
		} else {
			clear(p.Moved)
		}
		clear(p.Shifts)
		return
	}
	if len(p.Moved) > 1024 {
		p.Moved = nil
	} else {
		for key := range p.Moved {
			p.StepWork()
			delete(p.Moved, key)
		}
	}
	for key := range p.Shifts {
		p.StepWork()
		delete(p.Shifts, key)
	}
}

// AdvanceEntry applies to the memo entry e the edits it does not account for yet (those after the
// first e.Vgen of edits), and reports whether it is still valid. For each edit [start, end), in
// order, an entry that examined only input before it is kept as is, one that examined only input
// after it is shifted (unless its result depends on positions or contains recovered errors, whose
// messages contain positions), and any other is invalid.
// The entry is changed only if it is still valid: one that is not may be looked up again (when a
// parse is aborted before the entry is replaced).
func AdvanceEntry(e *MemoEntry, edits []DocEdit) bool {
	if e.Growing || e.Provisional && e.Vgen != uint32(len(edits)) {
		return false
	}
	from, examined, shift, shifted := e.From, e.Examined, 0, e.shifted
	for _, ed := range edits[e.Vgen:] {
		switch {
		case examined <= ed.Start:
		case from >= ed.End && !e.Positional && len(e.Errs) == 0:
			from += ed.Delta
			examined += ed.Delta
			shift += ed.Delta
			shifted = shifted || ed.Delta != 0 // an edit that keeps the length moves nothing
		default:
			return false
		}
	}
	e.Pos += shift
	e.End += shift
	e.From, e.Examined = from, examined
	e.Far += shift
	e.shift += int32(shift)
	e.shifted = shifted
	e.Vgen = uint32(len(edits))
	return true
}
