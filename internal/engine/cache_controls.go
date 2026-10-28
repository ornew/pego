package engine

import (
	"unsafe"
	"weak"

	"github.com/ornew/pego/internal/value"
)

// CacheStats describes retained incremental history. EstimatedBytes includes
// backing buffers and run records, but excludes map buckets, allocator metadata,
// document text, result trees, shared expectation/error/environment payloads
// and strings shared with caller-held results.
// Inspection is read-only and requires exclusive Document ownership.
type CacheStats struct {
	LiveMemoEntries    int
	RetiredMemoEntries int
	RepetitionElements int
	EstimatedBytes     uint64
}

// ResetHistory releases incremental reuse history without changing the text,
// construction options or last parse statistics. Previously returned trees are
// detached from future node movement; this does not undo earlier movement.
func (d *Document) ResetHistory() {
	d.memo = newMemoTable()
	d.edits, d.runs, d.kids, d.vals = nil, nil, nil, nil
	d.runGen, d.resumed = 0, 0
	d.in.offs, d.in.lines = nil, nil
	d.previousSnapshot = weak.Pointer[snapshotGraph]{}
}

// CacheStats returns a point-in-time estimate of cache-owned retained buffers.
// It does not measure process heap usage or bytes returned to the OS.
func (d *Document) CacheStats() CacheStats {
	var s CacheStats
	t := d.memo
	if t != nil {
		for _, head := range t.slots {
			for e := head; e != nil; e = e.next {
				s.LiveMemoEntries++
			}
		}
		for e := t.free; e != nil; e = e.next {
			s.RetiredMemoEntries++
		}
		s.EstimatedBytes += uint64(cap(t.slots)) * uint64(unsafe.Sizeof((*memoEntry)(nil)))
		s.EstimatedBytes += uint64(cap(t.chunks)) * uint64(unsafe.Sizeof([]memoEntry(nil)))
		for _, chunk := range t.chunks {
			s.EstimatedBytes += uint64(cap(chunk)) * uint64(unsafe.Sizeof(memoEntry{}))
		}
		s.EstimatedBytes += uint64(cap(t.seen)) * uint64(unsafe.Sizeof(uint64(0)))
		s.EstimatedBytes += uint64(cap(t.calls)) * uint64(unsafe.Sizeof(seenCalls{}))
		s.EstimatedBytes += uint64(cap(t.pages)) * uint64(unsafe.Sizeof(seenRulePages{}))
		for _, dir := range t.pages {
			s.EstimatedBytes += uint64(cap(dir.pages)) * uint64(unsafe.Sizeof((*[16]uint64)(nil)))
		}
		s.EstimatedBytes += uint64(cap(t.bitPages.chunks)) * uint64(unsafe.Sizeof((*[64][16]uint64)(nil)))
		s.EstimatedBytes += uint64(len(t.bitPages.chunks)) * uint64(unsafe.Sizeof([64][16]uint64{}))
	}
	for _, run := range d.runs {
		s.RepetitionElements += len(run.elems)
		s.EstimatedBytes += uint64(unsafe.Sizeof(runRecord{}))
		s.EstimatedBytes += uint64(cap(run.elems)) * uint64(unsafe.Sizeof(runElem{}))
	}
	s.EstimatedBytes += uint64(cap(d.edits)) * uint64(unsafe.Sizeof(docEdit{}))
	s.EstimatedBytes += uint64(cap(d.kids)) * uint64(unsafe.Sizeof((*value.Node)(nil)))
	s.EstimatedBytes += uint64(cap(d.vals)) * uint64(unsafe.Sizeof(any(nil)))
	return s
}
