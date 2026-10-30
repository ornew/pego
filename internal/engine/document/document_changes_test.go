package document_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func TestDocumentChangesComparisonInterruptionPublication(t *testing.T) {
	if !core.BudgetControlsEnabled {
		t.Skip("the reference build has no limits to interrupt the snapshot")
	}
	prog := mustCompile(t, `rule main = .* #atomic $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", strings.Repeat("x", 8192), o)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.EnableChanges(8); err != nil {
			t.Fatal(err)
		}
		if _, err := d.SnapshotWith(o); err != nil {
			t.Fatal(err)
		}
		d.DrainChanges()
		// Force interruption during the shared parse/capture/comparison invocation.
		limited := o
		limited.Limits = limits.Limits{Budget: 1}
		if s, err := d.SnapshotWith(limited); s != nil || !errors.Is(err, limits.ErrLimit) {
			t.Fatal(s, err)
		}
		events := d.DrainChanges()
		if len(events) != 1 || events[0].Kind != ChangeOutcome || events[0].Output != OutputUnavailable || d.ChangeBaseline() != nil {
			t.Fatal("interrupted outcome published baseline", events)
		}
		if _, err := d.SnapshotWith(o); err != nil {
			t.Fatal(err)
		}
		if got := d.DrainChanges()[0].Output; got != OutputMayHaveChanged {
			t.Fatal("retry bridged unavailable comparison", got)
		}
	})
}

func TestDocumentChangesDownstreamMatchesFullRecomputation(t *testing.T) {
	prog := mustCompile(t, `rule main = word* $$
rule word = [a-zé]+ #atomic`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d := newChangeTestDocument(t, prog, "abcé", o, 2)
		var index map[string]int
		var highlights []string
		var consumerVersion DocumentVersion
		for _, edit := range []TextEdit{{0, 0, ""}, {0, 1, "z"}, {0, 0, "q"}, {0, 1, "!"}, {0, 1, "b"}, {0, 1, ""}} {
			if err := d.Edit(edit.Start, edit.End, edit.Text); err != nil {
				t.Fatal(err)
			}
			s, _ := d.Snapshot()
			if s == nil {
				t.Fatal("missing completed outcome")
			}
			refresh := index == nil
			for _, c := range d.DrainChanges() {
				refresh = refresh || c.FullRefresh || c.From != consumerVersion || c.Invalidated || c.Output != OutputUnchanged
				consumerVersion = c.To
			}
			if refresh {
				index, highlights = changeConsumerViews(s)
			}
			fresh := newChangeTestDocument(t, prog, d.Text(), o, 2)
			fs, _ := fresh.Snapshot()
			wantIndex, wantHighlights := changeConsumerViews(fs)
			if !reflect.DeepEqual(index, wantIndex) || !reflect.DeepEqual(highlights, wantHighlights) {
				t.Fatalf("downstream mismatch for %q", d.Text())
			}
		}
	})
}

