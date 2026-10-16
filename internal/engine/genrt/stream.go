package genrt

import (
	"bufio"
	"io"
	"unicode/utf8"
)

// streamInput is the input of a stream parse. Positions are absolute; the
// buffer holds the input from base on, read from reader as matching needs it.
// The generated input fields (in, bs and n) stay empty, so inlined fast paths
// fall back to the stream-aware peek and literal matching.
type streamInput struct {
	reader *bufio.Reader
	in     []rune // CodePoints
	bs     []byte // Bytes
	base   int
	// baseLine and baseCol are the line and column (1-based, in the unit) of base.
	baseLine, baseCol int
	eof               bool
	// emit receives the committed elements of the start rule's #stream
	// repetition. Nil collects the repetition as a batch parse does.
	emit func(*Node) error
	// memoBase is the position of memo.slots[0]; pruned is the position up to
	// which memo entries were last discarded.
	memoBase, pruned int
	// scratch is the capture scratch in use when the #stream repetition
	// started. Frames allocated after it belong to committed elements and
	// are reused.
	scratch frameScratchMark
	// nodeChunks counts the node chunks started since the last split.
	nodeChunks int
	text       streamTextSnapshot
	// lines holds the line starts once the whole input is loaded and
	// nothing was discarded.
	lines []int
}

const streamTextChunk = 1024

// streamTextSnapshot owns a small immutable copy of already loaded input, so
// that the texts of nearby nodes share one allocation. Returned substrings
// retain at most one chunk; offsets (CodePoints) remain parser-owned.
type streamTextSnapshot struct {
	base, end int
	source    string
	offsets   []int32
}

// frameScratchMark is the allocation state of a frameScratch.
type frameScratchMark struct{ frameN, slotCI, slotOff int }

// rewind clears and reuses the frames and slots allocated after m.
func (a *frameScratch) rewind(m frameScratchMark) {
	for i := m.frameN; i < a.frameN; i++ {
		a.frames[i/nodeChunk][i%nodeChunk] = frame{}
	}
	for ci := m.slotCI; ci <= a.slotCI && ci < len(a.slots); ci++ {
		lo, hi := 0, ptrChunk
		if ci == m.slotCI {
			lo = m.slotOff
		}
		if ci == a.slotCI {
			hi = a.slotOff
		}
		if lo < hi {
			clear(a.slots[ci][lo:hi])
		}
	}
	a.frameN, a.slotCI, a.slotOff = m.frameN, m.slotCI, m.slotOff
}

// streamReader charges one step per caller Read and reports a non-EOF error
// from that Read ahead of cancellation observed after it.
type streamReader struct {
	reader  io.Reader
	control *workControl
}

func (r *streamReader) Read(dst []byte) (int, error) {
	r.control.check()
	r.control.charge(0, 1)
	n, err := r.reader.Read(dst)
	if r.control.ctx != nil {
		if cerr := contextErr(r.control.ctx); cerr != nil {
			if err != nil && err != io.EOF {
				cerr = err
			}
			r.control.interrupted = true
			panic(fatal{err: cerr})
		}
	}
	return n, err
}

// parseStream parses the input read from reader with rule r, passing each
// committed element of the rule's #stream repetition to emit.
func parseStream(r *rule, seen int, reader io.Reader, emit func(*Node) error, opts []ParseOption) (err error) {
	o, err := resolveOptions(opts)
	if err != nil {
		return err
	}
	var n *Node
	defer recoverParse(&n, &err)
	o = prepareOptions(o)
	if workControlsEnabled && o.control != nil {
		reader = &streamReader{reader: reader, control: o.control}
	}
	p := &parser{unit: o.unit, maxDepth: o.maxDepth}
	p.control = o.control
	p.stream = &streamInput{reader: bufio.NewReader(reader), baseLine: 1, baseCol: 1, emit: emit}
	p.memo.stride = seen
	p.control.check()
	_, ok := p.call(r, 0)
	p.control.check()
	if ok && p.streamAtEnd() {
		if len(p.recovered) > 0 {
			return SyntaxErrors(p.recovered)
		}
		return nil
	}
	if ok {
		p.expect(p.pos, idEndInput)
	}
	return p.makeError(p.farthest, p.exp[p.expBase:])
}

