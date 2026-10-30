package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

// PositionUnit is the unit of positions in the input.
type PositionUnit int

const (
	// CodePoints counts positions in Unicode code points (the default).
	CodePoints PositionUnit = iota
	// Bytes counts positions in UTF-8 bytes.
	Bytes
)

func (u PositionUnit) String() string {
	if u == Bytes {
		return "bytes"
	}
	return "codepoints"
}

// ParseOptions holds per-parse settings.
type ParseOptions struct {
	// MissingTokens bounds attempted #missing insertions. Zero disables insertion.
	// Only Closure whole-input parsing and recognition support a positive limit.
	MissingTokens int
	// Context cooperatively interrupts this invocation. Nil means no cancellation.
	Context context.Context
	// Limits optionally bound cumulative input inspections and execution steps.
	Limits limits.Limits
	// Unit is the position unit. Matching is done per code point regardless of the unit.
	Unit PositionUnit
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
	// DiagnosticCallback receives committed recoveries during stream parsing, immediately before
	// the corresponding element. It is invalid for batch parses and Documents.
	DiagnosticCallback func(*SyntaxError) error
	// DiagnosticLimit retains only the latest committed stream recoveries when positive.
	// DiagnosticLimitSet records an explicit zero limit, which is stream-only too.
	DiagnosticLimit    int
	DiagnosticLimitSet bool
}

// Default limits on call nesting depth. Backends that run recursively use the host stack, so
// their limit is smaller.
const (
	DefaultMaxDepth          = limits.DefaultMaxDepth
	DefaultMaxDepthIterative = limits.DefaultMaxDepthIterative
)

// DepthLimit returns the rule call nesting limit that the options set for backend b.
func DepthLimit(o ParseOptions, b Backend) int {
	switch {
	case o.MaxDepth > 0:
		return o.MaxDepth
	case b == BytecodeIterative:
		return DefaultMaxDepthIterative
	}
	return DefaultMaxDepth
}

// Input is the parser's input. Positions are in Unit, and the start of in or bs is at position
// base. When parsing a stream, more input is read from reader as needed and the committed part is
// discarded.
type Input struct {
	Control  *BudgetControl // invocation-local; never retained by results
	Unit     PositionUnit
	In       []rune // for CodePoints
	Bs       []byte // for Bytes
	Base     int
	BaseLine int // line and column of base (1-based; columns in Unit)
	BaseCol  int
	Reader   *bufio.Reader // nil if the whole input has been read
	Eof      bool

	// ReaderCall distinguishes buffered reader execution from VM execution.
	ReaderCall   bool
	readerResult error // most recent caller Read result, before bufio defers its error
	// Src is the whole input as a string (unused for streams). If SrcOK, text returns substrings
	// of src without building new strings. In code points, offs[i] is the byte offset of position i
	// (its length is the number of characters + 1); it is built when text first needs it, which a
	// recognition often never does. For input containing invalid UTF-8, text in code points returns
	// strings with U+FFFD substituted, so src is not used.
	Src   string
	SrcOK bool
	Offs  []int32
	// Lines holds the positions where lines start, for lineCol on fully loaded input (built on
	// first use; nil until then).
	Lines      []int
	StreamText StreamTextSnapshot
	// Only Documents use pieces. Batch and stream inputs retain their slices.
	Pieces *documentPieces
}

// setSource records the whole input string s for use by text.
func (in *Input) setSource(s string) {
	in.Src, in.Offs = s, nil
	in.SrcOK = in.Unit == Bytes || utf8.ValidString(s) && len(s) <= 1<<31-1
}

// buildOffs builds the offset table of src (code points).
func (in *Input) buildOffs() {
	offs := make([]int32, 0, len(in.In)+1)
	for i := 0; i < len(in.Src); {
		offs = append(offs, int32(i))
		_, size := utf8.DecodeRuneInString(in.Src[i:])
		in.spendRead(uint64(size))
		i += size
	}
	in.Offs = append(offs, int32(len(in.Src)))
}

