//go:build pego_snapshot_position_overlays

package engine

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestSnapshotPositionOverlaysAcrossEdits(t *testing.T) {
	for _, src := range []string{
		`rule main = -[ ]* item* $$
rule item = [a-z]`,
		`type Item = { At int; Text string }
rule main = -[ ]* item* $$
rule item = x:[a-z] => Item{At: $x.startPos, Text: text($x)}`,
	} {
		prog := compile(t, src)
		snapshotVariants(t, func(t *testing.T, o ParseOptions) {
			d, err := prog.NewDocumentWith("main", strings.Repeat("a", 1025), o)
			if err != nil {
				t.Fatal(err)
			}
			var versions []*Snapshot
			var expected []*Node
			for _, edit := range []struct {
				start, end int
				text       string
			}{
				{0, 0, ""}, {0, 0, " "}, {0, 0, " "}, {514, 515, "b"},
				{0, 2, ""}, {0, 0, " "},
			} {
				if err := d.Edit(edit.start, edit.end, edit.text); err != nil {
					t.Fatal(err)
				}
				s, err := d.SnapshotWith(o)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := prog.ParseWith("main", d.Text(), o)
				if err != nil {
					t.Fatal(err)
				}
				if len(versions) == 1 {
					wantOverlay := !strings.Contains(src, "$x.startPos")
					if (s.graph.positions != nil) != wantOverlay {
						t.Fatal("prefix-shift overlay eligibility ignored action position values")
					}
				}
				versions = append(versions, s)
				expected = append(expected, fresh.Clone())
			}
			for i, s := range versions {
				got, want := s.Materialize(), expected[i]
				if !equalSnapshotNodePositions(s.Root(), want) || got.String() != want.String() {
					t.Fatalf("version %d differs from fresh parse", i)
				}
			}
			runtime.KeepAlive(versions)
		})
	}
}

func equalSnapshotNodePositions(got SnapshotNode, want *Node) bool {
	if !got.Valid() || want == nil {
		return got.Valid() == (want != nil)
	}
	if got.Start() != want.Start || got.End() != want.End || got.Type() != want.Type() || got.Rule() != want.Rule() || got.ChildCount() != len(want.Children) {
		return false
	}
	for i, child := range want.Children {
		if !equalSnapshotNodePositions(got.Child(i), child) {
			return false
		}
	}
	for _, field := range want.Fields {
		if child, ok := field.Value.(*Node); ok {
			v, exists := got.Field(field.Name)
			handle, valid := v.(SnapshotNode)
			if !exists || !valid || !equalSnapshotNodePositions(handle, child) {
				return false
			}
		}
	}
	return true
}

func TestSnapshotPositionOverlayZeroAndOverflow(t *testing.T) {
	previous := snapshotGraph{snapshotPositionState: snapshotPositionState{positions: make([]snapshotPositionDelta, 128)}, nodes: make([]snapshotRecord, 128), root: 1}
	next := snapshotGraph{nodes: make([]snapshotRecord, 128), root: 1}
	for i := range previous.nodes {
		previous.nodes[i] = snapshotRecord{start: 3, end: 5, gen: 1}
		previous.positions[i] = snapshotPositionDelta{start: -3, end: 4, gen: 9}
		next.nodes[i] = snapshotRecord{start: 0, end: 9, gen: 9}
	}
	p := &parser{control: newWorkControl(nil, WorkLimits{})}
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

func TestSnapshotPositionOverlaysUnicodeAndInvalidUTF8(t *testing.T) {
	prog := compile(t, `rule main = -[ ]* item* $$
rule item = .`)
	for _, text := range []string{strings.Repeat("é🙂", 513), strings.Repeat("a\xff", 513)} {
		snapshotVariants(t, func(t *testing.T, o ParseOptions) {
			d, err := prog.NewDocumentWith("main", text, o)
			if err != nil {
				t.Fatal(err)
			}
			first, err := d.SnapshotWith(o)
			if err != nil {
				t.Fatal(err)
			}
			oldText := first.Text()
			if err := d.Edit(0, 0, " "); err != nil {
				t.Fatal(err)
			}
			shifted, err := d.SnapshotWith(o)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := prog.ParseWith("main", d.Text(), o)
			if err != nil {
				t.Fatal(err)
			}
			if shifted.graph.positions == nil || !equalSnapshotNodePositions(shifted.Root(), fresh) || shifted.Materialize().String() != fresh.String() {
				t.Fatal("shifted byte/code-point positions differ from fresh parse")
			}
			if first.Text() != oldText || first.Root().Start() != 0 {
				t.Fatal("earlier version changed")
			}
			runtime.KeepAlive(first)
		})
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
	if !workControlsEnabled {
		t.Skip("work control reference")
	}
	previous := snapshotGraph{nodes: make([]snapshotRecord, 128), root: 1}
	next := snapshotGraph{nodes: make([]snapshotRecord, 128), root: 1}
	for i := range next.nodes {
		next.nodes[i] = snapshotRecord{start: 3, end: 5, gen: 1}
	}
	p := &parser{control: newWorkControl(nil, WorkLimits{MaxSteps: 264})}
	err := catchFatal(t, func() { shareSnapshotGraph(p, &previous, next) })
	var limit *WorkLimitError
	if !errors.As(err, &limit) || limit.Kind != StepWorkLimit || limit.Requested != 3 {
		t.Fatal("overlay metadata copy escaped work limit", err)
	}
	if previous.positions != nil || next.positions != nil || previous.nodes[0].start != 0 || next.nodes[0].start != 3 {
		t.Fatal("interrupted copy changed the input graphs")
	}
	shared := shareSnapshotGraph(&parser{}, &previous, next)
	if shared.positions == nil || snapshotHandle(&shared, 1).Start() != 3 {
		t.Fatal("retry lost shifted positions")
	}
}

func TestSnapshotPositionOverlayCancellationDuringMetadataCopy(t *testing.T) {
	if !workControlsEnabled {
		t.Skip("work control reference")
	}
	prog := compile(t, `rule main = -[ ]* item* $$
rule item = [a-z]`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", strings.Repeat("a", 4096), o)
		if err != nil {
			t.Fatal(err)
		}
		first, err := d.SnapshotWith(o)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Edit(0, 0, " "); err != nil {
			t.Fatal(err)
		}
		ctx := &snapshotPhaseContext{Context: context.Background(), done: make(chan struct{}), phase: "setPositions"}
		o.Context = ctx
		s, err := d.SnapshotWith(o)
		if !ctx.seen || s != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("overlay copy cancellation not reached", s, err)
		}
		if first.Root().Start() != 0 || first.Text() != strings.Repeat("a", 4096) {
			t.Fatal("cancel changed earlier version")
		}
		o.Context = nil
		retry, err := d.SnapshotWith(o)
		fresh, freshErr := prog.ParseWith("main", d.Text(), o)
		if err != nil || freshErr != nil || !equalSnapshotNodePositions(retry.Root(), fresh) {
			t.Fatal("retry after overlay cancellation", err, freshErr)
		}
		runtime.KeepAlive(first)
	})
}
