package engine

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// documentPiece is an immutable range of source text. In CodePoints mode text
// is normalized to the same rune sequence that the flat Document stores.
type documentPiece struct {
	start int
	text  string
	runes []rune
	offs  []int32
	// Large source chunks need native-width byte offsets. Ordinary chunks keep
	// the smaller table, including when they are slices of a larger document.
	largeOffs []int
}

func newDocumentPiece(text string, unit Unit) documentPiece {
	if unit == Bytes {
		return documentPiece{text: text}
	}
	runes := []rune(text)
	if !utf8.ValidString(text) {
		text = string(runes) // Normalize malformed UTF-8 at each insertion.
	}
	if int64(len(text)) > 1<<31-1 {
		offs := make([]int, 0, len(runes)+1)
		for i := range text {
			offs = append(offs, i)
		}
		offs = append(offs, len(text))
		return documentPiece{text: text, runes: runes, largeOffs: offs}
	}
	offs := make([]int32, 0, len(runes)+1)
	for i := range text {
		offs = append(offs, int32(i))
	}
	offs = append(offs, int32(len(text)))
	return documentPiece{text: text, runes: runes, offs: offs}
}

func (p documentPiece) byteOffset(i int) int {
	if p.largeOffs != nil {
		return p.largeOffs[i] - p.largeOffs[0]
	}
	return int(p.offs[i] - p.offs[0])
}

func (p documentPiece) length(unit Unit) int {
	if unit == Bytes {
		return len(p.text)
	}
	return len(p.runes)
}

func (p documentPiece) sub(from, to int, unit Unit) documentPiece {
	if unit == Bytes {
		return documentPiece{text: p.text[from:to]}
	}
	lo, hi := p.byteOffset(from), p.byteOffset(to)
	sub := documentPiece{text: p.text[lo:hi], runes: p.runes[from:to]}
	if p.largeOffs != nil {
		sub.largeOffs = p.largeOffs[from : to+1]
	} else {
		sub.offs = p.offs[from : to+1]
	}
	return sub
}

func (p documentPiece) substring(from, to int, unit Unit) string {
	if unit == Bytes {
		return p.text[from:to]
	}
	lo, hi := p.byteOffset(from), p.byteOffset(to)
	return p.text[lo:hi]
}

// documentPieces keeps the editable text separate from the contiguous batch
// and stream input paths. Each edit copies descriptors, not unchanged text.
type documentPieces struct {
	unit   Unit
	parts  []documentPiece
	n      int
	full   string
	fullOK bool
}

func newDocumentPieces(text string, unit Unit) *documentPieces {
	p := newDocumentPiece(text, unit)
	d := &documentPieces{unit: unit, n: p.length(unit)}
	if d.n != 0 {
		d.parts = []documentPiece{p}
	}
	return d
}

// find locates a logical position. An end position has no containing piece.
func (d *documentPieces) find(pos int) (int, int) {
	i := sort.Search(len(d.parts), func(i int) bool {
		p := &d.parts[i]
		return p.start+p.length(d.unit) > pos
	})
	if i == len(d.parts) {
		return i, 0
	}
	return i, pos - d.parts[i].start
}

func (d *documentPieces) at(pos int) (rune, bool) {
	if pos < 0 || pos >= d.n {
		return 0, false
	}
	i, off := d.find(pos)
	if d.unit == Bytes {
		return rune(d.parts[i].text[off]), true
	}
	return d.parts[i].runes[off], true
}

func (in *input) decodePieces(i int) (r rune, size, examined int, ok bool) {
	if i >= in.pieces.n {
		return 0, 0, i + 1, false
	}
	if in.unit == CodePoints {
		r, _ = in.pieces.at(i)
		return r, 1, i + 1, true
	}
	var buf [utf8.UTFMax]byte
	available := min(utf8.UTFMax, in.pieces.n-i)
	in.inputWork(uint64(available))
	for j := 0; j < available; j++ {
		b, _ := in.pieces.at(i + j)
		buf[j] = byte(b)
	}
	b := buf[:available]
	r, size = utf8.DecodeRune(b)
	examined = i + size
	if r == utf8.RuneError && size == 1 {
		examined = i + min(utf8.UTFMax, in.pieces.n-i)
		if !utf8.FullRune(b) {
			examined++
		}
	}
	return r, size, examined, true
}

