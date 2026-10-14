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
			err := catchFatal(t, func() { sameSnapshotRecord(p, &tc.a, &tc.b) })
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
	legacy.Children = []*Node{legacy, nil}
	legacy.Fields = Fields{{"self", legacy}, {"typedNil", (*Node)(nil)}, {"int", 7}, {"text", "a"}}
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
	if materialized.Children[0] != materialized || materialized.Fields[0].Value != materialized {
		t.Fatal("materialized cycle")
	}
	legacy.Fields[3].Value = "changed"
	if first.Root().Fields()[3].Value != "a" || second.Root().Fields()[3].Value != "a" {
		t.Fatal("legacy mutation reached snapshots")
	}
	runtime.KeepAlive(first)
}
