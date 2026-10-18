package engine

import (
	"strings"
	"unicode/utf8"
)

// Controlled construction selects pieces after the charged decoding pass,
// avoiding an uninterruptible rune-count scan merely to choose storage.
func newControlledDocumentInput(text string, unit PositionUnit, c *workControl) input {
	if !workControlsEnabled || c == nil {
		if useDocumentPieces(text, unit) {
			return input{unit: unit, eof: true, baseLine: 1, baseCol: 1, pieces: newDocumentPieces(text, unit)}
		}
		return newTextInputIn(text, unit, true, nil)
	}
	if documentPieceInput && unit == Bytes && len(text) >= documentPieceMinSize {
		// Immutable byte pieces retain the supplied string without copying it.
		return input{unit: unit, eof: true, baseLine: 1, baseCol: 1, pieces: newDocumentPieces(text, unit)}
	}
	in := newControlledTextInput(text, unit, true, nil, c)
	if !documentPieceInput || in.loaded() < documentPieceMinSize {
		return in
	}
	piece := documentPiece{text: text, runes: in.in, offs: in.offs}
	if !in.srcOK {
		piece.text = in.detachedText(0, len(in.in))
		piece.offs = nil
		if int64(len(piece.text)) > 1<<31-1 {
			piece.largeOffs = make([]int, len(in.in)+1)
		} else {
			piece.offs = make([]int32, len(in.in)+1)
		}
		offset := 0
		for i, r := range in.in {
			in.inputWork(1)
			if piece.largeOffs != nil {
				piece.largeOffs[i] = offset
			} else {
				piece.offs[i] = int32(offset)
			}
			offset += utf8.RuneLen(r)
		}
		if piece.largeOffs != nil {
			piece.largeOffs[len(in.in)] = offset
		} else {
			piece.offs[len(in.in)] = int32(offset)
		}
	}
	in.pieces = &documentPieces{unit: unit, parts: []documentPiece{piece}, n: len(in.in)}
	in.in, in.offs, in.src, in.srcOK = nil, nil, "", false
	return in
}

// Materialization across immutable pieces charges bounded copies. Publish a
// cached whole-document string only once every copy succeeds.
func (in *input) controlledPieceText(start, end int) string {
	d := in.pieces
	if start == end {
		return ""
	}
	if start == 0 && end == d.n && d.fullOK {
		return d.full
	}
	i, off := d.find(start)
	if piece := d.parts[i]; end <= piece.start+piece.length(d.unit) {
		return piece.substring(off, off+end-start, d.unit)
	}
	var out strings.Builder
	for pos := start; pos < end; i++ {
		in.control.charge(0, 1)
		piece := d.parts[i]
		until := min(end, piece.start+piece.length(d.unit))
		text := piece.substring(pos-piece.start, until-piece.start, d.unit)
		for offset := 0; offset < len(text); {
			n := min(4096, len(text)-offset)
			in.control.charge(uint64(n), 1)
			out.WriteString(text[offset : offset+n])
			offset += n
		}
		pos = until
	}
	text := out.String()
	if start == 0 && end == d.n {
		d.full, d.fullOK = text, true
	}
	return text
}