func TestDocumentChangesEditCoordinatesAndQueue(t *testing.T) {
	prog := mustCompile(t, `rule main = .* $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d := newChangeTestDocument(t, prog, "aéz", o, 8)
		base := d.Version()
		end := 2
		if o.Unit == Bytes {
			end = 3
		}
		if err := d.Edit(1, end, "é"); err != nil {
			t.Fatal(err)
		}
		if err := d.Edit(-1, 0, ""); err == nil {
			t.Fatal("invalid edit accepted")
		}
		if err := d.EditBatch(d.Version()); err != nil {
			t.Fatal(err)
		}
		edits := []TextEdit{{Start: 0, End: 1, Text: "b"}, {Start: end, End: end + 1, Text: "c"}}
		batchBase := d.Version()
		if err := d.EditBatch(batchBase, edits...); err != nil {
			t.Fatal(err)
		}
		edits[0].Text = "mutated"
		changes := d.DrainChanges()
		if len(changes) != 2 || changes[0].From != base || changes[0].To != batchBase || changes[1].From != batchBase || changes[1].To != d.Version() {
			t.Fatal("operation versions", changes)
		}
		if changes[0].Edits[0].End != end || changes[1].Edits[0].Text != "b" || changes[1].Edits[1].Start != end {
			t.Fatal("base coordinates or ownership", changes)
		}
		for _, c := range changes {
			if c.Unit != o.Unit || !c.Invalidated || c.Reevaluation != "unknown" || c.Output != OutputMayHaveChanged {
				t.Fatal("edit status", c)
			}
		}
		if len(d.DrainChanges()) != 0 || d.Text() != "béc" {
			t.Fatal("drain or text")
		}
		if err := d.EditBatch(base, TextEdit{Start: 0, End: 1}); err == nil {
			t.Fatal("stale batch accepted")
		}
		if len(d.DrainChanges()) != 0 {
			t.Fatal("rejected batch notified")
		}
	})
}

func TestDocumentChangesOutputAndInterruptedRetry(t *testing.T) {
	prog := mustCompile(t, `type Entry = { Name string; At int }
rule main Entry = x:([a-z]+ #atomic) $$ => Entry{Name: text($x), At: $x.Start}`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d := newChangeTestDocument(t, prog, "aa", o, 16)
		old, err := d.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if got := d.DrainChanges()[0].Output; got != OutputMayHaveChanged {
			t.Fatal("initial status", got)
		}
		if _, err := d.Snapshot(); err != nil {
			t.Fatal(err)
		}
		if got := d.DrainChanges()[0].Output; got != OutputUnchanged {
			t.Fatal("same revision", got)
		}
		if err := d.Edit(0, 2, "bb"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Snapshot(); err != nil {
			t.Fatal(err)
		}
		if got := d.DrainChanges()[1].Output; got != OutputChanged {
			t.Fatal("scalar replacement", got)
		}
		if val, _ := old.Root().Field("Name"); val != "aa" {
			t.Fatal("old snapshot mutated", val)
		}
		if err := d.Edit(0, 2, "bb"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Snapshot(); err != nil {
			t.Fatal(err)
		}
		if got := d.DrainChanges()[1].Output; got != OutputUnchanged {
			t.Fatal("identity replacement", got)
		}
		// The reference build has no cancellation to interrupt the snapshot.
		if core.BudgetControlsEnabled {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			cancelled := o
			cancelled.Context = ctx
			if s, err := d.SnapshotWith(cancelled); s != nil || !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled snapshot", s, err)
			}
			if got := d.DrainChanges()[0].Output; got != OutputUnavailable {
				t.Fatal("unavailable status", got)
			}
			if _, err := d.Snapshot(); err != nil {
				t.Fatal(err)
			}
			if got := d.DrainChanges()[0].Output; got != OutputMayHaveChanged {
				t.Fatal("retry bridged unavailable outcome", got)
			}
		}
	})
}

func TestDocumentChangesTerminalTextAndDiagnostics(t *testing.T) {
	prog := mustCompile(t, `rule main = [a-z]+ #atomic $$`)
	snapshotVariants(t, func(t *testing.T, o ParseOptions) {
		d := newChangeTestDocument(t, prog, "aa", o, 8)
		if _, err := d.Snapshot(); err != nil {
			t.Fatal(err)
		}
		d.DrainChanges()
		if err := d.Edit(0, 2, "bb"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Snapshot(); err != nil {
			t.Fatal(err)
		}
		if got := d.DrainChanges()[1].Output; got != OutputChanged {
			t.Fatal("terminal text omitted", got)
		}
		if err := d.Edit(0, 2, "!"); err != nil {
			t.Fatal(err)
		}
		if s, err := d.Snapshot(); s == nil || err == nil {
			t.Fatal("syntax outcome missing", s, err)
		}
		if got := d.DrainChanges()[1].Output; got != OutputChanged {
			t.Fatal("syntax output omitted", got)
		}
		if s, err := d.Snapshot(); s == nil || err == nil {
			t.Fatal("syntax retry missing", s, err)
		}
		if got := d.DrainChanges()[0].Output; got != OutputUnchanged {
			t.Fatal("identical diagnostic", got)
		}
	})
}
