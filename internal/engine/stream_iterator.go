package engine

import (
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/ornew/pego/internal/value"
)

// ErrStreamClosed is returned after an iterator is abandoned before termination.
var ErrStreamClosed = errors.New("stream iterator is closed")
var errIteratorStop = errors.New("stream iterator stopped")

// StreamIterator advances synchronously to one committed element at a time.
// It is single-owner: Next and Close must not overlap, including callbacks.
// Close releases parser state but never closes or interrupts its reader.
// The standard library pull coroutine has no independently scheduled producer.
type StreamIterator struct {
	next     func() (*value.Node, bool)
	stop     func()
	terminal bool
	err      error
}

// Iterate validates the entry point before returning a lazy iterator. Reader
// access starts with Next. Parser lookahead and buffering may read ahead.
func (prog *Program) Iterate(start string, r io.Reader, o ParseOptions) (it *StreamIterator, err error) {
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok {
				it, err = nil, f.err
			} else if cp, ok := x.(contextPanic); ok {
				panic(cp.value)
			} else {
				panic(x)
			}
		}
	}()
	if err := o.validateStreamDiagnostics(true); err != nil {
		return nil, err
	}
	if o.Recognize {
		return nil, fmt.Errorf("recognition is not supported for stream parsing")
	}
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, false); err != nil {
		return nil, err
	}
	if _, _, _, err = prog.admitParse(start, o, true); err != nil {
		return nil, err
	}
	it = &StreamIterator{}
	it.next, it.stop = iter.Pull(func(yield func(*value.Node) bool) {
		it.err = prog.ParseStreamWith(start, r, func(n *value.Node) error {
			if !yield(n) {
				return errIteratorStop
			}
			return nil
		}, o)
	})
	return it, nil
}

// Next returns (node,true,nil), even for a successful discarded nil value.
// Clean EOF is (nil,false,nil); terminal errors remain stable on later calls.
func (it *StreamIterator) Next() (n *value.Node, ok bool, err error) {
	if it.terminal {
		return nil, false, it.err
	}
	defer func() {
		if x := recover(); x != nil {
			// Pull propagates the original caller panic after the parser unwinds.
			it.terminal = true
			it.next, it.stop = nil, nil
			panic(x)
		}
	}()
	n, ok = it.next()
	if ok {
		return n, true, nil
	}
	it.terminal = true
	it.next, it.stop = nil, nil
	return nil, false, it.err
}

// Close abandons unfinished parsing and is idempotent. It cannot interrupt a
// Read already blocked in caller code, and must not overlap a call to Next.
func (it *StreamIterator) Close() {
	if it.terminal {
		return
	}
	it.terminal = true
	stop := it.stop
	it.next, it.stop = nil, nil
	defer func() { it.err = ErrStreamClosed }()
	stop()
}
