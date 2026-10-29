package core

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// DocumentPiece is an immutable range of source text. In CodePoints mode text
// is normalized to the same rune sequence that the flat Document stores.
type DocumentPiece struct {
	start int
	Text  string
	Runes []rune
	Offs  []int32
	// Large source chunks need native-width byte offsets. Ordinary chunks keep
	// the smaller table, including when they are slices of a larger document.
	LargeOffs []int
}

func newDocumentPiece(text string, unit PositionUnit) DocumentPiece {
	if unit == Bytes {
		return DocumentPiece{Text: text}
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
		return DocumentPiece{Text: text, Runes: runes, LargeOffs: offs}
	}
	offs := make([]int32, 0, len(runes)+1)
	for i := range text {
		offs = append(offs, int32(i))
	}
	offs = append(offs, int32(len(text)))
	return DocumentPiece{Text: text, Runes: runes, Offs: offs}
}

func (p DocumentPiece) byteOffset(i int) int {
	if p.LargeOffs != nil {
		return p.LargeOffs[i] - p.LargeOffs[0]
	}
	return int(p.Offs[i] - p.Offs[0])
}

func (p DocumentPiece) length(unit PositionUnit) int {
	if unit == Bytes {
		return len(p.Text)
	}
	return len(p.Runes)
}

func (p DocumentPiece) Sub(from, to int, unit PositionUnit) DocumentPiece {
	if unit == Bytes {
		return DocumentPiece{Text: p.Text[from:to]}
	}
	lo, hi := p.byteOffset(from), p.byteOffset(to)
	sub := DocumentPiece{Text: p.Text[lo:hi], Runes: p.Runes[from:to]}
	if p.LargeOffs != nil {
		sub.LargeOffs = p.LargeOffs[from : to+1]
	} else {
		sub.Offs = p.Offs[from : to+1]
	}
	return sub
}

func (p DocumentPiece) Substring(from, to int, unit PositionUnit) string {
	if unit == Bytes {
		return p.Text[from:to]
	}
	lo, hi := p.byteOffset(from), p.byteOffset(to)
	return p.Text[lo:hi]
}

// documentPieces keeps the editable text separate from the contiguous batch
// and stream input paths. Each edit copies descriptors, not unchanged text.
type documentPieces struct {
	unit   PositionUnit
	Parts  []DocumentPiece
	N      int
	Full   string
	FullOK bool
}

func NewDocumentPieces(text string, unit PositionUnit) *documentPieces {
	p := newDocumentPiece(text, unit)
	d := &documentPieces{unit: unit, N: p.length(unit)}
	if d.N != 0 {
		d.Parts = []DocumentPiece{p}
	}
	return d
}

// find locates a logical position. An end position has no containing piece.
func (d *documentPieces) find(pos int) (int, int) {
	i := sort.Search(len(d.Parts), func(i int) bool {
		p := &d.Parts[i]
		return p.start+p.length(d.unit) > pos
	})
	if i == len(d.Parts) {
		return i, 0
	}
	return i, pos - d.Parts[i].start
}

func (d *documentPieces) At(pos int) (rune, bool) {
	if pos < 0 || pos >= d.N {
		return 0, false
	}
	i, off := d.find(pos)
	if d.unit == Bytes {
		return rune(d.Parts[i].Text[off]), true
	}
	return d.Parts[i].Runes[off], true
}

func (in *Input) decodePieces(i int) (r rune, size, examined int, ok bool) {
	if i >= in.Pieces.N {
		return 0, 0, i + 1, false
	}
	if in.Unit == CodePoints {
		r, _ = in.Pieces.At(i)
		return r, 1, i + 1, true
	}
	var buf [utf8.UTFMax]byte
	available := min(utf8.UTFMax, in.Pieces.N-i)
	in.inputWork(uint64(available))
	for j := 0; j < available; j++ {
		b, _ := in.Pieces.At(i + j)
		buf[j] = byte(b)
	}
	b := buf[:available]
	r, size = utf8.DecodeRune(b)
	examined = i + size
	if r == utf8.RuneError && size == 1 {
		examined = i + min(utf8.UTFMax, in.Pieces.N-i)
		if !utf8.FullRune(b) {
			examined++
		}
	}
	return r, size, examined, true
}

