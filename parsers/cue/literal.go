package cue

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Unquote returns the string that a string or bytes literal denotes: the text of a String without
// interpolation, quotes, hashes, escape sequences and multi-line indentation interpreted as
// cuelang.org/go/cue/literal does. A bytes literal ('...') gives its bytes as a Go string. A fragment of an
// interpolation, which is a String too, is an error.
func (s *String) Unquote() (string, error) { return unquoteLit(s.Text) }

var (
	errSyntax                = errors.New("invalid syntax")
	errMissingOpeningNewline = errors.New("invalid string: opening quote of multiline string must be followed by newline")
	errMissingClosingNewline = errors.New("invalid string: closing quote of multiline string must follow a newline")
	errUnmatchedQuote        = errors.New("invalid string: unmatched quote")
	errSurrogate             = errors.New("unmatched surrogate pair")
	errInvalidUTF8           = errors.New("invalid UTF-8 encoding")
	errEscapedLastNewline    = errors.New("last newline of multiline string cannot be escaped")
)

func invalidWhitespaceError(expected, actual string) error {
	return fmt.Errorf("invalid string: non-matching whitespace for multiline string (expected %q, got %q)", expected, actual)
}

// The decoding of string literals is adapted from cuelang.org/go/cue/literal (Apache License 2.0,
// Copyright The CUE Authors).

func unquoteLit(s string) (string, error) {
	info, nStart, _, err := parseQuotes(s, s)
	if err != nil {
		return "", err
	}
	return info.unquote(s[nStart:])
}

// quoteInfo describes the quotes of a string literal.
type quoteInfo struct {
	quote      string
	whitespace string
	numHash    int
	multiline  bool
	char       byte
	numChar    byte
}

// parseQuotes checks that the opening quotes in start match the closing quotes in end, and returns the kind of
// quotes and the number of bytes of the opening and of the closing.
func parseQuotes(start, end string) (q quoteInfo, nStart, nEnd int, err error) {
	for i, c := range start {
		if c != '#' {
			break
		}
		q.numHash = i + 1
	}
	s := start[q.numHash:]
	if len(s) == 0 {
		return quoteInfo{}, 0, 0, errSyntax
	}
	switch s[0] {
	case '"', '\'':
		q.char = s[0]
		if len(s) > 3 && s[1] == q.char && s[2] == q.char && s[3] != '#' {
			switch s[3] {
			case '\n':
				q.quote = start[:3+q.numHash]
			case '\r':
				if len(s) > 4 && s[4] == '\n' {
					q.quote = start[:4+q.numHash]
					break
				}
				fallthrough
			default:
				return q, 0, 0, errMissingOpeningNewline
			}
			q.multiline = true
			q.numChar = 3
			nStart = len(q.quote) + 1 // add whitespace later
		} else {
			q.quote = start[:1+q.numHash]
			q.numChar = 1
			nStart = len(q.quote)
		}
	default:
		return q, 0, 0, errSyntax
	}
	quote := start[:int(q.numChar)+q.numHash]
	for i := 0; i < len(quote); i++ {
		if j := len(end) - i - 1; j < 0 || quote[i] != end[j] {
			return q, 0, 0, errUnmatchedQuote
		}
	}
	if q.multiline {
		i := len(end) - len(quote)
		hasNewline := false
		for i > 0 {
			r, size := utf8.DecodeLastRuneInString(end[:i])
			if r == '\n' || !unicode.IsSpace(r) {
				hasNewline = r == '\n'
				break
			}
			i -= size
		}
		if !hasNewline {
			return q, 0, 0, errMissingClosingNewline
		}
		q.whitespace = end[i : len(end)-len(quote)]
		if len(start) > nStart && start[nStart] != '\n' {
			if !strings.HasPrefix(start[nStart:], q.whitespace) {
				actual := start[nStart:]
				actual = actual[:len(actual)-len(strings.TrimLeft(actual, " \t"))]
				return q, 0, 0, invalidWhitespaceError(q.whitespace, actual)
			}
			nStart += len(q.whitespace)
		}
	}
	return q, nStart, int(q.numChar) + q.numHash, nil
}

