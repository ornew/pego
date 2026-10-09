package postgresql

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// NAMEDATALEN of PostgreSQL: names are cut to at most NAMEDATALEN-1 = 63 bytes.
const maxNameBytes = 63

// Value returns the name that the identifier denotes, as PostgreSQL's scanner makes it: an unquoted
// identifier or keyword is folded to lower case (ASCII letters only), a quoted identifier is unquoted ("" is
// a quote), a U&"..." identifier has its Unicode escapes decoded, and a name longer than 63 bytes is cut
// at a character boundary. The error is that of an invalid Unicode escape.
func (id *Ident) Value() (string, error) {
	t := id.Text
	var s string
	switch {
	case strings.HasPrefix(t, `"`):
		s = strings.ReplaceAll(t[1:len(t)-1], `""`, `"`)
	case len(t) > 2 && (t[0] == 'U' || t[0] == 'u') && t[1] == '&':
		body, esc := splitUescape(t[2:])
		body = strings.ReplaceAll(body[1:len(body)-1], `""`, `"`)
		var err error
		if s, err = unicodeEscapes(body, esc); err != nil {
			return "", err
		}
	default:
		s = lowerASCII(t)
	}
	return truncateName(s), nil
}

// MustValue is Value for an identifier that the parser accepted: an invalid Unicode escape, the only error
// of Value, is checked by the parser itself (it never accepts one), so MustValue does not fail.
func (id *Ident) MustValue() string {
	s, err := id.Value()
	if err != nil {
		return id.Text
	}
	return s
}

// Quoted reports whether the identifier was written in double quotes (also as U&"...").
func (id *Ident) Quoted() bool {
	return strings.HasPrefix(id.Text, `"`) || len(id.Text) > 2 && (id.Text[0] == 'U' || id.Text[0] == 'u') && id.Text[1] == '&'
}