// streamStart is called when the start rule's #stream repetition starts.
// The capture frames allocated so far, including the start rule's, stay in
// use; elements allocate their nodes from chunks of their own.
func (p *parser) streamStart() {
	if nodeFrameScratch {
		a := &p.captureScratch
		p.stream.scratch = frameScratchMark{a.frameN, a.slotCI, a.slotOff}
	} else {
		p.frameSlab = nil
	}
	p.nodeSlab, p.ptrSlab, p.fieldSlab = nil, nil, nil
	p.stream.nodeChunks = 0
}

// emitStream passes a committed element of the start rule's #stream
// repetition to the caller and commits the input before end. Elements are
// final once emitted: a caller error ends the parse unchanged.
func (p *parser) emitStream(v *Node, end int) {
	p.control.check()
	if v != nil {
		v.fresh = false
	}
	if err := p.stream.emit(v); err != nil {
		panic(fatal{err})
	}
	p.control.check()
	p.streamCommit(end)
}

// streamCommit makes it final that the parse never backtracks before pos. The
// preceding unit is kept for detecting line starts.
func (p *parser) streamCommit(pos int) {
	s := p.stream
	p.streamDiscard(pos - 1)
	p.trail = p.trail[:0]
	// Capture frames are referenced only by active calls, and the elements'
	// calls have returned, so the frames allocated since the repetition
	// started can be reused.
	if nodeFrameScratch {
		p.captureScratch.rewind(s.scratch)
	}
	// A chunk stays alive while anything in it is referenced and keeps alive
	// what its objects point to, so chunks shared by consecutive elements
	// would chain every emitted element to the parser. New chunks start once
	// the node chunk is nearly used up or more than one chunk was started.
	if len(p.nodeSlab) < nodeChunk/8 || s.nodeChunks >= 2 {
		p.nodeSlab, p.ptrSlab, p.fieldSlab = nil, nil, nil
		if !nodeFrameScratch {
			p.frameSlab = nil
		}
		s.nodeChunks = 0
	}
	if pos-s.pruned >= 1024 {
		// Completed calls may have shortened the trail before the commit;
		// its inactive capacity can still refer to committed elements.
		clear(p.trail[:cap(p.trail)])
		p.pruneStreamMemo(pos)
		s.pruned = pos
	}
}

// streamDiscard discards the input before keep. The buffer holds read-ahead
// input, so it is compacted only once at least half of it can go.
func (p *parser) streamDiscard(keep int) {
	s := p.stream
	n := keep - s.base
	if n <= 0 || 2*n < len(s.in)+len(s.bs) {
		return
	}
	s.baseLine, s.baseCol = p.streamLineCol(keep)
	if p.unit == Bytes {
		p.inputWork(uint64(len(s.bs) - n))
		s.bs = s.bs[:copy(s.bs, s.bs[n:])]
	} else {
		p.inputWork(uint64(len(s.in) - n))
		s.in = s.in[:copy(s.in, s.in[n:])]
	}
	s.base = keep
}

// pruneStreamMemo discards the memo entries before pos, which no later call
// can look up. Growing left-recursive entries are owned by active calls.
func (p *parser) pruneStreamMemo(pos int) {
	s, t := p.stream, &p.memo
	k := pos - s.memoBase
	if k <= 0 {
		return
	}
	for _, head := range t.slots[:min(k, len(t.slots))] {
		for e := head; e != nil; {
			p.stepWork()
			next := e.next
			if !e.growing {
				*e = memoEntry{next: t.free}
				t.free = e
			}
			e = next
		}
	}
	if k >= len(t.slots) {
		clear(t.slots)
		t.slots = t.slots[:0]
	} else {
		n := copy(t.slots, t.slots[k:])
		clear(t.slots[n:])
		t.slots = t.slots[:n]
	}
	s.memoBase = pos
}

