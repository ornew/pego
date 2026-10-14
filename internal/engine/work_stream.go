package engine

import (
	"io"
)

// Only controlled streams wrap the caller's Reader. Check actual caller reads,
// including bufio refills, and remember errors returned together with data.
// Ordinary streams retain their original buffer reuse and prefetch path.
type streamReaderObserver struct {
	reader  io.Reader
	result  *error
	control *workControl
}

func (r *streamReaderObserver) Read(dst []byte) (int, error) {
	r.control.check()
	r.control.charge(0, 1) // one step per underlying reader attempt, including empty refills
	n, err := r.reader.Read(dst)
	*r.result = err
	r.control.checkAfterReader(err)
	return n, err
}

// A non-EOF error from this caller read wins over cancellation first observed
// after the read. Without cancellation, bufio still delivers its buffered data
// before exposing the error, preserving the ordinary delivered prefix.
func (c *workControl) checkAfterReader(returned error) {
	if c == nil || c.ctx == nil {
		return
	}
	if err := contextErr(c.ctx); err != nil {
		if returned != nil && returned != io.EOF {
			err = returned
		}
		panic(fatal{err: err})
	}
	c.inputSinceCheck = 0
	c.stepsSinceCheck = 0
}

// copyStreamPrefix compacts to an earlier address in the same slice. Forward
// chunks are safe for this leftward overlap; it is not a general memmove helper.
func copyStreamPrefix[T any](c *workControl, dst, src []T) int {
	n := min(len(dst), len(src))
	if !workControlsEnabled || c == nil {
		return copy(dst, src)
	}
	for off := 0; off < n; {
		k := min(valueWorkChunk, n-off)
		c.charge(0, uint64(k))
		copy(dst[off:off+k], src[off:off+k])
		off += k
	}
	return n
}

// Pruning is normal execution work. A partial prune belongs to an interrupted
// invocation and is discarded, while emitted Nodes keep independent ownership.
func (p *parser) pruneStreamMemo(pos int) {
	t := p.memo
	if !workControlsEnabled || p.control == nil {
		t.prune(pos)
		return
	}
	k := pos - t.base
	if k <= 0 {
		return
	}
	for _, head := range t.slots[:min(k, len(t.slots))] {
		p.stepWork()
		for e := head; e != nil; {
			p.stepWork()
			next := e.next
			if !e.growing {
				t.retire(e)
			}
			e = next
		}
	}
	if k >= len(t.slots) {
		clearWork(p, t.slots)
		t.slots = t.slots[:0]
	} else {
		n := copyStreamPrefix(p.control, t.slots, t.slots[k:])
		clearWork(p, t.slots[n:])
		t.slots = t.slots[:n]
	}
	t.base = pos
}
