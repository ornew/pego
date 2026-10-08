package python

import (
	"sort"
	"unicode/utf8"
)

// LineIndex converts the positions of a parse (the Spans of the AST and the Pos of a
// SyntaxError) into lines and columns as CPython reports them: lines count from 1, and a newline
// is \n, \r\n or \r; columns count UTF-8 bytes from the start of the line, from 0 (Python's
// col_offset).
type LineIndex struct {
	src        string
	lineStarts []int // byte offsets of the starts of lines
	byteOff    []int // byte offset of each code point (nil if positions are bytes or src is ASCII)
}

// NewLineIndex indexes src, whose positions are in unit (code points by default).
func NewLineIndex(src string, unit ...Unit) *LineIndex {
	x := &LineIndex{src: src, lineStarts: []int{0}}
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			x.lineStarts = append(x.lineStarts, i+1)
		case '\n':
			x.lineStarts = append(x.lineStarts, i+1)
		}
	}
	if (len(unit) == 0 || unit[0] == CodePoints) && !isASCII(src) {
		x.byteOff = make([]int, 0, utf8.RuneCountInString(src)+1)
		for i := range src {
			x.byteOff = append(x.byteOff, i)
		}
		x.byteOff = append(x.byteOff, len(src))
	}
	return x
}

// Offset returns the byte offset of a position.
func (x *LineIndex) Offset(pos int) int {
	if x.byteOff != nil {
		if pos >= len(x.byteOff) {
			return len(x.src)
		}
		return x.byteOff[pos]
	}
	return min(pos, len(x.src))
}

// Position returns the line (from 1) and the column (in UTF-8 bytes from 0) of a position.
func (x *LineIndex) Position(pos int) (line, col int) {
	off := x.Offset(pos)
	i := sort.Search(len(x.lineStarts), func(i int) bool { return x.lineStarts[i] > off }) - 1
	return i + 1, off - x.lineStarts[i]
}
