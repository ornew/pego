// Package sourcepos maps immutable UTF-8 snapshots to editor coordinates.
package sourcepos

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// Encoding selects the unit used for offsets and line columns.
type Encoding uint8

const (
	Bytes Encoding = iota
	CodePoints
	UTF16
)

// Position is a zero-based line and character coordinate.
type Position struct{ Line, Character int }
type checkpoint struct{ off, points, units int }

// Map is an immutable source index safe for concurrent reads. Each malformed
// UTF-8 byte counts as one replacement character and one UTF-16 unit.
type Map struct {
	text        string
	lines       []int
	checkpoints []checkpoint
}

// New indexes text without changing its bytes. LF, CRLF and lone CR end lines.
func New(text string) *Map {
	m := &Map{text: text, lines: []int{0}}
	points, units, next := 0, 0, 0
	for i := 0; i < len(text); {
		if i >= next {
			m.checkpoints = append(m.checkpoints, checkpoint{i, points, units})
			next = i + 64
		}
		if text[i] == '\n' || text[i] == '\r' && (i+1 == len(text) || text[i+1] != '\n') {
			m.lines = append(m.lines, i+1)
		}
		r, n := utf8.DecodeRuneInString(text[i:])
		points++
		units += width(r)
		i += n
	}
	m.checkpoints = append(m.checkpoints, checkpoint{len(text), points, units})
	return m
}
func width(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}
func valid(e Encoding) bool { return e <= UTF16 }
func key(c checkpoint, e Encoding) int {
	if e == UTF16 {
		return c.units
	}
	return c.points
}
func (m *Map) lineEnd(line int) int {
	if line+1 == len(m.lines) {
		return len(m.text)
	}
	end := m.lines[line+1] - 1
	if m.text[end] == '\n' && end > m.lines[line] && m.text[end-1] == '\r' {
		end--
	}
	return end
}

// UnitOffset converts a byte boundary into a flat offset. Bytes permits the
// interior of a valid scalar; the other encodings require scalar boundaries.
func (m *Map) UnitOffset(off int, e Encoding) (int, error) {
	if !valid(e) || off < 0 || off > len(m.text) {
		return 0, fmt.Errorf("invalid source offset or encoding")
	}
	if e == Bytes {
		return off, nil
	}
	k := sort.Search(len(m.checkpoints), func(i int) bool { return m.checkpoints[i].off > off }) - 1
	c := m.checkpoints[k]
	n := key(c, e)
	for i := c.off; i < off; {
		r, size := utf8.DecodeRuneInString(m.text[i:])
		if i+size > off {
			return 0, fmt.Errorf("byte offset is inside a UTF-8 scalar")
		}
		i += size
		if e == UTF16 {
			n += width(r)
		} else {
			n++
		}
	}
	return n, nil
}

// ByteOffsetFromUnit converts a flat unit offset to bytes. UTF-16 surrogate
// interiors are rejected. Line terminators count as their individual scalars.
func (m *Map) ByteOffsetFromUnit(n int, e Encoding) (int, error) {
	if !valid(e) || n < 0 {
		return 0, fmt.Errorf("invalid source offset or encoding")
	}
	if e == Bytes {
		if n > len(m.text) {
			return 0, fmt.Errorf("offset exceeds source")
		}
		return n, nil
	}
	k := sort.Search(len(m.checkpoints), func(i int) bool { return key(m.checkpoints[i], e) > n }) - 1
	if k < 0 {
		return 0, fmt.Errorf("invalid offset")
	}
	c := m.checkpoints[k]
	off, cur := c.off, key(c, e)
	for cur < n && off < len(m.text) {
		r, size := utf8.DecodeRuneInString(m.text[off:])
		w := 1
		if e == UTF16 {
			w = width(r)
		}
		if cur+w > n {
			return 0, fmt.Errorf("offset is inside a UTF-16 surrogate pair")
		}
		cur += w
		off += size
	}
	if cur != n {
		return 0, fmt.Errorf("offset exceeds source")
	}
	return off, nil
}

// ByteOffset converts a line/character position, excluding line terminators.
func (m *Map) ByteOffset(p Position, e Encoding) (int, error) {
	if !valid(e) || p.Line < 0 || p.Line >= len(m.lines) || p.Character < 0 {
		return 0, fmt.Errorf("invalid source position or encoding")
	}
	start, end := m.lines[p.Line], m.lineEnd(p.Line)
	lo, _ := m.UnitOffset(start, e)
	hi, _ := m.UnitOffset(end, e)
	// Compare before addition to avoid overflowing a caller-provided character.
	if p.Character > hi-lo {
		return 0, fmt.Errorf("character exceeds line")
	}
	return m.ByteOffsetFromUnit(lo+p.Character, e)
}

// Position converts a byte offset to line/character coordinates. The CRLF
// interior is rejected even in byte encoding, because it cannot round-trip.
func (m *Map) Position(off int, e Encoding) (Position, error) {
	n, err := m.UnitOffset(off, e)
	if err != nil {
		return Position{}, err
	}
	line := sort.Search(len(m.lines), func(i int) bool { return m.lines[i] > off }) - 1
	if off > m.lineEnd(line) {
		return Position{}, fmt.Errorf("offset is inside a CRLF terminator")
	}
	lo, _ := m.UnitOffset(m.lines[line], e)
	return Position{Line: line, Character: n - lo}, nil
}
