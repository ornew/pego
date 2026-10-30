package runtime

import "testing"

// TestClassHas builds the tables the way the generator does for [a-c_ à-ÿ] and its negation and
// compares membership with the definition.
func TestClassHas(t *testing.T) {
	member := func(r rune) bool {
		return r == 'a' || r == 'b' || r == 'c' || r == '_' || r >= 0xe0 && r <= 0xff
	}
	for _, neg := range []bool{false, true} {
		c := Class{Wide: []rune{0xe0, 0xff}, Neg: neg}
		for r := rune(0); r < 128; r++ {
			if member(r) != neg {
				c.ASCII[r>>6] |= 1 << (uint(r) & 63)
			}
		}
		for r := rune(0); r < 0x300; r++ {
			if got, want := c.Has(r), member(r) != neg; got != want {
				t.Fatalf("neg=%v Has(%U) = %v, want %v", neg, r, got, want)
			}
		}
	}
}

// TestClassSkip runs ClassSkip and ClassesSkip over code-point and byte inputs, with multi-byte,
// invalid UTF-8 and end-of-input cases, and checks the position and the recorded expectation.
func TestClassSkip(t *testing.T) {
	letters := &Class{Wide: []rune{0xe0, 0xff}}
	for _, r := range "az?" {
		letters.ASCII[r>>6] |= 1 << (uint(r) & 63)
	}
	notQ := &Class{Neg: true}
	for r := 0; r < 128; r++ {
		if r != 'q' {
			notQ.ASCII[r>>6] |= 1 << (uint(r) & 63)
		}
	}
	parser := func(unit PositionUnit, in string, pos int) *TParser {
		p := &TParser{Parser: &Parser{unit: unit, Expectations: true}}
		if unit == Bytes {
			p.bs, p.n = in, len(in)
		} else {
			p.In = []rune(in)
			p.n = len(p.In)
		}
		p.Pos = pos
		return p
	}
	for _, tc := range []struct {
		name    string
		unit    PositionUnit
		in      string
		pos     int
		c       *Class
		ok      bool
		wantPos int
	}{
		{"ascii rune", CodePoints, "xa", 1, letters, true, 2},
		{"bit 63", CodePoints, "?", 0, letters, true, 1},
		{"bit 64 miss", CodePoints, "@", 0, letters, false, 0},
		{"bit 127 miss", CodePoints, "\x7f", 0, letters, false, 0},
		{"wide rune", CodePoints, "é", 0, letters, true, 1},
		{"wide miss", CodePoints, "ā", 0, letters, false, 0},
		{"end rune", CodePoints, "a", 1, letters, false, 1},
		{"ascii byte", Bytes, "xz", 1, letters, true, 2},
		{"ascii byte miss", Bytes, "b", 0, letters, false, 0},
		{"wide byte", Bytes, "é", 0, letters, true, 2},
		{"invalid byte", Bytes, "\xff", 0, letters, false, 0},
		{"invalid byte negated", Bytes, "\xff", 0, notQ, true, 1},
		{"negated wide", Bytes, "ā", 0, notQ, true, 2},
		{"negated miss", CodePoints, "q", 0, notQ, false, 0},
		{"end byte", Bytes, "a", 1, letters, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parser(tc.unit, tc.in, tc.pos)
			if got := p.ClassSkip(tc.c, 7); got != tc.ok || p.Pos != tc.wantPos {
				t.Fatalf("ClassSkip = %v at %d, want %v at %d", got, p.Pos, tc.ok, tc.wantPos)
			}
			wantExp := []ExpID(nil)
			if !tc.ok {
				wantExp = []ExpID{7}
			}
			if len(p.exp) != len(wantExp) || len(wantExp) == 1 && (p.exp[0] != 7 || p.farthest != tc.pos) {
				t.Fatalf("expectations %v at %d, want %v at %d", p.exp, p.farthest, wantExp, tc.pos)
			}
		})
	}
	steps := []ClassStep{{letters, 1}, {notQ, 2}, {letters, 3}}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		p := parser(unit, "aéz", 0)
		if !p.ClassesSkip(steps) || p.Pos != p.n {
			t.Fatalf("unit %v: ClassesSkip(aéz) stopped at %d", unit, p.Pos)
		}
		p = parser(unit, "aqz", 0)
		if p.ClassesSkip(steps) || p.Pos != 1 || len(p.exp) != 1 || p.exp[0] != 2 || p.farthest != 1 {
			t.Fatalf("unit %v: ClassesSkip(aqz) = pos %d, expectations %v at %d; want the second step at 1", unit, p.Pos, p.exp, p.farthest)
		}
	}
}