func (p *parser) matchLiteralPieces(rs []rune, bs []byte) bool {
	var k, n int
	if p.unit == Bytes {
		n = len(bs)
		for k < n {
			p.inputWork(1)
			b, ok := p.pieces.at(p.pos + k)
			if !ok || byte(b) != bs[k] {
				break
			}
			k++
		}
	} else {
		n = len(rs)
		for k < n {
			p.inputWork(1)
			r, ok := p.pieces.at(p.pos + k)
			if !ok || r != rs[k] {
				break
			}
			k++
		}
	}
	if k == n {
		p.touch(p.pos + n)
		p.pos += n
		return true
	}
	if p.unit == Bytes && p.literalHasReplacement(rs) {
		return p.matchLiteralRunes(rs)
	}
	p.touch(p.pos + k + 1)
	p.pos += k
	return false
}

// validBoundary applies the byte-position edit rule to the logical sequence,
// including UTF-8 scalars that cross two immutable pieces.
func (d *documentPieces) validBoundary(pos int) error {
	if d.unit != Bytes || pos == d.n {
		return nil
	}
	b, _ := d.at(pos)
	if utf8.RuneStart(byte(b)) {
		return nil
	}
	for start := pos - 1; start >= max(0, pos-utf8.UTFMax+1); start-- {
		b, _ := d.at(start)
		if !utf8.RuneStart(byte(b)) {
			continue
		}
		var buf [utf8.UTFMax]byte
		n := min(utf8.UTFMax, d.n-start)
		for i := 0; i < n; i++ {
			b, _ := d.at(start + i)
			buf[i] = byte(b)
		}
		_, size := utf8.DecodeRune(buf[:n])
		if start+size > pos {
			return fmt.Errorf("position %d is not at a character boundary", pos)
		}
		break
	}
	return nil
}

func (d *documentPieces) partition(pos int) (before, after []documentPiece) {
	for _, p := range d.parts {
		end := p.start + p.length(d.unit)
		switch {
		case end <= pos:
			before = append(before, p)
		case p.start >= pos:
			after = append(after, p)
		default:
			before = append(before, p.sub(0, pos-p.start, d.unit))
			after = append(after, p.sub(pos-p.start, end-p.start, d.unit))
		}
	}
	return before, after
}

func (d *documentPieces) replace(start, end int, text string) int {
	left, _ := d.partition(start)
	_, right := d.partition(end)
	added := newDocumentPiece(text, d.unit)
	parts := make([]documentPiece, 0, len(left)+len(right)+1)
	parts = append(parts, left...)
	if added.length(d.unit) != 0 {
		parts = append(parts, added)
	}
	parts = append(parts, right...)
	n := 0
	for i := range parts {
		parts[i].start = n
		n += parts[i].length(d.unit)
	}
	d.parts, d.n, d.full, d.fullOK = parts, n, "", false
	if len(d.parts) > 512 {
		// Bound descriptor search and references to abandoned source chunks.
		flat := d.text(0, d.n)
		*d = *newDocumentPieces(flat, d.unit)
	}
	return added.length(d.unit) - (end - start)
}

func (d *documentPieces) text(start, end int) string {
	if start == end {
		return ""
	}
	if start == 0 && end == d.n && d.fullOK {
		return d.full
	}
	i, offset := d.find(start)
	if p := d.parts[i]; end <= p.start+p.length(d.unit) {
		return p.substring(offset, offset+end-start, d.unit)
	}
	var b strings.Builder
	for pos := start; pos < end; i++ {
		p := d.parts[i]
		until := min(end, p.start+p.length(d.unit))
		b.WriteString(p.substring(pos-p.start, until-p.start, d.unit))
		pos = until
	}
	s := b.String()
	if start == 0 && end == d.n {
		d.full, d.fullOK = s, true
	}
	return s
}