// unquote decodes the body of a literal (without the opening quotes), up to its closing quotes.
func (q quoteInfo) unquote(s string) (string, error) {
	if len(s) > 0 && !q.multiline {
		if strings.ContainsRune(s, '\n') {
			return "", errSyntax
		}
		if s[len(s)-1] == q.char && q.numHash == 0 {
			if s := s[:len(s)-1]; isSimple(s, rune(q.char)) {
				return s, nil
			}
		}
	}
	buf := make([]byte, 0, 3*len(s)/2)
	if q.multiline && hasClosingDelimPrefix(s, q) && len(s) > int(q.numChar)+q.numHash {
		return "", errSyntax
	}
	stripNL := false
	wasEscapedNewline := false
	for len(s) > 0 {
		switch s[0] {
		case '\r':
			s = s[1:]
			wasEscapedNewline = false
			continue
		case '\n':
			var err error
			s, err = skipWhitespaceAfterNewline(s[1:], q)
			if err != nil {
				return "", err
			}
			if q.multiline && hasClosingDelimPrefix(s, q) && len(s) > int(q.numChar)+q.numHash {
				return "", errSyntax
			}
			stripNL = true
			wasEscapedNewline = false
			buf = append(buf, '\n')
			continue
		}
		c, multibyte, ss, err := unquoteChar(s, q)
		if surHigh <= c && c < surEnd {
			if c >= surLow {
				return "", errSurrogate
			}
			var cl rune
			cl, _, ss, err = unquoteChar(ss, q)
			if cl < surLow || surEnd <= cl {
				return "", errSurrogate
			}
			c = 0x10000 + (c-surHigh)*0x400 + (cl - surLow)
		}
		if err != nil {
			return "", err
		}
		s = ss
		if c < 0 {
			switch c {
			case escapedNewline:
				var err error
				s, err = skipWhitespaceAfterNewline(s, q)
				if err != nil {
					return "", err
				}
				wasEscapedNewline = true
				continue
			case terminatedByQuote:
				if wasEscapedNewline {
					return "", errEscapedLastNewline
				}
				if stripNL {
					buf = buf[:len(buf)-1] // the last newline comes from the closing quote
				}
			case terminatedByExpr:
				// a fragment of an interpolation
				return "", errSyntax
			}
			return string(buf), nil
		}
		stripNL = false
		wasEscapedNewline = false
		if !multibyte {
			buf = append(buf, byte(c))
		} else {
			buf = utf8.AppendRune(buf, c)
		}
	}
	return "", errUnmatchedQuote
}

func hasClosingDelimPrefix(s string, q quoteInfo) bool {
	if len(s) < int(q.numChar)+q.numHash {
		return false
	}
	for i := range int(q.numChar) {
		if s[i] != q.char {
			return false
		}
	}
	for i := range q.numHash {
		if s[int(q.numChar)+i] != '#' {
			return false
		}
	}
	return true
}

func skipWhitespaceAfterNewline(s string, q quoteInfo) (string, error) {
	switch {
	case strings.HasPrefix(s, q.whitespace):
		s = s[len(q.whitespace):]
	case strings.HasPrefix(s, "\n"):
	case strings.HasPrefix(s, "\r\n"):
	default:
		actual, _, _ := strings.Cut(s, "\n")
		actual = actual[:len(actual)-len(strings.TrimLeft(actual, " \t"))]
		return "", invalidWhitespaceError(q.whitespace, actual)
	}
	return s, nil
}

const (
	surHigh = 0xD800
	surLow  = 0xDC00
	surEnd  = 0xE000
)

func isSimple(s string, quote rune) bool {
	for _, r := range s {
		if r == quote || r == '\\' || r == 0 || r == utf8.RuneError {
			return false
		}
		if surHigh <= r && r < surEnd {
			return false
		}
	}
	return true
}

const (
	terminatedByQuote = rune(-1)
	terminatedByExpr  = rune(-2)
	escapedNewline    = rune(-3)
)

