package engine

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"
)

// Unit is the unit of positions in the input.
type Unit int

const (
	// CodePoints counts positions in Unicode code points (the default).
	CodePoints Unit = iota
	// Bytes counts positions in UTF-8 bytes.
	Bytes
)

func (u Unit) String() string {
	if u == Bytes {
		return "bytes"
	}
	return "codepoints"
}

// ParseOptions holds per-parse settings.
type ParseOptions struct {
	// Unit is the position unit. Matching is done per code point regardless of the unit.
	Unit Unit
	// Backend is the backend that runs the parse.
	Backend Backend
	// Recognize checks only whether the input conforms to the grammar and reports syntax errors,
	// without building a tree (the resulting node is nil). Actions are not evaluated, so runtime
	// errors in actions are not reported. Values of captures referenced by predicates are still
	// built.
	Recognize bool
	// MaxDepth is the limit on rule call nesting depth; the parse fails when it is exceeded. Zero
	// means the default (DefaultMaxDepthIterative for the iterative model, DefaultMaxDepth
	// otherwise).
	MaxDepth int
	// Trace, if not nil, receives an event at the start and at the end of every rule call (see
	// TraceEvent). Tracing does not change the result of the parse.
	Trace func(TraceEvent)
}

// Default limits on call nesting depth. Backends that run recursively use the host stack, so
// their limit is smaller.
const (
	DefaultMaxDepth          = 100_000
	DefaultMaxDepthIterative = 10_000_000
)

func (o ParseOptions) maxDepth(b Backend) int {
	switch {
	case o.MaxDepth > 0:
		return o.MaxDepth
	case b == BytecodeIterative:
		return DefaultMaxDepthIterative
	}
	return DefaultMaxDepth
}

// input is the parser's input. Positions are in Unit, and the start of in or bs is at position
// base. When parsing a stream, more input is read from reader as needed and the committed part is
// discarded.
type input struct {
	unit     Unit
	in       []rune // for CodePoints
	bs       []byte // for Bytes
	base     int
	baseLine int // line and column of base (1-based; columns in Unit)
	baseCol  int
	reader   *bufio.Reader // nil if the whole input has been read
	eof      bool
	// src is the whole input as a string (unused for streams). If srcOK, text returns substrings
	// of src without building new strings. In code points, offs[i] is the byte offset of position i
	// (its length is the number of characters + 1); it is built when text first needs it, which a
	// recognition often never does. For input containing invalid UTF-8, text in code points returns
	// strings with U+FFFD substituted, so src is not used.
	src   string
	srcOK bool
	offs  []int32
	// lines holds the positions where lines start, for lineCol on fully loaded input (built on
	// first use; nil until then).
	lines      []int
	streamText streamTextSnapshot
}

// setSource records the whole input string s for use by text.
func (in *input) setSource(s string) {
	in.src, in.offs = s, nil
	in.srcOK = in.unit == Bytes || utf8.ValidString(s) && len(s) <= 1<<31-1
}

// buildOffs builds the offset table of src (code points).
func (in *input) buildOffs() {
	in.offs = make([]int32, 0, len(in.in)+1)
	for i := range in.src {
		in.offs = append(in.offs, int32(i))
	}
	in.offs = append(in.offs, int32(len(in.src)))
}

