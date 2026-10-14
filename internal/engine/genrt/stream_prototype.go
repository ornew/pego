package genrt

import (
	"fmt"
	"io"
	"unicode/utf8"
)

// prototypeStreamInput is a demand-read cursor, not a batch ParseStream wrapper.
// Generated matchers must suspend/read at actual demand before using it; the
// existing p.n EOF checks cannot be reused as temporary starvation checks.
type prototypeStreamInput struct {
	reader   io.Reader
	control  *workControl
	buffer   []byte
	at       int
	position int
	unit     Unit
	pending  error
	previous rune
	hasPrev  bool
}

func newPrototypeStreamInput(reader io.Reader, opts ...ParseOption) (input *prototypeStreamInput, err error) {
	defer recoverParse(&input, &err)
	o, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	if o.unit != Bytes && o.unit != CodePoints {
		return nil, fmt.Errorf("invalid stream position unit: %d", o.unit)
	}
	if reader == nil {
		return nil, fmt.Errorf("stream reader is required")
	}
	o = prepareOptions(o)
	return &prototypeStreamInput{reader: reader, unit: o.unit, control: o.control}, nil
}

func (s *prototypeStreamInput) next() (rune, error) {
	empty := 0
	for {
		s.control.check()
		bytes := s.buffer[s.at:]
		if len(bytes) != 0 && (utf8.FullRune(bytes) || s.pending != nil) {
			r, size := utf8.DecodeRune(bytes)
			s.control.charge(uint64(size), 1)
			s.at += size
			if s.unit == Bytes {
				s.position += size
			} else {
				s.position++
			}
			s.previous, s.hasPrev = r, true
			return r, nil
		}
		if s.pending != nil {
			return 0, s.pending
		}
		// A small fixed request is sufficient for the cursor prototype. No read
		// occurs while a complete character remains in the retained buffer.
		var chunk [4096]byte
		n, err := s.reader.Read(chunk[:])
		if n < 0 || n > len(chunk) {
			return 0, fmt.Errorf("invalid stream reader count: %d", n)
		}
		if n != 0 {
			s.buffer = append(s.buffer, chunk[:n]...)
			empty = 0
		} else if err == nil {
			empty++
			if empty == 100 {
				return 0, io.ErrNoProgress
			}
		}
		// Preserve n>0,error: expose bytes first, then the original error.
		s.pending = err
	}
}

// discard is legal only after the grammar's top-level repetition commits.
// Previous-character state and absolute position survive compaction. Suffix
// actions must own captured prefix text before this call.
func (s *prototypeStreamInput) discard() {
	remaining := len(s.buffer) - s.at
	copy(s.buffer, s.buffer[s.at:])
	clear(s.buffer[remaining:])
	s.buffer = s.buffer[:remaining]
	s.at = 0
}

// prototypeEmitter makes the per-element ownership/conversion boundary
// executable independently of continuation lowering. The converter must own
// any returned array/object storage and never return a pooled converter arena.
// A nil callback collects values; a callback error terminally stops publication.
type prototypeEmitter[T any] struct {
	convert func(*Node) (T, error)
	emit    func(T) error
	values  []T
	stopped bool
	err     error
}

func (e *prototypeEmitter[T]) commit(node *Node) (err error) {
	if e.stopped {
		if e.err != nil {
			return e.err
		}
		return fmt.Errorf("stream emitter is stopped")
	}
	// Keep terminal state even when caller conversion/publication panics; the
	// original panic is propagated without classification or later callbacks.
	defer func() {
		if !e.stopped && err != nil {
			e.stopped, e.err = true, err
		}
		if x := recover(); x != nil {
			e.stopped = true
			panic(x)
		}
	}()
	detached := prototypeCloneNode(node, map[*Node]*Node{})
	value, err := e.convert(detached)
	if err != nil {
		return err
	}
	if e.emit == nil {
		e.values = append(e.values, value)
		return nil
	}
	return e.emit(value)
}
