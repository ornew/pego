package typescript

import (
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Value returns the string the literal stands for: the text between the quotes with its escapes decoded (\n, \xHH,
// \uHHHH, \u{H...}, line continuations and the escapes of any other character). An escape of an unpaired surrogate
// gives U+FFFD, as Go strings cannot hold one.
func (s *StringLiteral) Value() string {
	if len(s.Text) < 2 {
		return ""
	}
	return decodeEscapes(s.Text[1 : len(s.Text)-1])
}

// Value returns the text of the template without its backquotes, with its escapes decoded (the cooked value).
func (t *NoSubstitutionTemplateLiteral) Value() string {
	if len(t.Text) < 2 {
		return ""
	}
	return decodeEscapes(strings.ReplaceAll(t.Text[1:len(t.Text)-1], "\r\n", "\n"))
}

// decodeEscapes decodes the escape sequences of a string or template literal body.
func decodeEscapes(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	var high rune // a pending high surrogate from \uD83D
	flush := func() {
		if high != 0 {
			b.WriteRune(utf8.RuneError)
			high = 0
		}
	}
	for i := 0; i < len(s); {
		c, size := utf8.DecodeRuneInString(s[i:])
		if c != '\\' {
			flush()
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		c, size = utf8.DecodeRuneInString(s[i:])
		i += size
		var r rune
		switch c {
		case 'b':
			r = '\b'
		case 'f':
			r = '\f'
		case 'n':
			r = '\n'
		case 'r':
			r = '\r'
		case 't':
			r = '\t'
		case 'v':
			r = '\v'
		case '0':
			r = 0
		case '\n', ' ', ' ': // line continuation
			flush()
			continue
		case '\r':
			if i < len(s) && s[i] == '\n' {
				i++
			}
			flush()
			continue
		case 'x':
			v, n := hexValue(s[i:], 2)
			i += n
			r = v
		case 'u':
			if i < len(s) && s[i] == '{' {
				j := strings.IndexByte(s[i:], '}')
				if j < 0 {
					r = utf8.RuneError
					break
				}
				v, _ := hexValue(s[i+1:i+j], j-1)
				i += j + 1
				r = v
			} else {
				v, n := hexValue(s[i:], 4)
				i += n
				r = v
			}
		default:
			r = c
		}
		switch {
		case r >= 0xD800 && r < 0xDC00:
			flush()
			high = r
			continue
		case r >= 0xDC00 && r < 0xE000:
			if high != 0 {
				r = 0x10000 + (high-0xD800)<<10 + (r - 0xDC00)
				high = 0
			} else {
				r = utf8.RuneError
			}
		default:
			flush()
		}
		b.WriteRune(r)
	}
	flush()
	return b.String()
}

// hexValue reads up to n hexadecimal digits of s, and returns their value and how many it read.
func hexValue(s string, n int) (rune, int) {
	var v rune
	i := 0
	for ; i < n && i < len(s); i++ {
		d := strings.IndexByte("0123456789abcdef", s[i]|0x20)
		if d < 0 {
			break
		}
		v = v<<4 | rune(d)
	}
	return v, i
}

// Value returns the number as a float64, from a decimal, hexadecimal (0x), binary (0b) or octal (0o) literal with
// numeric separators. A literal too large for a float64 is +Inf, as in JavaScript.
func (n *NumericLiteral) Value() float64 {
	t := strings.ReplaceAll(n.Text, "_", "")
	if base := literalBase(t); base != 10 {
		v, ok := new(big.Int).SetString(t[2:], base)
		if !ok {
			return 0
		}
		f, _ := new(big.Float).SetInt(v).Float64()
		return f
	}
	f, _ := strconv.ParseFloat(t, 64) // a range error gives ±Inf
	return f
}

// Value returns the integer of a bigint literal, without its n.
func (n *BigIntLiteral) Value() *big.Int {
	t := strings.TrimSuffix(strings.ReplaceAll(n.Text, "_", ""), "n")
	base := literalBase(t)
	if base != 10 {
		t = t[2:]
	}
	v, _ := new(big.Int).SetString(t, base)
	if v == nil {
		return new(big.Int)
	}
	return v
}

// literalBase returns the base of a numeric literal by its prefix.
func literalBase(t string) int {
	if len(t) > 2 && t[0] == '0' {
		switch t[1] {
		case 'x', 'X':
			return 16
		case 'b', 'B':
			return 2
		case 'o', 'O':
			return 8
		}
	}
	return 10
}

// Value returns the name with its unicode escapes (a, \u{61}) decoded.
func (i *Identifier) Value() string {
	return decodeIdentifier(i.Text)
}

// Value returns the name with its unicode escapes decoded.
func (i *PrivateIdentifier) Value() string {
	return decodeIdentifier(i.Text)
}

func decodeIdentifier(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	return decodeEscapes(s)
}