// replace replaces the positions [start, end) of a fully loaded input with text and returns the
// change in length. The source string and its offset table are spliced rather than rebuilt; the
// text is updated in place, since only the parser reads it (nodes refer to the source string, which
// is never modified).
func (in *input) replace(start, end int, text string) (delta int) {
	in.lines = nil
	if in.unit == Bytes {
		in.bs = slices.Replace(in.bs, start, end, []byte(text)...)
		in.src = in.src[:start] + text + in.src[end:]
		return len(text) - (end - start)
	}
	ins := []rune(text)
	in.in = slices.Replace(in.in, start, end, ins...)
	delta = len(ins) - (end - start)
	enc := text
	if !utf8.ValidString(text) {
		enc = string(ins) // invalid bytes read as U+FFFD
	}
	if !in.srcOK || len(in.src)+len(enc) > 1<<31-1 {
		in.setSource(string(in.in))
		return delta
	}
	if in.offs == nil {
		in.buildOffs()
	}
	from, to := in.offs[start], in.offs[end]
	in.src = in.src[:from] + enc + in.src[to:]
	added := make([]int32, 0, len(ins))
	for i := range enc {
		added = append(added, from+int32(i))
	}
	in.offs = slices.Replace(in.offs, start, end, added...)
	shift := int32(len(enc)) - (to - from)
	for i := start + len(ins); i < len(in.offs); i++ {
		in.offs[i] += shift
	}
	return delta
}

func newInput(s string, unit Unit) input { return newTextInput(s, unit, false) }

// newTextInput returns the input s. If offsets is set (the parse will need token text), the
// offset table is built in the same pass as the decoding; otherwise it is built on demand.
func newTextInput(s string, unit Unit, offsets bool) input {
	return newTextInputIn(s, unit, offsets, nil)
}

// newTextInputIn is newTextInput with the buffers of sc, if not nil.
func newTextInputIn(s string, unit Unit, offsets bool, sc *scratch) input {
	in := input{unit: unit, eof: true, baseLine: 1, baseCol: 1}
	if unit == Bytes {
		if sc != nil {
			in.bs = append(sc.bytes[:0], s...)
		} else {
			in.bs = []byte(s)
		}
		in.setSource(s)
		return in
	}
	// Decode and check validity in one pass (a U+FFFD that decodes from three bytes is valid).
	n := utf8.RuneCountInString(s)
	var offs []int32
	if sc != nil {
		in.in = grow(sc.runes, n)
		if offsets {
			offs = grow(sc.offs, n+1)
		}
	} else {
		in.in = make([]rune, n)
		if offsets {
			offs = make([]int32, n+1)
		}
	}
	if offsets {
		offs[n] = int32(len(s))
	}
	valid := true
	i := 0
	for off, r := range s {
		if r == utf8.RuneError && valid {
			if _, size := utf8.DecodeRuneInString(s[off:]); size == 1 {
				valid = false
			}
		}
		in.in[i] = r
		if offsets {
			offs[i] = int32(off)
		}
		i++
	}
	in.src, in.srcOK = s, valid && len(s) <= 1<<31-1
	if in.srcOK {
		in.offs = offs
	}
	return in
}

// newParser returns a parser of the input s; offsets is as for newTextInput.
func newParser(prog *Program, s string, unit Unit, offsets bool) *parser {
	return &parser{prog: prog, input: newTextInput(s, unit, offsets), memo: newMemoTable(), maxDepth: DefaultMaxDepth}
}

// scratch holds the buffers of a whole-input parse that nothing refers to once it returns: the
// decoded input, its offset table, the memo table and the stack of repetition values. Node text
// slices the input string itself, and errors are built before the parse returns.
type scratch struct {
	runes []rune
	offs  []int32
	bytes []byte
	memo  memoTable
	kids  []*Node
}

// maxScratch is the size, in elements, beyond which a buffer is not kept for later parses.
const maxScratch = 1 << 22

// newPooledParser is newParser with buffers left by earlier parses of prog. The caller passes the
// parser to releaseScratch when the parse has returned.
func newPooledParser(prog *Program, s string, unit Unit, offsets bool) *parser {
	sc, _ := prog.scratch.Get().(*scratch)
	if sc == nil {
		sc = &scratch{}
	}
	p := &parser{prog: prog, input: newTextInputIn(s, unit, offsets, sc), memo: &sc.memo, maxDepth: DefaultMaxDepth, kidStack: sc.kids[:0]}
	p.scratch = sc
	return p
}

