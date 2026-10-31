package zp

import (
	"io"

	"github.com/ornew/zeroparse/internal/engine"
)

// StreamIterator delivers one committed stream element per Next call.
// It is single-owner; Next and Close must not overlap. The reader remains
// caller-owned, and Close cannot interrupt a blocked Read.
type StreamIterator = engine.StreamIterator

// ErrStreamClosed identifies an iterator closed before its terminal result.
var ErrStreamClosed = engine.ErrStreamClosed

// Iterate validates a #stream parser and returns a lazy pull iterator. Reader
// access begins on Next; lookahead and buffering may still read ahead. With a
// diagnostic callback, Next reports committed recoveries before their element.
func (p *Parser) Iterate(r io.Reader, opts ...ParseOption) (*StreamIterator, error) {
	return p.prog.Iterate(p.start, r, parseOptions(opts))
}
