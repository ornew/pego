//go:build !pego_snapshot_sharing_reference

package document_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/document"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
)

func TestSnapshotSharedChildRunsAfterMiddleEdit(t *testing.T) {
	if !document.SnapshotChildChunksEnabled {
		t.Skip("child run reference")
	}
	prog := mustCompile(t, `rule main = item* $$
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
	var changed document.SnapshotRecord
	for i := 0; i < min(first.Graph().Count(), second.Graph().Count()); i++ {
		old, next := first.Graph().At(i), second.Graph().At(i)
		if first.Graph().ChildCount(old) == 4096 && second.Graph().ChildCount(next) == 4096 && old != next {
			changed = next
			chunks := second.Graph().ChildChunks(next)
			if chunks == nil || second.Graph().HasChildSlice(next) {
				t.Fatal("wide changed record has no shared child runs")
			}
			if document.ChunkChildID(chunks, 0, 0) != first.Graph().ChildID(old, 0) || document.ChunkChildID(chunks, 15, 255) != first.Graph().ChildID(old, 4095) {
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
	for i := 0; i < third.Graph().Count(); i++ {
		if r := third.Graph().At(i); third.Graph().Chunked(r) && fourth.Graph().At(i) == r && fourth.Graph().ChildChunks(r) == third.Graph().ChildChunks(r) {
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

func TestSnapshotSharedRecordsAndInterruption(t *testing.T) {
	if !core.WorkControlsEnabled {
		t.Skip("work controls reference")
	}
	prog := mustCompile(t, `rule main = item* $$
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
		if err != nil || !second.Graph().Shared() {
			t.Fatal("shared capture", err)
		}
		shared := 0
		for i := range first.Graph().NodeCount() {
			if second.Graph().At(i) == first.Graph().At(i) {
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
		if interrupted != nil || !errors.Is(err, context.Canceled) || !ctx.seen || d.Memo().Len() != 0 {
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

func TestSnapshotSharingAcrossEditsPreservesOldVersions(t *testing.T) {
	prog := mustCompile(t, `rule main = item* $$
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
