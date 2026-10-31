package zp

import "github.com/ornew/zeroparse/internal/engine"

// DocumentChangeKind distinguishes source operations, captured outcomes and
// lost history requiring a full refresh.
type DocumentChangeKind = engine.DocumentChangeKind

const (
	ChangeEdit        = engine.ChangeEdit
	ChangeOutcome     = engine.ChangeOutcome
	ChangeFullRefresh = engine.ChangeFullRefresh
)

// OutputChange is conservative whole-output comparison, never a minimal diff.
type OutputChange = engine.OutputChange

const (
	OutputUnchanged      = engine.OutputUnchanged
	OutputChanged        = engine.OutputChanged
	OutputMayHaveChanged = engine.OutputMayHaveChanged
	OutputUnavailable    = engine.OutputUnavailable
)

// DocumentChange owns an edit operation or an outcome comparison. Edit From/To
// advance source revisions; outcome From/To identify compared snapshots. Consumers
// track those streams separately. FullRefresh requires rebuilding current state.
type DocumentChange = engine.DocumentChange

// EnableChanges opts into bounded notifications and one retained comparison
// snapshot. queueLimit bounds record count, not replacement or snapshot bytes.
// It must be positive; enabling twice is rejected. Future Parse and Snapshot calls
// capture and compare immutable outcomes inside their cumulative spend.
// Comparison is conservative about aliases and nil-versus-empty collections;
// this policy does not define language value equality. Interrupted invocations
// return nil and notify unavailable, breaking the comparison chain.
func (d *Document) EnableChanges(queueLimit int) error { return d.doc.EnableChanges(queueLimit) }

// DrainChanges transfers queued events. Overflow produces one full-refresh
// marker; draining resets the queue but retains the last comparison snapshot.
func (d *Document) DrainChanges() []DocumentChange { return d.doc.DrainChanges() }
