package engine

import "fmt"

// OutputChange describes whole-result equality, not a minimal tree difference.
type OutputChange string

const (
	OutputUnchanged      OutputChange = "unchanged"
	OutputChanged        OutputChange = "changed"
	OutputMayHaveChanged OutputChange = "may-have-changed"
	OutputUnavailable    OutputChange = "unavailable"
)

// DocumentChange keeps one operation's original coordinate basis. FullRefresh
// means consumers must rebuild from the latest snapshot instead of replaying edits.
type DocumentChangeKind string

const (
	ChangeEdit        DocumentChangeKind = "edit"
	ChangeOutcome     DocumentChangeKind = "outcome"
	ChangeFullRefresh DocumentChangeKind = "full-refresh"
)

type DocumentChange struct {
	Kind        DocumentChangeKind
	From, To    DocumentVersion
	Unit        PositionUnit
	Edits       []TextEdit
	FullRefresh bool
	// Invalidated is conservative: an accepted edit may affect the whole output.
	// Reevaluation remains unknown; invalidation is not evidence of execution.
	Invalidated  bool
	Reevaluation string
	Output       OutputChange
}

// documentChanges retains bounded events and one immutable comparison baseline.
type documentChanges struct {
	limit    int
	queue    []DocumentChange
	previous *Snapshot
}

// EnableChanges enables bounded notification retention. Re-enabling is rejected
// so queued events and the comparison baseline cannot be silently discarded.
func (d *Document) EnableChanges(queueLimit int) error {
	if queueLimit < 1 {
		return fmt.Errorf("change queue limit must be positive")
	}
	if d.changes != nil {
		return fmt.Errorf("document change notifications already enabled")
	}
	d.changes = &documentChanges{limit: queueLimit}
	return nil
}

func (d *Document) recordEdit(from DocumentVersion, edits []TextEdit) {
	if d.changes == nil {
		return
	}
	d.changes.record(DocumentChange{Kind: ChangeEdit, From: from, To: d.Version(), Unit: d.Unit(), Edits: append([]TextEdit(nil), edits...), Invalidated: true, Reevaluation: "unknown", Output: OutputMayHaveChanged})
}

func (d *Document) recordOutcome(s *Snapshot, output OutputChange) {
	if d.changes == nil {
		return
	}
	from := d.Version()
	if d.changes.previous != nil {
		from = d.changes.previous.Version()
	}
	d.changes.record(DocumentChange{Kind: ChangeOutcome, From: from, To: d.Version(), Unit: d.Unit(), Reevaluation: "unknown", Output: output})
	d.changes.previous = s
}

func (d *documentChanges) record(change DocumentChange) {
	if len(d.queue) != 0 && d.queue[0].FullRefresh {
		d.queue[0].To = change.To
		d.queue[0].Output = OutputMayHaveChanged
		return
	}
	if len(d.queue) >= d.limit {
		d.queue = []DocumentChange{{Kind: ChangeFullRefresh, From: d.queue[0].From, To: change.To, Unit: change.Unit, FullRefresh: true, Invalidated: true, Reevaluation: "unknown", Output: OutputMayHaveChanged}}
		return
	}
	d.queue = append(d.queue, change)
}

// DrainChanges transfers owned events. Edit events advance the source revision;
// outcome events compare captured revisions and do not replay source edits.
// A full-refresh event or a gap in edit revisions requires a fresh snapshot.
func (d *Document) DrainChanges() []DocumentChange {
	if d.changes == nil {
		return nil
	}
	out := d.changes.queue
	d.changes.queue = nil
	return out
}

// ChangeDocument retains the private prototype adapter for its permanent tests.
// Public callers enable tracking on Document instead.
type ChangeDocument struct{ *Document }

func (p *Program) NewChangeDocumentWith(start, text string, o ParseOptions, queueLimit int) (*ChangeDocument, error) {
	d, err := p.NewDocumentWith(start, text, o)
	if err != nil {
		return nil, err
	}
	if err := d.EnableChanges(queueLimit); err != nil {
		return nil, err
	}
	return &ChangeDocument{d}, nil
}
func (d *ChangeDocument) Snapshot() (*Snapshot, error) { return d.SnapshotWith(d.ParseDefaults()) }
