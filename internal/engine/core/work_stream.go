package core

import (
	"io"
)

// Only controlled streams wrap the caller's Reader. Check actual caller reads,
// including bufio refills, and remember errors returned together with data.
// Ordinary streams retain their original buffer reuse and prefetch path.
type StreamReaderObserver struct {
	Reader  io.Reader
	Result  *error
	Control *WorkControl
}

func (r *StreamReaderObserver) Read(dst []byte) (int, error) {
	r.Control.Check()
	r.Control.Charge(1) // one step per underlying reader attempt, including empty refills
	n, err := r.Reader.Read(dst)
	*r.Result = err
	r.Control.checkAfterReader(err)
	return n, err
}

// A non-EOF error from this caller read wins over cancellation first observed
// after the read. Without cancellation, bufio still delivers its buffered data
// before exposing the error, preserving the ordinary delivered prefix.
func (c *WorkControl) checkAfterReader(returned error) {
	if c == nil || c.Ctx == nil {
		return
	}
	if err := contextErr(c.Ctx); err != nil {
		if returned != nil && returned != io.EOF {
			err = returned
		}
		panic(Fatal{Err: err})
	}
	c.SinceCheck = 0
}

// CopyStreamPrefix compacts to an earlier address in the same slice. Forward
// chunks are safe for this leftward overlap; it is not a general memmove helper.
func CopyStreamPrefix[T any](c *WorkControl, dst, src []T) int {
	n := min(len(dst), len(src))
	if !WorkControlsEnabled || c == nil {
		return copy(dst, src)
	}
	for off := 0; off < n; {
		k := min(valueWorkChunk, n-off)
		c.Charge(uint64(k))
		copy(dst[off:off+k], src[off:off+k])
		off += k
	}
	return n
}

// Pruning is normal execution work. A partial prune belongs to an interrupted
// invocation and is discarded, while emitted Nodes keep independent ownership.
func (p *Parser) PruneStreamMemo(pos int) {
	t := p.Memo
	if !WorkControlsEnabled || p.Control == nil {
		t.Prune(pos)
		return
	}
	k := pos - t.Base
	if k <= 0 {
		return
	}
	for _, head := range t.Slots[:min(k, len(t.Slots))] {
		p.StepWork()
		for e := head; e != nil; {
			p.StepWork()
			next := e.Next
			if !e.Growing {
				t.Retire(e)
			}
			e = next
		}
	}
	if k >= len(t.Slots) {
		ClearWork(p, t.Slots)
		t.Slots = t.Slots[:0]
	} else {
		n := CopyStreamPrefix(p.Control, t.Slots, t.Slots[k:])
		ClearWork(p, t.Slots[n:])
		t.Slots = t.Slots[:n]
	}
	t.Base = pos
}
