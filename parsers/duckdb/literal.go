package duckdb

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Value returns the string the literal denotes: the quotes removed and ” decoded, the segments of a string
// continued over a line break joined, the escapes of E'...' and U&'...' decoded, the text between the delimiters
// of a dollar-quoted string as it is.
//
// An escape of E'...' that makes an invalid UTF-8 sequence (\xff) is kept as the byte, as the scanner of DuckDB
// does before it checks the string.
func (s *StringLit) Value() string { return stringValue(s.Text) }

// Kind returns how the string is written.
func (s *StringLit) Kind() StringKind {
	t := s.Text
	switch {
	case t != "" && t[0] == '$':
		return DollarQuoted
	case len(t) > 1 && (t[0] == 'e' || t[0] == 'E') && t[1] == '\'':
		return EscapeString
	case len(t) > 2 && (t[0] == 'u' || t[0] == 'U') && t[1] == '&':
		return UnicodeString
	}
	return StandardString
}

// A StringKind tells how a string literal is written.
type StringKind int

// The notations of string literals.
const (
	StandardString StringKind = iota // 'a''b'
	EscapeString                     // E'a\nb'
	UnicodeString                    // U&'a\0062'
	DollarQuoted                     // $$a$$, $tag$a$tag$
)

func stringValue(t string) string {
	switch {
	case t == "":
		return ""
	case t[0] == '$':
		j := strings.IndexByte(t[1:], '$') + 2 // the end of the opening delimiter
		tag := t[:j]
		return t[j : len(t)-len(tag)]
	case t[0] == 'e' || t[0] == 'E':
		return quotedValue(t[1:], true, 0)
	case t[0] == 'u' || t[0] == 'U':
		return quotedValue(t[2:], false, '\\')
	}
	return quotedValue(t, false, 0)
}

// quotedValue decodes the segments of a quoted string. With esc, backslash escapes are decoded (E'...'); with
// uesc, the escapes of U&'...' (the escape character is uesc).
func quotedValue(t string, esc bool, uesc byte) string {
	var b strings.Builder
	i := 0
	for i < len(t) {
		// t[i] is an opening quote
		i++
		for i < len(t) {
			c := t[i]
			switch {
			case c == '\'':
				if i+1 < len(t) && t[i+1] == '\'' {
					b.WriteByte('\'')
					i += 2
					continue
				}
				goto closed
			case c == '\\' && esc && i+1 < len(t):
				i = unescape(&b, t, i+1)
			default:
				b.WriteByte(c)
				i++
			}
		}
	closed:
		i++ // the closing quote
		// whitespace and comments up to the next segment
		for i < len(t) && t[i] != '\'' {
			if t[i] == '-' && i+1 < len(t) && t[i+1] == '-' {
				for i < len(t) && t[i] != '\n' && t[i] != '\r' {
					i++
				}
				continue
			}
			i++
		}
	}
	if uesc != 0 {
		return decodeUnicodeEscapes(b.String(), uesc)
	}
	return b.String()
}

