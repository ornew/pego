package zp

import "github.com/ornew/zeroparse/internal/engine"

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
