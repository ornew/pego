// Package syntax parses PEGO source code into a grammar AST.
package syntax

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tString    // "..."
	tInt       // 123
	tCharClass // [...]
	tCapture   // $name
	tIndex     // $1
	tPunct     // punctuation
)

type token struct {
	kind tokenKind
	text string // identifier, punctuation, or string value
	num  int
	cc   *grammar.CharClass
	pos  grammar.Pos
	end  grammar.Pos // position just after the token
	// spaceBefore reports whether whitespace or a comment directly
	// precedes the token.
	spaceBefore bool
	// lineBefore reports whether a line break directly precedes the token.
	lineBefore bool
	// blankBefore reports whether a blank line directly precedes the token.
	blankBefore bool
	// lineComment is a comment on the line of the previous token, and
	// comments are the following comments before this token.
	lineComment *grammar.Comment
	comments    []*grammar.Comment
	// source is independent of the formatter trivia moved by the parser.
	source *commentBoundary
}

// endsScope reports whether the token ends the scope of the comments before it: a closing
// delimiter, a semicolon or the end of the file.
func (t token) endsScope() bool {
	return t.kind == tEOF || t.kind == tPunct && (t.text == ")" || t.text == "]" || t.text == "}" || t.text == ";")
}

// opensList reports whether the token opens a list or separates its elements.
func (t token) opensList() bool {
	if t.kind != tPunct {
		return false
	}
	switch t.text {
	case "(", "[", "{", ",":
		return true
	}
	return false
}

func (t token) String() string {
	switch t.kind {
	case tEOF:
		return "end of file"
	case tString:
		return strconv.Quote(t.text)
	case tInt:
		return strconv.Itoa(t.num)
	case tCharClass:
		return "character class"
	case tCapture:
		return "$" + t.text
	case tIndex:
		return "$" + strconv.Itoa(t.num)
	}
	return "'" + t.text + "'"
}

// puncts lists the multi-character punctuation, longest first.
var puncts = []string{
	"_|_", "...", "^^", "$$", "--", "=>", ":=", "==", "!=", "<=", ">=", "&&", "||", "[]",
	":", "=", "/", "(", ")", "{", "}", "[", "]", ",", ".", "*", "+", "?", "&", "!",
	"@", "-", "^", "$", "#", "|", "<", ">", "%", ";", "~",
}

type lexer struct {
	ctx       context.Context
	src       []rune
	srcASCII  bool
	i         int
	line      int
	col       int
	lineStart int
	errs      *errorList

	// State of the white space and comments before the next token.
	ntoks int // tokens read so far
	// prev is the previous token, which decides whether a "[" directly after it is a bracket.
	prev token
	// Action tracking: a "}" directly followed by "[" is an index only inside an
	// action, which starts at "=>" and ends with its statement or enclosing delimiter.
	// depth counts the open delimiters; actionDepth is the depth at the "=>".
	depth, actionDepth int
	inAction           bool
	newlines           int // line breaks since the previous token or comment
	lineComment        *grammar.Comment
	comments           []*grammar.Comment
}

func (l *lexer) pos() grammar.Pos { return grammar.Pos{Line: l.line, Col: l.col} }

func (l *lexer) peekAt(k int) rune {
	if l.i+k < len(l.src) {
		return l.src[l.i+k]
	}
	return 0
}

// lineBreak reports whether the character at i ends a line: a line feed, or a carriage return
// that no line feed follows ("\r\n" ends a line at its "\n").
func (l *lexer) lineBreak(i int) bool {
	r := l.src[i]
	return r == '\n' || r == '\r' && (i+1 >= len(l.src) || l.src[i+1] != '\n')
}

func (l *lexer) advance() rune {
	r := l.src[l.i]
	l.i++
	if l.i&255 == 0 {
		l.checkCancel()
	}
	if l.lineBreak(l.i - 1) {
		l.line++
		l.col = 1
		l.lineStart = l.i
	} else {
		l.col++
	}
	return r
}

type lexCancelled struct{ err error }

func (l *lexer) checkCancel() {
	if l.ctx != nil {
		if err := l.ctx.Err(); err != nil {
			panic(lexCancelled{err})
		}
	}
}

func (l *lexer) cancelCheck() func() {
	if l.ctx == nil {
		return nil
	}
	return l.checkCancel
}

