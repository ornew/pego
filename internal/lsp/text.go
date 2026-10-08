package lsp

import (
	"sort"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Positions are converted through byte offsets in the document text. Three kinds of positions
// meet here:
//
//   - LSP positions: zero-based lines separated by "\n", "\r\n" or "\r", and columns in UTF-16
//     code units;
//   - PEGO positions (grammar.Pos): one-based lines separated by "\n" only (the lexer reads "\r" as
//     white space), and one-based columns in code points;
//   - byte offsets into the UTF-8 text, which both are converted to and from.

// textIndex converts positions in a text.
type textIndex struct {
	text string
	// lines are the byte offsets at which LSP lines start, and pegoLines those at which PEGO lines
	// start.
	lines, pegoLines []int
}

func newTextIndex(text string) *textIndex {
	t := &textIndex{text: text, lines: []int{0}, pegoLines: []int{0}}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			t.lines = append(t.lines, i+1)
			t.pegoLines = append(t.pegoLines, i+1)
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				continue
			}
			t.lines = append(t.lines, i+1)
		}
	}
	return t
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
	off, end := t.lines[p.Line], t.lineEnd(p.Line)
	for n := 0; off < end; {
		r, size := utf8.DecodeRuneInString(t.text[off:end])
		w := utf16Len(r)
		if n+w > p.Character {
			break
		}
		n += w
		off += size
	}
	return off
}

// position returns the LSP position of the byte offset off.
func (t *textIndex) position(off int) Position {
	off = max(0, min(off, len(t.text)))
	line := sort.Search(len(t.lines), func(i int) bool { return t.lines[i] > off }) - 1
	// An offset inside a line terminator ("\r\n") is the end of the line.
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
	if p.Line > len(t.pegoLines) {
		return len(t.text)
	}
	off := t.pegoLines[p.Line-1]
	for col := 1; col < p.Col && off < len(t.text) && t.text[off] != '\n'; col++ {
		_, size := utf8.DecodeRuneInString(t.text[off:])
		off += size
	}
	return off
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
