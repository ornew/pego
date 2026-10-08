package lsp

import (
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	// Columns count code points, and the \r of \r\n is a column of its line.
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
		{grammar.Pos{Line: 2, Col: 2}, 11}, // the lone \r
		{grammar.Pos{Line: 3, Col: 1}, 12}, // x, after the lone \r
		{grammar.Pos{Line: 4, Col: 1}, 14},
		{grammar.Pos{Line: 4, Col: 2}, 15},
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
	if got := idx.pegoOffset(grammar.Pos{Line: 2, Col: 9}); got != 11 {
		t.Errorf("past the end of a line that ends with \\r: %d", got)
	}
	// The x after the lone \r is on LSP line 2.
	if got := idx.position(idx.pegoOffset(grammar.Pos{Line: 3, Col: 1})); got != (Position{2, 0}) {
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

// The straightforward conversions, which walk each line from its start, are the reference for the
// indexed ones.

func naiveOffset(t *textIndex, p Position) int {
	if p.Line < 0 {
		return 0
	}
	if p.Line >= len(t.lines) {
		return len(t.text)
	}
	off, end := t.lines[p.Line], t.lineEnd(p.Line)
	for n := 0; off < end; {
		r, size := utf8.DecodeRuneInString(t.text[off:end])
		if n+utf16Len(r) > p.Character {
			break
		}
		n += utf16Len(r)
		off += size
	}
	return off
}

func naivePosition(t *textIndex, off int) Position {
	off = max(0, min(off, len(t.text)))
	line := sort.Search(len(t.lines), func(i int) bool { return t.lines[i] > off }) - 1
	off = min(off, t.lineEnd(line))
	col := 0
	for i := t.lines[line]; i < off; {
		r, size := utf8.DecodeRuneInString(t.text[i:])
		if i+size > off {
			break
		}
		col += utf16Len(r)
		i += size
	}
	return Position{Line: line, Character: col}
}

func naivePegoOffset(t *textIndex, p grammar.Pos) int {
	if p.Line < 1 {
		return 0
	}
	if p.Line > len(t.lines) {
		return len(t.text)
	}
	off := t.lines[p.Line-1]
	end := len(t.text)
	if p.Line < len(t.lines) {
		end = t.lines[p.Line] - 1
	}
	for col := 1; col < p.Col && off < end; col++ {
		_, size := utf8.DecodeRuneInString(t.text[off:])
		off += size
	}
	return off
}

// TestTextIndexMatchesNaive compares the conversions with the reference ones on random texts with
// long lines, every kind of line end and characters of every UTF-8 and UTF-16 width.
func TestTextIndexMatchesNaive(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))
	pieces := []string{"a", "é", "名", "😀", "\n", "\r\n", "\r", " ", "\xff"}
	for iter := 0; iter < 200; iter++ {
		var b strings.Builder
		n := rnd.IntN(3000)
		for b.Len() < n {
			// Long runs of one piece make lines longer than the index's checkpoint spacing.
			p := pieces[rnd.IntN(len(pieces))]
			for k := rnd.IntN(200); k >= 0; k-- {
				b.WriteString(p)
			}
		}
		text := b.String()
		idx := newTextIndex(text)
		for k := 0; k < 300; k++ {
			off := rnd.IntN(len(text) + 2)
			if got, want := idx.position(off), naivePosition(idx, off); got != want {
				t.Fatalf("text %q: position(%d) = %v, want %v", text, off, got, want)
			}
			p := Position{Line: rnd.IntN(len(idx.lines)+1) - rnd.IntN(2), Character: rnd.IntN(1000)}
			if got, want := idx.offset(p), naiveOffset(idx, p); got != want {
				t.Fatalf("text %q: offset(%v) = %d, want %d", text, p, got, want)
			}
			pp := grammar.Pos{Line: rnd.IntN(len(idx.lines) + 1), Col: rnd.IntN(1000) + 1}
			if got, want := idx.pegoOffset(pp), naivePegoOffset(idx, pp); got != want {
				t.Fatalf("text %q: pegoOffset(%v) = %d, want %d", text, pp, got, want)
			}
		}
	}
}

// TestLongLine checks that a document with a long line is analyzed in about linear time. The
// conversions used to walk the line from its start for every token, so a line of 64 KB took
// seconds and one of 1 MB did not open.
func TestLongLine(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///long.pego"
	const n = 1 << 17
	text := "def a = " + strings.Repeat(`"😀" b `, n) + "\ndef b = \"x\"" // 1 MB, 260,000 tokens
	start := time.Now()
	c.open(uri, text) // waits up to 10 seconds
	var res struct {
		Data []int `json:"data"`
	}
	c.requestInto("textDocument/semanticTokens/full", docParams(uri), &res)
	if len(res.Data) != 5*(2+2*n+3) {
		t.Errorf("%d semantic token integers", len(res.Data))
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
	c.exit()
}