// unquoteChar decodes the first character or byte of s: its value (or one of the negative constants above), whether it
// takes several bytes in UTF-8, the rest of s and an error.
func unquoteChar(s string, info quoteInfo) (value rune, multibyte bool, tail string, err error) {
	switch c := s[0]; {
	case c == info.char && info.char != 0:
		for i := 1; byte(i) < info.numChar; i++ {
			if i >= len(s) || s[i] != info.char {
				return rune(info.char), false, s[1:], nil
			}
		}
		for i := 0; i < info.numHash; i++ {
			if i+int(info.numChar) >= len(s) || s[i+int(info.numChar)] != '#' {
				return rune(info.char), false, s[1:], nil
			}
		}
		if ln := int(info.numChar) + info.numHash; len(s) != ln {
			if info.numChar == 3 {
				return rune(info.char), false, s[1:], nil
			}
			return 0, false, s[ln:], errSyntax
		}
		return terminatedByQuote, false, "", nil
	case c >= utf8.RuneSelf:
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			return 0, false, s, errInvalidUTF8
		}
		return r, true, s[size:], nil
	case c != '\\':
		if c == 0 {
			return 0, false, s, errSyntax
		}
		return rune(s[0]), false, s[1:], nil
	}
	if len(s) <= 1+info.numHash {
		return '\\', false, s[1:], nil
	}
	for i := 1; i <= info.numHash && i < len(s); i++ {
		if s[i] != '#' {
			return '\\', false, s[1:], nil
		}
	}
	c := s[1+info.numHash]
	s = s[2+info.numHash:]
	switch c {
	case 'a':
		value = '\a'
	case 'b':
		value = '\b'
	case 'f':
		value = '\f'
	case 'n':
		value = '\n'
	case 'r':
		value = '\r'
	case 't':
		value = '\t'
	case 'v':
		value = '\v'
	case '/':
		value = '/'
	case 'x', 'u', 'U':
		n := 0
		switch c {
		case 'x':
			n = 2
		case 'u':
			n = 4
		case 'U':
			n = 8
		}
		var v rune
		if len(s) < n {
			err = errSyntax
			return
		}
		for j := 0; j < n; j++ {
			x, ok := unhex(s[j])
			if !ok {
				err = errSyntax
				return
			}
			v = v<<4 | x
		}
		s = s[n:]
		if c == 'x' {
			if info.char == '"' {
				err = errSyntax
				return
			}
			value = v // a byte, possibly not UTF-8
			break
		}
		if v > utf8.MaxRune {
			err = errSyntax
			return
		}
		value = v
		multibyte = true
	case '0', '1', '2', '3', '4', '5', '6', '7':
		if info.char == '"' {
			err = errSyntax
			return
		}
		v := rune(c) - '0'
		if len(s) < 2 {
			err = errSyntax
			return
		}
		for j := range 2 {
			x := rune(s[j]) - '0'
			if x < 0 || x > 7 {
				err = errSyntax
				return
			}
			v = (v << 3) | x
		}
		s = s[2:]
		if v > 255 {
			err = errSyntax
			return
		}
		value = v
	case '\\':
		value = '\\'
	case '\'', '"':
		if c != info.char {
			err = errSyntax
			return
		}
		value = rune(c)
	case '(':
		if s != "" {
			return 0, false, s, errSyntax
		}
		value = terminatedByExpr
	case '\r':
		if len(s) == 0 || s[0] != '\n' {
			err = errSyntax
			return
		}
		s = s[1:]
		value = escapedNewline
	case '\n':
		value = escapedNewline
	default:
		err = errSyntax
		return
	}
	tail = s
	return
}

func unhex(b byte) (v rune, ok bool) {
	c := rune(b)
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return
}

// isValidImport reports whether an import path literal denotes a valid path (parser.isValidImport): graphic
// characters other than a set that paths cannot contain, ignoring the package qualifier after the last ":".
func isValidImport(lit string) bool {
	const illegalChars = `!"#$%&'()*,:;<=>?[\]^{|}` + "`�"
	s, _ := unquoteLit(lit)
	if p := strings.LastIndexByte(s, ':'); p >= 0 {
		s = s[:p]
	}
	for _, r := range s {
		if !unicode.IsGraphic(r) || unicode.IsSpace(r) || strings.ContainsRune(illegalChars, r) {
			return false
		}
	}
	return s != ""
}

