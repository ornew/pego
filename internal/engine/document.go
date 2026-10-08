package engine

import "fmt"

// Document parses a text that is edited repeatedly. It reuses the memo of the previous parse for
// the parts the edits do not affect.
//
// Each memo result records the input range [from, examined) it examined. For an edit
// [start, end):
//   - Results with examined <= start are reused as is (they examined only input before the edit).
//   - Results with from >= end are reused with their positions shifted by the edit's length
//     difference (they examined only input after the edit). However, results of rules whose
//     result depends on position values, and results containing recovered errors, cannot be
//     shifted.
//   - All other results are discarded.
//
// The nodes of a shifted result are moved in place when the result is reused (moveResult), so
// trees returned by earlier parses change with them.
type Document struct {
	prog  *Program
	start string
	back  Backend
	depth int   // limit on call nesting depth
	in    input // current text (fully loaded)
	memo  *memoTable
	stats Stats
	edits []docEdit // all edits so far; nodes record how many their positions account for
}

// maxEdits bounds the edit log: when it is full, the memo is dropped and the log restarts. It is a
// variable for tests.
var maxEdits = 1 << 16

// NewDocument creates a Document that parses text with the rule start. Positions are in code
// points.
func (prog *Program) NewDocument(start, text string) (*Document, error) {
	return prog.NewDocumentWith(start, text, ParseOptions{})
}

// NewDocumentWith creates a Document with options such as the position unit. Positions passed
// to Edit use the same unit.
func (prog *Program) NewDocumentWith(start, text string, o ParseOptions) (*Document, error) {
	if o.Recognize {
		return nil, fmt.Errorf("recognition is not supported for documents")
	}
	if _, err := prog.rule(o.Backend, start); err != nil {
		return nil, err
	}
	return &Document{prog: prog, start: start, back: o.Backend, depth: o.maxDepth(o.Backend), in: newTextInput(text, o.Unit, true), memo: newMemoTable()}, nil
}

// Text returns the current text.
func (d *Document) Text() string { return d.in.text(0, d.in.loaded()) }

// Stats returns the evaluation and memo usage counts of the last Parse.
func (d *Document) Stats() Stats { return d.stats }

// Parse parses the current text.
func (d *Document) Parse() (*Node, error) {
	p := &parser{prog: d.prog, input: d.in, memo: d.memo, memoAll: true, maxDepth: d.depth, gen: uint32(len(d.edits)), edits: d.edits}
	n, err := d.prog.run(p, d.back, d.start)
	d.stats = p.stats
	// Keep the tables the parse built on demand, so that later parses and edits reuse them.
	d.in.offs, d.in.lines = p.offs, p.lines
	return n, err
}

// Edit replaces [start, end) of the text (in the Document's position unit) with text.
// With byte positions, start and end must lie on character boundaries.
func (d *Document) Edit(start, end int, text string) error {
	n := d.in.loaded()
	if start < 0 || end < start || end > n {
		return fmt.Errorf("invalid range [%d,%d) for text of length %d", start, end, n)
	}
	if d.in.unit == Bytes {
		for _, pos := range []int{start, end} {
			if err := validBoundary(d.in.src, Bytes, pos); err != nil {
				return err
			}
		}
	}
	delta := d.in.replace(start, end, text)
	if len(d.edits) == maxEdits {
		d.memo, d.edits = newMemoTable(), nil
		return nil
	}
	d.edits = append(d.edits, docEdit{start, end, delta})
	d.memo.splice(start, end, delta, func(e *memoEntry) int {
		switch {
		case e.growing:
		case e.examined <= start:
			return keepEntry
		case e.from >= end && !e.positional && len(e.errs) == 0:
			e.end += delta
			e.from += delta
			e.examined += delta
			e.far += delta
			e.shift += int32(delta)
			e.shifted = e.shifted || delta != 0 // an edit that keeps the length moves nothing
			return shiftEntry
		}
		return dropEntry
	})
	return nil
}