// Replace replaces the positions [start, end) of a fully loaded input with text and returns the
// change in length. The source string and its offset table are spliced rather than rebuilt; the
// text is updated in place, since only the parser reads it (nodes refer to the source string, which
// is never modified).
func (in *Input) Replace(start, end int, text string) (delta int) {
	in.Lines = nil
	if DocumentPieceInput && in.Pieces != nil {
		return in.Pieces.Replace(start, end, text)
	}
	if in.Unit == Bytes {
		in.Bs = slices.Replace(in.Bs, start, end, []byte(text)...)
		in.Src = in.Src[:start] + text + in.Src[end:]
		return len(text) - (end - start)
	}
	ins := []rune(text)
	in.In = slices.Replace(in.In, start, end, ins...)
	delta = len(ins) - (end - start)
	enc := text
	if !utf8.ValidString(text) {
		enc = string(ins) // invalid bytes read as U+FFFD
	}
	if !in.SrcOK || len(in.Src)+len(enc) > 1<<31-1 {
		in.setSource(string(in.In))
		return delta
	}
	if in.Offs == nil {
		in.buildOffs()
	}
	from, to := in.Offs[start], in.Offs[end]
	in.Src = in.Src[:from] + enc + in.Src[to:]
	added := make([]int32, 0, len(ins))
	for i := range enc {
		added = append(added, from+int32(i))
	}
	in.Offs = slices.Replace(in.Offs, start, end, added...)
	shift := int32(len(enc)) - (to - from)
	for i := start + len(ins); i < len(in.Offs); i++ {
		in.Offs[i] += shift
	}
	return delta
}

func NewInput(s string, unit PositionUnit) Input { return NewTextInput(s, unit, false) }

// NewTextInput returns the input s. If offsets is set (the parse will need token text), the
// offset table is built in the same pass as the decoding; otherwise it is built on demand.
func NewTextInput(s string, unit PositionUnit, offsets bool) Input {
	return newTextInputIn(s, unit, offsets, nil)
}

// newTextInputIn is NewTextInput with the buffers of sc, if not nil.
func newTextInputIn(s string, unit PositionUnit, offsets bool, sc *scratch) Input {
	in := Input{Unit: unit, Eof: true, BaseLine: 1, BaseCol: 1}
	if unit == Bytes {
		if sc != nil {
			in.Bs = append(sc.bytes[:0], s...)
		} else {
			in.Bs = []byte(s)
		}
		in.setSource(s)
		return in
	}
	// Decode and check validity in one pass (a U+FFFD that decodes from three bytes is valid).
	n := utf8.RuneCountInString(s)
	var offs []int32
	if sc != nil {
		in.In = grow(sc.runes, n)
		if offsets {
			offs = grow(sc.offs, n+1)
		}
	} else {
		in.In = make([]rune, n)
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
		in.In[i] = r
		if offsets {
			offs[i] = int32(off)
		}
		i++
	}
	in.Src, in.SrcOK = s, valid && len(s) <= 1<<31-1
	if in.SrcOK {
		in.Offs = offs
	}
	return in
}

// NewParser returns a parser of the input s; offsets is as for NewTextInput.
func NewParser(prog *SharedProgram, s string, unit PositionUnit, offsets bool) *Parser {
	return &Parser{Prog: prog, Input: NewTextInput(s, unit, offsets), Memo: NewMemoTable(), MaxDepth: DefaultMaxDepth}
}

// scratch holds the buffers of a whole-input parse that nothing refers to once it returns: the
// decoded input, its offset table, the memo table and the stack of repetition values. Node text
// slices the input string itself, and errors are built before the parse returns.
type scratch struct {
	runes []rune
	offs  []int32
	bytes []byte
	memo  MemoTable
	kids  []*value.Node
}

// MaxScratch is the size, in elements, beyond which a buffer is not kept for later parses.
const MaxScratch = 1 << 22

// AcquirePooledParser separates scratch acquisition from input initialization,
// so a caller can arm release before controlled decoding can interrupt.
func AcquirePooledParser(prog *SharedProgram) *Parser {
	sc, _ := prog.scratch.Get().(*scratch)
	if sc == nil {
		sc = &scratch{}
	}
	p := &Parser{Prog: prog, Memo: &sc.memo, MaxDepth: DefaultMaxDepth, KidStack: sc.kids[:0]}
	p.Scratch = sc
	return p
}

