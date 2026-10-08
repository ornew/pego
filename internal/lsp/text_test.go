package lsp

import (
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestTextIndexPositions(t *testing.T) {
	// "é" is 2 bytes and 1 UTF-16 unit, "😀" 4 bytes and 2 units, "名" 3 bytes and 1 unit.
	text := "aé😀b\r\n名\rx\n\nlast"
	idx := newTextIndex(text)
	for _, tc := range []struct {
		off int
		pos Position
	}{
		{0, Position{0, 0}},
		{1, Position{0, 1}},  // é
		{3, Position{0, 2}},  // 😀
		{7, Position{0, 4}},  // b
		{8, Position{0, 5}},  // \r
		{10, Position{1, 0}}, // 名
		{13, Position{1, 1}}, // \r (a line terminator on its own)
		{14, Position{2, 0}}, // x
		{16, Position{3, 0}}, // empty line
		{17, Position{4, 0}},
		{21, Position{4, 4}}, // end of text
	} {
		if got := idx.position(tc.off); got != tc.pos {
			t.Errorf("position(%d) = %v, want %v", tc.off, got, tc.pos)
		}
		if got := idx.offset(tc.pos); got != tc.off {
			t.Errorf("offset(%v) = %d, want %d", tc.pos, got, tc.off)
		}
	}
	// Offsets inside a character or a line terminator.
	for off, want := range map[int]Position{
		9: {0, 5}, // between \r and \n
	} {
		if got := idx.position(off); got != want {
			t.Errorf("position(%d) = %v, want %v", off, got, want)
		}
	}
	// Positions that are not on a character boundary or past the end of a line.
	for pos, want := range map[Position]int{
		{0, 3}:  3,  // between the two units of 😀: its start
		{0, 99}: 8,  // past the end of the line: before \r\n
		{1, 99}: 13, // before the lone \r
		{9, 0}:  21, // past the last line
		{-1, 0}: 0,
	} {
		if got := idx.offset(pos); got != want {
			t.Errorf("offset(%v) = %d, want %d", pos, got, want)
		}
	}
}

func TestTextIndexPegoPositions(t *testing.T) {
	// PEGO lines end only at \n; \r is a character of the line, and columns count code points.
	text := "a😀b\r\n名\rx\nz"
	idx := newTextIndex(text)
	for _, tc := range []struct {
		pos grammar.Pos
		off int
	}{
		{grammar.Pos{Line: 1, Col: 1}, 0},
		{grammar.Pos{Line: 1, Col: 2}, 1},
		{grammar.Pos{Line: 1, Col: 3}, 5},
		{grammar.Pos{Line: 1, Col: 4}, 6}, // \r
		{grammar.Pos{Line: 2, Col: 1}, 8},
		{grammar.Pos{Line: 2, Col: 3}, 12}, // x, after the lone \r
		{grammar.Pos{Line: 3, Col: 1}, 14},
		{grammar.Pos{Line: 3, Col: 2}, 15},
	} {
		if got := idx.pegoOffset(tc.pos); got != tc.off {
			t.Errorf("pegoOffset(%v) = %d, want %d", tc.pos, got, tc.off)
		}
	}
	if got := idx.pegoOffset(grammar.Pos{Line: 1, Col: 99}); got != 7 {
		t.Errorf("past the end of the line: %d", got)
	}
	if got := idx.pegoOffset(grammar.Pos{Line: 9, Col: 1}); got != len(text) {
		t.Errorf("past the last line: %d", got)
	}
	// The x after the lone \r is on LSP line 2.
	if got := idx.position(idx.pegoOffset(grammar.Pos{Line: 2, Col: 3})); got != (Position{2, 0}) {
		t.Errorf("x at %v", got)
	}
}

func TestApplyChange(t *testing.T) {
	text := "def a = \"😀\" b\r\ndef b = \"x\""
	for _, tc := range []struct {
		r    *Range
		new  string
		want string
	}{
		{nil, "all", "all"},
		{&Range{Position{0, 4}, Position{0, 5}}, "名前", "def 名前 = \"😀\" b\r\ndef b = \"x\""},
		// After 😀 (two UTF-16 units).
		{&Range{Position{0, 11}, Position{0, 11}}, "!", "def a = \"😀!\" b\r\ndef b = \"x\""},
		// Across the line break.
		{&Range{Position{0, 13}, Position{1, 3}}, "c\ndef", "def a = \"😀\" c\ndef b = \"x\""},
		// Reversed range.
		{&Range{Position{1, 3}, Position{0, 13}}, "", "def a = \"😀\"  b = \"x\""},
	} {
		if got := applyChange(text, contentChange{Range: tc.r, Text: tc.new}); got != tc.want {
			t.Errorf("%v %q: got %q, want %q", tc.r, tc.new, got, tc.want)
		}
	}
}

func TestMinimalEdit(t *testing.T) {
	for _, tc := range []struct{ old, new string }{
		{"abc", "abc"},
		{"abc", "abXc"},
		{"a😀c", "a😁c"}, // the code points share their first bytes
		{"a\r\nb", "a\r\n\r\nb"},
		{"x\r\n", "x\r\ny\r\n"},
		{"", "new"},
		{"old", ""},
		{"名前", "名称"},
	} {
		idx := newTextIndex(tc.old)
		e := minimalEdit(idx, tc.old, tc.new)
		if got := applyChange(tc.old, contentChange{Range: &e.Range, Text: e.NewText}); got != tc.new {
			t.Errorf("%q -> %q: edit %+v gives %q", tc.old, tc.new, e, got)
		}
	}
}
