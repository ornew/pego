package engine

import (
	"fmt"
	"unicode/utf8"
	"weak"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// Small Documents retain the flat representation: at 1 KiB, piece lookup and
// descriptor updates cost more than copying, while the measured 4 KiB edit
// cases improve in both position units.
const documentPieceMinSize = 4 << 10

func useDocumentPieces(text string, unit PositionUnit) bool {
	if !documentPieceInput {
		return false
	}
	if unit == Bytes {
		return len(text) >= documentPieceMinSize
	}
	return utf8.RuneCountInString(text) >= documentPieceMinSize
}

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
	changes  *documentChanges
	identity *documentIdentity
	sequence uint64
	prog     *Program
	start    string
	back     Backend // resolved construction backend
	defaults ParseOptions
	in       input // current text (fully loaded)
	memo     *memoTable
	stats    Stats
	edits    []docEdit // all edits so far; nodes record how many their positions account for
	runs     map[runKey]*runRecord
	runGen   uint32        // edit generation of runs; unchanged parses keep this map
	kids     []*value.Node // the parser's stack of repetition values, kept for the next parse
	vals     []any         // cleared VM value-stack storage, kept for the next parse
	// resumed is the number of repetition elements the last Parse resumed (for tests).
	resumed int
	// Candidate-only weak reference: dropped snapshots must remain collectible.
	previousSnapshot weak.Pointer[snapshotGraph]
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
func (prog *Program) NewDocumentWith(start, text string, o ParseOptions) (d *Document, err error) {
	if prog.HasMissing || o.MissingTokens != 0 {
		return nil, compile.MissingUnsupported("Document parsing")
	}
	if err := o.validateStreamDiagnostics(false); err != nil {
		return nil, err
	}
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok {
				d, err = nil, f.err
			} else if cp, ok := x.(contextPanic); ok {
				panic(cp.value)
			} else {
				panic(x)
			}
		}
	}()
	if o.Recognize {
		return nil, fmt.Errorf("recognition is not supported for documents")
	}
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, true); err != nil {
		return nil, err
	}
	_, _, c, err := prog.admitParse(start, o, false)
	if err != nil {
		return nil, err
	}
	c.check()
	in := newControlledDocumentInput(text, o.Unit, c)
	c.check()
	in.control = nil // construction counters never become parse defaults
	return &Document{identity: new(documentIdentity), sequence: 1, prog: prog, start: start, back: prog.backend(o.Backend), defaults: o, in: in, memo: newMemoTable()}, nil
}

// ParseDefaults returns a copy of the construction options. Invocation counters
// are separate from these defaults.
func (d *Document) ParseDefaults() ParseOptions { return d.defaults }

// Text returns the current text.
func (d *Document) Text() string { return d.in.text(0, d.in.loaded()) }

// Stats returns the evaluation and memo usage counts of the last Parse.
func (d *Document) Stats() Stats { return d.stats }

// Parse parses the current text. Parser-produced syntax diagnostics belong to
// this call; returned Nodes can share the Document's mutable graph.
func (d *Document) Parse() (*value.Node, error) { return d.ParseWith(d.defaults) }

// ParseWith parses using fully resolved options. Unit, effective backend and
// recognition mode must match construction; this method does not merge defaults.
func (d *Document) ParseWith(o ParseOptions) (*value.Node, error) {
	if d.changes != nil {
		var n *value.Node
		_, err := d.snapshotWith(o, &n)
		return n, err
	}
	return d.parseWithCapture(o, nil)
}

