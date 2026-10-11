package engine

import "unicode/utf8"

// Snapshot materialization follows the bounded detached-text path. Build a new
// source and offsets privately: interruption must not corrupt the previous
// cache record or any immutable string already returned from it.
func (in *input) controlledSnapshotText(start, end int) string {
	if end-start > streamTextChunk || in.loaded()-start < 64 {
		return in.detachedText(start-in.base, end-in.base)
	}
	candidate := streamTextSnapshot{base: start, end: start + min(streamTextChunk, in.loaded()-start)}
	candidate.source = in.detachedText(candidate.base-in.base, candidate.end-in.base)
	if in.unit == Bytes {
		in.streamText = candidate
		return candidate.source[:end-start]
	}
	// Reusing the previous cache's offset backing array could expose partial
	// writes after interruption; active snapshots use separate parser storage.
	candidate.offsets = make([]int32, candidate.end-candidate.base+1)
	i := 0
	for off := 0; off < len(candidate.source); {
		in.inputWork(uint64(min(utf8.UTFMax, len(candidate.source)-off)))
		candidate.offsets[i] = int32(off)
		_, size := utf8.DecodeRuneInString(candidate.source[off:])
		off += size
		i++
	}
	candidate.offsets[i] = int32(len(candidate.source))
	in.streamText = candidate
	return candidate.source[:candidate.offsets[end-start]]
}
