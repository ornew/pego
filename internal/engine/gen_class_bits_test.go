package engine

import (
	"testing"

	"github.com/ornew/pego/grammar"
	pegort "github.com/ornew/pego/runtime"
)

// TestClassBits builds the runtime's Class from the generator's tables for classes whose ranges
// straddle the ASCII bitset words (63 and 64) and the boundary at 128, and compares membership
// with the definition for every character up to U+0200.
func TestClassBits(t *testing.T) {
	ranges := [][]grammar.CharRange{
		{{Lo: 0x3e, Hi: 0x41}, {Lo: 0x7e, Hi: 0x101}},
		{{Lo: 0, Hi: 0}, {Lo: 63, Hi: 64}},
		{{Lo: 0x7f, Hi: 0x80}},
		{{Lo: 0x80, Hi: 0x10ffff}},
		{{Lo: 'a', Hi: 'z'}, {Lo: 'A', Hi: 'Z'}, {Lo: '_', Hi: '_'}, {Lo: 0x100, Hi: 0x1ff}},
		nil,
	}
	for _, rs := range ranges {
		for _, neg := range []bool{false, true} {
			e := &grammar.CharClass{Ranges: rs, Negated: neg}
			ascii, wide := classBits(e)
			c := pegort.Class{ASCII: ascii, Wide: wide, Neg: neg}
			for r := rune(0); r < 0x200; r++ {
				in := false
				for _, x := range rs {
					in = in || r >= x.Lo && r <= x.Hi
				}
				if got, want := c.Has(r), in != neg; got != want {
					t.Fatalf("ranges %v neg=%v: Has(%U) = %v, want %v", rs, neg, r, got, want)
				}
			}
		}
	}
}
