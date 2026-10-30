package core

const StreamTextChunk = 1024

// StreamTextSnapshot owns a small immutable copy of already loaded input.
// Returned substrings retain at most one chunk; offsets remain parser-owned.
type StreamTextSnapshot struct {
	Base, End int
	Source    string
	Offsets   []int32
}

func (in *Input) snapshotText(start, end int) string {
	if start == end {
		return ""
	}
	c := &in.StreamText
	if start >= c.Base && end <= c.End && c.Source != "" {
		if in.Unit == Bytes {
			return c.Source[start-c.Base : end-c.Base]
		}
		return c.Source[c.Offsets[start-c.Base]:c.Offsets[end-c.Base]]
	}
	if BudgetControlsEnabled && in.Control != nil {
		return in.controlledSnapshotText(start, end)
	}
	// Do not read ahead for text or repeatedly copy tiny reader fragments.
	// Oversized ranges use the ordinary detached-string path.
	if end-start > StreamTextChunk || in.Loaded()-start < 64 {
		if in.Unit == Bytes {
			return string(in.Bs[start-in.Base : end-in.Base])
		}
		return string(in.In[start-in.Base : end-in.Base])
	}
	c.Base, c.End = start, start+min(StreamTextChunk, in.Loaded()-start)
	if in.Unit == Bytes {
		c.Source = string(in.Bs[c.Base-in.Base : c.End-in.Base])
		return c.Source[:end-start]
	}
	c.Source = string(in.In[c.Base-in.Base : c.End-in.Base])
	c.Offsets = grow(c.Offsets, c.End-c.Base+1)
	i := 0
	for off := range c.Source {
		c.Offsets[i] = int32(off)
		i++
	}
	c.Offsets[i] = int32(len(c.Source))
	return c.Source[:c.Offsets[end-start]]
}
