package genrt

import (
	"fmt"
	"testing"
	"unicode/utf8"
)

// BenchmarkLiteralMatcher excludes generated rule calls, input setup and tree
// construction, and includes ordinary mismatches as well as valid successes.
func BenchmarkLiteralMatcher(b *testing.B) {
	for _, text := range []string{"keyword", "日本語", "�", "é�x"} {
		rs := []rune(text)
		for _, match := range []bool{true, false} {
			input := text
			if !match {
				chars := []rune(text)
				chars[len(chars)-1] = '!'
				input = string(chars)
			}
			for _, unit := range []Unit{Bytes, CodePoints} {
				b.Run(fmt.Sprintf("%x/%t/%d", text, match, unit), func(b *testing.B) {
					p := parser{unit: unit, bs: input, n: len(input), in: []rune(input), silent: 1}
					if unit == CodePoints {
						p.n = utf8.RuneCountInString(input)
					}
					b.ReportAllocs()
					for b.Loop() {
						p.pos = 0
						if _, ok := p.matchLiteral(rs, text, 0, false); ok != match {
							b.Fatalf("match=%v want %v", ok, match)
						}
					}
				})
			}
		}
	}
}
