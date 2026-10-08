// Package syntax parses PEGO source code into a grammar AST.
package syntax

import (
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
	tCharClass // (?...)
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
	"_|_", "^^", "$$", "--", "->", "=>", "==", "!=", "<=", ">=", "&&", "||", "[]",
	":", "=", "/", "(", ")", "{", "}", "[", "]", ",", ".", "*", "+", "?", "&", "!",
	"@", "-", "^", "$", "#", "|", "<", ">", "%",
}

type lexer struct {
	src  []rune
	i    int
	line int
	col  int
	errs *errorList

	// State of the white space and comments before the next token.
	ntoks       int // tokens read so far
	newlines    int // line breaks since the previous token or comment
	lineComment *grammar.Comment
	comments    []*grammar.Comment
}

func (l *lexer) pos() grammar.Pos { return grammar.Pos{Line: l.line, Col: l.col} }

func (l *lexer) peekAt(k int) rune {
	if l.i+k < len(l.src) {
		return l.src[l.i+k]
	}
	return 0
}

func (l *lexer) advance() rune {
	r := l.src[l.i]
	l.i++
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

func (l *lexer) errorf(pos grammar.Pos, format string, args ...any) {
	l.errs.add(pos, fmt.Sprintf(format, args...))
}

func lex(src string, errs *errorList) []token {
	l := &lexer{src: []rune(src), line: 1, col: 1, errs: errs}
	var toks []token
	for {
		t := l.next()
		t.end = l.pos()
		toks = append(toks, t)
		if t.kind == tEOF {
			return toks
		}
	}
}

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isIdentPart(r rune) bool  { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

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
	case isIdentStart(r):
		start := l.i
		for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
			l.advance()
		}
		t.kind, t.text = tIdent, string(l.src[start:l.i])
	case unicode.IsDigit(r):
		start := l.i
		for l.i < len(l.src) && unicode.IsDigit(l.src[l.i]) {
			l.advance()
		}
		t.kind, t.text = tInt, string(l.src[start:l.i])
		n, err := strconv.Atoi(t.text)
		if err != nil {
			l.errorf(t.pos, "invalid number %s", t.text)
		}
		t.num = n
	case r == '"':
		t.kind, t.text = tString, l.string()
	case r == '(' && l.peekAt(1) == '?':
		t.kind, t.cc = tCharClass, l.charClass()
		t.cc.Pos = t.pos
	case r == '$' && l.peekAt(1) != '$' && isIdentStart(l.peekAt(1)):
		l.advance()
		start := l.i
		for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
			l.advance()
		}
		t.kind, t.text = tCapture, string(l.src[start:l.i])
	case r == '$' && unicode.IsDigit(l.peekAt(1)):
		l.advance()
		start := l.i
		for l.i < len(l.src) && unicode.IsDigit(l.src[l.i]) {
			l.advance()
		}
		t.kind = tIndex
		t.num, _ = strconv.Atoi(string(l.src[start:l.i]))
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
		l.errorf(pos, "unexpected character %q", l.src[start])
		return
	}
	s := string(l.src[start:min(l.i, start+10)])
	if l.i-start > 10 {
		s += "..."
	}
	l.errorf(pos, "unexpected characters %q", s)
}

// skipSpace skips white space and comments, recording the comments. It reports whether it
// skipped anything, and whether that included a line break.
func (l *lexer) skipSpace() (space, newline bool) {
	for l.i < len(l.src) {
		r := l.src[l.i]
		if r == '\n' {
			newline = true
			l.newlines++
		}
		if unicode.IsSpace(r) {
			space = true
			l.advance()
			continue
		}
		if r == '/' && l.peekAt(1) == '/' {
			space = true
			c := &grammar.Comment{Pos: l.pos(), Blank: l.newlines >= 2}
			start := l.i
			for l.i < len(l.src) && l.src[l.i] != '\n' {
				l.advance()
			}
			c.Text = strings.TrimRightFunc(string(l.src[start:l.i]), unicode.IsSpace)
			if l.newlines == 0 && l.ntoks > 0 && l.lineComment == nil && len(l.comments) == 0 {
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
func (l *lexer) escape() rune {
	pos := l.pos()
	if l.i >= len(l.src) {
		l.errorf(pos, "unterminated escape")
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
		n, err := strconv.ParseUint(hex.String(), 16, 32)
		if err != nil {
			l.errorf(pos, "invalid unicode escape \\u%s", hex.String())
		}
		return rune(n)
	}
	if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
		// Only punctuation stands for itself; an unknown letter or digit escape is almost
		// certainly a mistake (such as \x41) and would otherwise match the wrong character.
		l.errorf(pos, "unknown escape sequence \\%c", r)
	}
	return r
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
	l.advance() // (
	l.advance() // ?
	cc := &grammar.CharClass{}
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
		if l.src[l.i] == ')' {
			l.advance()
			if len(cc.Ranges) == 0 {
				l.errorf(start, "empty character class")
			}
			return cc
		}
		lo, _ := read()
		hi := lo
		if l.peekAt(0) == '-' && l.peekAt(1) != ')' {
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