// ReleaseScratch returns the buffers of a parser made by AcquirePooledParser for later parses.
func (p *Parser) ReleaseScratch() {
	p.Control = nil
	sc := p.Scratch
	if sc == nil {
		return
	}
	p.Scratch = nil
	if cap(p.In) > cap(sc.runes) && cap(p.In) <= MaxScratch {
		sc.runes = p.In[:0]
	}
	if p.Offs != nil && cap(p.Offs) > cap(sc.offs) && cap(p.Offs) <= MaxScratch {
		sc.offs = p.Offs[:0]
	}
	if cap(p.Bs) > cap(sc.bytes) && cap(p.Bs) <= MaxScratch {
		sc.bytes = p.Bs[:0]
	}
	if cap(p.KidStack) <= MaxScratch {
		clear(p.KidStack[:cap(p.KidStack)])
		sc.kids = p.KidStack[:0]
	} else {
		sc.kids = nil
	}
	if !sc.memo.Reset() {
		sc.memo = MemoTable{}
	}
	p.Prog.scratch.Put(sc)
}

// ReleaseBatchScratch finishes a whole-input invocation. Saved trace events
// may retain its parser, so detach private references after recycling scratch.
// Node storage is separately allocated and remains owned by returned results.
func (p *Parser) ReleaseBatchScratch() {
	p.Missing, p.MissingLen = nil, 0
	p.ReleaseScratch()
	p.ReleaseTraceReferences()
}

// ReleaseTraceReferences detaches a finished traced parser after its owner has
// transferred reusable state. It does not mutate result or event backing arrays.
func (p *Parser) ReleaseTraceReferences() {
	if p.Tr != nil {
		*p = Parser{Aborted: p.Aborted}
	}
}

// grow returns a slice of n elements, reusing buf if it is large enough. The elements are not
// cleared.
func grow[T any](buf []T, n int) []T {
	if cap(buf) >= n {
		return buf[:n]
	}
	return make([]T, n)
}

func NewStreamParser(prog *SharedProgram, r io.Reader, unit PositionUnit) *Parser {
	return NewControlledStreamParser(prog, r, unit, nil)
}

func NewControlledStreamParser(prog *SharedProgram, r io.Reader, unit PositionUnit, control *BudgetControl) *Parser {
	p := &Parser{Prog: prog, Input: Input{Unit: unit, Control: control, BaseLine: 1, BaseCol: 1}, Memo: NewMemoTable(), MaxDepth: DefaultMaxDepth}
	if BudgetControlsEnabled && control != nil {
		r = &StreamReaderObserver{Reader: r, Result: &p.readerResult, Control: control}
	}
	p.Reader = bufio.NewReader(r)
	if StreamPrefixIsolation {
		p.NodeChunks = -NodeChunk / 8 // Small prefixes use private exact-size storage.
	}
	return p
}

// Loaded returns the position of the end of the input read so far.
func (in *Input) Loaded() int {
	if DocumentPieceInput && in.Pieces != nil {
		return in.Pieces.N
	}
	if in.Unit == Bytes {
		return in.Base + len(in.Bs)
	}
	return in.Base + len(in.In)
}

// Fill reads the input up to just before position n and reports whether it succeeded.
// Once the required amount has been read, it reads only what is already buffered and does not
// wait for more.
func (in *Input) Fill(n int) bool {
	for in.Loaded() < n && !in.Eof {
		in.read()
		for in.Reader.Buffered() > 0 {
			if in.Unit == CodePoints {
				if b, _ := in.Reader.Peek(in.Reader.Buffered()); !utf8.FullRune(b) {
					break
				}
			}
			in.read()
		}
	}
	return in.Loaded() >= n
}

func (in *Input) read() {
	if BudgetControlsEnabled && in.Control != nil {
		in.Control.Check()
		in.Control.Charge(1) // reading and staging one byte or code point
	}
	var err error
	var b byte
	var r rune
	in.ReaderCall = true
	if in.Unit == Bytes {
		b, err = in.Reader.ReadByte()
	} else {
		r, _, err = in.Reader.ReadRune()
	}
	in.ReaderCall = false
	if err != nil && err != io.EOF {
		panic(Fatal{err})
	}
	if BudgetControlsEnabled && in.Control != nil {
		returned := in.readerResult
		in.readerResult = nil
		in.Control.checkAfterReader(returned)
	}
	if err == io.EOF {
		in.Eof = true
	} else if in.Unit == Bytes {
		in.Bs = append(in.Bs, b)
	} else {
		in.In = append(in.In, r)
	}
}

