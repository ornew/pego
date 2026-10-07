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

func newInput(s string, unit Unit) input {
	in := input{unit: unit, eof: true, baseLine: 1, baseCol: 1}
	if unit == Bytes {
		in.bs = []byte(s)
		in.setSource(s)
		return in
	}
	// Decode and check validity in one pass (a U+FFFD that decodes from three bytes is valid).
	in.in = make([]rune, utf8.RuneCountInString(s))
	valid := true
	i := 0
	for off, r := range s {
		if r == utf8.RuneError && valid {
			if _, size := utf8.DecodeRuneInString(s[off:]); size == 1 {
				valid = false
			}
		}
		in.in[i] = r
		i++
	}
	in.src, in.srcOK = s, valid && len(s) <= 1<<31-1
	return in
}

func newParser(prog *Program, s string, unit Unit) *parser {
	return &parser{prog: prog, input: newInput(s, unit), memo: newMemoTable(), maxDepth: DefaultMaxDepth}
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
	in.fill(i + utf8.UTFMax)
	b := in.bs[min(i-in.base, len(in.bs)):]
	if len(b) == 0 {
		return 0, 0, i + 1, false
	}
	if b[0] < utf8.RuneSelf {
		return rune(b[0]), 1, i + 1, true
	}
	r, size = utf8.DecodeRune(b)
	examined = i + size
	if r == utf8.RuneError && size == 1 {
		// Whether the bytes are invalid depends on the following bytes.
		examined = i + min(utf8.UTFMax, len(b))
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
	if start < in.base {
		start = in.base // the part discarded by the stream cannot be returned
	}
	if in.srcOK {
		if in.unit == Bytes {
			return in.src[start:end]
		}
		if in.offs == nil {
			in.buildOffs()
		}
		return in.src[in.offs[start]:in.offs[end]]
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
	line, col := in.baseLine, in.baseCol
	for i := in.base; i < pos && i < in.loaded(); i++ {
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
}

// commit makes it final that the parse never backtracks before position pos, and discards the
// input and memo entries no longer needed. The preceding unit is kept for detecting line starts
// (^).
func (p *parser) commit(pos int) {
	p.discard(pos - 1)
	p.trail = p.trail[:0]
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
	if p.unit == Bytes {
		for _, b := range bs {
			p.touch(p.pos + 1)
			c, ok := p.byteAt(p.pos)
			if !ok || c != b {
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

func (p *parser) atEOF() bool {
	p.touch(p.pos + 1)
	return !p.fill(p.pos + 1)
}

// atLineStart reports whether the current position is at the start of a line.
func (p *parser) atLineStart() bool {
	if p.pos == 0 {
		return true
	}
	p.lw = min2(p.lw, p.pos-1)
	return p.isNewline(p.pos - 1)
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
		return fmt.Errorf("position %d is not at a character boundary", pos)
	}
	return nil
}