func (p *Parser) matchLiteralPieces(rs []rune, bs []byte) bool {
	var k, n int
	if p.Unit == Bytes {
		n = len(bs)
		for k < n {
			p.inputWork(1)
			b, ok := p.Pieces.At(p.Pos + k)
			if !ok || byte(b) != bs[k] {
				break
			}
			k++
		}
	} else {
		n = len(rs)
		for k < n {
			p.inputWork(1)
			r, ok := p.Pieces.At(p.Pos + k)
			if !ok || r != rs[k] {
				break
			}
			k++
		}
	}
	if k == n {
		p.touch(p.Pos + n)
		p.Pos += n
		return true
	}
	if p.Unit == Bytes && p.LiteralHasReplacement(rs) {
		return p.matchLiteralRunes(rs)
	}
	p.touch(p.Pos + k + 1)
	p.Pos += k
	return false
}

// ValidBoundary applies the byte-position edit rule to the logical sequence,
// including UTF-8 scalars that cross two immutable pieces.
func (d *documentPieces) ValidBoundary(pos int) error {
	if d.unit != Bytes || pos == d.N {
		return nil
	}
	b, _ := d.At(pos)
	if utf8.RuneStart(byte(b)) {
		return nil
	}
	for start := pos - 1; start >= max(0, pos-utf8.UTFMax+1); start-- {
		b, _ := d.At(start)
		if !utf8.RuneStart(byte(b)) {
			continue
		}
		var buf [utf8.UTFMax]byte
		n := min(utf8.UTFMax, d.N-start)
		for i := 0; i < n; i++ {
			b, _ := d.At(start + i)
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

func (d *documentPieces) partition(pos int) (before, after []DocumentPiece) {
	for _, p := range d.Parts {
		end := p.start + p.length(d.unit)
		switch {
		case end <= pos:
			before = append(before, p)
		case p.start >= pos:
			after = append(after, p)
		default:
			before = append(before, p.Sub(0, pos-p.start, d.unit))
			after = append(after, p.Sub(pos-p.start, end-p.start, d.unit))
		}
	}
	return before, after
}

func (d *documentPieces) Replace(start, end int, text string) int {
	left, _ := d.partition(start)
	_, right := d.partition(end)
	added := newDocumentPiece(text, d.unit)
	parts := make([]DocumentPiece, 0, len(left)+len(right)+1)
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
	d.Parts, d.N, d.Full, d.FullOK = parts, n, "", false
	if len(d.Parts) > 512 {
		// Bound descriptor search and references to abandoned source chunks.
		flat := d.Text(0, d.N)
		*d = *NewDocumentPieces(flat, d.unit)
	}
	return added.length(d.unit) - (end - start)
}

func (d *documentPieces) Text(start, end int) string {
	if start == end {
		return ""
	}
	if start == 0 && end == d.N && d.FullOK {
		return d.Full
	}
	i, offset := d.find(start)
	if p := d.Parts[i]; end <= p.start+p.length(d.unit) {
		return p.Substring(offset, offset+end-start, d.unit)
	}
	var b strings.Builder
	for pos := start; pos < end; i++ {
		p := d.Parts[i]
		until := min(end, p.start+p.length(d.unit))
		b.WriteString(p.Substring(pos-p.start, until-p.start, d.unit))
		pos = until
	}
	s := b.String()
	if start == 0 && end == d.N {
		d.Full, d.FullOK = s, true
	}
	return s
}

// Small Documents retain the flat representation: at 1 KiB, piece lookup and
// descriptor updates cost more than copying, while the measured 4 KiB edit
// cases improve in both position units.
const DocumentPieceMinSize = 4 << 10

func UseDocumentPieces(text string, unit PositionUnit) bool {
	if !DocumentPieceInput {
		return false
	}
	if unit == Bytes {
		return len(text) >= DocumentPieceMinSize
	}
	return utf8.RuneCountInString(text) >= DocumentPieceMinSize
}
