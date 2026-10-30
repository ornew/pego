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
	t.Parallel()
	for _, src := range []string{
		`rule main = -[ ]* item* $$
rule item = [a-z]`,
		`type Item = { At int; Text string }
rule main = -[ ]* item* $$
rule item = x:[a-z] => Item{At: $x.Start, Text: text($x)}`,
	} {
		prog := mustCompile(t, src)
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
					wantOverlay := !strings.Contains(src, "$x.Start")
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

func TestSnapshotPositionOverlaysUnicodeAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = -[ ]* item* $$
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

func TestSnapshotPositionOverlayCancellationDuringMetadataCopy(t *testing.T) {
	t.Parallel()
	if !budgetControlsEnabled {
		t.Skip("budget control reference")
	}
	prog := mustCompile(t, `rule main = -[ ]* item* $$
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
