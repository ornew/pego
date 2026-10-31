package document

import (
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/value"
)

// The external tests of this package parse with the engine's programs, which import this
// package, so they cannot be internal tests. These declarations give them the state that a
// Document and a Snapshot keep between parses.

var (
	CopySnapshotError     = copySnapshotError
	CheckedDocumentLength = checkedDocumentLength
)

const (
	ReuseDocumentVMValues      = reuseDocumentVMValues
	SnapshotSharingEnabled     = snapshotSharingEnabled
	SnapshotChildChunksEnabled = snapshotChildChunksEnabled
)

// KeepProvisional makes later parses of Documents reuse provisional results, which they must not,
// and returns a function that restores removing them.
func KeepProvisional() (restore func()) {
	removeProvisional = false
	return func() { removeProvisional = true }
}

// SetMaxEdits sets the bound of the edit log to n and returns a function that restores it.
func SetMaxEdits(n int) (restore func()) {
	old := maxEdits
	maxEdits = n
	return func() { maxEdits = old }
}

func (d *Document) Memo() *core.MemoTable                 { return d.memo }
func (d *Document) Runs() map[core.RunKey]*core.RunRecord { return d.runs }
func (d *Document) SetRuns(r map[core.RunKey]*core.RunRecord) {
	d.runs = r
}
func (d *Document) RunGen() uint32                          { return d.runGen }
func (d *Document) Kids() []*value.Node                     { return d.kids }
func (d *Document) Machine() core.BackendState              { return d.machine }
func (d *Document) SetMachine(m core.BackendState)          { d.machine = m }
func (d *Document) Input() *core.Input                      { return &d.in }
func (d *Document) Resumed() int                            { return d.resumed }
func (d *Document) Edits() []core.DocEdit                   { return d.edits }
func (d *Document) SetEdits(e []core.DocEdit)               { d.edits = e }
func (d *Document) SetSequence(n uint64)                    { d.sequence = n }
func (d *Document) SetDefaultTrace(f func(core.TraceEvent)) { d.defaults.Trace = f }
func (d *Document) Start() string                           { return d.start }
func (d *Document) Runner() Runner                          { return d.runner }
func (d *Document) HasPreviousSnapshot() bool               { return d.previousSnapshot.Value() != nil }
func (d *Document) ChangeBaseline() *Snapshot               { return d.changes.previous }
func (d *Document) ParseWithCapture(o core.ParseOptions, capture func(*core.Parser, *value.Node, error) error) (*value.Node, error) {
	return d.parseWithCapture(o, capture)
}

// SnapshotRecord identifies one record of a snapshot graph; records compare by identity.
type SnapshotRecord = *snapshotRecord

// SnapshotGraph is the record graph of a snapshot.
type SnapshotGraph struct{ g *snapshotGraph }

func (s *Snapshot) Graph() SnapshotGraph { return SnapshotGraph{&s.graph} }

func (g SnapshotGraph) Shared() bool                        { return g.g.refs != nil }
func (g SnapshotGraph) Count() int                          { return g.g.count() }
func (g SnapshotGraph) At(i int) SnapshotRecord             { return g.g.at(i) }
func (g SnapshotGraph) ChildCount(r SnapshotRecord) int     { return g.g.childCount(r) }
func (g SnapshotGraph) ChildID(r SnapshotRecord, i int) int { return g.g.childID(r, i) }
func (g SnapshotGraph) Handle(id int) SnapshotNode          { return snapshotHandle(g.g, id) }

func (g SnapshotGraph) NodeCount() int { return len(g.g.nodes) }

// Chunked reports whether r keeps its children in shared child runs.
func (g SnapshotGraph) Chunked(r SnapshotRecord) bool { return r.childChunked }

// ChildChunks returns the shared child runs of r, or nil.
func (g SnapshotGraph) ChildChunks(r SnapshotRecord) *snapshotChildChunks { return g.g.childChunks[r] }

// HasChildSlice reports whether r keeps its children in a slice of its own.
func (g SnapshotGraph) HasChildSlice(r SnapshotRecord) bool { return r.children != nil }

// ChunkChildID returns child i of part p of the child runs c.
func ChunkChildID(c *snapshotChildChunks, p, i int) int { return c.parts[p][i] }

var EqualChangeSnapshots = equalChangeSnapshots
