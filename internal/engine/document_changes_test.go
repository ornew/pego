package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func newChangeTestDocument(t *testing.T, prog *Program, text string, o ParseOptions, limit int) *ChangeDocument {
	t.Helper()
	d, err := prog.NewChangeDocumentWith("main", text, o, limit)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDocumentChangesEditCoordinatesAndQueue(t *testing.T) {
	prog := compile(t, `rule main = .* $$`)
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

func TestDocumentChangesOverflowRequiresFullRefresh(t *testing.T) {
	prog := compile(t, `rule main = .* $$`)
	d := newChangeTestDocument(t, prog, "a", ParseOptions{}, 1)
	base := d.Version()
	for _, text := range []string{"b", "c", "d"} {
		if err := d.Edit(0, 1, text); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Snapshot(); err != nil {
		t.Fatal(err)
	}
	changes := d.DrainChanges()
	if len(changes) != 1 || !changes[0].FullRefresh || changes[0].From != base || changes[0].To != d.Version() || changes[0].Unit != d.Unit() || changes[0].Edits != nil || changes[0].Output != OutputMayHaveChanged {
		t.Fatal("overflow marker", changes)
	}
	if err := d.Edit(0, 1, "e"); err != nil {
		t.Fatal(err)
	}
	if changes := d.DrainChanges(); len(changes) != 1 || changes[0].FullRefresh {
		t.Fatal("drain did not reset queue", changes)
	}
	if _, err := prog.NewChangeDocumentWith("main", "", ParseOptions{}, 0); err == nil {
		t.Fatal("zero queue limit accepted")
	}
}

func TestDocumentChangesOutputAndInterruptedRetry(t *testing.T) {
	prog := compile(t, `type Entry = { Name string; At int }
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
		if value, _ := old.Root().Field("Name"); value != "aa" {
			t.Fatal("old snapshot mutated", value)
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
	})
}

// A consumer rebuilds on any source invalidation, changed/unavailable outcome,
// or a version gap. Compare both indexes and highlights with fresh computation.
func changeConsumerViews(s *Snapshot) (map[string]int, []string) {
	index := map[string]int{}
	var highlights []string
	stack := []SnapshotNode{s.Root()}
	for len(stack) != 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !n.Valid() {
			continue
		}
		if n.IsTerminal() {
			index[n.Text()]++
			highlights = append(highlights, n.Text())
		}
		for i := n.ChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	return index, highlights
}

func TestDocumentChangesDownstreamMatchesFullRecomputation(t *testing.T) {
	prog := compile(t, `rule main = word* $$
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

func TestDocumentChangesTerminalTextAndDiagnostics(t *testing.T) {
	prog := compile(t, `rule main = [a-z]+ #atomic $$`)
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

func TestDocumentChangesByteBoundaryRejectionAndNormalizedText(t *testing.T) {
	prog := compile(t, `rule main = .* $$`)
	d := newChangeTestDocument(t, prog, "é", ParseOptions{Unit: Bytes}, 8)
	v := d.Version()
	if err := d.Edit(1, 2, "x"); err == nil {
		t.Fatal("split character accepted")
	}
	if d.Version() != v || len(d.DrainChanges()) != 0 {
		t.Fatal("failed boundary changed notifications")
	}
	cp := newChangeTestDocument(t, prog, "a", ParseOptions{Unit: CodePoints}, 8)
	invalid := string([]byte{0xff})
	if err := cp.Edit(0, 1, invalid); err != nil {
		t.Fatal(err)
	}
	changes := cp.DrainChanges()
	if changes[0].Edits[0].Text != invalid || cp.Text() != "\ufffd" {
		t.Fatal("operation text and normalized state confused")
	}
}

func changeGraphSnapshot(t *testing.T, root *Node) *Snapshot {
	t.Helper()
	graph, err := freezeSnapshotGraph(&parser{}, root)
	if err != nil {
		t.Fatal(err)
	}
	return &Snapshot{graph: graph}
}

func TestDocumentChangesConservativeGraphEquality(t *testing.T) {
	leaf := func() *Node { return &Node{kind: kindOf("Leaf", "leaf"), start: 1, end: 2, terminal: true, text: "x"} }
	shared := leaf()
	a := &Node{children: []*Node{shared, shared, nil}}
	a.fields = Fields{{"self", a}, {"nil", (*Node)(nil)}, {"value", 4}, {"flag", true}}
	b := a.Clone()
	if !equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("equal cyclic graph")
	}
	b.children[1] = leaf()
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("changed alias topology omitted")
	}
	b = a.Clone()
	b.children[0].start++
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("position omitted")
	}
	b = a.Clone()
	b.fields[2].Value = 5
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("scalar omitted")
	}
	b = a.Clone()
	b.fields[1].Value = nil
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("typed nil omitted")
	}
	b = a.Clone()
	b.fields[2], b.fields[3] = b.fields[3], b.fields[2]
	if equalChangeSnapshots(changeGraphSnapshot(t, a), changeGraphSnapshot(t, b)) {
		t.Fatal("field order omitted")
	}
	for _, pair := range [][2]*Node{{{}, {children: []*Node{}}}, {{}, {fields: Fields{}}}} {
		if equalChangeSnapshots(changeGraphSnapshot(t, pair[0]), changeGraphSnapshot(t, pair[1])) {
			t.Fatal("nil versus empty omitted")
		}
	}
	x, y := changeGraphSnapshot(t, a), changeGraphSnapshot(t, a.Clone())
	x.parseError = &SyntaxError{Pos: 1, Line: 1, Col: 2, Expected: []string{"x"}}
	y.parseError = &SyntaxError{Pos: 2, Line: 1, Col: 2, Expected: []string{"x"}}
	if equalChangeSnapshots(x, y) {
		t.Fatal("diagnostic position hidden by formatting")
	}
}

func TestDocumentChangesComparisonUsesCumulativeBudget(t *testing.T) {
	leaf := &Node{kind: kindOf("Leaf", "leaf"), terminal: true, text: strings.Repeat("x", 8192)}
	a, b := changeGraphSnapshot(t, leaf), changeGraphSnapshot(t, leaf.Clone())
	control := newWorkControl(nil, WorkLimits{MaxInputWork: 8192})
	control.charge(4096, 1) // Work already spent by parsing/capture in this invocation.
	p := &parser{control: control}
	if err := catchFatal(t, func() { equalChangeSnapshotsControlled(p, a, b) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatal("comparison escaped cumulative budget", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catchFatal(t, func() { equalChangeSnapshotsControlled(&parser{control: newWorkControl(ctx, WorkLimits{})}, a, b) }); !errors.Is(err, context.Canceled) {
		t.Fatal("comparison ignored cancellation", err)
	}
}

func TestDocumentChangesComparisonInterruptionPublication(t *testing.T) {
	prog := compile(t, `rule main = .* #atomic $$`)
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
		limited.WorkLimits = WorkLimits{MaxInputWork: 1}
		if s, err := d.SnapshotWith(limited); s != nil || !errors.Is(err, ErrWorkLimit) {
			t.Fatal(s, err)
		}
		events := d.DrainChanges()
		if len(events) != 1 || events[0].Kind != ChangeOutcome || events[0].Output != OutputUnavailable || d.changes.previous != nil {
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
