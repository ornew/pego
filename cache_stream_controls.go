package pego

import (
	"io"

	"github.com/ornew/pego/internal/engine"
)

// CacheStats describes the incremental reuse history retained by a Document.
// EstimatedBytes is an approximation of cache-owned backing storage, excluding
// text, caller-held trees, shared strings and allocator overhead.
type CacheStats = engine.CacheStats

// ResetHistory discards incremental reuse history while preserving text,
// construction options, version and the statistics of the last parse. Earlier
// result trees are detached from later node movement.
func (d *Document) ResetHistory() { d.doc.ResetHistory() }

// CacheStats inspects retained incremental history without changing it.
// Document methods, including this one, require exclusive ownership.
func (d *Document) CacheStats() CacheStats { return d.doc.CacheStats() }

// StreamIterator delivers one committed stream element per Next call.
// It is single-owner; Next and Close must not overlap. The reader remains
// caller-owned, and Close cannot interrupt a blocked Read.
type StreamIterator = engine.StreamIterator

// ErrStreamClosed identifies an iterator closed before its terminal result.
var ErrStreamClosed = engine.ErrStreamClosed

// Iterate validates a #stream parser and returns a lazy pull iterator. Reader
// access begins on Next; lookahead and buffering may still read ahead.
func (p *Parser) Iterate(r io.Reader, opts ...ParseOption) (*StreamIterator, error) {
	return p.prog.Iterate(p.start, r, parseOptions(opts))
}
