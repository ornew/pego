package document

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/internal/engine/core"
)

// documentIdentity deliberately has nonzero size. Tokens retain only this
// allocation, not their Document, input, memo tables or parsed results.
type documentIdentity struct{ marker byte }

// DocumentVersion identifies one text revision of one Document. Its zero value
// is invalid. Tokens are comparable and do not retain document contents.
type DocumentVersion struct {
	identity *documentIdentity
	sequence uint64
}

// Sequence returns the text revision number, starting at one.
func (v DocumentVersion) Sequence() uint64 { return v.sequence }

// TextEdit replaces [Start, End) in the position unit of its base Document.
type TextEdit struct {
	Start, End int
	Text       string
}

// Version returns the current text revision. Parsing does not advance it.
func (d *Document) Version() DocumentVersion { return DocumentVersion{d.identity, d.sequence} }

// Unit returns the position unit selected at document construction.
func (d *Document) Unit() core.PositionUnit { return d.in.Unit }

func (d *Document) nextVersion() error {
	if d.sequence == ^uint64(0) {
		return fmt.Errorf("document version sequence exhausted")
	}
	return nil
}

// EditBatch atomically applies ordered, nonoverlapping replacements in base
// coordinates. All edits are validated before any document state is changed.
// A nonempty batch advances one revision and discards reusable parse caches.
func (d *Document) EditBatch(base DocumentVersion, edits ...TextEdit) error {
	if base.identity == nil || base.identity != d.identity {
		return fmt.Errorf("invalid document version identity")
	}
	if base.sequence != d.sequence {
		return fmt.Errorf("stale document version %d; current version is %d", base.sequence, d.sequence)
	}
	if len(edits) == 0 {
		return nil
	}
	n := d.in.Loaded()
	for i, edit := range edits {
		fail := func(err error) error { return fmt.Errorf("edit %d [%d,%d): %w", i, edit.Start, edit.End, err) }
		if edit.Start < 0 || edit.End < edit.Start || edit.End > n {
			return fail(fmt.Errorf("invalid range for text of length %d", n))
		}
		if i > 0 && (edits[i-1].Start >= edit.Start || edits[i-1].End > edit.Start) {
			return fail(fmt.Errorf("edits must have strictly increasing starts and nonoverlapping ranges"))
		}
	}
	if d.in.Unit == core.Bytes {
		for i, edit := range edits {
			for _, pos := range []int{edit.Start, edit.End} {
				var err error
				if core.DocumentPieceInput && d.in.Pieces != nil {
					err = d.in.Pieces.ValidBoundary(pos)
				} else {
					err = core.ValidBoundary(d.in.Src, core.Bytes, pos)
				}
				if err != nil {
					return fmt.Errorf("edit %d [%d,%d): %w", i, edit.Start, edit.End, err)
				}
			}
		}
	}
	if err := d.nextVersion(); err != nil {
		return err
	}
	// Reading code-point text may populate offset tables or a piece full-text
	// cache. Prepare from private copies so any later size error is atomic too.
	source := d.in
	if source.Pieces != nil {
		pieces := *source.Pieces
		source.Pieces = &pieces
	}
	var parts []string
	finalUnits, finalBytes, pos := 0, 0, 0
	add := func(text string, units int) error {
		var err error
		finalUnits, err = checkedDocumentLength(finalUnits, units)
		if err != nil {
			return err
		}
		finalBytes, err = checkedDocumentLength(finalBytes, len(text))
		if err != nil {
			return err
		}
		parts = append(parts, text)
		return nil
	}
	for i, edit := range edits {
		if err := add(source.Text(pos, edit.Start), edit.Start-pos); err != nil {
			return fmt.Errorf("edit %d [%d,%d): %w", i, edit.Start, edit.End, err)
		}
		text := edit.Text
		units := core.TextLen(d.in.Unit, text)
		if d.in.Unit != core.Bytes && !utf8.ValidString(text) {
			text = string([]rune(text))
		}
		if err := add(text, units); err != nil {
			return fmt.Errorf("edit %d [%d,%d): %w", i, edit.Start, edit.End, err)
		}
		pos = edit.End
	}
	if err := add(source.Text(pos, n), n-pos); err != nil {
		return fmt.Errorf("edit %d [%d,%d): %w", len(edits)-1, edits[len(edits)-1].Start, edits[len(edits)-1].End, err)
	}
	var builder strings.Builder
	builder.Grow(finalBytes)
	for _, part := range parts {
		builder.WriteString(part)
	}
	text := builder.String()
	in := newDocumentInput(text, d.in.Unit)
	if d.in.Pieces != nil && in.Pieces == nil {
		// Promotion is one-way, including batches that shrink the input.
		in = core.Input{Unit: d.in.Unit, Eof: true, BaseLine: 1, BaseCol: 1, Pieces: core.NewDocumentPieces(text, d.in.Unit)}
	}
	// Publication follows all ordinary failure points. No Parse is performed;
	// old result Nodes remain untouched until a later ordinary Parse.
	d.in = in
	d.memo = core.NewMemoTable()
	d.edits, d.runs, d.kids, d.machine = nil, nil, nil, nil
	d.runGen, d.resumed = 0, 0
	d.sequence++
	d.recordEdit(base, edits)
	return nil
}

func checkedDocumentLength(length, added int) (int, error) {
	if length < 0 || added < 0 || added > int(^uint(0)>>1)-length {
		return 0, fmt.Errorf("edited document length exceeds maximum int")
	}
	return length + added, nil
}

func newDocumentInput(text string, unit core.PositionUnit) core.Input {
	if core.UseDocumentPieces(text, unit) {
		return core.Input{Unit: unit, Eof: true, BaseLine: 1, BaseCol: 1, Pieces: core.NewDocumentPieces(text, unit)}
	}
	return core.NewTextInput(text, unit, true)
}
