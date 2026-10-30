package core

import "unicode/utf8"

// Snapshot materialization follows the bounded detached-text path. Build a new
// source and offsets privately: interruption must not corrupt the previous
// cache record or any immutable string already returned from it.
func (in *Input) controlledSnapshotText(start, end int) string {
	if end-start > StreamTextChunk || in.Loaded()-start < 64 {
		return in.detachedText(start-in.Base, end-in.Base)
	}
	candidate := StreamTextSnapshot{Base: start, End: start + min(StreamTextChunk, in.Loaded()-start)}
	candidate.Source = in.detachedText(candidate.Base-in.Base, candidate.End-in.Base)
	if in.Unit == Bytes {
		in.StreamText = candidate
		return candidate.Source[:end-start]
	}
	// Reusing the previous cache's offset backing array could expose partial
	// writes after interruption; active snapshots use separate parser storage.
	candidate.Offsets = make([]int32, candidate.End-candidate.Base+1)
	i := 0
	for off := 0; off < len(candidate.Source); {
		candidate.Offsets[i] = int32(off)
		_, size := utf8.DecodeRuneInString(candidate.Source[off:])
		in.spendRead(uint64(size))
		off += size
		i++
	}
	candidate.Offsets[i] = int32(len(candidate.Source))
	in.StreamText = candidate
	return candidate.Source[:candidate.Offsets[end-start]]
}