// streamLoaded returns the position of the end of the input read so far.
func (s *streamInput) loaded() int {
	return s.base + len(s.in) + len(s.bs)
}

// streamFill reads the input up to just before position n and reports whether
// it is available. Once the required amount has been read, it reads only what
// is already buffered and does not wait for more.
func (p *parser) streamFill(n int) bool {
	s := p.stream
	for s.loaded() < n && !s.eof {
		p.streamRead()
		for s.reader.Buffered() > 0 {
			if p.unit == CodePoints {
				if b, _ := s.reader.Peek(s.reader.Buffered()); !utf8.FullRune(b) {
					break
				}
			}
			p.streamRead()
		}
	}
	return s.loaded() >= n
}

func (p *parser) streamRead() {
	p.control.check()
	p.stepWork()
	s := p.stream
	var err error
	if p.unit == Bytes {
		var b byte
		if b, err = s.reader.ReadByte(); err == nil {
			s.bs = append(s.bs, b)
		}
	} else {
		var r rune
		if r, _, err = s.reader.ReadRune(); err == nil {
			s.in = append(s.in, r)
		}
	}
	if err == io.EOF {
		s.eof = true
	} else if err != nil {
		panic(fatal{err})
	}
}

// streamPeek is peek on a stream. A complete character never waits for more
// input; an incomplete UTF-8 prefix waits for more bytes or the end of input.
func (p *parser) streamPeek() (rune, int, bool) {
	p.inputWork(1)
	s := p.stream
	if p.pos < s.base || !p.streamFill(p.pos+1) {
		return 0, 0, false
	}
	if p.unit == CodePoints {
		return s.in[p.pos-s.base], 1, true
	}
	b := s.bs[p.pos-s.base:]
	if b[0] < utf8.RuneSelf {
		return rune(b[0]), 1, true
	}
	for !s.eof && !utf8.FullRune(b) {
		p.streamFill(s.loaded() + 1)
		b = s.bs[p.pos-s.base:]
	}
	p.inputWork(uint64(min(utf8.UTFMax, len(b))))
	r, size := utf8.DecodeRune(b)
	return r, size, true
}

// streamByte returns the byte at position i (Bytes).
func (p *parser) streamByte(i int) (byte, bool) {
	p.inputWork(1)
	s := p.stream
	if i < s.base || !p.streamFill(i+1) {
		return 0, false
	}
	return s.bs[i-s.base], true
}

// streamMatchLiteral is matchLiteral on a stream. It reads only as far as the
// literal matches.
func (p *parser) streamMatchLiteral(rs []rune, text string, desc expID, build bool) (*Node, bool) {
	start := p.pos
	s := p.stream
	if p.unit == Bytes {
		for i := 0; i < len(text); i++ {
			if c, ok := p.streamByte(p.pos); !ok || c != text[i] {
				p.pos = start
				return p.matchReplacementLiteral(rs, text, desc, build)
			}
			p.pos++
		}
	} else {
		for _, r := range rs {
			p.inputWork(1)
			if p.pos < s.base || !p.streamFill(p.pos+1) || s.in[p.pos-s.base] != r {
				p.pos = start
				p.expect(start, desc)
				return nil, false
			}
			p.pos++
		}
	}
	if !build {
		return nil, true
	}
	return p.newNode(Node{kind: kindMatch, Start: int32(start), End: int32(p.pos), Text: p.literalText(start, text), terminal: true, fresh: true}), true
}

