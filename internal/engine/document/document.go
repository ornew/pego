package document

import (
	"fmt"
	"weak"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/value"
)

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
// the edits made since its last use when it is next looked up (AdvanceEntry).
//
// The nodes of a shifted result are moved in place when the result is reused (moveResult), so
// trees returned by earlier parses change with them.
type Document struct {
	changes  *documentChanges
	identity *documentIdentity
	sequence uint64
	runner   Runner
	start    string
	back     core.Backend // resolved construction backend
	defaults core.ParseOptions
	in       core.Input // current text (fully loaded)
	memo     *core.MemoTable
	stats    core.Stats
	edits    []core.DocEdit // all edits so far; nodes record how many their positions account for
	runs     map[core.RunKey]*core.RunRecord
	runGen   uint32            // edit generation of runs; unchanged parses keep this map
	kids     []*value.Node     // the parser's stack of repetition values, kept for the next parse
	machine  core.BackendState // the backend state of the last parse, cleared and kept for the next one
	// resumed is the number of repetition elements the last Parse resumed (for tests).
	resumed int
	// Candidate-only weak reference: dropped snapshots must remain collectible.
	previousSnapshot weak.Pointer[snapshotGraph]
}

// Runner is the program a Document parses with. The engine's Program
// implements it; the Document only resolves the backend, prepares the program
// for one parse and runs it.
type Runner interface {
	// SelectBackend returns the backend that a request for b runs on.
	SelectBackend(b core.Backend) core.Backend
	// PrepareRun returns the runner that parses on backend b under the budget control c.
	PrepareRun(b core.Backend, c *core.BudgetControl) (Runner, error)
	// Shared returns the program part that a parser for this runner reads.
	Shared() *core.SharedProgram
	// Run parses p's input from the rule start on backend b.
	Run(p *core.Parser, b core.Backend, start string) (*value.Node, error)
}

// New returns a Document that parses the text in from the rule start
// with runner on backend back. o holds the construction options.
func New(runner Runner, start string, back core.Backend, o core.ParseOptions, in core.Input) *Document {
	return &Document{identity: new(documentIdentity), sequence: 1, runner: runner, start: start, back: back, defaults: o, in: in, memo: core.NewMemoTable()}
}

// maxEdits bounds the edit log: when it is full, the memo is dropped and the log restarts. It is a
// variable for tests.
var maxEdits = 1 << 16

// removeProvisional is false only in tests that check that their checks detect provisional
// results reused by a later parse.
var removeProvisional = true

// ParseDefaults returns a copy of the construction options. Invocation counters
// are separate from these defaults.
func (d *Document) ParseDefaults() core.ParseOptions { return d.defaults }

// Text returns the current text.
func (d *Document) Text() string { return d.in.Text(0, d.in.Loaded()) }

// Stats returns the evaluation and memo usage counts of the last Parse.
func (d *Document) Stats() core.Stats { return d.stats }

// Parse parses the current text. Parser-produced syntax diagnostics belong to
// this call; returned Nodes can share the Document's mutable graph.
func (d *Document) Parse() (*value.Node, error) { return d.ParseWith(d.defaults) }

// ParseWith parses using fully resolved options. Unit, effective backend and
// recognition mode must match construction; this method does not merge defaults.
func (d *Document) ParseWith(o core.ParseOptions) (*value.Node, error) {
	if d.changes != nil {
		var n *value.Node
		_, err := d.snapshotWith(o, &n)
		return n, err
	}
	return d.parseWithCapture(o, nil)
}

