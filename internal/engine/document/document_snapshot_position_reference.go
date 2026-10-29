//go:build !pego_snapshot_position_overlays

package document

import "github.com/ornew/pego/internal/engine/core"

const snapshotPositionOverlayEnabled = false

// SnapshotNode is a comparable read-only handle. Its zero value denotes nil.
type SnapshotNode struct {
	graph  *snapshotGraph
	ref    *snapshotRecord
	chunks *snapshotChildChunks
}

func newSnapshotHandle(g *snapshotGraph, ref *snapshotRecord, _ int) SnapshotNode {
	return SnapshotNode{graph: g, ref: ref}
}

// Start returns the inclusive start position.
func (n SnapshotNode) Start() int32 { return n.record().start }

// End returns the exclusive end position.
func (n SnapshotNode) End() int32 { return n.record().end }

// The empty state keeps the default graph layout unchanged.
type snapshotPositionState struct{}

func (g *snapshotGraph) position(_ int, r *snapshotRecord) snapshotPosition {
	return snapshotPosition{r.start, r.end, r.gen}
}

func (g *snapshotGraph) setPositions(_ *core.Parser, _ *snapshotGraph) {}