// unescape decodes the escape that starts at t[i] (after the backslash) into b and returns the index after it.
func unescape(b *strings.Builder, t string, i int) int {
	c := t[i]
	switch c {
	case 'b':
		b.WriteByte('\b')
	case 'f':
		b.WriteByte('\f')
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case 'x':
		j := i + 1
		for j < len(t) && j < i+3 && isHex(t[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte('x')
			return i + 1
		}
		v, _ := strconv.ParseUint(t[i+1:j], 16, 8)
		b.WriteByte(byte(v))
		return j
	case 'u', 'U':
		n := 4
		if c == 'U' {
			n = 8
		}
		if i+1+n > len(t) {
			b.WriteRune(utf8.RuneError)
			return len(t)
		}
		v, _ := strconv.ParseUint(t[i+1:i+1+n], 16, 32)
		r := rune(v)
		j := i + 1 + n
		if r >= 0xd800 && r < 0xdc00 && j+1 < len(t) && t[j] == '\\' && (t[j+1] == 'u' || t[j+1] == 'U') {
			m := 4
			if t[j+1] == 'U' {
				m = 8
			}
			if j+2+m <= len(t) {
				if lo, err := strconv.ParseUint(t[j+2:j+2+m], 16, 32); err == nil && lo >= 0xdc00 && lo < 0xe000 {
					b.WriteRune(0x10000 + (r-0xd800)<<10 + (rune(lo) - 0xdc00))
					return j + 2 + m
				}
			}
		}
		b.WriteRune(r)
		return j
	default:
		if c >= '0' && c <= '7' {
			j := i
			for j < len(t) && j < i+3 && t[j] >= '0' && t[j] <= '7' {
				j++
			}
			v, _ := strconv.ParseUint(t[i:j], 8, 16)
			b.WriteByte(byte(v))
			return j
		}
		// any other character stands for itself, a multi-byte one included
		_, n := utf8.DecodeRuneInString(t[i:])
		b.WriteString(t[i : i+n])
		return i + n
	}
	return i + 1
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// Bits returns the digits of a bit string literal (B'101' gives "101") or its hexadecimal digits (X'FF' gives
// "FF"), and whether it is a hexadecimal one.
func (b *BitLit) Bits() (digits string, hex bool) {
	t := b.Text
	if t == "" {
		return "", false
	}
	var out strings.Builder
	for i := 2; i < len(t); i++ {
		if t[i] == '\'' {
			// the end of a segment; the next segment starts at the next quote
			for i++; i < len(t) && t[i] != '\''; i++ {
			}
			continue
		}
		out.WriteByte(t[i])
	}
	return out.String(), t[0] == 'x' || t[0] == 'X'
}

// Digits returns the text of the number without the underscores that separate its digits (1_000).
func (n *Number) Digits() string {
	if strings.IndexByte(n.Text, '_') < 0 {
		return n.Text
	}
	return strings.ReplaceAll(n.Text, "_", "")
}

// IsInteger reports whether the number is written as a sequence of digits.
func (n *Number) IsInteger() bool {
	for i := 0; i < len(n.Text); i++ {
		if c := n.Text[i]; c < '0' || c > '9' {
			if c != '_' {
				return false
			}
		}
	}
	return n.Text != ""
}

// Int64 returns the value of an integer literal, and whether it is one that fits in an int64.
func (n *Number) Int64() (int64, bool) {
	if !n.IsInteger() {
		return 0, false
	}
	v, err := strconv.ParseInt(n.Digits(), 10, 64)
	return v, err == nil
}

// Float64 returns the value of the number as a float64; the error is an overflow.
func (n *Number) Float64() (float64, error) {
	return strconv.ParseFloat(n.Digits(), 64)
}

// Type returns the type DuckDB gives the literal: INTEGER for an integer that fits in 32 bits, BIGINT in 64 bits,
// HUGEINT in 128 bits, DECIMAL for a number with a point and at most 38 digits, and DOUBLE for the others (those
// with an exponent included).
func (n *Number) Type() string {
	d := n.Digits()
	if n.IsInteger() {
		if v, err := strconv.ParseInt(d, 10, 64); err == nil {
			if v >= math.MinInt32 && v <= math.MaxInt32 {
				return "INTEGER"
			}
			return "BIGINT"
		}
		if v, ok := new(big.Int).SetString(d, 10); ok && v.BitLen() <= 127 {
			return "HUGEINT"
		}
		return "DOUBLE"
	}
	if strings.ContainsAny(d, "eE") {
		return "DOUBLE"
	}
	if w, _ := n.DecimalWidthScale(); w > 0 && w <= 38 {
		return "DECIMAL"
	}
	return "DOUBLE"
}

// DecimalWidthScale returns the width and the scale of a number written with a point (1.50 has width 3 and
// scale 2; the zeros in front of the point count: 0.5 has width 2), or 0, 0.
func (n *Number) DecimalWidthScale() (width, scale int) {
	d := n.Digits()
	i := strings.IndexByte(d, '.')
	if i < 0 || strings.ContainsAny(d, "eE") {
		return 0, 0
	}
	intPart, frac := d[:i], d[i+1:]
	scale = len(frac)
	width = len(intPart) + scale
	return width, scale
}

// Index returns the number of a parameter written $1 or ?1.
func (p *Param) Index() (int, bool) {
	t := p.Text
	if len(t) < 2 || (t[0] != '$' && t[0] != '?') || t[1] < '0' || t[1] > '9' {
		return 0, false
	}
	v, err := strconv.Atoi(t[1:])
	return v, err == nil
}

// ParamName returns the name of a parameter written $name.
func (p *Param) ParamName() (string, bool) {
	t := p.Text
	if len(t) < 2 || t[0] != '$' || (t[1] >= '0' && t[1] <= '9') {
		return "", false
	}
	return strings.TrimLeft(t[1:], " \t\r\n\f"), true
}

// Positional reports whether the parameter is a plain ? (its number is the order in the statement).
func (p *Param) Positional() bool { return p.Text == "?" }
