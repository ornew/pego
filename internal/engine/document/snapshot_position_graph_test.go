//go:build pego_snapshot_position_overlays

package document

import (
	"errors"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func TestSnapshotPositionOverlayZeroAndOverflow(t *testing.T) {
	previous := snapshotGraph{snapshotPositionState: snapshotPositionState{positions: make([]snapshotPositionDelta, 128)}, nodes: make([]snapshotRecord, 128), root: 1}
	next := snapshotGraph{nodes: make([]snapshotRecord, 128), root: 1}
	for i := range previous.nodes {
		previous.nodes[i] = snapshotRecord{start: 3, end: 5, gen: 1}
		previous.positions[i] = snapshotPositionDelta{start: -3, end: 4, gen: 9}
		next.nodes[i] = snapshotRecord{start: 0, end: 9, gen: 9}
	}
	p := &parser{control: newBudgetControl(nil, limits.Limits{})}
	shared := shareSnapshotGraph(p, &previous, next)
	n := snapshotHandle(&shared, 1)
	materialized := (&Snapshot{graph: shared}).Materialize()
	if shared.at(0) != previous.at(0) || shared.positions == nil || n.Start() != 0 || n.End() != 9 || materialized.Start != 0 || materialized.End != 9 || materialized.gen != 9 {
		t.Fatal("zero start or current generation was lost during exact re-sharing")
	}
	for i := range previous.nodes {
		previous.nodes[i] = snapshotRecord{start: -(1 << 31), end: -(1 << 31)}
		next.nodes[i] = snapshotRecord{start: (1 << 31) - 1, end: (1 << 31) - 1}
	}
	previous.positions = nil
	result := shareSnapshotGraph(p, &previous, next)
	if result.refs != nil || result.positions != nil || result.at(0).start != (1<<31)-1 {
		t.Fatal("unrepresentable relative offsets must remain owned records")
	}
}

func TestSnapshotPositionOverlayCyclesAndTypedNil(t *testing.T) {
	root := &Node{Start: 0, End: 128, Children: make([]*Node, 128)}
	root.Fields = Fields{{"self", root}, {"typedNil", (*Node)(nil)}, {"scalarPosition", 42}}
	for i := range root.Children {
		root.Children[i] = &Node{Start: int32(i), End: int32(i + 1), gen: 1}
	}
	previous, err := freezeSnapshotGraph(&parser{}, root)
	if err != nil {
		t.Fatal(err)
	}
	shifted := root.Clone()
	shifted.Start, shifted.End, shifted.gen = 3, 131, 2
	for _, n := range shifted.Children {
		n.Start += 3
		n.End += 3
		n.gen = 2
	}
	next, err := freezeSnapshotGraph(&parser{}, shifted)
	if err != nil {
		t.Fatal(err)
	}
	shared := shareSnapshotGraph(&parser{}, &previous, next)
	s := &Snapshot{graph: shared}
	if shared.positions == nil {
		t.Fatal("shifted cyclic graph did not share payloads")
	}
	if self, _ := s.Root().Field("self"); self != s.Root() {
		t.Fatal("cycle identity changed")
	}
	if nilNode, _ := s.Root().Field("typedNil"); nilNode != (SnapshotNode{}) {
		t.Fatal("typed nil changed")
	}
	if scalar, _ := s.Root().Field("scalarPosition"); scalar != 42 {
		t.Fatal("scalar action value shifted")
	}
	materialized := s.Materialize()
	if self, _ := materialized.Fields.Get("self"); self != materialized {
		t.Fatal("materialized cycle changed")
	}
	for i, n := range materialized.Children {
		if n.Start != int32(i+3) || n.End != int32(i+4) || n.gen != 2 {
			t.Fatal("current metadata lost")
		}
	}
}

func TestSnapshotPositionOverlayBudgetAndRetry(t *testing.T) {
	if !budgetControlsEnabled {
		t.Skip("budget control reference")
	}
	previous := snapshotGraph{nodes: make([]snapshotRecord, 128), root: 1}
	next := snapshotGraph{nodes: make([]snapshotRecord, 128), root: 1}
	for i := range next.nodes {
		next.nodes[i] = snapshotRecord{start: 3, end: 5, gen: 1}
	}
	p := &parser{control: newBudgetControl(nil, limits.Limits{Budget: 264})}
	err := catchFatal(t, func() { shareSnapshotGraph(p, &previous, next) })
	var limit *limits.LimitError
	if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget || limit.Requested != 3 {
		t.Fatal("overlay metadata copy escaped budget limit", err)
	}
	if previous.positions != nil || next.positions != nil || previous.nodes[0].start != 0 || next.nodes[0].start != 3 {
		t.Fatal("interrupted copy changed the input graphs")
	}
	shared := shareSnapshotGraph(&parser{}, &previous, next)
	if shared.positions == nil || snapshotHandle(&shared, 1).Start() != 3 {
		t.Fatal("retry lost shifted positions")
	}
}
