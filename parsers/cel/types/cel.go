package types

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// trivia skips the whitespace and comments at the start of s that the grammar allows between the tokens
// of a literal or a name, and returns the rest.
func trivia(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n\f")
		if !strings.HasPrefix(s, "//") {
			return s
		}
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return ""
		}
		s = s[i:]
	}
}

// compact removes the whitespace and comments that the grammar allows between the tokens of a name.
func compact(s string) string {
	if !strings.ContainsAny(s, " \t\r\n\f/") {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		t := trivia(s)
		if len(t) == 0 {
			break
		}
		s = t
		b.WriteByte(s[0])
		s = s[1:]
	}
	return b.String()
}

// sign splits the text of a numeric literal into its sign ("" or "-") and the rest.
func sign(text string) (string, string) {
	if strings.HasPrefix(text, "-") {
		return "-", trivia(text[1:])
	}
	return "", text
}

// Name returns the name of an identifier, a function or a message type (a dotted name, as a.b.C, for a message),
// without the whitespace and comments that may be in the text, and without the leading dot that Rooted tells of.
func (n *Ident) Name() string {
	s := compact(n.Value)
	if strings.HasPrefix(s, ".") {
		return s[1:]
	}
	return s
}

// Rooted reports whether the identifier starts with a dot, which resolves it in the root scope.
func (n *Ident) Rooted() bool { return strings.HasPrefix(n.Value, ".") }

// Value returns the name of a field, without the backquotes of an escaped name.
func (n *Name) Value() string {
	if strings.HasPrefix(n.TNode.Value, "`") {
		return n.TNode.Value[1 : len(n.TNode.Value)-1]
	}
	return n.TNode.Value
}

// Value returns the integer. A value outside the range of int64 is an error.
func (n *IntLit) Value() (int64, error) {
	sg, t := sign(n.TNode.Value)
	base := 10
	if strings.HasPrefix(t, "0x") {
		base, t = 16, t[2:]
	}
	i, err := strconv.ParseInt(sg+t, base, 64)
	if err != nil {
		return 0, errors.New("invalid int literal")
	}
	return i, nil
}

// Value returns the unsigned integer. A value outside the range of uint64 is an error.
func (n *UintLit) Value() (uint64, error) {
	t := n.TNode.Value[:len(n.TNode.Value)-1]
	base := 10
	if strings.HasPrefix(t, "0x") {
		base, t = 16, t[2:]
	}
	i, err := strconv.ParseUint(t, base, 64)
	if err != nil {
		return 0, errors.New("invalid uint literal")
	}
	return i, nil
}

// Value returns the floating-point number. A value too large for float64 is an error.
func (n *DoubleLit) Value() (float64, error) {
	sg, t := sign(n.TNode.Value)
	f, err := strconv.ParseFloat(sg+t, 64)
	if err != nil {
		return 0, errors.New("invalid double literal")
	}
	return f, nil
}

// Value returns the boolean.
func (n *BoolLit) Value() bool { return n.TNode.Value == "true" }

// Value returns the string the literal denotes, with its escape sequences decoded.
func (n *StringLit) Value() string {
	s, _ := unquote(n.TNode.Value, false)
	return s
}

// Value returns the bytes the literal denotes. The UTF-8 encoding of the characters of the literal and the
// values of its octal and hexadecimal escapes make up the bytes.
func (n *BytesLit) Value() []byte {
	s, _ := unquote(n.TNode.Value[1:], true)
	return []byte(s)
}

// unquote decodes a string literal, with its optional r prefix, as cel-go does: line breaks (CRLF and CR)
// become LF, a raw literal keeps its content, and otherwise the escape sequences are decoded. In bytes, octal
// and hexadecimal escapes are single bytes, and \u and \U are errors; in strings, they are code points.
func unquote(lit string, isBytes bool) (string, error) {
	if strings.ContainsRune(lit, '\r') {
		lit = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(lit)
	}
	raw := false
	if lit[0] == 'r' || lit[0] == 'R' {
		raw, lit = true, lit[1:]
	}
	q := 1
	if len(lit) >= 6 && lit[1] == lit[0] && lit[2] == lit[0] {
		q = 3
	}
	body := lit[q : len(lit)-q]
	if raw || strings.IndexByte(body, '\\') < 0 {
		return body, nil
	}
	buf := make([]byte, 0, len(body))
	for len(body) > 0 {
		c := body[0]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(body)
			buf = utf8.AppendRune(buf, r)
			body = body[size:]
			continue
		}
		if c != '\\' {
			buf = append(buf, c)
			body = body[1:]
			continue
		}
		var err error
		buf, body, err = unescape(buf, body, isBytes)
		if err != nil {
			return "", err
		}
	}
	return string(buf), nil
}

// unescape decodes the escape sequence at the start of s, which begins with a backslash, and appends it to buf.
func unescape(buf []byte, s string, isBytes bool) ([]byte, string, error) {
	if len(s) < 2 {
		return nil, "", errors.New("unable to unescape string, found '\\' as last character")
	}
	c, s := s[1], s[2:]
	switch c {
	case 'a':
		return append(buf, '\a'), s, nil
	case 'b':
		return append(buf, '\b'), s, nil
	case 'f':
		return append(buf, '\f'), s, nil
	case 'n':
		return append(buf, '\n'), s, nil
	case 'r':
		return append(buf, '\r'), s, nil
	case 't':
		return append(buf, '\t'), s, nil
	case 'v':
		return append(buf, '\v'), s, nil
	case '\\', '\'', '"', '`', '?':
		return append(buf, c), s, nil
	case 'x', 'X', 'u', 'U':
		n := 2
		switch c {
		case 'u':
			n = 4
		case 'U':
			n = 8
		}
		if (c == 'u' || c == 'U') && isBytes {
			return nil, "", errors.New("unable to unescape string")
		}
		if len(s) < n {
			return nil, "", errors.New("unable to unescape string")
		}
		v, err := strconv.ParseUint(s[:n], 16, 32)
		if err != nil || s[0] == '+' || s[0] == '-' {
			return nil, "", errors.New("unable to unescape string")
		}
		s = s[n:]
		if isBytes && (c == 'x' || c == 'X') {
			return append(buf, byte(v)), s, nil
		}
		if !utf8.ValidRune(rune(v)) {
			return nil, "", errors.New("invalid unicode code point")
		}
		return utf8.AppendRune(buf, rune(v)), s, nil
	case '0', '1', '2', '3':
		if len(s) < 2 || s[0] < '0' || s[0] > '7' || s[1] < '0' || s[1] > '7' {
			return nil, "", errors.New("unable to unescape octal sequence in string")
		}
		v := int(c-'0')*64 + int(s[0]-'0')*8 + int(s[1]-'0')
		s = s[2:]
		if isBytes {
			return append(buf, byte(v)), s, nil
		}
		return utf8.AppendRune(buf, rune(v)), s, nil
	}
	return nil, "", errors.New("unable to unescape string")
}

// Text returns the text of the literal without the whitespace and comments between its sign and its digits.
func (n *IntLit) Text() string { return compact(n.TNode.Value) }

// Text returns the text of the literal without the whitespace and comments between its sign and its digits.
func (n *DoubleLit) Text() string { return compact(n.TNode.Value) }
