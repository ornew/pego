package engine

import "github.com/ornew/zeroparse/internal/engine/document"

// The Document, its snapshots and change notifications are declared in the document package. The
// engine re-exports them under the same names for the root package.

type (
	Document             = document.Document
	ChangeDocument       = document.ChangeDocument
	DocumentVersion      = document.DocumentVersion
	DocumentChange       = document.DocumentChange
	DocumentChangeKind   = document.DocumentChangeKind
	OutputChange         = document.OutputChange
	TextEdit             = document.TextEdit
	CacheStats           = document.CacheStats
	Cursor               = document.Cursor
	Snapshot             = document.Snapshot
	SnapshotNode         = document.SnapshotNode
	SnapshotField        = document.SnapshotField
	SnapshotCaptureError = document.SnapshotCaptureError
)

const (
	ChangeEdit        = document.ChangeEdit
	ChangeOutcome     = document.ChangeOutcome
	ChangeFullRefresh = document.ChangeFullRefresh

	OutputUnchanged      = document.OutputUnchanged
	OutputChanged        = document.OutputChanged
	OutputMayHaveChanged = document.OutputMayHaveChanged
	OutputUnavailable    = document.OutputUnavailable
)
