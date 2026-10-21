package types

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Value returns the string the literal denotes, with its escape sequences decoded. Invalid UTF-8 and
// unpaired surrogates (\ud800 without a low surrogate after it) become U+FFFD, as in encoding/json.
func (s *String) Value() string {
	t := s.TNode.Value
	if !strings.ContainsRune(t, '\\') && utf8.ValidString(t) {
		return t
	}
	var b strings.Builder
	b.Grow(len(t))
	for i := 0; i < len(t); {
		c := t[i]
		if c != '\\' {
			r, n := utf8.DecodeRuneInString(t[i:])
			b.WriteRune(r) // RuneError for invalid UTF-8
			i += n
			continue
		}
		if i+1 >= len(t) {
			b.WriteRune(utf8.RuneError)
			break
		}
		switch e := t[i+1]; e {
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
		case 'u':
			r, ok := hex4(t, i+2)
			if !ok {
				b.WriteRune(utf8.RuneError)
				i += 2
				continue
			}
			i += 6
			if utf16.IsSurrogate(r) {
				if lo, ok := hex4(t, i+2); ok && i+1 < len(t) && t[i] == '\\' && t[i+1] == 'u' {
					if d := utf16.DecodeRune(r, lo); d != utf8.RuneError {
						b.WriteRune(d)
						i += 6
						continue
					}
				}
				r = utf8.RuneError
			}
			b.WriteRune(r)
			continue
		default: // '"', '\\' and '/'
			b.WriteByte(e)
		}
		i += 2
	}
	return b.String()
}

// hex4 decodes the four hexadecimal digits at t[i:].
func hex4(t string, i int) (rune, bool) {
	if i+4 > len(t) {
		return 0, false
	}
	n, err := strconv.ParseUint(t[i:i+4], 16, 16)
	return rune(n), err == nil
}

// Float64 returns the number as a float64. A number outside the range of float64 is an error.
func (n *Number) Float64() (float64, error) {
	f, err := strconv.ParseFloat(n.Value, 64)
	if err != nil {
		return 0, fmt.Errorf("json: number %s: %w", n.Value, err.(*strconv.NumError).Err)
	}
	return f, nil
}

// Int64 returns the number as an int64. A number with a fraction or an exponent, or outside the range of
// int64, is an error.
func (n *Number) Int64() (int64, error) {
	i, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("json: number %s: %w", n.Value, err.(*strconv.NumError).Err)
	}
	return i, nil
}

// Value returns the boolean.
func (b *Bool) Value() bool { return b.TNode.Value == "true" }