func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if 'A' <= b[j] && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// truncateName cuts a name to 63 bytes without splitting a character.
func truncateName(s string) string {
	if len(s) <= maxNameBytes {
		return s
	}
	n := maxNameBytes
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// splitUescape separates the quoted body of a U&"..." identifier from a following UESCAPE 'c'. The escape
// character is '\' by default. text starts at the opening quote.
func splitUescape(text string) (body string, esc byte) {
	esc = '\\'
	i := 1
	for i < len(text) {
		if text[i] == '"' {
			if i+1 < len(text) && text[i+1] == '"' {
				i += 2
				continue
			}
			break
		}
		i++
	}
	body = text[:i+1]
	rest := skipWhite(text, i+1)
	if rest+7 <= len(text) && strings.EqualFold(text[rest:rest+7], "uescape") {
		r := skipWhite(text, rest+7)
		if r+1 < len(text) {
			esc = text[r+1]
		}
	}
	return body, esc
}

// unicodeEscapes decodes the escapes of a U&'...' or U&"..." literal body: \XXXX, \+XXXXXX and the
// escape character twice.
func unicodeEscapes(s string, esc byte) (string, error) {
	var b strings.Builder
	var hi rune
	for i := 0; i < len(s); {
		c := s[i]
		if c != esc {
			if hi != 0 {
				return "", errors.New("invalid Unicode surrogate pair")
			}
			r, n := utf8.DecodeRuneInString(s[i:])
			b.WriteRune(r)
			i += n
			continue
		}
		if i+1 < len(s) && s[i+1] == esc {
			if hi != 0 {
				return "", errors.New("invalid Unicode surrogate pair")
			}
			b.WriteByte(esc)
			i += 2
			continue
		}
		var digits string
		switch {
		case i+1 < len(s) && s[i+1] == '+' && i+8 <= len(s):
			digits = s[i+2 : i+8]
			i += 8
		case i+5 <= len(s):
			digits = s[i+1 : i+5]
			i += 5
		default:
			return "", errors.New("invalid Unicode escape")
		}
		v, err := strconv.ParseUint(digits, 16, 32)
		if err != nil {
			return "", errors.New("invalid Unicode escape")
		}
		r := rune(v)
		switch {
		case r >= 0xD800 && r <= 0xDBFF:
			if hi != 0 {
				return "", errors.New("invalid Unicode surrogate pair")
			}
			hi = r
			continue
		case r >= 0xDC00 && r <= 0xDFFF:
			if hi == 0 {
				return "", errors.New("invalid Unicode surrogate pair")
			}
			r = 0x10000 + (hi-0xD800)<<10 + (r - 0xDC00)
			hi = 0
		default:
			if hi != 0 {
				return "", errors.New("invalid Unicode surrogate pair")
			}
		}
		if r == 0 || r > 0x10FFFF {
			return "", errors.New("invalid Unicode escape value")
		}
		b.WriteRune(r)
	}
	if hi != 0 {
		return "", errors.New("invalid Unicode surrogate pair")
	}
	return b.String(), nil
}

// Value returns the string that the literal denotes: ” is a quote, an E'...' literal has its backslash
// escapes decoded, a U&'...' literal its Unicode escapes, a dollar-quoted string is its text, and parts that
// are separated by white space with a newline are concatenated. The error is that of an invalid escape or
// of a result that is not valid UTF-8 (or contains NUL).
func (c *Sconst) Value() (string, error) {
	t := c.Text
	var out strings.Builder
	for i := 0; i < len(t); {
		ch := t[i]
		switch {
		case ch == '\'':
			j, s := readQuoted(t, i+1)
			out.WriteString(s)
			i = j
		case (ch == 'e' || ch == 'E') && i+1 < len(t) && t[i+1] == '\'':
			j, s, err := readEscaped(t, i+2)
			if err != nil {
				return "", err
			}
			out.WriteString(s)
			i = j
		case (ch == 'u' || ch == 'U') && i+2 < len(t) && t[i+1] == '&' && t[i+2] == '\'':
			// the parts of a U& string, then an optional UESCAPE 'c'
			j := i + 2
			esc := byte('\\')
			var sb strings.Builder
			for {
				k, s := readQuoted(t, j+1)
				sb.WriteString(s)
				j = skipWhite(t, k)
				if j < len(t) && t[j] == '\'' {
					continue
				}
				if j+7 <= len(t) && strings.EqualFold(t[j:j+7], "uescape") {
					j = skipWhite(t, j+7)
					esc = t[j+1]
					j += 3
				}
				break
			}
			dec, err := unicodeEscapes(sb.String(), esc)
			if err != nil {
				return "", err
			}
			out.WriteString(dec)
			i = j
		case ch == '$':
			k := strings.IndexByte(t[i+1:], '$')
			tag := t[i : i+k+2]
			end := strings.Index(t[i+len(tag):], tag)
			out.WriteString(t[i+len(tag) : i+len(tag)+end])
			i += len(tag) + end + len(tag)
		default: // white space or comments between the parts
			i++
		}
	}
	s := out.String()
	if !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return "", errors.New("invalid byte sequence for encoding UTF8")
	}
	return s, nil
}

// skipWhite skips white space and -- comments.
func skipWhite(t string, j int) int {
	for j < len(t) {
		switch {
		case t[j] == ' ' || t[j] == '\t' || t[j] == '\n' || t[j] == '\r' || t[j] == '\f' || t[j] == '\v':
			j++
		case strings.HasPrefix(t[j:], "--"):
			for j < len(t) && t[j] != '\n' && t[j] != '\r' {
				j++
			}
		default:
			return j
		}
	}
	return j
}

// readQuoted reads a standard quoted string from t[i:], after the opening quote; it returns the index after
// the closing quote.
func readQuoted(t string, i int) (int, string) {
	var b strings.Builder
	for i < len(t) {
		j := strings.IndexByte(t[i:], '\'')
		b.WriteString(t[i : i+j])
		i += j
		if i+1 < len(t) && t[i+1] == '\'' {
			b.WriteByte('\'')
			i += 2
			continue
		}
		return i + 1, b.String()
	}
	return i, b.String()
}

