package engine

const streamTextChunk = 1024

// streamTextSnapshot owns a small immutable copy of already loaded input.
// Returned substrings retain at most one chunk; offsets remain parser-owned.
type streamTextSnapshot struct {
	base, end int
	source    string
	offsets   []int32
}

func (in *input) snapshotText(start, end int) string {
	if start == end {
		return ""
	}
	c := &in.streamText
	if start >= c.base && end <= c.end && c.source != "" {
		if in.unit == Bytes {
			return c.source[start-c.base : end-c.base]
		}
		return c.source[c.offsets[start-c.base]:c.offsets[end-c.base]]
	}
	// Do not read ahead for text or repeatedly copy tiny reader fragments.
	// Oversized ranges use the ordinary detached-string path.
	if end-start > streamTextChunk || in.loaded()-start < 64 {
		if in.unit == Bytes {
			return string(in.bs[start-in.base : end-in.base])
		}
		return string(in.in[start-in.base : end-in.base])
	}
	c.base, c.end = start, start+min(streamTextChunk, in.loaded()-start)
	if in.unit == Bytes {
		c.source = string(in.bs[c.base-in.base : c.end-in.base])
		return c.source[:end-start]
	}
	c.source = string(in.in[c.base-in.base : c.end-in.base])
	c.offsets = grow(c.offsets, c.end-c.base+1)
	i := 0
	for off := range c.source {
		c.offsets[i] = int32(off)
		i++
	}
	c.offsets[i] = int32(len(c.source))
	return c.source[:c.offsets[end-start]]
}
