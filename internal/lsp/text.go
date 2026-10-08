package lsp

import (
	"sort"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Positions are converted through byte offsets in the document text. Three kinds of positions
// meet here:
//
//   - LSP positions: zero-based lines and columns in UTF-16 code units;
//   - PEGO positions (grammar.Pos): one-based lines and columns in code points, in which the "\r"
//     of a "\r\n" counts as a column of its line;
//   - byte offsets into the UTF-8 text, which both are converted to and from.
//
// For both, a line ends at "\n", "\r\n" or a lone "\r".

// textIndex converts positions in a text.
//
// Columns are found from checkpoints rather than by walking a line from its start, so that each
// conversion takes constant time even on a long line: every checkpointEvery bytes, the index
// records how many UTF-16 code units and code points precede that offset in the whole text. The
// column of an offset is then its count minus that of the start of its line, and a count is found
// by walking from the nearest checkpoint.
type textIndex struct {
	text string
	// lines are the byte offsets at which lines start.
	lines []int
	// checkpoints are at the first character boundaries at or after multiples of checkpointEvery.
	checkpoints []checkpoint
}

// checkpoint records the numbers of UTF-16 code units and code points before a byte offset.
type checkpoint struct {
	off, units, runes int
}

const checkpointEvery = 64

func newTextIndex(text string) *textIndex {
	t := &textIndex{text: text, lines: []int{0}}
	units, runes, next := 0, 0, 0
	for i := 0; i < len(text); {
		if i >= next {
			t.checkpoints = append(t.checkpoints, checkpoint{i, units, runes})
			next = i + checkpointEvery
		}
		switch text[i] {
		case '\n':
			t.lines = append(t.lines, i+1)
		case '\r':
			if i+1 >= len(text) || text[i+1] != '\n' {
				t.lines = append(t.lines, i+1)
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		units += utf16Len(r)
		runes++
		i += size
	}
	t.checkpoints = append(t.checkpoints, checkpoint{len(text), units, runes})
	return t
}

// count returns the numbers of UTF-16 code units and code points before the byte offset off, or
// before the start of the character that contains off.
func (t *textIndex) count(off int) (units, runes int) {
	k := sort.Search(len(t.checkpoints), func(k int) bool { return t.checkpoints[k].off > off }) - 1
	c := t.checkpoints[k]
	units, runes = c.units, c.runes
	for i := c.off; i < off; {
		r, size := utf8.DecodeRuneInString(t.text[i:])
		if i+size > off {
			break
		}
		units += utf16Len(r)
		runes++
		i += size
	}
	return units, runes
}

// seek returns the largest offset in [lo, hi] at a character boundary before which there are at
// most n UTF-16 code units (if units is set) or code points (otherwise) in the whole text.
func (t *textIndex) seek(lo, hi, n int, units bool) int {
	key := func(c checkpoint) int {
		if units {
			return c.units
		}
		return c.runes
	}
	// Start from the last checkpoint in [lo, hi] that is not past n, or from lo.
	k := sort.Search(len(t.checkpoints), func(k int) bool {
		c := t.checkpoints[k]
		return c.off > hi || key(c) > n
	}) - 1
	off := lo
	cu, cr := t.count(lo)
	if k >= 0 && t.checkpoints[k].off > lo {
		c := t.checkpoints[k]
		off, cu, cr = c.off, c.units, c.runes
	}
	for off < hi {
		r, size := utf8.DecodeRuneInString(t.text[off:hi])
		w := 1
		if units {
			w = utf16Len(r)
		}
		cur := cr
		if units {
			cur = cu
		}
		if cur+w > n {
			break
		}
		cu += utf16Len(r)
		cr++
		off += size
	}
	return off
}

// lineEnd returns the offset of the end of the LSP line i, before its line terminator.
func (t *textIndex) lineEnd(i int) int {
	if i+1 >= len(t.lines) {
		return len(t.text)
	}
	end := t.lines[i+1] - 1
	if t.text[end] == '\n' && end > t.lines[i] && t.text[end-1] == '\r' {
		end--
	}
	return end
}

// offset returns the byte offset of the LSP position p. A position after the end of its line is
// the end of the line, and one after the last line is the end of the text. A position in the
// middle of a character (between the two code units of a surrogate pair) is the start of the
// character.
func (t *textIndex) offset(p Position) int {
	if p.Line < 0 {
		return 0
	}
	if p.Line >= len(t.lines) {
		return len(t.text)
	}
	start := t.lines[p.Line]
	units, _ := t.count(start)
	return t.seek(start, t.lineEnd(p.Line), units+max(p.Character, 0), true)
}

// position returns the LSP position of the byte offset off.
func (t *textIndex) position(off int) Position {
	off = max(0, min(off, len(t.text)))
	line := sort.Search(len(t.lines), func(i int) bool { return t.lines[i] > off }) - 1
	// An offset inside a line terminator ("\r\n") is the end of the line.
	off = min(off, t.lineEnd(line))
	u0, _ := t.count(t.lines[line])
	u1, _ := t.count(off)
	return Position{Line: line, Character: u1 - u0}
}

// rangeOf returns the LSP range of the byte offsets [start, end).
func (t *textIndex) rangeOf(start, end int) Range {
	return Range{Start: t.position(start), End: t.position(end)}
}

// pegoOffset returns the byte offset of the PEGO position p. A column after the end of its line
// is the end of the line (the offset of its "\n").
func (t *textIndex) pegoOffset(p grammar.Pos) int {
	if p.Line < 1 {
		return 0
	}
	if p.Line > len(t.lines) {
		return len(t.text)
	}
	// The end of the line is its last line break character: the "\r" of a "\r\n" is a column.
	start, end := t.lines[p.Line-1], len(t.text)
	if p.Line < len(t.lines) {
		end = t.lines[p.Line] - 1
	}
	_, runes := t.count(start)
	return t.seek(start, end, runes+max(p.Col-1, 0), false)
}

func utf16Len(r rune) int {
	if r >= 0x10000 && r <= utf8.MaxRune {
		return 2
	}
	return 1
}

// applyChange applies a content change to text and returns the new text. A change without a range
// replaces the whole text.
func applyChange(text string, c contentChange) string {
	if c.Range == nil {
		return c.Text
	}
	idx := newTextIndex(text)
	start, end := idx.offset(c.Range.Start), idx.offset(c.Range.End)
	if end < start {
		start, end = end, start
	}
	return text[:start] + c.Text + text[end:]
}
