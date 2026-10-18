//go:build !pego_snapshot_sharing_reference

package engine

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestSnapshotSharedRecordsAndInterruption(t *testing.T) {
	if !workControlsEnabled {
		t.Skip("work controls reference")
	}
	prog := compile(t, `rule main = item* $$
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
		if err := d.Edit(0, 1, "b"); err != nil {
			t.Fatal(err)
		}
		second, err := d.SnapshotWith(o)
		if err != nil || second.graph.refs == nil {
			t.Fatal("shared capture", err)
		}
		shared := 0
		for i := range first.graph.nodes {
			if second.graph.at(i) == first.graph.at(i) {
				shared++
			}
		}
		if shared < 4000 || first.Text()[0] != 'a' || second.Text()[0] != 'b' {
			t.Fatal("insufficient exact reuse or changed prior text", shared)
		}
		if resultJSON(second.Materialize(), second.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("shared graph differs from fresh parse")
		}
		ctx := &snapshotPhaseContext{Context: context.Background(), done: make(chan struct{}), phase: "shareSnapshotGraph"}
		o.Context = ctx
		interrupted, err := d.SnapshotWith(o)
		if interrupted != nil || !errors.Is(err, context.Canceled) || !ctx.seen || d.memo.len() != 0 {
			t.Fatal("share interruption", interrupted, err)
		}
		o.Context = nil
		retry, err := d.SnapshotWith(o)
		if retry == nil || err != nil || resultJSON(retry.Materialize(), retry.Error()) != resultJSON(prog.ParseWith("main", d.Text(), o)) {
			t.Fatal("retry", err)
		}
		runtime.KeepAlive(first)
		runtime.KeepAlive(second)
	})
}

func TestSnapshotSharedStringComparisonRespectsWorkLimit(t *testing.T) {
	if !workControlsEnabled {
		t.Skip("work controls reference")
	}
	long := strings.Repeat("a", 1<<16)
	for _, tc := range []struct {
		name string
		a, b snapshotRecord
	}{
		{"terminal text", snapshotRecord{text: long}, snapshotRecord{text: strings.Clone(long)}},
		{"late text mismatch", snapshotRecord{text: long + "x"}, snapshotRecord{text: long + "y"}},
		{"field name", snapshotRecord{fields: Fields{{Name: long, Value: 1}}}, snapshotRecord{fields: Fields{{Name: strings.Clone(long), Value: 1}}}},
		{"field value", snapshotRecord{fields: Fields{{Name: "x", Value: long}}}, snapshotRecord{fields: Fields{{Name: "x", Value: strings.Clone(long)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &parser{control: newWorkControl(nil, WorkLimits{MaxSteps: 1024})}
			err := catchFatal(t, func() { sameSnapshotRecord(p, &snapshotGraph{}, &tc.a, &snapshotGraph{}, &tc.b) })
			var limit *WorkLimitError
			if !errors.As(err, &limit) || limit.Kind != StepWorkLimit {
				t.Fatal("string comparison escaped step limit", err)
			}
		})
	}
}

func TestSnapshotSharingAcrossEditsPreservesOldVersions(t *testing.T) {
	prog := compile(t, `rule main = item* $$
rule item = [a-z]`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", strings.Repeat("a", 4096), o)
		if err != nil {
			t.Fatal(err)
		}
		var versions []*Snapshot
		var expected []string
		capture := func() {
			s, err := d.SnapshotWith(o)
			if err != nil || s == nil {
				t.Fatal("capture", err)
			}
			fresh := resultJSON(prog.ParseWith("main", s.Text(), o))
			if got := resultJSON(s.Materialize(), s.Error()); got != fresh {
				t.Fatal("fresh parse mismatch")
			}
			versions = append(versions, s)
			expected = append(expected, fresh)
		}
		capture()
		for _, edit := range []struct {
			start, end int
			text       string
		}{
			{0, 1, "b"}, {2048, 2049, "c"}, {0, 0, "z"},
			{0, 1, ""}, {4095, 4096, "d"}, {4096, 4096, "e"},
		} {
			if err := d.Edit(edit.start, edit.end, edit.text); err != nil {
				t.Fatal(err)
			}
			capture()
		}
		for i, s := range versions {
			if resultJSON(s.Materialize(), s.Error()) != expected[i] {
				t.Fatalf("old version %d changed", i)
			}
		}
	})
}

func TestSnapshotSharedChildRunsAfterMiddleEdit(t *testing.T) {
	if !snapshotChildChunksEnabled {
		t.Skip("child run reference")
	}
	prog := compile(t, `rule main = item* $$
rule item = [a-z]`)
	d, err := prog.NewDocument("main", strings.Repeat("a", 4096))
	if err != nil {
		t.Fatal(err)
	}
	first, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Edit(2048, 2049, "b"); err != nil {
		t.Fatal(err)
	}
	second, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var changed *snapshotRecord
	for i := 0; i < min(first.graph.count(), second.graph.count()); i++ {
		old, next := first.graph.at(i), second.graph.at(i)
		if first.graph.childCount(old) == 4096 && second.graph.childCount(next) == 4096 && old != next {
			changed = next
			chunks := second.graph.childChunks[next]
			if chunks == nil || next.children != nil {
				t.Fatal("wide changed record has no shared child runs")
			}
			if chunks.parts[0][0] != first.graph.childID(old, 0) || chunks.parts[15][255] != first.graph.childID(old, 4095) {
				t.Fatal("child IDs changed outside edit")
			}
			break
		}
	}
	if changed == nil {
		t.Fatal("no changed wide record")
	}
	if got, want := resultJSON(second.Materialize(), second.Error()), resultJSON(prog.ParseWith("main", d.Text(), ParseOptions{})); got != want {
		t.Fatal("shared children differ from fresh parse")
	}
	if first.Text()[2048] != 'a' || second.Text()[2048] != 'b' {
		t.Fatal("old version changed")
	}
	if err := d.Edit(1024, 1025, "c"); err != nil {
		t.Fatal(err)
	}
	third, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resultJSON(third.Materialize(), third.Error()), resultJSON(prog.ParseWith("main", d.Text(), ParseOptions{})); got != want {
		t.Fatal("second shared generation differs from fresh parse")
	}
	if first.Text()[1024] != 'a' || second.Text()[1024] != 'a' || third.Text()[1024] != 'c' || second.Text()[2048] != 'b' {
		t.Fatal("a retained version changed")
	}
	fourth, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sharedChunked := false
	for i := 0; i < third.graph.count(); i++ {
		if r := third.graph.at(i); r.childChunked && fourth.graph.at(i) == r && fourth.graph.childChunks[r] == third.graph.childChunks[r] {
			sharedChunked = true
			break
		}
	}
	if !sharedChunked || resultJSON(fourth.Materialize(), fourth.Error()) != resultJSON(third.Materialize(), third.Error()) {
		t.Fatal("unchanged capture did not preserve shared child runs")
	}
	runtime.KeepAlive(first)
	runtime.KeepAlive(second)
	runtime.KeepAlive(third)
}

func TestSnapshotChildRunCopyRespectsWorkLimit(t *testing.T) {
	if !snapshotChildChunksEnabled || !workControlsEnabled {
		t.Skip("child run or work control reference")
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
	p := &parser{control: newWorkControl(nil, WorkLimits{MaxSteps: 1170})}
	err := catchFatal(t, func() { shareSnapshotChildren(p, &graph, &graph.nodes[0], &next) })
	var limit *WorkLimitError
	if !errors.As(err, &limit) || limit.Kind != StepWorkLimit {
		t.Fatal("copy escaped step limit", err)
	}
	if next.childChunked || len(next.children) != 1025 || next.children[512] != 9999 {
		t.Fatal("interrupted copy changed the source record")
	}
	p = &parser{control: newWorkControl(nil, WorkLimits{})}
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
	p := &parser{control: newWorkControl(nil, WorkLimits{})}
	chunks := shareSnapshotChildren(p, &previous, &previous.nodes[0], &wide)
	if chunks == nil {
		t.Fatal("wide child sequence did not share")
	}
	g := snapshotGraph{nodes: []snapshotRecord{
		{children: []int{2}, fields: Fields{{Name: "wide", Value: snapshotRef(2)}}},
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
	if materialized.children[0] != materialized.Field("wide") || materialized.children[0].children[0] != materialized || materialized.children[0].children[1] != nil {
		t.Fatal("materialization lost sharing, cycle or nil identity")
	}
}

func TestSnapshotSharingCyclesAndTypedNil(t *testing.T) {
	prog := compile(t, `rule main = "a"`)
	d, err := prog.NewDocument("main", "a")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := d.Parse()
	if err != nil {
		t.Fatal(err)
	}
	legacy.children = []*Node{legacy, nil}
	legacy.fields = Fields{{"self", legacy}, {"typedNil", (*Node)(nil)}, {"int", 7}, {"text", "a"}}
	first, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal("shared cycle", err)
	}
	if second.Root().Child(0) != second.Root() {
		t.Fatal("cycle identity")
	}
	if v, ok := second.Root().Field("typedNil"); !ok || v != (SnapshotNode{}) {
		t.Fatal("typed nil")
	}
	materialized := second.Materialize()
	if materialized.children[0] != materialized || materialized.fields[0].Value != materialized {
		t.Fatal("materialized cycle")
	}
	legacy.fields[3].Value = "changed"
	if first.Root().Fields()[3].Value != "a" || second.Root().Fields()[3].Value != "a" {
		t.Fatal("legacy mutation reached snapshots")
	}
	runtime.KeepAlive(first)
}