// parseWithCapture keeps parsing and result capture inside one interruption and
// cleanup boundary. capture uses the same controller as the parser.
func (d *Document) parseWithCapture(o core.ParseOptions, capture func(*core.Parser, *value.Node, error) error) (n *value.Node, err error) {
	if d.runner.Shared().HasMissing || o.MissingTokens != 0 {
		return nil, compile.MissingUnsupported("Document parsing")
	}
	if err := core.ValidateStreamDiagnostics(o, false); err != nil {
		return nil, err
	}
	if o.Unit != d.in.Unit || d.runner.SelectBackend(o.Backend) != d.back || o.Recognize || o.Expectations != d.defaults.Expectations {
		return nil, fmt.Errorf("document parse cannot change unit, backend, recognition mode or expectations")
	}
	var p *core.Parser
	executionStarted := false
	defer func() {
		x := recover()
		if p != nil {
			if executionStarted {
				if x != nil {
					p.Aborted = true
				}
				d.finishParse(p)
			}
			// Setup has not changed Document-owned state. Detach only the
			// temporary parser if admission or the pre-run check failed.
			p.Control = nil
			p.ReleaseTraceReferences()
		}
		if x != nil {
			if f, ok := x.(core.Fatal); ok {
				n, err = nil, f.Err
			} else if cp, ok := x.(core.ContextPanic); ok {
				panic(cp.Value)
			} else {
				// run already classified VM faults; never classify again.
				panic(x)
			}
		}
	}()
	lim, _ := core.ResolveLimits(o.Limits, uint64(d.in.Loaded()), false)
	if err := core.CheckInputUnits(lim, uint64(d.in.Loaded())); err != nil {
		return nil, err
	}
	c := core.NewBudgetControl(o.Context, lim)
	if !core.BudgetControlsEnabled && c != nil {
		return nil, core.ErrNoControls
	}
	c.Check()
	active, setupErr := d.runner.PrepareRun(d.back, c)
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
		runs = map[core.RunKey]*core.RunRecord{}
	}
	p = &core.Parser{Prog: active.Shared(), Input: d.in, Memo: d.memo, MemoAll: true, NoPlain: true, MaxDepth: core.DepthLimit(o, d.back), Gen: gen, Edits: d.edits, Expectations: o.Expectations,
		Runs: runs, LastRuns: lastRuns, KidStack: d.kids}
	p.Control = c
	p.SetTrace(o.Trace)
	c.Check()
	if reuseDocumentVMValues && d.machine != nil {
		d.machine.Keep(d.in.Loaded(), false)
		d.machine.Attach(p)
		p.Backend = d.machine
	}
	executionStarted = true
	n, err = active.Run(p, d.back, d.start)
	if !p.Aborted {
		c.Check()
		// Snapshot capture publishes its own diagnostics. Ordinary Parse must
		// detach recovered errors from memo entries before exposing them.
		if capture == nil {
			if recovered, ok := err.(core.SyntaxErrors); ok {
				err = copyDocumentDiagnostics(p, recovered)
				c.Check()
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
func (d *Document) finishParse(p *core.Parser) {
	d.stats = p.ReportedStats()
	d.kids = p.KidStack[:0]
	clear(d.kids[:cap(d.kids)]) // let go of the nodes, even on a trace panic
	if reuseDocumentVMValues {
		d.machine = p.Backend
		if d.machine != nil {
			// A whole-rule memo hit evaluates no bodies and never writes the
			// backend's stacks. Their already cleared storage needs no second scan.
			d.machine.Keep(d.in.Loaded(), p.Stats.Evaluated != 0)
		}
	}
	// Keep the tables the parse built on demand, so later parses and edits reuse them.
	d.in.Offs, d.in.Lines = p.Offs, p.Lines
	if p.Aborted {
		// Completed calls may depend on provisional LR seeds too, so
		// removing only entries marked growing is insufficient.
		d.invalidateParseState()
	} else {
		// A provisional result depends on the order in which this parse evaluated the calls, so
		// a later parse does not reuse it, even of the same text (see "Evaluations of Rules" in
		// spec/runtime/parsing.md).
		if removeProvisional {
			d.memo.RemoveProvisional(p.ProvisionalEntries)
		}
		d.runs, d.resumed = p.Runs, p.Resumed
		d.runGen = p.Gen
	}
}

func (d *Document) invalidateParseState() {
	clear(d.kids[:cap(d.kids)])
	if d.machine != nil {
		d.machine.Release()
	}
	d.memo = core.NewMemoTable()
	d.runs, d.kids, d.machine, d.resumed = nil, nil, nil, 0
}

// Edit replaces [start, end) of the text (in the Document's position unit) with text.
// With byte positions, start and end must lie on character boundaries.
// Invalid UTF-8 bytes are separate characters; an edit may complete a prefix.
func (d *Document) Edit(start, end int, text string) error {
	n := d.in.Loaded()
	if start < 0 || end < start || end > n {
		return fmt.Errorf("invalid range [%d,%d) for text of length %d", start, end, n)
	}
	if d.in.Unit == core.Bytes {
		for _, pos := range []int{start, end} {
			var err error
			if core.DocumentPieceInput && d.in.Pieces != nil {
				err = d.in.Pieces.ValidBoundary(pos)
			} else {
				err = core.ValidBoundary(d.in.Src, core.Bytes, pos)
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
	delta := d.in.Replace(start, end, text)
	d.sequence++
	d.recordEdit(from, []TextEdit{{Start: start, End: end, Text: text}})
	if core.DocumentPieceInput && d.in.Pieces == nil && d.in.Loaded() >= core.DocumentPieceMinSize {
		// A small Document can grow into the measured range where the piece
		// representation wins. Promote once, after the flat edit has completed.
		d.in.Pieces = core.NewDocumentPieces(d.in.Text(0, d.in.Loaded()), d.in.Unit)
		d.in.In, d.in.Bs, d.in.Src, d.in.Offs, d.in.SrcOK = nil, nil, "", nil, false
	}
	if len(d.edits) == maxEdits {
		d.memo, d.edits, d.runs = core.NewMemoTable(), nil, nil
		return nil
	}
	d.edits = append(d.edits, core.DocEdit{Start: start, End: end, Delta: delta})
	d.memo.Splice(start, end, delta, func(e *core.MemoEntry) bool { return core.AdvanceEntry(e, d.edits) })
	return nil
}
