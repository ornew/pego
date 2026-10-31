//go:build zp_snapshot_position_overlays

package document

import "testing"

func TestDocumentChangesDirectoryEqualityUsesPositionOverlay(t *testing.T) {
	a := changeGraphSnapshot(t, &Node{kind: kindOf("Leaf", "item"), Start: 1, End: 2, Text: "x", terminal: true})
	// Same immutable record pointer, but version-owned observed positions differ.
	b := &Snapshot{graph: snapshotGraph{root: a.graph.root, refs: []*snapshotRecord{a.graph.at(0)}, snapshotPositionState: snapshotPositionState{positions: []snapshotPositionDelta{{start: 1, end: 1}}}}}
	if equalChangeSnapshots(a, b) {
		t.Fatal("overlay positions omitted")
	}
	b.graph.positions[0] = snapshotPositionDelta{}
	if !equalChangeSnapshots(a, b) {
		t.Fatal("identical observed positions differ")
	}
}