// Value returns the integer that the literal denotes: its digits with underscores removed, in decimal,
// hexadecimal (0x), octal (0o) or binary (0b), or with a multiplier (1K is 1,000, 1Ki is 1,024, and M, G, T, P
// likewise). A fraction in front of a multiplier is allowed where the product is an integer (1.5G is
// 1,500,000,000); otherwise (1.3Ki is 1,331.2) the value is an error, as it is for cue/literal.
func (n *Int) Value() (*big.Int, error) {
	t := strings.ReplaceAll(n.Text, "_", "")
	var base int
	switch {
	case len(t) > 2 && t[0] == '0' && (t[1] == 'x' || t[1] == 'X'):
		base, t = 16, t[2:]
	case len(t) > 2 && t[0] == '0' && t[1] == 'b':
		base, t = 2, t[2:]
	case len(t) > 2 && t[0] == '0' && t[1] == 'o':
		base, t = 8, t[2:]
	}
	if base != 0 {
		v, ok := new(big.Int).SetString(t, base)
		if !ok {
			return nil, fmt.Errorf("cue: invalid integer %s", n.Text)
		}
		return v, nil
	}
	// A decimal, possibly with a fraction and a multiplier.
	mult := big.NewInt(1)
	if m := t[len(t)-1]; strings.IndexByte("KMGTP", m) >= 0 || t[len(t)-1] == 'i' {
		t = t[:len(t)-1]
		binary := false
		if m == 'i' {
			binary = true
			m = t[len(t)-1]
			t = t[:len(t)-1]
		}
		base := int64(1000)
		if binary {
			base = 1024
		}
		exp := strings.IndexByte("KMGTP", m) + 1
		mult = new(big.Int).Exp(big.NewInt(base), big.NewInt(int64(exp)), nil)
	}
	r, ok := new(big.Rat).SetString(t)
	if !ok {
		return nil, fmt.Errorf("cue: invalid integer %s", n.Text)
	}
	r.Mul(r, new(big.Rat).SetInt(mult))
	if !r.IsInt() {
		return nil, fmt.Errorf("cue: number %s cannot be represented as an int", n.Text)
	}
	return new(big.Int).Set(r.Num()), nil
}

// Int64 returns the integer as an int64; a value out of the range of int64 is an error.
func (n *Int) Int64() (int64, error) {
	v, err := n.Value()
	if err != nil {
		return 0, err
	}
	if !v.IsInt64() {
		return 0, fmt.Errorf("cue: integer %s out of the range of int64", n.Text)
	}
	return v.Int64(), nil
}

// Rat returns the exact value of the number: CUE floats are decimal numbers of any precision.
func (f *Float) Rat() (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(strings.ReplaceAll(f.Text, "_", ""))
	if !ok {
		return nil, fmt.Errorf("cue: invalid float %s", f.Text)
	}
	return r, nil
}

// Float64 returns the number as a float64; a value out of its range is an error.
func (f *Float) Float64() (float64, error) {
	v, err := strconv.ParseFloat(strings.ReplaceAll(f.Text, "_", ""), 64)
	if err != nil {
		return 0, fmt.Errorf("cue: float %s: %w", f.Text, err.(*strconv.NumError).Err)
	}
	return v, nil
}

// Value returns the boolean.
func (b *Bool) Value() bool { return b.Text == "true" }

// IsDefinition reports whether the identifier names a definition (#Name or _#Name).
func (id *Ident) IsDefinition() bool {
	return strings.HasPrefix(id.Text, "#") || strings.HasPrefix(id.Text, "_#")
}

// IsHidden reports whether the identifier names a hidden field (_name or _#Name).
func (id *Ident) IsHidden() bool { return strings.HasPrefix(id.Text, "_") && id.Text != "_" }

// Split returns the name and the body of an attribute: for @go(Field), "go" and "Field".
func (a *Attribute) Split() (name, body string) {
	name, body, ok := strings.Cut(a.Text, "(")
	if !ok || !strings.HasPrefix(a.Text, "@") || !strings.HasSuffix(a.Text, ")") {
		return "", ""
	}
	return name[1:], body[:len(body)-1]
}
