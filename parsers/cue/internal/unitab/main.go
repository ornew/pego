// Command unitab prints the character classes of cue.pego that depend on the Unicode tables of the Go
// standard library: the letters (unicode.IsLetter) and digits (unicode.IsDigit) above ASCII, which the
// scanner of CUE accepts in identifiers. The grammar cannot call a function, so the classes are written
// into cue.pego; TestUnicodeTables checks them against the standard library of the Go version in use.
package main

import (
	"fmt"
	"strings"
	"unicode"
)

func main() {
	fmt.Println("def uletter = " + class(unicode.IsLetter))
	fmt.Println("def udigit = " + class(unicode.IsDigit))
}

// class returns the class of the runes above ASCII for which f is true.
func class(f func(rune) bool) string {
	var b strings.Builder
	b.WriteString("(?")
	start := rune(-1)
	flush := func(end rune) {
		if start < 0 {
			return
		}
		if start == end {
			fmt.Fprintf(&b, `\u{%x}`, start)
		} else {
			fmt.Fprintf(&b, `\u{%x}-\u{%x}`, start, end)
		}
		start = -1
	}
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		if f(r) {
			if start < 0 {
				start = r
			}
		} else {
			flush(r - 1)
		}
	}
	flush(unicode.MaxRune)
	b.WriteString(")")
	return b.String()
}
