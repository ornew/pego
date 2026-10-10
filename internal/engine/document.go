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
//   - Completed intermediate results that depended on an unfinished left-recursion
//     seed are discarded after any edit, even outside their examined input range.
//
// Edit applies these rules only to the results at the edited positions; any other result applies
// the edits made since its last use when it is next looked up (advanceEntry).
//
// The nodes of a shifted result are moved in place when the result is reused (moveResult), so
// trees returned by earlier parses change with them.
type Document struct {
	prog   *Program
	start  string
	back   Backend
	depth  int // limit on call nesting depth
	trace  func(TraceEvent)
	in     input // current text (fully loaded)
	memo   *memoTable
	stats  Stats
	edits  []docEdit // all edits so far; nodes record how many their positions account for
	runs   map[runKey]*runRecord
	runGen uint32  // edit generation of runs; unchanged parses keep this map
	kids   []*Node // the parser's stack of repetition values, kept for the next parse
	// resumed is the number of repetition elements the last Parse resumed (for tests).
	resumed int
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
	return &Document{prog: prog, start: start, back: o.Backend, depth: o.maxDepth(o.Backend), trace: o.Trace, in: newTextInput(text, o.Unit, true), memo: newMemoTable()}, nil
}

// Text returns the current text.
func (d *Document) Text() string { return d.in.text(0, d.in.loaded()) }

// Stats returns the evaluation and memo usage counts of the last Parse.
func (d *Document) Stats() Stats { return d.stats }

// Parse parses the current text.
func (d *Document) Parse() (*Node, error) {
	runs, lastRuns := d.runs, d.runs
	gen := uint32(len(d.edits))
	if runs != nil && d.runGen == gen {
		// Memo hits may skip every repetition. Keep valid same-generation
		// records without copying the map or advancing their edit generation.
		lastRuns = nil
	} else {
		runs = map[runKey]*runRecord{}
	}
	p := &parser{prog: d.prog, input: d.in, memo: d.memo, memoAll: true, noPlain: true, maxDepth: d.depth, gen: gen, edits: d.edits,
		runs: runs, lastRuns: lastRuns, kidStack: d.kids}
	p.setTrace(d.trace)
	defer func() {
		d.stats = p.stats
		d.kids = p.kidStack[:0]
		clear(d.kids[:cap(d.kids)]) // let go of the nodes, even on a trace panic
		// Keep the tables the parse built on demand, so later parses and edits reuse them.
		d.in.offs, d.in.lines = p.offs, p.lines
		if p.aborted {
			// Completed calls may depend on provisional LR seeds too, so
			// removing only entries marked growing is insufficient.
			d.memo = newMemoTable()
			d.runs, d.kids, d.resumed = nil, nil, 0
		} else {
			d.runs, d.resumed = p.runs, p.resumed
			d.runGen = p.gen
		}
	}()
	return d.prog.run(p, d.back, d.start)
}

// Edit replaces [start, end) of the text (in the Document's position unit) with text.
// With byte positions, start and end must lie on character boundaries.
// Invalid UTF-8 bytes are separate characters; an edit may complete a prefix.
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
		d.memo, d.edits, d.runs = newMemoTable(), nil, nil
		return nil
	}
	d.edits = append(d.edits, docEdit{start, end, delta})
	d.memo.splice(start, end, delta, func(e *memoEntry) bool { return advanceEntry(e, d.edits) })
	return nil
}

// advanceEntry applies to the memo entry e the edits it does not account for yet (those after the
// first e.vgen of edits), and reports whether it is still valid. For each edit [start, end), in
// order, an entry that examined only input before it is kept as is, one that examined only input
// after it is shifted (unless its result depends on positions or contains recovered errors, whose
// messages contain positions), and any other is invalid.
// The entry is changed only if it is still valid: one that is not may be looked up again (when a
// parse is aborted before the entry is replaced).
func advanceEntry(e *memoEntry, edits []docEdit) bool {
	if e.growing || e.provisional && e.vgen != uint32(len(edits)) {
		return false
	}
	from, examined, shift, shifted := e.from, e.examined, 0, e.shifted
	for _, ed := range edits[e.vgen:] {
		switch {
		case examined <= ed.start:
		case from >= ed.end && !e.positional && len(e.errs) == 0:
			from += ed.delta
			examined += ed.delta
			shift += ed.delta
			shifted = shifted || ed.delta != 0 // an edit that keeps the length moves nothing
		default:
			return false
		}
	}
	e.pos += shift
	e.end += shift
	e.from, e.examined = from, examined
	e.far += shift
	e.shift += int32(shift)
	e.shifted = shifted
	e.vgen = uint32(len(edits))
	return true
}