// parseWithCapture keeps parsing and result capture inside one interruption and
// cleanup boundary. capture uses the same controller as the parser.
func (d *Document) parseWithCapture(o ParseOptions, capture func(*parser, *value.Node, error) error) (n *value.Node, err error) {
	if d.prog.HasMissing || o.MissingTokens != 0 {
		return nil, compile.MissingUnsupported("Document parsing")
	}
	if err := o.validateStreamDiagnostics(false); err != nil {
		return nil, err
	}
	if o.Unit != d.in.unit || d.prog.backend(o.Backend) != d.back || o.Recognize {
		return nil, fmt.Errorf("document parse cannot change unit, backend or recognition mode")
	}
	var p *parser
	executionStarted := false
	defer func() {
		x := recover()
		if p != nil {
			if executionStarted {
				if x != nil {
					p.aborted = true
				}
				d.finishParse(p)
			}
			// Setup has not changed Document-owned state. Detach only the
			// temporary parser if admission or the pre-run check failed.
			p.control = nil
			p.releaseTraceReferences()
		}
		if x != nil {
			if f, ok := x.(fatal); ok {
				n, err = nil, f.err
			} else if cp, ok := x.(contextPanic); ok {
				panic(cp.value)
			} else {
				// run already classified VM faults; never classify again.
				panic(x)
			}
		}
	}()
	c := newWorkControl(o.Context, o.WorkLimits)
	if !workControlsEnabled && c != nil {
		return nil, fmt.Errorf("work controls are unavailable in the reference build")
	}
	c.check()
	active, setupErr := d.prog.prepareControlledProgram(d.back, c)
	if setupErr != nil {
		return nil, setupErr
	}
	runs, lastRuns := d.runs, d.runs
	gen := uint32(len(d.edits))
	if runs != nil && d.runGen == gen {
		// Memo hits may skip every repetition. Keep valid same-generation
		// records without copying the map or advancing their edit generation.
		lastRuns = nil
	} else {
		runs = map[runKey]*runRecord{}
	}
	p = &parser{prog: active, input: d.in, memo: d.memo, memoAll: true, noPlain: true, maxDepth: o.maxDepth(d.back), gen: gen, edits: d.edits,
		runs: runs, lastRuns: lastRuns, kidStack: d.kids}
	if reuseDocumentVMValues {
		// A large document may have been replaced with a small input.
		// Bound cached capacity by the current size, with room for small
		// grammars' fixed stack overhead. Division avoids int overflow.
		p.vals = d.vals
		if cap(p.vals)/2 > max(512, d.in.loaded()) {
			p.vals = nil
		}
	}
	p.control = c
	p.setTrace(o.Trace)
	c.check()
	executionStarted = true
	n, err = active.run(p, d.back, d.start)
	if !p.aborted {
		c.check()
		// Snapshot capture publishes its own diagnostics. Ordinary Parse must
		// detach recovered errors from memo entries before exposing them.
		if capture == nil {
			if recovered, ok := err.(SyntaxErrors); ok {
				err = copyDocumentDiagnostics(p, recovered)
				c.check()
			}
		}
	}
	if capture != nil {
		err = capture(p, n, err)
	}
	return n, err
}

// finishParse transfers reusable state before any traced parser is detached.
// Mandatory cleanup is unbudgeted, so interruption cannot abort it.
func (d *Document) finishParse(p *parser) {
	d.stats = p.reportedStats()
	d.kids = p.kidStack[:0]
	clear(d.kids[:cap(d.kids)]) // let go of the nodes, even on a trace panic
	if reuseDocumentVMValues {
		d.vals = p.vals[:0]
		if cap(d.vals)/2 > max(512, d.in.loaded()) {
			d.vals = nil
		}
		// A whole-rule memo hit evaluates no bodies and never writes the
		// VM stack. Its already cleared storage needs no second scan.
		if p.stats.Evaluated != 0 {
			clear(d.vals[:cap(d.vals)])
		}
	}
	// Keep the tables the parse built on demand, so later parses and edits reuse them.
	d.in.offs, d.in.lines = p.offs, p.lines
	if p.aborted {
		// Completed calls may depend on provisional LR seeds too, so
		// removing only entries marked growing is insufficient.
		d.invalidateParseState()
	} else {
		d.runs, d.resumed = p.runs, p.resumed
		d.runGen = p.gen
	}
}

func (d *Document) invalidateParseState() {
	clear(d.kids[:cap(d.kids)])
	clear(d.vals[:cap(d.vals)])
	d.memo = newMemoTable()
	d.runs, d.kids, d.vals, d.resumed = nil, nil, nil, 0
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
			var err error
			if documentPieceInput && d.in.pieces != nil {
				err = d.in.pieces.validBoundary(pos)
			} else {
				err = validBoundary(d.in.src, Bytes, pos)
			}
			if err != nil {
				return err
			}
		}
	}
	if err := d.nextVersion(); err != nil {
		return err
	}
	from := d.Version()
	delta := d.in.replace(start, end, text)
	d.sequence++
	d.recordEdit(from, []TextEdit{{Start: start, End: end, Text: text}})
	if documentPieceInput && d.in.pieces == nil && d.in.loaded() >= documentPieceMinSize {
		// A small Document can grow into the measured range where the piece
		// representation wins. Promote once, after the flat edit has completed.
		d.in.pieces = newDocumentPieces(d.in.text(0, d.in.loaded()), d.in.unit)
		d.in.in, d.in.bs, d.in.src, d.in.offs, d.in.srcOK = nil, nil, "", nil, false
	}
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
