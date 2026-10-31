//go:build !zp_budget_controls_reference

package engine

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/limits"
	"github.com/ornew/zeroparse/internal/value"
)

func TestBudgetDocumentMemoReplayAtomicReservation(t *testing.T) {
	t.Parallel()
	edits := []core.DocEdit{{Start: 0, End: 1, Delta: 2}, {Start: 0, End: 1, Delta: 3}, {Start: 0, End: 1, Delta: 4}}
	before := core.MemoEntry{Pos: 10, End: 12, From: 10, Examined: 13, Far: 12}
	e := before
	p := stateBudgetParser(limits.Limits{Budget: 2})
	if err := catchFatal(t, func() { p.AdvanceEntry(&e, edits) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("interruption = %v", err)
	}
	if !reflect.DeepEqual(e, before) {
		t.Fatal("interrupted replay published partial memo state")
	}
	if p.Control.Used != 2 {
		t.Fatal("edit replay charged unexpected spend")
	}
	p = stateBudgetParser(limits.Limits{Budget: 3})
	want := before
	if !core.AdvanceEntry(&want, edits) || !p.AdvanceEntry(&e, edits) || !reflect.DeepEqual(e, want) {
		t.Fatal("completed replay differs from ordinary edit replay")
	}
}

func TestBudgetDocumentResyncSearchAndTailAccounting(t *testing.T) {
	t.Parallel()
	elems := make([]core.RunElem, 513)
	for i := range elems {
		elems[i] = core.RunElem{Start: i * 2, From: 100}
	}
	for _, at := range []int{-1, 0, 1, 510, 1024, 1025} {
		p := stateBudgetParser(limits.Limits{Budget: 100})
		want := (&core.Parser{}).RunSearch(elems, 3, at)
		if got := p.RunSearch(elems, 3, at); got != want {
			t.Fatalf("search(%d) = %d, want %d", at, got, want)
		}
	}
	p := stateBudgetParser(limits.Limits{Budget: 1})
	if err := catchFatal(t, func() { p.RunSearch(elems, 0, 510) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("search interruption = %v", err)
	}
	p = stateBudgetParser(limits.Limits{Budget: 2})
	if err := catchFatal(t, func() { p.RunAfter(elems[:3], 100) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("tail interruption = %v", err)
	}
	if p.Control.Used != 2 {
		t.Fatalf("tail interruption charged %d, want the two visited elements", p.Control.Used)
	}
	p = stateBudgetParser(limits.Limits{Budget: 3})
	if !p.RunAfter(elems[:3], 100) || p.Control.Used != 3 {
		t.Fatal("tail traversal did not account for each element")
	}
}

func TestBudgetDocumentRunPublicationCopyIsProvisional(t *testing.T) {
	t.Parallel()
	old := make([]core.RunElem, 128)
	mid := make([]core.RunElem, 1)
	for i := range old {
		old[i].Start = i
	}
	mid[0].Start = 900
	r := core.RunState{Key: core.RunKey{Site: 1}, Old: old, K: 128, J: -1, Mid: mid, Count: 129}
	retained := &core.RunRecord{Elems: old}
	p := stateBudgetParser(limits.Limits{Budget: 128})
	p.Runs = map[core.RunKey]*core.RunRecord{r.Key: retained}
	if err := catchFatal(t, func() { p.FinishRun(&r) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("copy interruption = %v", err)
	}
	if p.Runs[r.Key] != retained || len(retained.Elems) != 128 || retained.Elems[127].Start != 127 {
		t.Fatal("interrupted assembly replaced retained run")
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 129})
	p.FinishRun(&r)
	if len(p.Runs[r.Key].Elems) != 129 || p.Runs[r.Key].Elems[128].Start != 900 {
		t.Fatal("completed run assembly differs")
	}
}

func TestBudgetDocumentMovementChargesEdges(t *testing.T) {
	t.Parallel()
	child := value.Ptr(value.Data{Start: 2, End: 3})
	field := value.Ptr(value.Data{Start: 4, End: 5})
	root := value.Ptr(value.Data{Start: 1, End: 6, Children: []*value.Node{child}, Fields: value.Fields{{Name: "field", Value: field}}})
	p := stateBudgetParser(limits.Limits{Budget: 3})
	p.Gen = 1
	if err := catchFatal(t, func() { p.MoveTree(root, 10) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("movement interruption = %v", err)
	}
	// Legacy movement is non-transactional, but later unvisited fields remain.
	if value.D(root).Start != 11 || value.D(child).Start != 12 || value.D(field).Start != 4 {
		t.Fatal("movement checkpoints do not precede individual edges")
	}
}

func TestBudgetDocumentRunAssemblyMatchesOrdinarySplice(t *testing.T) {
	defer func(n int) { core.MinRecorded = n }(core.MinRecorded)
	core.MinRecorded = 1 // Keep the splice fixture small, independent of the recording heuristic.
	for _, controlled := range []bool{false, true} {
		old := make([]core.RunElem, 8)
		for i := range old {
			old[i].Start = i
		}
		r := core.RunState{Key: core.RunKey{Site: 1}, Old: old, K: 2, J: 4, T: 3, Mid: []core.RunElem{{Start: 900}}, Post: []core.RunElem{{Start: 901}, {Start: 902}}, Count: 8}
		p := &core.Parser{Runs: map[core.RunKey]*core.RunRecord{}}
		if controlled {
			p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 8})
		}
		p.FinishRun(&r)
		var got []int
		for _, el := range p.Runs[r.Key].Elems {
			got = append(got, el.Start)
		}
		if !reflect.DeepEqual(got, []int{0, 1, 900, 4, 5, 6, 901, 902}) {
			t.Fatalf("controlled=%v: %v", controlled, got)
		}
	}
}

func TestBudgetDocumentGrowthCopyReservation(t *testing.T) {
	t.Parallel()
	values := make([]*value.Node, 129)
	for i := range values {
		values[i] = value.Ptr(value.Data{Start: int32(i)})
	}
	before := append([]*value.Node(nil), values...)
	p := stateBudgetParser(limits.Limits{Budget: 128})
	if err := catchFatal(t, func() { core.GrowSpend(p, values, 1) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("growth interruption = %v", err)
	}
	if !reflect.DeepEqual(values, before) {
		t.Fatal("growth changed source references")
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 129})
	got := core.GrowSpend(p, values, 1)
	if len(got) != 129 || cap(got) < 130 || !reflect.DeepEqual(got, values) {
		t.Fatal("growth did not preserve length and values")
	}
}

func TestBudgetDocumentMovementMapCleanupIsCounted(t *testing.T) {
	t.Parallel()
	p := stateBudgetParser(limits.Limits{Budget: 1})
	a, b := value.Ptr(value.Data{Text: "a"}), value.Ptr(value.Data{Text: "b"})
	p.Moved = map[*value.Node]*value.Node{a: a, b: b}
	p.Shifts = map[uint32]int{1: 3, 2: 4}
	if err := catchFatal(t, func() { p.ClearMovementSpend() }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("cleanup interruption = %v", err)
	}
	if len(p.Moved) != 1 || len(p.Shifts) != 2 || value.D(a).Text != "a" || value.D(b).Text != "b" {
		t.Fatal("cleanup changed pointees or skipped reservations")
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 3})
	p.ClearMovementSpend()
	if len(p.Moved) != 0 || len(p.Shifts) != 0 || p.Control.Used != 3 {
		t.Fatal("cleanup did not finish remaining entries")
	}
}

func TestBudgetDocumentShiftReplayAndSharedEmptyGraph(t *testing.T) {
	t.Parallel()
	p := stateBudgetParser(limits.Limits{Budget: 2})
	p.Gen = 4
	p.Edits = []core.DocEdit{{Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}}
	n := value.Ptr(value.Data{Start: 10, End: 11, Gen: 1})
	if err := catchFatal(t, func() { p.ShiftOf(n) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("shift interruption = %v", err)
	}
	if p.Shifts != nil || value.D(n).Start != 10 {
		t.Fatal("interrupted shift replay published state")
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 3})
	if got := p.ShiftOf(n); got != 3 || p.Shifts[1] != 3 {
		t.Fatalf("shift = %d, cache=%v", got, p.Shifts)
	}
	empty := value.Ptr(value.Data{Start: 0, End: 0})
	leaf := value.Ptr(value.Data{Start: 3, End: 4})
	root := value.Ptr(value.Data{Start: 0, End: 0, Children: []*value.Node{empty, leaf}, Fields: value.Fields{{Name: "empty", Value: empty}, {Name: "leaf", Value: leaf}}})
	clone := root.Clone()
	history := gentest.ResultJSON(clone, nil)
	p = stateBudgetParser(limits.Limits{Budget: 32})
	p.Gen = 1
	p.Moved = map[*value.Node]*value.Node{}
	got := p.MoveEmpty(root, 2)
	if got == root || value.D(root).Start != 0 || value.D(empty).Start != 0 || value.D(got).Children[0] != value.D(got).Fields[0].Value.(*value.Node) {
		t.Fatal("empty shared nodes were mutated or duplicated")
	}
	if value.D(leaf).Start != 5 || value.D(got).Children[1] != value.D(got).Fields[1].Value.(*value.Node) {
		t.Fatal("shared non-empty descendant was moved more than once")
	}
	if gentest.ResultJSON(clone, nil) != history {
		t.Fatal("movement changed historical Clone")
	}
}

func TestBudgetDocumentStaleMemoCallReplayIsBounded(t *testing.T) {
	t.Parallel()
	p := &core.Parser{Memo: core.NewMemoTable(), Pos: 19, Gen: 3, Edits: []core.DocEdit{{Start: 0, End: 1, Delta: 2}, {Start: 0, End: 1, Delta: 3}, {Start: 0, End: 1, Delta: 4}}}
	r := &core.Rule{ID: 1}
	e := &core.MemoEntry{Ok: true, End: 12, From: 10, Examined: 13, Far: 12}
	p.Memo.Put(core.MemoKey{Rule: 1, Pos: 19}, e)
	e.Pos = 10 // A shifted slot can retain older value coordinates until replay.
	before := *e
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 4}) // Lookup costs two; only two of three edits fit.
	if err := catchFatal(t, func() { p.CallBegin(r, 0) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("stale hit interruption = %v", err)
	}
	if !reflect.DeepEqual(*e, before) {
		t.Fatal("stale hit published partial replay")
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 5})
	_, _, ok, hit := p.CallBegin(r, 0)
	if !ok || !hit || p.Pos != 21 || e.Vgen != 3 || e.Gen != 3 {
		t.Fatalf("completed stale hit: ok=%v hit=%v pos=%d entry=%+v", ok, hit, p.Pos, e)
	}
}

func TestBudgetDocumentGrowthRejectsNegativeCapacity(t *testing.T) {
	t.Parallel()
	panicOf := func(run func()) (got any) { defer func() { got = recover() }(); run(); return nil }
	values := []int{1}
	want := panicOf(func() { _ = slices.Grow(values, -1) })
	for _, p := range []*core.Parser{{}, stateBudgetParser(limits.Limits{Budget: 1})} {
		got := panicOf(func() { core.GrowSpend(p, values, -1) })
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("negative capacity panic=%v, want=%v", got, want)
		}
		if p.Control != nil && p.Control.Used != 0 {
			t.Fatal("invalid growth consumed spend")
		}
	}
}
