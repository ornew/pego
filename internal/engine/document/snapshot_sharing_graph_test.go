//go:build !zp_snapshot_sharing_reference

package document

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/limits"
	"github.com/ornew/zeroparse/internal/value"
)

func TestSnapshotSharedStringComparisonRespectsBudgetLimit(t *testing.T) {
	if !core.BudgetControlsEnabled {
		t.Skip("budget controls reference")
	}
	long := strings.Repeat("a", 1<<16)
	for _, tc := range []struct {
		name string
		a, b snapshotRecord
	}{
		{"terminal text", snapshotRecord{text: long}, snapshotRecord{text: strings.Clone(long)}},
		{"late text mismatch", snapshotRecord{text: long + "x"}, snapshotRecord{text: long + "y"}},
		{"field name", snapshotRecord{fields: value.Fields{{Name: long, Value: 1}}}, snapshotRecord{fields: value.Fields{{Name: strings.Clone(long), Value: 1}}}},
		{"field value", snapshotRecord{fields: value.Fields{{Name: "x", Value: long}}}, snapshotRecord{fields: value.Fields{{Name: "x", Value: strings.Clone(long)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &core.Parser{Control: core.NewBudgetControl(nil, limits.Limits{Budget: 1024})}
			err := catchFatal(t, func() { sameSnapshotRecord(p, &snapshotGraph{}, &tc.a, &snapshotGraph{}, &tc.b) })
			var limit *limits.LimitError
			if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget {
				t.Fatal("string comparison escaped budget limit", err)
			}
		})
	}
}

func TestSnapshotChildRunCopyRespectsBudgetLimit(t *testing.T) {
	if !snapshotChildChunksEnabled || !core.BudgetControlsEnabled {
		t.Skip("child run or budget control reference")
	}
	children := make([]int, 1025)
	for i := range children {
		if i%50 != 0 {
			children[i] = i + 1
		}
	}
	old := snapshotRecord{children: children}
	changed := append([]int(nil), children...)
	changed[512] = 9999
	next := snapshotRecord{children: changed}
	graph := snapshotGraph{nodes: []snapshotRecord{old}}
	p := &core.Parser{Control: core.NewBudgetControl(nil, limits.Limits{Budget: 1170})}
	err := catchFatal(t, func() { shareSnapshotChildren(p, &graph, &graph.nodes[0], &next) })
	var limit *limits.LimitError
	if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget {
		t.Fatal("copy escaped budget limit", err)
	}
	if next.childChunked || len(next.children) != 1025 || next.children[512] != 9999 {
		t.Fatal("interrupted copy changed the source record")
	}
	p = &core.Parser{Control: core.NewBudgetControl(nil, limits.Limits{})}
	chunks := shareSnapshotChildren(p, &graph, &graph.nodes[0], &next)
	if chunks == nil || !next.childChunked || next.children != nil || chunks.length != 1025 || len(chunks.parts[4]) != 1 {
		t.Fatal("successful copy did not retain the exact child shape")
	}
	if &chunks.parts[0][0] != &children[0] || &chunks.parts[2][0] == &children[512] || chunks.parts[2][0] != 9999 || chunks.parts[4][0] != children[1024] {
		t.Fatal("unchanged and changed runs were not partitioned correctly")
	}
	tooDifferent := snapshotRecord{children: append([]int(nil), children...)}
	tooDifferent.children[0], tooDifferent.children[512] = 7, 8
	if chunks := shareSnapshotChildren(p, &graph, &graph.nodes[0], &tooDifferent); chunks != nil || tooDifferent.childChunked {
		t.Fatal("low-reuse child sequence should remain flat")
	}
}

func TestSnapshotChunkedHandleIdentity(t *testing.T) {
	children := make([]int, 1025)
	children[0] = 1 // The wide child points back to the root.
	wide := snapshotRecord{children: children}
	previous := snapshotGraph{nodes: []snapshotRecord{wide}}
	p := &core.Parser{Control: core.NewBudgetControl(nil, limits.Limits{})}
	chunks := shareSnapshotChildren(p, &previous, &previous.nodes[0], &wide)
	if chunks == nil {
		t.Fatal("wide child sequence did not share")
	}
	g := snapshotGraph{nodes: []snapshotRecord{
		{children: []int{2}, fields: value.Fields{{Name: "wide", Value: snapshotRef(2)}}},
		wide,
	}, root: 1}
	g.childChunks = map[*snapshotRecord]*snapshotChildChunks{&g.nodes[1]: chunks}
	root := snapshotHandle(&g, 1)
	child := root.Child(0)
	field, ok := root.Field("wide")
	if !ok || child != field || child != root.Fields()[0].Value || child.Child(0) != root || child.Child(1) != (SnapshotNode{}) {
		t.Fatal("chunked handles lost sharing, cycle or nil identity")
	}
	other := snapshotGraph{refs: []*snapshotRecord{&g.nodes[0], &g.nodes[1]}, root: 1, childChunks: g.childChunks}
	if snapshotHandle(&other, 2) == child {
		t.Fatal("different snapshot graphs must have distinct handles")
	}
	materialized := (&Snapshot{graph: g}).Materialize()
	if value.D(materialized).Children[0] != materialized.Field("wide") || value.D(value.D(materialized).Children[0]).Children[0] != materialized || value.D(value.D(materialized).Children[0]).Children[1] != nil {
		t.Fatal("materialization lost sharing, cycle or nil identity")
	}
}