func (l *lexer) runeString(start, end int) string {
	if l.ctx == nil {
		return string(l.src[start:end])
	}
	var out strings.Builder
	if l.srcASCII {
		out.Grow(end - start)
	} else {
		bytes := 0
		for i := start; i < end; i++ {
			if (i-start)&255 == 0 {
				l.checkCancel()
			}
			size := utf8.RuneLen(l.src[i])
			if size < 0 {
				size = utf8.RuneLen(utf8.RuneError)
			}
			bytes += size
		}
		l.checkCancel()
		out.Grow(bytes)
	}
	for i := start; i < end; i++ {
		if (i-start)&255 == 0 {
			l.checkCancel()
		}
		if r := l.src[i]; r >= 0 && r < utf8.RuneSelf {
			out.WriteByte(byte(r))
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (l *lexer) decodeRunes(src string) []rune {
	l.srcASCII = true
	count := 0
	for _, r := range src {
		count++
		if r >= utf8.RuneSelf {
			l.srcASCII = false
		}
		if count&255 == 0 {
			l.checkCancel()
		}
	}
	decoded := make([]rune, count)
	i := 0
	for _, r := range src {
		decoded[i] = r
		i++
		if i&255 == 0 {
			l.checkCancel()
		}
	}
	return decoded
}

func (l *lexer) errorf(pos grammar.Pos, format string, args ...any) {
	l.errs.add(pos, fmt.Sprintf(format, args...))
}

func (l *lexer) errorWithText(pos grammar.Pos, prefix, value string) {
	if l.ctx == nil {
		l.errs.addRange(pos, l.pos(), prefix+value)
		return
	}
	l.checkCancel()
	var b strings.Builder
	b.Grow(len(prefix) + len(value))
	b.WriteString(prefix)
	for i := 0; i < len(value); i += 256 {
		l.checkCancel()
		b.WriteString(value[i:min(i+256, len(value))])
	}
	l.errs.addRange(pos, l.pos(), b.String())
}

func lex(src string, errs *errorList) []token {
	toks, _ := lexContext(nil, src, errs)
	return toks
}

// lexContext discards all tokens on cancellation. A nil context is the ordinary,
// uncancellable path used by Parse and formatting.
func lexContext(ctx context.Context, src string, errs *errorList) (toks []token, err error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		defer func() {
			if x := recover(); x != nil {
				if cancelled, ok := x.(lexCancelled); ok {
					toks, err = nil, cancelled.err
				} else {
					panic(x)
				}
			}
		}()
	}
	l := &lexer{ctx: ctx, line: 1, col: 1, errs: errs}
	if ctx == nil {
		l.src = []rune(src)
	} else {
		// Count first so multibyte-heavy input does not retain a byte-sized
		// rune buffer. Both passes poll, including before the first token.
		l.src = l.decodeRunes(src)
	}
	l.checkCancel()
	var previous grammar.Pos
	for {
		l.checkCancel()
		t := l.next()
		t.end = l.pos()
		if t.lineComment != nil || len(t.comments) != 0 {
			cs := t.comments
			if t.lineComment != nil {
				cs = make([]*grammar.Comment, len(t.comments)+1)
				cs[0] = t.lineComment
				for i, c := range t.comments {
					if i&255 == 0 {
						l.checkCancel()
					}
					cs[i+1] = c
				}
			}
			t.source = collectCommentsWithCheck(previous, l.prev.opensList(), t, cs, l.cancelCheck())
		}
		toks = append(toks, t)
		l.prev = t
		l.track(t)
		if t.kind == tEOF {
			l.checkCancel()
			return toks, nil
		}
		previous = t.end
	}
}

// bracketAfter reports whether a "[" directly after the previous token is a bracket rather than a
// character class: after a name, a capture, ")", "]" or the "}" of a constructor or list literal. A character class after one of those
// is written with a space before it.
func (l *lexer) bracketAfter() bool {
	switch t := l.prev; t.kind {
	case tIdent, tCapture:
		return true
	case tPunct:
		return t.text == ")" || t.text == "]" || t.text == "}" && l.inAction
	}
	return false
}

// track follows the delimiters and actions of the tokens read so far.
func (l *lexer) track(t token) {
	if l.inAction && t.lineBefore && l.depth <= l.actionDepth {
		l.inAction = false
	}
	if t.kind != tPunct {
		return
	}
	switch t.text {
	case "(", "{", "[":
		l.depth++
	case ")", "}", "]":
		l.depth--
		if l.inAction && l.depth < l.actionDepth {
			l.inAction = false
		}
	case ";":
		if l.inAction && l.depth <= l.actionDepth {
			l.inAction = false
		}
	case "=>":
		if !l.inAction {
			l.inAction, l.actionDepth = true, l.depth
		}
	}
}

func isIdentStart(r rune) bool { return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
func isIdentPart(r rune) bool  { return isIdentStart(r) || r >= '0' && r <= '9' }

func (l *lexer) next() token {
	space, newline := l.skipSpace()
	for l.i < len(l.src) && !l.startsToken() {
		l.skipInvalid()
		space, newline = l.skipSpace()
	}
	t := token{
		pos: l.pos(), spaceBefore: space, lineBefore: newline, blankBefore: l.newlines >= 2,
		lineComment: l.lineComment, comments: l.comments,
	}
	l.ntoks++
	l.newlines, l.lineComment, l.comments = 0, nil, nil
	if l.i >= len(l.src) {
		t.kind = tEOF
		return t
	}
	r := l.src[l.i]
	switch {
	case r == '_' && l.peekAt(1) == '|' && l.peekAt(2) == '_':
		l.advance()
		l.advance()
		l.advance()
		t.kind, t.text = tPunct, "_|_"
	case r == '[' && l.peekAt(1) != ']' && !space && l.bracketAfter():
		// x[i], $x[i], f(x)[i] and x[i][j] index; T[U] applies a type.
		l.advance()
		t.kind, t.text = tPunct, "["
	case r == '[' && l.peekAt(1) != ']':
		t.kind, t.cc = tCharClass, l.charClass()
		t.cc.Pos = t.pos
	case isIdentStart(r):
		start := l.i
		for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
			l.advance()
		}
		t.kind, t.text = tIdent, l.runeString(start, l.i)
	case r == '-' && l.minimumInteger():
		// The minimum int is representable, but its positive magnitude is not.
		// Preserve it as one signed token so generic attribute values round trip.
		t.kind, t.text, t.num = tInt, minimumIntegerText, minimumIntegerValue
		for range minimumIntegerText {
			l.advance()
		}
	case unicode.IsDigit(r):
		t.kind = tInt
		t.text, t.num = l.decimal(t.pos, "number", "")
	case r == '"':
		t.kind, t.text = tString, l.string()
	case r == '$' && l.peekAt(1) != '$' && isIdentStart(l.peekAt(1)):
		l.advance()
		start := l.i
		for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
			l.advance()
		}
		t.kind, t.text = tCapture, l.runeString(start, l.i)
	case r == '$' && unicode.IsDigit(l.peekAt(1)):
		l.advance()
		t.kind = tIndex
		t.text, t.num = l.decimal(t.pos, "positional reference", "$")
	default:
		// startsToken guarantees that a punctuation token starts here.
		for _, p := range puncts {
			if l.hasPrefix(p) {
				for range []rune(p) {
					l.advance()
				}
				t.kind, t.text = tPunct, p
				return t
			}
		}
		panic(fmt.Sprintf("syntax: no token starts with %q", r))
	}
	return t
}

// decimal consumes a whole digit run so unsupported Unicode digits cannot be
// mistaken for a valid ASCII prefix. Atoi accepts ASCII decimal digits only
// and checks the implementation's int range. Invalid values are placeholders
// in a partial AST, always accompanied by a diagnostic.
func (l *lexer) decimal(pos grammar.Pos, kind, prefix string) (string, int) {
	start := l.i
	for l.i < len(l.src) && unicode.IsDigit(l.src[l.i]) {
		l.advance()
	}
	text := l.runeString(start, l.i)
	n, valid := l.decimalValue(text)
	if !valid {
		l.errorWithText(pos, "invalid "+kind+" "+prefix, text)
		return text, 0
	}
	return text, n
}

func (l *lexer) decimalValue(text string) (int, bool) {
	if l.ctx == nil {
		n, err := strconv.Atoi(text)
		return n, err == nil
	}
	if text == "" {
		return 0, false
	}
	limit := int(^uint(0) >> 1)
	n := 0
	for i := 0; i < len(text); i++ {
		if i&255 == 0 {
			l.checkCancel()
		}
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
		digit := int(text[i] - '0')
		if n > (limit-digit)/10 {
			return 0, false
		}
		n = n*10 + digit
	}
	return n, true
}

// punctStart holds the first characters of the punctuation tokens.
var punctStart = map[rune]bool{}

func init() {
	for _, p := range puncts {
		r, _ := utf8.DecodeRuneInString(p)
		punctStart[r] = true
	}
}

// startsToken reports whether a token starts at the current character, which is not white space
// or a comment.
func (l *lexer) startsToken() bool {
	r := l.src[l.i]
	return isIdentStart(r) || unicode.IsDigit(r) || r == '"' || punctStart[r]
}

// skipInvalid skips a run of characters that start no token, up to white space or the start of a
// token, and reports the run as one error.
func (l *lexer) skipInvalid() {
	pos, start := l.pos(), l.i
	for l.i < len(l.src) && !unicode.IsSpace(l.src[l.i]) && !l.startsToken() {
		l.advance()
	}
	if l.i-start == 1 {
		l.errs.addRange(pos, l.pos(), fmt.Sprintf("unexpected character %q", l.src[start]))
		return
	}
	s := string(l.src[start:min(l.i, start+10)])
	if l.i-start > 10 {
		s += "..."
	}
	l.errs.addRange(pos, l.pos(), fmt.Sprintf("unexpected characters %q", s))
}

// skipSpace skips white space and comments, recording the comments. It reports whether it
// skipped anything, and whether that included a line break.
func (l *lexer) skipSpace() (space, newline bool) {
	for l.i < len(l.src) {
		r := l.src[l.i]
		if l.lineBreak(l.i) {
			newline = true
			l.newlines++
		}
		if unicode.IsSpace(r) {
			space = true
			l.advance()
			continue
		}
		if r == '/' && (l.peekAt(1) == '/' || l.peekAt(1) == '*') {
			space = true
			c := &grammar.Comment{Pos: l.pos(), Blank: l.newlines >= 2}
			sameLine := l.newlines == 0 // the lines inside the comment do not count
			start := l.i
			block := l.peekAt(1) == '*'
			if block {
				l.advance()
				l.advance()
				depth := 1
				for l.i < len(l.src) && depth > 0 {
					switch {
					case l.hasPrefix("/*"):
						depth++
						l.advance()
						l.advance()
					case l.hasPrefix("*/"):
						depth--
						l.advance()
						l.advance()
					default:
						if l.lineBreak(l.i) {
							newline = true
							l.newlines++
						}
						l.advance()
					}
				}
				if depth != 0 {
					l.errs.addRange(c.Pos, l.pos(), "unterminated block comment")
				}
			} else {
				// A line comment ends before CR, LF, or CRLF.
				for l.i < len(l.src) && l.src[l.i] != '\n' && l.src[l.i] != '\r' {
					l.advance()
				}
			}
			c.End = l.pos()
			c.Text = l.runeString(start, l.i)
			ending := ""
			if !block && l.i < len(l.src) {
				ending = string(l.src[l.i : l.i+1])
				if l.src[l.i] == '\r' && l.peekAt(1) == '\n' {
					ending = "\r\n"
				}
			}
			c.Content = commentContentWithCheck(c.Text, ending, l.cancelCheck())
			if !block {
				end := len(c.Text)
				trimmed := 0
				for end > 0 {
					if trimmed&255 == 0 {
						l.checkCancel()
					}
					r, size := utf8.DecodeLastRuneInString(c.Text[:end])
					if !unicode.IsSpace(r) {
						break
					}
					end -= size
					trimmed++
				}
				c.Text = c.Text[:end]
				c.End.Col -= trimmed
			}
			if sameLine && l.ntoks > 0 && l.lineComment == nil && len(l.comments) == 0 {
				l.lineComment = c
			} else {
				l.comments = append(l.comments, c)
			}
			l.newlines = 0
			continue
		}
		break
	}
	return space, newline
}

func (l *lexer) hasPrefix(s string) bool {
	for k, r := range []rune(s) {
		if l.peekAt(k) != r {
			return false
		}
	}
	return true
}

// escape reads the escape sequence after a backslash.
// escapeError reports an error about the escape sequence that starts at start and ends at the
// current position.
func (l *lexer) escapeError(start grammar.Pos, format string, args ...any) {
	l.errs.addRange(start, l.pos(), fmt.Sprintf(format, args...))
}

func (l *lexer) escape() rune {
	start := l.pos()
	start.Col-- // the backslash, which is not a line break
	if l.i >= len(l.src) {
		l.escapeError(start, "unterminated escape")
		return 0
	}
	r := l.advance()
	switch r {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case 'f':
		return '\f'
	case 'v':
		return '\v'
	case 'a':
		return '\a'
	case 'b':
		return '\b'
	case '0':
		return 0
	case 'u':
		var hex strings.Builder
		if l.peekAt(0) == '{' {
			l.advance()
			for l.i < len(l.src) && l.src[l.i] != '}' {
				hex.WriteRune(l.advance())
			}
			if l.i < len(l.src) {
				l.advance()
			}
		} else {
			for k := 0; k < 4 && l.i < len(l.src); k++ {
				hex.WriteRune(l.advance())
			}
		}
		value := hex.String()
		n, valid := l.unicodeHexValue(value)
		if !valid || !utf8.ValidRune(rune(n)) {
			l.errorWithText(start, "invalid unicode escape \\u", value)
			return utf8.RuneError
		}
		return rune(n)
	}
	if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
		// Only punctuation stands for itself; an unknown letter or digit escape is almost
		// certainly a mistake (such as \x41) and would otherwise match the wrong character.
		l.escapeError(start, "unknown escape sequence \\%c", r)
	}
	return r
}