// releaseScratch returns the buffers of a parser made by newPooledParser for later parses.
func (p *parser) releaseScratch() {
	sc := p.scratch
	if sc == nil {
		return
	}
	p.scratch = nil
	if cap(p.in) > cap(sc.runes) && cap(p.in) <= maxScratch {
		sc.runes = p.in[:0]
	}
	if p.offs != nil && cap(p.offs) > cap(sc.offs) && cap(p.offs) <= maxScratch {
		sc.offs = p.offs[:0]
	}
	if cap(p.bs) > cap(sc.bytes) && cap(p.bs) <= maxScratch {
		sc.bytes = p.bs[:0]
	}
	if cap(p.kidStack) <= maxScratch {
		clear(p.kidStack[:cap(p.kidStack)])
		sc.kids = p.kidStack[:0]
	} else {
		sc.kids = nil
	}
	if !sc.memo.reset() {
		sc.memo = memoTable{}
	}
	p.prog.scratch.Put(sc)
}

// grow returns a slice of n elements, reusing buf if it is large enough. The elements are not
// cleared.
func grow[T any](buf []T, n int) []T {
	if cap(buf) >= n {
		return buf[:n]
	}
	return make([]T, n)
}

func newStreamParser(prog *Program, r io.Reader, unit Unit) *parser {
	return &parser{prog: prog, input: input{unit: unit, reader: bufio.NewReader(r), baseLine: 1, baseCol: 1}, memo: newMemoTable(), maxDepth: DefaultMaxDepth}
}

// loaded returns the position of the end of the input read so far.
func (in *input) loaded() int {
	if in.unit == Bytes {
		return in.base + len(in.bs)
	}
	return in.base + len(in.in)
}

// fill reads the input up to just before position n and reports whether it succeeded.
// Once the required amount has been read, it reads only what is already buffered and does not
// wait for more.
func (in *input) fill(n int) bool {
	for in.loaded() < n && !in.eof {
		in.read()
		for in.reader.Buffered() > 0 {
			if in.unit == CodePoints {
				if b, _ := in.reader.Peek(in.reader.Buffered()); !utf8.FullRune(b) {
					break
				}
			}
			in.read()
		}
	}
	return in.loaded() >= n
}

func (in *input) read() {
	var err error
	if in.unit == Bytes {
		var b byte
		if b, err = in.reader.ReadByte(); err == nil {
			in.bs = append(in.bs, b)
		}
	} else {
		var r rune
		if r, _, err = in.reader.ReadRune(); err == nil {
			in.in = append(in.in, r)
		}
	}
	if err != nil {
		if err != io.EOF {
			panic(fatal{err})
		}
		in.eof = true
	}
}

// decode returns the character at position i, its size (in units), and the end of the examined
// range.
func (in *input) decode(i int) (r rune, size, examined int, ok bool) {
	if i < in.base {
		return 0, 0, i + 1, false
	}
	if in.unit == CodePoints {
		if !in.fill(i + 1) {
			return 0, 0, i + 1, false
		}
		return in.in[i-in.base], 1, i + 1, true
	}
	if !in.fill(i + 1) {
		return 0, 0, i + 1, false
	}
	b := in.bs[i-in.base:]
	if b[0] < utf8.RuneSelf {
		return rune(b[0]), 1, i + 1, true
	}
	// A complete rune (including an invalid prefix) needs no future bytes.
	// Wait only while the available UTF-8 prefix is genuinely incomplete.
	for !in.eof && !utf8.FullRune(b) {
		in.fill(in.loaded() + 1)
		b = in.bs[i-in.base:]
	}
	r, size = utf8.DecodeRune(b)
	examined = i + size
	if r == utf8.RuneError && size == 1 {
		// Whether the bytes are invalid depends on the following bytes.
		examined = i + min(utf8.UTFMax, len(b))
		if !utf8.FullRune(b) {
			// EOF completed the decoding decision, not the character. An
			// insertion here may complete the prefix and change its size.
			examined++
		}
	}
	return r, size, examined, true
}