func (p *parser) streamAtLineStart() bool {
	if p.pos == 0 {
		return true
	}
	if p.unit == Bytes {
		c, ok := p.streamByte(p.pos - 1)
		return ok && c == '\n'
	}
	s := p.stream
	i := p.pos - 1
	return i >= s.base && p.streamFill(i+1) && s.in[i-s.base] == '\n'
}

func (p *parser) streamAtEnd() bool {
	p.inputWork(1)
	return !p.streamFill(p.pos + 1)
}

// streamText returns owned text between start and end. The part already
// discarded is cut off.
func (p *parser) streamText(start, end int) string {
	s := p.stream
	start, end = max(start, s.base), max(end, s.base)
	if start == end {
		return ""
	}
	c := &s.text
	if start >= c.base && end <= c.end && c.source != "" {
		if p.unit == Bytes {
			return c.source[start-c.base : end-c.base]
		}
		return c.source[c.offsets[start-c.base]:c.offsets[end-c.base]]
	}
	// Do not read ahead for text or repeatedly copy tiny reader fragments.
	// Oversized ranges are copied on their own.
	if end-start > streamTextChunk || s.loaded()-start < 64 {
		return p.streamCopy(start, end)
	}
	snap := streamTextSnapshot{base: start, end: start + min(streamTextChunk, s.loaded()-start)}
	snap.source = p.streamCopy(snap.base, snap.end)
	if p.unit == Bytes {
		*c = snap
		return snap.source[:end-start]
	}
	if workControlsEnabled && p.control != nil {
		// An interruption while filling must not leave partial offsets in
		// the snapshot in use, so a controlled parse fills separate storage.
		snap.offsets = make([]int32, snap.end-snap.base+1)
	} else if n := snap.end - snap.base + 1; cap(c.offsets) >= n {
		snap.offsets = c.offsets[:n]
	} else {
		snap.offsets = make([]int32, n)
	}
	i := 0
	for off := range snap.source {
		if i&127 == 0 {
			p.inputWork(uint64(min(128, snap.end-snap.base-i)))
		}
		snap.offsets[i] = int32(off)
		i++
	}
	snap.offsets[i] = int32(len(snap.source))
	*c = snap
	return snap.source[:snap.offsets[end-start]]
}

// streamCopy copies the loaded text between start and end.
func (p *parser) streamCopy(start, end int) string {
	s := p.stream
	start, end = start-s.base, end-s.base
	if p.unit == Bytes {
		if workControlsEnabled && p.control != nil {
			p.control.charge(uint64(end-start), 1)
		}
		return string(s.bs[start:end])
	}
	if workControlsEnabled && p.control != nil {
		p.control.charge(uint64(end-start), uint64(end-start))
	}
	return string(s.in[start:end])
}

// streamLineCol returns the line and column of pos, counted from base.
func (p *parser) streamLineCol(pos int) (int, int) {
	s := p.stream
	if s.eof && s.base == 0 {
		// The whole input is loaded: look the line up in a table of line
		// starts, built on first use, instead of scanning for each error.
		if s.lines == nil {
			s.lines = []int{0}
			for i := range s.loaded() {
				p.inputWork(1)
				if p.unit == Bytes && s.bs[i] == '\n' || p.unit == CodePoints && s.in[i] == '\n' {
					s.lines = append(s.lines, i+1)
				}
			}
		}
		pos = min(pos, s.loaded())
		lo, hi := 0, len(s.lines) // the number of line starts at or before pos
		for lo < hi {
			if m := int(uint(lo+hi) >> 1); s.lines[m] <= pos {
				lo = m + 1
			} else {
				hi = m
			}
		}
		return lo, pos - s.lines[lo-1] + 1
	}
	line, col := s.baseLine, s.baseCol
	n := s.loaded()
	for i := s.base; i < pos && i < n; i++ {
		p.inputWork(1)
		var nl bool
		if p.unit == Bytes {
			nl = s.bs[i-s.base] == '\n'
		} else {
			nl = s.in[i-s.base] == '\n'
		}
		if nl {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
