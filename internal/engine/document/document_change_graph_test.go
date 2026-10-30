package document

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func changeGraphSnapshot(t *testing.T, root *value.Node) *Snapshot {
	t.Helper()
	graph, err := freezeSnapshotGraph(&core.Parser{}, root)
	if err != nil {
		t.Fatal(err)
	}
	return &Snapshot{graph: graph}
}

func TestDocumentChangesConservativeGraphEquality(t *testing.T) {
	leaf := func() *value.Node {
		return value.Ptr(value.Data{Kind: value.KindOf("Leaf", "leaf"), Start: 1, End: 2, Terminal: true, Text: "x"})
	}
	shared := leaf()
	a := value.Ptr(value.Data{Children: []*value.Node{shared, shared, nil}})
	value.D(a).Fields = value.Fields{{Name: "self", Value: a}, {Name: "nil", Value: (*value.Node)(nil)}, {Name: "value", Value: 4}, {Name: "flag", Value: true}}
	b := a.Clone()
	if !equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("equal cyclic graph")
	}
	value.D(b).Children[1] = leaf()
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("changed alias topology omitted")
	}
	b = a.Clone()
	value.D(value.D(b).Children[0]).Start++
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("position omitted")
	}
	b = a.Clone()
	value.D(b).Fields[2].Value = 5
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("scalar omitted")
	}
	b = a.Clone()
	value.D(b).Fields[1].Value = nil
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("typed nil omitted")
	}
	b = a.Clone()
	value.D(b).Fields[2], value.D(b).Fields[3] = value.D(b).Fields[3], value.D(b).Fields[2]
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("field order omitted")
	}
	for _, pair := range [][2]*value.Node{{{}, value.Ptr(value.Data{Children: []*value.Node{}})}, {{}, value.Ptr(value.Data{Fields: value.Fields{}})}} {
		if equalChangeSnapshots(changeGraphSnapshot(t, pair[0]), changeGraphSnapshot(t, pair[1])) {
			t.Fatal("nil versus empty omitted")
		}
	}
	x, y := changeGraphSnapshot(t, a), changeGraphSnapshot(t, a.Clone())
	x.parseError = &core.SyntaxError{Pos: 1, Line: 1, Col: 2, Expected: []string{"x"}}
	y.parseError = &core.SyntaxError{Pos: 2, Line: 1, Col: 2, Expected: []string{"x"}}
	if equalChangeSnapshots(x, y) {
		t.Fatal("diagnostic position hidden by formatting")
	}
}

func TestDocumentChangesComparisonUsesCumulativeBudget(t *testing.T) {
	leaf := value.Ptr(value.Data{Kind: value.KindOf("Leaf", "leaf"), Terminal: true, Text: strings.Repeat("x", 8192)})
	a, b := changeGraphSnapshot(t, leaf), changeGraphSnapshot(t, leaf.Clone())
	control := core.NewWorkControl(nil, limits.Limits{MaxWork: 8192})
	control.Charge(4096) // Work already spent by parsing/capture in this invocation.
	p := &core.Parser{Control: control}
	if err := catchFatal(t, func() { equalChangeSnapshotsControlled(p, a, b) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatal("comparison escaped cumulative budget", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catchFatal(t, func() {
		equalChangeSnapshotsControlled(&core.Parser{Control: core.NewWorkControl(ctx, limits.Limits{})}, a, b)
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("comparison ignored cancellation", err)
	}
}
