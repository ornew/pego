// Package text holds the character-level rules of XML 1.0 that both the syntax tree types and the decoder use.
package text

import (
	"strconv"
	"strings"
)

// Newlines normalizes line ends (XML 1.0, 2.11): CR LF and CR become LF.
func Newlines(s string) string {
	i := strings.IndexByte(s, '\r')
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for ; i >= 0; i = strings.IndexByte(s, '\r') {
		b.WriteString(s[:i])
		b.WriteByte('\n')
		if i+1 < len(s) && s[i+1] == '\n' {
			i++
		}
		s = s[i+1:]
	}
	b.WriteString(s)
	return b.String()
}

// CharRef decodes the digits of a character reference (after "&#", without ";").
func CharRef(digits string) (rune, bool) {
	base := 10
	if strings.HasPrefix(digits, "x") {
		digits, base = digits[1:], 16
	}
	n, err := strconv.ParseUint(digits, base, 32)
	if err != nil || n > 0x10FFFF || !IsChar(rune(n)) {
		return 0, false
	}
	return rune(n), true
}

// IsChar reports whether r matches the production Char.
func IsChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || 0x20 <= r && r <= 0xD7FF || 0xE000 <= r && r <= 0xFFFD ||
		0x10000 <= r && r <= 0x10FFFF
}