// readEscaped reads the body of an E'...' string from t[i:] (after the opening quote), up to and including
// its closing quote.
func readEscaped(t string, i int) (int, string, error) {
	var b []byte
	var hi rune
	for i < len(t) {
		c := t[i]
		if c == '\'' {
			if i+1 < len(t) && t[i+1] == '\'' {
				b = append(b, '\'')
				i += 2
				continue
			}
			if hi != 0 {
				return 0, "", errors.New("invalid Unicode surrogate pair")
			}
			return i + 1, string(b), nil
		}
		if c != '\\' {
			if hi != 0 {
				return 0, "", errors.New("invalid Unicode surrogate pair")
			}
			b = append(b, c)
			i++
			continue
		}
		i++
		if i >= len(t) {
			return 0, "", errors.New("unterminated quoted string")
		}
		d := t[i]
		switch {
		case d >= '0' && d <= '7':
			n := 0
			k := 0
			for k < 3 && i < len(t) && t[i] >= '0' && t[i] <= '7' {
				n = n*8 + int(t[i]-'0')
				i++
				k++
			}
			b = append(b, byte(n))
		case d == 'x' && i+1 < len(t) && isHex(t[i+1]):
			n := 0
			k := 0
			i++
			for k < 2 && i < len(t) && isHex(t[i]) {
				v, _ := strconv.ParseUint(t[i:i+1], 16, 8)
				n = n*16 + int(v)
				i++
				k++
			}
			b = append(b, byte(n))
		case d == 'u' || d == 'U':
			w := 4
			if d == 'U' {
				w = 8
			}
			if i+1+w > len(t) {
				return 0, "", errors.New("invalid Unicode escape")
			}
			v, err := strconv.ParseUint(t[i+1:i+1+w], 16, 32)
			if err != nil {
				return 0, "", errors.New("invalid Unicode escape")
			}
			i += 1 + w
			r := rune(v)
			switch {
			case r >= 0xD800 && r <= 0xDBFF:
				if hi != 0 {
					return 0, "", errors.New("invalid Unicode surrogate pair")
				}
				hi = r
				continue
			case r >= 0xDC00 && r <= 0xDFFF:
				if hi == 0 {
					return 0, "", errors.New("invalid Unicode surrogate pair")
				}
				r = 0x10000 + (hi-0xD800)<<10 + (r - 0xDC00)
				hi = 0
			default:
				if hi != 0 {
					return 0, "", errors.New("invalid Unicode surrogate pair")
				}
			}
			if r == 0 || r > 0x10FFFF {
				return 0, "", errors.New("invalid Unicode escape value")
			}
			b = utf8.AppendRune(b, r)
		default:
			if hi != 0 {
				return 0, "", errors.New("invalid Unicode surrogate pair")
			}
			switch d {
			case 'b':
				b = append(b, '\b')
			case 'f':
				b = append(b, '\f')
			case 'n':
				b = append(b, '\n')
			case 'r':
				b = append(b, '\r')
			case 't':
				b = append(b, '\t')
			default:
				b = append(b, d)
			}
			i++
		}
	}
	return 0, "", errors.New("unterminated quoted string")
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// Int64 returns the value of the literal: decimal, 0x hexadecimal, 0o octal or 0b binary, with underscores
// between digits.
func (n *Iconst) Int64() (int64, error) {
	return parseInteger(n.Text)
}

func parseInteger(text string) (int64, error) {
	t := strings.ReplaceAll(text, "_", "")
	base := 10
	if len(t) > 2 && t[0] == '0' {
		switch t[1] {
		case 'x', 'X':
			base, t = 16, t[2:]
		case 'o', 'O':
			base, t = 8, t[2:]
		case 'b', 'B':
			base, t = 2, t[2:]
		}
	}
	v, err := strconv.ParseInt(t, base, 64)
	if err != nil {
		return 0, fmt.Errorf("integer %s: %w", text, err.(*strconv.NumError).Err)
	}
	return v, nil
}

// Value returns the number of the parameter: 1 for $1.
func (p *Param) Value() int {
	n, _ := strconv.Atoi(p.Text[1:])
	return n
}

// Value returns the name of the operator as PostgreSQL's parser makes it: the text, except that != is <>.
func (o *OpTok) Value() string {
	if o.Text == "!=" {
		return "<>"
	}
	return o.Text
}

// Value returns the value of the bit string literal as PostgreSQL's parser makes it: the digits of B'101'
// with a leading b ("b101"), those of X'1F' with a leading x ("x1F"). Parts of the literal that are
// separated by white space with a newline are concatenated.
func (b *Bconst) Value() string { return "b" + quotedParts(b.Text) }

// Value returns the value of the hexadecimal string literal; see Bconst.Value.
func (x *Xconst) Value() string { return "x" + quotedParts(x.Text) }

func quotedParts(t string) string {
	var out strings.Builder
	for i := 1; i < len(t); i++ { // t[0] is B or X
		if t[i] != '\'' {
			continue
		}
		j := strings.IndexByte(t[i+1:], '\'')
		out.WriteString(t[i+1 : i+1+j])
		i += j + 1
	}
	return out.String()
}