// byteAt returns the byte at position i when the unit is Bytes.
func (in *input) byteAt(i int) (byte, bool) {
	if i < in.base || !in.fill(i+1) {
		return 0, false
	}
	return in.bs[i-in.base], true
}

func (in *input) text(start, end int) string {
	// The part discarded by a stream cannot be returned: the text is cut to what is still held
	// (possibly nothing).
	start, end = max(start, in.base), max(end, in.base)
	if in.srcOK {
		if in.unit == Bytes {
			return in.src[start:end]
		}
		if in.offs == nil {
			in.buildOffs()
		}
		return in.src[in.offs[start]:in.offs[end]]
	}
	if streamTextSnapshots && in.reader != nil {
		return in.snapshotText(start, end)
	}
	if in.unit == Bytes {
		return string(in.bs[start-in.base : end-in.base])
	}
	return string(in.in[start-in.base : end-in.base])
}

// isNewline reports whether position i holds a newline character (used to detect line starts).
func (in *input) isNewline(i int) bool {
	if in.unit == Bytes {
		b, ok := in.byteAt(i)
		return ok && b == '\n'
	}
	if i < in.base || !in.fill(i+1) {
		return false
	}
	return in.in[i-in.base] == '\n'
}

// lineCol returns the line and column (in Unit) of position pos.
func (in *input) lineCol(pos int) (int, int) {
	if in.eof && in.base == 0 {
		// The whole input is loaded: look the line up in a table of line starts, built on first
		// use, instead of scanning from the start for each error.
		if in.lines == nil {
			in.buildLines()
		}
		pos = min(pos, in.loaded())
		i, _ := slices.BinarySearch(in.lines, pos+1)
		return i, pos - in.lines[i-1] + 1
	}
	line, col := in.baseLine, in.baseCol
	n := in.loaded()
	for i := in.base; i < pos && i < n; i++ {
		var nl bool
		if in.unit == Bytes {
			nl = in.bs[i-in.base] == '\n'
		} else {
			nl = in.in[i-in.base] == '\n'
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

// buildLines builds the table of the positions where lines start.
func (in *input) buildLines() {
	in.lines = append(in.lines[:0], 0)
	if in.unit == Bytes {
		for i, b := range in.bs {
			if b == '\n' {
				in.lines = append(in.lines, i+1)
			}
		}
		return
	}
	for i, r := range in.in {
		if r == '\n' {
			in.lines = append(in.lines, i+1)
		}
	}
}

// discard discards the input before position keep. The buffer holds read-ahead input, so it is
// compacted in place only once at least half of it can go: copying the rest on every call would
// cost the size of the buffer per element of a stream.
func (in *input) discard(keep int) {
	n := keep - in.base
	if n <= 0 || 2*n < len(in.in)+len(in.bs) {
		return
	}
	in.baseLine, in.baseCol = in.lineCol(keep)
	if in.unit == Bytes {
		in.bs = in.bs[:copy(in.bs, in.bs[n:])]
	} else {
		in.in = in.in[:copy(in.in, in.in[n:])]
	}
	in.base = keep
	if streamTextSnapshots && in.streamText.end <= keep {
		in.streamText.source = ""
	}
}

// commit makes it final that the parse never backtracks before position pos, and discards the
// input and memo entries no longer needed. The preceding unit is kept for detecting line starts
// (^).
func (p *parser) commit(pos int) {
	p.discard(pos - 1)
	p.trail = p.trail[:0]
	if streamFrameScratch {
		p.resetStreamFrames()
	}
	p.splitChunks()
	if pos-p.pruned >= 1024 {
		p.memo.prune(pos)
		p.pruned = pos
	}
}

// peek returns the character at the current position and its size.
func (p *parser) peek() (rune, int, bool) {
	r, size, examined, ok := p.decode(p.pos)
	p.touch(examined)
	return r, size, ok
}

// matchLiteral advances past the string lit if it matches at the current position.
func (p *parser) matchLiteral(rs []rune, bs []byte) bool {
	if p.eof && p.pos >= p.base {
		// The whole input is loaded: compare in one go. As below, a mismatch leaves pos after the
		// matching prefix and the examined range includes the first character that differs.
		var k, n int
		if p.unit == Bytes {
			in := p.bs[p.pos-p.base:]
			n = len(bs)
			for k < n && k < len(in) && in[k] == bs[k] {
				k++
			}
		} else {
			in := p.in[p.pos-p.base:]
			n = len(rs)
			for k < n && k < len(in) && in[k] == rs[k] {
				k++
			}
		}
		if k == n {
			p.touch(p.pos + n)
			p.pos += n
			return true
		}
		if p.unit == Bytes && slices.Contains(rs, utf8.RuneError) {
			return p.matchLiteralRunes(rs)
		}
		p.touch(p.pos + k + 1)
		p.pos += k
		return false
	}
	if p.unit == Bytes {
		start := p.pos
		for _, b := range bs {
			p.touch(p.pos + 1)
			c, ok := p.byteAt(p.pos)
			if !ok || c != b {
				if slices.Contains(rs, utf8.RuneError) {
					p.pos = start
					return p.matchLiteralRunes(rs)
				}
				return false
			}
			p.pos++
		}
		return true
	}
	for _, r := range rs {
		p.touch(p.pos + 1)
		if p.pos < p.base || !p.fill(p.pos+1) || p.in[p.pos-p.base] != r {
			return false
		}
		p.pos++
	}
	return true
}

// literalText preserves matched input bytes when replacement decoding accepts
// a shorter spelling than the literal's valid UTF-8 encoding.
func (p *parser) literalText(start int, text string) string {
	if p.unit == Bytes && p.pos-start != len(text) {
		return p.text(start, p.pos)
	}
	return text
}

// matchLiteralRunes handles replacement characters that can occupy one invalid
// input byte or three valid UTF-8 bytes. peek records decoding dependencies.
func (p *parser) matchLiteralRunes(rs []rune) bool {
	for _, want := range rs {
		got, size, ok := p.peek()
		if !ok || got != want {
			return false
		}
		p.pos += size
	}
	return true
}

func (p *parser) atEOF() bool {
	p.touch(p.pos + 1)
	return !p.fill(p.pos + 1)
}

// atLineStart reports whether the current position is at the start of a line. It examines the
// preceding character, or the absence of one: in both cases the result depends on the input
// before the position, so it records that (lw), which keeps a Document from shifting the result
// past an edit before it.
func (p *parser) atLineStart() bool {
	p.lw = min2(p.lw, p.pos-1)
	if p.pos == 0 {
		return true
	}
	return p.isNewline(p.pos - 1)
}

// atInputStart reports whether the current position is the start of the input. Like
// atLineStart, the result depends on the input before the position.
func (p *parser) atInputStart() bool {
	p.lw = min2(p.lw, p.pos-1)
	return p.pos == 0
}

// textLen returns the length of the string in Unit.
func (u Unit) textLen(s string) int {
	if u == Bytes {
		return len(s)
	}
	return utf8.RuneCountInString(s)
}

// validBoundary checks, when the unit is Bytes, that the position lies on a character boundary.
func validBoundary(text string, unit Unit, pos int) error {
	if unit == Bytes && pos < len(text) && !utf8.RuneStart(text[pos]) {
		// A continuation byte is interior only when a valid rune covers
		// it. Invalid bytes are separate one-byte replacement characters.
		// No UTF-8 character starts more than three bytes before pos.
		for start := pos - 1; start >= max(0, pos-utf8.UTFMax+1); start-- {
			if utf8.RuneStart(text[start]) {
				_, size := utf8.DecodeRuneInString(text[start:])
				if start+size > pos {
					return fmt.Errorf("position %d is not at a character boundary", pos)
				}
				break
			}
		}
	}
	return nil
}