// Decode returns the character at position i, its size (in units), and the end of the examined
// range.
func (in *Input) Decode(i int) (r rune, size, examined int, ok bool) {
	in.spendRead(1)
	if i < in.Base {
		return 0, 0, i + 1, false
	}
	if DocumentPieceInput && in.Pieces != nil {
		return in.decodePieces(i)
	}
	if in.Unit == CodePoints {
		if !in.Fill(i + 1) {
			return 0, 0, i + 1, false
		}
		return in.In[i-in.Base], 1, i + 1, true
	}
	if !in.Fill(i + 1) {
		return 0, 0, i + 1, false
	}
	b := in.Bs[i-in.Base:]
	if b[0] < utf8.RuneSelf {
		return rune(b[0]), 1, i + 1, true
	}
	// A complete rune (including an invalid prefix) needs no future bytes.
	// Wait only while the available UTF-8 prefix is genuinely incomplete.
	for !in.Eof && !utf8.FullRune(b) {
		in.Fill(in.Loaded() + 1)
		b = in.Bs[i-in.Base:]
	}
	in.spendRead(uint64(min(utf8.UTFMax, len(b))))
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
func (in *Input) byteAt(i int) (byte, bool) {
	in.spendRead(1)
	if i < in.Base || !in.Fill(i+1) {
		return 0, false
	}
	if DocumentPieceInput && in.Pieces != nil {
		b, _ := in.Pieces.At(i)
		return byte(b), true
	}
	return in.Bs[i-in.Base], true
}

func (in *Input) Text(start, end int) string {
	// The part discarded by a stream cannot be returned: the text is cut to what is still held
	// (possibly nothing).
	start, end = max(start, in.Base), max(end, in.Base)
	if DocumentPieceInput && in.Pieces != nil {
		if BudgetControlsEnabled && in.Control != nil {
			return in.controlledPieceText(start, end)
		}
		return in.Pieces.Text(start, end)
	}
	if in.SrcOK {
		if in.Unit == Bytes {
			return in.Src[start:end]
		}
		if in.Offs == nil {
			in.buildOffs()
		}
		return in.Src[in.Offs[start]:in.Offs[end]]
	}
	if StreamTextSnapshots && in.Reader != nil {
		return in.snapshotText(start, end)
	}
	if BudgetControlsEnabled && in.Control != nil {
		return in.detachedText(start-in.Base, end-in.Base)
	}
	if in.Unit == Bytes {
		return string(in.Bs[start-in.Base : end-in.Base])
	}
	return string(in.In[start-in.Base : end-in.Base])
}

// isNewline reports whether position i holds a newline character (used to detect line starts).
func (in *Input) isNewline(i int) bool {
	if DocumentPieceInput && in.Pieces != nil {
		b, ok := in.Pieces.At(i)
		return ok && b == '\n'
	}
	if in.Unit == Bytes {
		b, ok := in.byteAt(i)
		return ok && b == '\n'
	}
	if i < in.Base || !in.Fill(i+1) {
		return false
	}
	return in.In[i-in.Base] == '\n'
}

// lineCol returns the line and column (in Unit) of position pos.
func (in *Input) lineCol(pos int) (int, int) {
	if in.Eof && in.Base == 0 {
		// The whole input is loaded: look the line up in a table of line starts, built on first
		// use, instead of scanning from the start for each error.
		if in.Lines == nil {
			in.buildLines()
		}
		pos = min(pos, in.Loaded())
		i, _ := slices.BinarySearch(in.Lines, pos+1)
		return i, pos - in.Lines[i-1] + 1
	}
	line, col := in.BaseLine, in.BaseCol
	n := in.Loaded()
	for i := in.Base; i < pos && i < n; i++ {
		in.spendRead(1)
		var nl bool
		if in.Unit == Bytes {
			nl = in.Bs[i-in.Base] == '\n'
		} else {
			nl = in.In[i-in.Base] == '\n'
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
func (in *Input) buildLines() {
	// A Document retains successful lookup tables across invocations. An
	// interrupted scan must leave no partial table for a later parse to reuse.
	complete := false
	defer func() {
		if !complete {
			in.Lines = nil
		}
	}()
	in.Lines = append(in.Lines[:0], 0)
	if DocumentPieceInput && in.Pieces != nil {
		for _, p := range in.Pieces.Parts {
			if in.Unit == Bytes {
				for i := 0; i < len(p.Text); i++ {
					in.spendRead(1)
					if p.Text[i] == '\n' {
						in.Lines = append(in.Lines, p.start+i+1)
					}
				}
			} else {
				for i, r := range p.Runes {
					in.spendRead(1)
					if r == '\n' {
						in.Lines = append(in.Lines, p.start+i+1)
					}
				}
			}
		}
		complete = true
		return
	}
	if in.Unit == Bytes {
		for i := range in.Bs {
			in.spendRead(1)
			b := in.Bs[i]
			if b == '\n' {
				in.Lines = append(in.Lines, i+1)
			}
		}
		complete = true
		return
	}
	for i := range in.In {
		in.spendRead(1)
		r := in.In[i]
		if r == '\n' {
			in.Lines = append(in.Lines, i+1)
		}
	}
	complete = true
}

// Discard discards the input before position keep. The buffer holds read-ahead input, so it is
// compacted in place only once at least half of it can go: copying the rest on every call would
// cost the size of the buffer per element of a stream.
func (in *Input) Discard(keep int) {
	n := keep - in.Base
	if n <= 0 || 2*n < len(in.In)+len(in.Bs) {
		return
	}
	in.BaseLine, in.BaseCol = in.lineCol(keep)
	if in.Unit == Bytes {
		in.Bs = in.Bs[:CopyStreamPrefix(in.Control, in.Bs, in.Bs[n:])]
	} else {
		in.In = in.In[:CopyStreamPrefix(in.Control, in.In, in.In[n:])]
	}
	in.Base = keep
	if StreamTextSnapshots && in.StreamText.End <= keep {
		in.StreamText.Source = ""
	}
}

// Commit makes it final that the parse never backtracks before position pos, and discards the
// input and memo entries no longer needed. The preceding unit is kept for detecting line starts
// (^).
func (p *Parser) Commit(pos int) {
	p.Discard(pos - 1)
	p.Trail = p.Trail[:0]
	if StreamFrameScratch {
		p.ResetStreamFrames()
	}
	p.splitChunks()
	if pos-p.Pruned >= 1024 {
		// Completed calls may have shortened these slices before commit.
		// Clear their inactive capacity along with memo pruning, so a large
		// element's references do not survive indefinitely in small streams.
		if StreamUndoCleanup {
			ClearSpend(p, p.Trail[:cap(p.Trail)])
		}
		if StreamStackCleanup && p.Backend != nil {
			p.Backend.ClearInactive()
		}
		p.PruneStreamMemo(pos)
		p.Pruned = pos
	}
	if p.DiagnosticTail.Limit > 0 {
		// The element boundary is irreversible. Its recovered errors have
		// already been delivered and collected. Without tracing, callbacks
		// own defensive copies and memo entries own separate slices, so a
		// small cleared backing array can serve the next element. A trace
		// callback may retain Recovered(), and large scratch is released.
		// The active start-rule call saved an empty recovery prefix;
		// speculative element marks are no longer active after emission.
		if p.Tr == nil && cap(p.Recovered) <= streamRecoveryScratchMax {
			ClearSpend(p, p.Recovered[:cap(p.Recovered)])
			p.Recovered = p.Recovered[:0]
		} else {
			p.Recovered = nil
		}
		p.Diagnosed = 0
	}
}

// Peek returns the character at the current position and its size.
func (p *Parser) Peek() (rune, int, bool) {
	r, size, examined, ok := p.Decode(p.Pos)
	p.touch(examined)
	return r, size, ok
}

// MatchLiteral advances past the string lit if it matches at the current position.
func (p *Parser) MatchLiteral(rs []rune, bs []byte) bool {
	if DocumentPieceInput && p.Pieces != nil {
		return p.matchLiteralPieces(rs, bs)
	}
	if p.Eof && p.Pos >= p.Base {
		// The whole input is loaded: compare in one go. As below, a mismatch leaves pos after the
		// matching prefix and the examined range includes the first character that differs.
		var k, n int
		if p.Unit == Bytes {
			in := p.Bs[p.Pos-p.Base:]
			n = len(bs)
			for k < n {
				p.spendRead(1)
				if k >= len(in) || in[k] != bs[k] {
					break
				}
				k++
			}
		} else {
			in := p.In[p.Pos-p.Base:]
			n = len(rs)
			for k < n {
				p.spendRead(1)
				if k >= len(in) || in[k] != rs[k] {
					break
				}
				k++
			}
		}
		if k == n {
			p.touch(p.Pos + n)
			p.Pos += n
			return true
		}
		if p.Unit == Bytes && p.LiteralHasReplacement(rs) {
			return p.matchLiteralRunes(rs)
		}
		p.touch(p.Pos + k + 1)
		p.Pos += k
		return false
	}
	if p.Unit == Bytes {
		start := p.Pos
		for _, b := range bs {
			p.touch(p.Pos + 1)
			c, ok := p.byteAt(p.Pos)
			if !ok || c != b {
				if p.LiteralHasReplacement(rs) {
					p.Pos = start
					return p.matchLiteralRunes(rs)
				}
				return false
			}
			p.Pos++
		}
		return true
	}
	for _, r := range rs {
		p.spendRead(1)
		p.touch(p.Pos + 1)
		if p.Pos < p.Base || !p.Fill(p.Pos+1) || p.In[p.Pos-p.Base] != r {
			return false
		}
		p.Pos++
	}
	return true
}

// MatchLiteralFold advances past a case-insensitive literal if the input matches it. rs are the
// folded runes of the literal (foldedRunes), and each input rune is folded before the comparison.
// It decodes the input rune by rune, as matchLiteralRunes does, so both position units agree.
func (p *Parser) MatchLiteralFold(rs []rune) bool {
	for _, want := range rs {
		p.spendRead(1)
		got, size, ok := p.Peek()
		if !ok || compile.FoldASCIIRune(got) != want {
			return false
		}
		p.Pos += size
	}
	return true
}

// LiteralHasReplacement bounds the grammar-side scan after a byte mismatch.
// Only visited literal runes consume steps; they do not inspect input.
func (p *Parser) LiteralHasReplacement(rs []rune) bool {
	if !BudgetControlsEnabled || p.Control == nil {
		return slices.Contains(rs, utf8.RuneError)
	}
	for _, r := range rs {
		p.Spend()
		if r == utf8.RuneError {
			return true
		}
	}
	return false
}

// LiteralText preserves matched input bytes when replacement decoding accepts
// a shorter spelling than the literal's valid UTF-8 encoding.
func (p *Parser) LiteralText(start int, text string) string {
	if p.Unit == Bytes && p.Pos-start != len(text) {
		return p.Text(start, p.Pos)
	}
	return text
}

// matchLiteralRunes handles replacement characters that can occupy one invalid
// input byte or three valid UTF-8 bytes. peek records decoding dependencies.
func (p *Parser) matchLiteralRunes(rs []rune) bool {
	for _, want := range rs {
		got, size, ok := p.Peek()
		if !ok || got != want {
			return false
		}
		p.Pos += size
	}
	return true
}

func (p *Parser) AtEOF() bool {
	p.spendRead(1)
	p.touch(p.Pos + 1)
	return !p.Fill(p.Pos + 1)
}

// AtLineStart reports whether the current position is at the start of a line. It examines the
// preceding character, or the absence of one: in both cases the result depends on the input
// before the position, so it records that (lw), which keeps a Document from shifting the result
// past an edit before it.
func (p *Parser) AtLineStart() bool {
	p.lw = Min2(p.lw, p.Pos-1)
	if p.Pos == 0 {
		return true
	}
	return p.isNewline(p.Pos - 1)
}

// AtInputStart reports whether the current position is the start of the input. Like
// AtLineStart, the result depends on the input before the position.
func (p *Parser) AtInputStart() bool {
	p.lw = Min2(p.lw, p.Pos-1)
	return p.Pos == 0
}

// TextLen returns the length of the string in unit u.
func TextLen(u PositionUnit, s string) int {
	if u == Bytes {
		return len(s)
	}
	return utf8.RuneCountInString(s)
}

// ValidBoundary checks, when the unit is Bytes, that the position lies on a character boundary.
func ValidBoundary(text string, unit PositionUnit, pos int) error {
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

// Backend is the backend used to run a parse.
type Backend int

const (
	// Default is the default backend. It uses the closure engine if the grammar's AST is available,
	// and bytecode otherwise.
	Default Backend = iota
	// Closure assembles parsing expressions into Go functions and runs them (the closure engine).
	Closure
	// Bytecode runs bytecode on the VM. Rule calls use host (Go) recursion (the recursive model).
	Bytecode
	// BytecodeIterative runs bytecode on the VM. Rule calls and Operators Rule expressions also run on the VM
	// stack, so deeply nested input does not consume the host stack (the iterative model).
	BytecodeIterative
)

func (b Backend) String() string {
	switch b {
	case Bytecode:
		return "bytecode"
	case BytecodeIterative:
		return "bytecode-iterative"
	case Closure:
		return "closure"
	}
	return "default"
}