func (l *lexer) unicodeHexValue(text string) (uint64, bool) {
	if l.ctx == nil {
		n, err := strconv.ParseUint(text, 16, 32)
		return n, err == nil
	}
	if text == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(text); i++ {
		if i&255 == 0 {
			l.checkCancel()
		}
		var digit uint64
		switch c := text[i]; {
		case c >= '0' && c <= '9':
			digit = uint64(c - '0')
		case c >= 'a' && c <= 'f':
			digit = uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			digit = uint64(c-'A') + 10
		default:
			return 0, false
		}
		if n > (1<<32-1-digit)/16 {
			return 0, false
		}
		n = n*16 + digit
	}
	return n, true
}

func (l *lexer) string() string {
	start := l.pos()
	l.advance() // "
	var b strings.Builder
	for {
		if l.i >= len(l.src) || l.src[l.i] == '\n' {
			l.errorf(start, "unterminated string")
			return b.String()
		}
		r := l.advance()
		switch r {
		case '"':
			return b.String()
		case '\\':
			b.WriteRune(l.escape())
		default:
			b.WriteRune(r)
		}
	}
}

func (l *lexer) charClass() *grammar.CharClass {
	start := l.pos()
	l.advance() // [
	cc := &grammar.CharClass{SourceInfo: grammar.SourceInfo{Pos: start}}
	if l.peekAt(0) == '^' {
		l.advance()
		cc.Negated = true
	}
	read := func() (rune, bool) {
		if l.i >= len(l.src) || l.src[l.i] == '\n' {
			return 0, false
		}
		r := l.advance()
		if r == '\\' {
			return l.escape(), true
		}
		return r, true
	}
	for {
		if l.i >= len(l.src) || l.src[l.i] == '\n' {
			l.errorf(start, "unterminated character class")
			return cc
		}
		if l.src[l.i] == ']' {
			l.advance()
			if len(cc.Ranges) == 0 {
				l.errorf(start, "empty character class")
			}
			return cc
		}
		lo, _ := read()
		hi := lo
		if l.peekAt(0) == '-' && l.peekAt(1) != ']' {
			l.advance()
			var ok bool
			if hi, ok = read(); !ok {
				continue
			}
			if hi < lo {
				l.errorf(start, "invalid range %q-%q", lo, hi)
			}
		}
		cc.Ranges = append(cc.Ranges, grammar.CharRange{Lo: lo, Hi: hi})
	}
}

var minimumIntegerValue = -int(^uint(0)>>1) - 1
var minimumIntegerText = strconv.Itoa(minimumIntegerValue)

func (l *lexer) minimumInteger() bool {
	if len(l.src)-l.i < len(minimumIntegerText) {
		return false
	}
	for i, r := range minimumIntegerText {
		if l.src[l.i+i] != r {
			return false
		}
	}
	return !isIdentPart(l.peekAt(len(minimumIntegerText)))
}
