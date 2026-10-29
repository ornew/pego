//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkDocumentMemoReplayAtomicReservation(t *testing.T) {
	edits := []core.DocEdit{{Start: 0, End: 1, Delta: 2}, {Start: 0, End: 1, Delta: 3}, {Start: 0, End: 1, Delta: 4}}
	before := core.MemoEntry{Pos: 10, End: 12, From: 10, Examined: 13, Far: 12}
	e := before
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 2})
	if err := catchFatal(t, func() { p.AdvanceEntry(&e, edits) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("interruption = %v", err)
	}
	if !reflect.DeepEqual(e, before) {
		t.Fatal("interrupted replay published partial memo state")
	}
	if p.Control.InputUsed != 0 || p.Control.StepsUsed != 2 {
		t.Fatal("edit replay charged input or failed reservation")
	}
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 3})
	want := before
	if !core.AdvanceEntry(&want, edits) || !p.AdvanceEntry(&e, edits) || !reflect.DeepEqual(e, want) {
		t.Fatal("completed replay differs from ordinary edit replay")
	}
}

func TestWorkDocumentResyncSearchAndTailAccounting(t *testing.T) {
	elems := make([]core.RunElem, 513)
	for i := range elems {
		elems[i] = core.RunElem{Start: i * 2, From: 100}
	}
	for _, at := range []int{-1, 0, 1, 510, 1024, 1025} {
		p := stateWorkParser(limits.WorkLimits{MaxSteps: 100})
		want := (&core.Parser{}).RunSearch(elems, 3, at)
		if got := p.RunSearch(elems, 3, at); got != want {
			t.Fatalf("search(%d) = %d, want %d", at, got, want)
		}
	}
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 1})
	if err := catchFatal(t, func() { p.RunSearch(elems, 0, 510) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("search interruption = %v", err)
	}
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 2})
	if err := catchFatal(t, func() { p.RunAfter(elems[:3], 100) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("tail interruption = %v", err)
	}
	if p.Control.InputUsed != 0 {
		t.Fatal("run traversal charged input")
	}
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 3})
	if !p.RunAfter(elems[:3], 100) || p.Control.StepsUsed != 3 {
		t.Fatal("tail traversal did not account for each element")
	}
}

func TestWorkDocumentRunPublicationCopyIsProvisional(t *testing.T) {
	old := make([]core.RunElem, 128)
	mid := make([]core.RunElem, 1)
	for i := range old {
		old[i].Start = i
	}
	mid[0].Start = 900
	r := core.RunState{Key: core.RunKey{Site: 1}, Old: old, K: 128, J: -1, Mid: mid, Count: 129}
	retained := &core.RunRecord{Elems: old}
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 128})
	p.Runs = map[core.RunKey]*core.RunRecord{r.Key: retained}
	if err := catchFatal(t, func() { p.FinishRun(&r) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("copy interruption = %v", err)
	}
	if p.Runs[r.Key] != retained || len(retained.Elems) != 128 || retained.Elems[127].Start != 127 {
		t.Fatal("interrupted assembly replaced retained run")
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 129})
	p.FinishRun(&r)
	if len(p.Runs[r.Key].Elems) != 129 || p.Runs[r.Key].Elems[128].Start != 900 {
		t.Fatal("completed run assembly differs")
	}
}

func TestWorkDocumentMovementChargesEdges(t *testing.T) {
	child := value.Ptr(value.Data{Start: 2, End: 3})
	field := value.Ptr(value.Data{Start: 4, End: 5})
	root := value.Ptr(value.Data{Start: 1, End: 6, Children: []*value.Node{child}, Fields: value.Fields{{Name: "field", Value: field}}})
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 3})
	p.Gen = 1
	if err := catchFatal(t, func() { p.MoveTree(root, 10) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("movement interruption = %v", err)
	}
	// Legacy movement is non-transactional, but later unvisited fields remain.
	if value.D(root).Start != 11 || value.D(child).Start != 12 || value.D(field).Start != 4 {
		t.Fatal("movement checkpoints do not precede individual edges")
	}
	if p.Control.InputUsed != 0 {
		t.Fatal("graph traversal charged input")
	}
}

func TestWorkDocumentRunAssemblyMatchesOrdinarySplice(t *testing.T) {
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
			p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 8})
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

func TestWorkDocumentGrowthCopyReservation(t *testing.T) {
	values := make([]*value.Node, 129)
	for i := range values {
		values[i] = value.Ptr(value.Data{Start: int32(i)})
	}
	before := append([]*value.Node(nil), values...)
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { core.GrowWork(p, values, 1) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("growth interruption = %v", err)
	}
	if !reflect.DeepEqual(values, before) {
		t.Fatal("growth changed source references")
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 129})
	got := core.GrowWork(p, values, 1)
	if len(got) != 129 || cap(got) < 130 || !reflect.DeepEqual(got, values) {
		t.Fatal("growth did not preserve length and values")
	}
}

type workDocumentReuseContext struct {
	context.Context
	observe func()
}

func (c *workDocumentReuseContext) Err() error { c.observe(); return c.Context.Err() }

func TestWorkDocumentReuseCancellationAndFreshRetry(t *testing.T) {
	prog := mustCompile(t, `rule main = line* $$

rule line = [a-z] #repeat(2) #atomic "\n"`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		for _, phase := range []string{"prefix", "shifted-tail"} {
			t.Run(phase, func(t *testing.T) {
				text := strings.Repeat("ab\n", 600)
				d, err := prog.NewDocumentWith("main", text, base)
				if err != nil {
					t.Fatal(err)
				}
				old, err := d.Parse()
				if err != nil {
					t.Fatal(err)
				}
				clone := old.Clone()
				history := resultJSON(clone, nil)
				start, end, replacement := 1500, 1501, "x"
				if phase == "shifted-tail" {
					start, end, replacement = 0, 0, "xy\n"
				}
				if err := d.Edit(start, end, replacement); err != nil {
					t.Fatal(err)
				}
				expectedText := text[:start] + replacement + text[end:]
				var running *core.Parser
				stoppedAt := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				observed := &workDocumentReuseContext{Context: ctx, observe: func() {
					if running != nil && running.Resumed >= 32 {
						stoppedAt = running.Resumed
						cancel()
					}
				}}
				o := base
				o.Context = observed
				o.Trace = func(e TraceEvent) {
					if e.Rule == "main" && e.Kind == TraceEnter {
						running = core.TraceParser(e)
					}
				}
				n, err := d.ParseWith(o)
				if n != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("node=%v error=%v", n, err)
				}
				if stoppedAt < 32 || stoppedAt >= 599 {
					t.Fatalf("canceled outside partial reuse: %d", stoppedAt)
				}
				if phase == "prefix" && stoppedAt >= 500 {
					t.Fatalf("prefix was fully replayed before cancellation: %d", stoppedAt)
				}
				workDocumentAssertAborted(t, d)
				if d.Text() != expectedText || d.ParseDefaults().Context != nil {
					t.Fatal("interruption changed text or defaults")
				}
				if running.Control != nil || running.Tr != nil || running.Prog != nil {
					t.Fatal("interruption retained trace or invocation references")
				}
				if resultJSON(clone, nil) != history {
					t.Fatal("reuse interruption changed historical Clone")
				}
				workDocumentAssertRetry(t, d)
			})
		}
	})
}

func TestWorkDocumentMovementMapCleanupIsCounted(t *testing.T) {
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 1})
	a, b := value.Ptr(value.Data{Text: "a"}), value.Ptr(value.Data{Text: "b"})
	p.Moved = map[*value.Node]*value.Node{a: a, b: b}
	p.Shifts = map[uint32]int{1: 3, 2: 4}
	if err := catchFatal(t, func() { p.ClearMovementWork() }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("cleanup interruption = %v", err)
	}
	if len(p.Moved) != 1 || len(p.Shifts) != 2 || value.D(a).Text != "a" || value.D(b).Text != "b" {
		t.Fatal("cleanup changed pointees or skipped reservations")
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 3})
	p.ClearMovementWork()
	if len(p.Moved) != 0 || len(p.Shifts) != 0 || p.Control.StepsUsed != 3 {
		t.Fatal("cleanup did not finish remaining entries")
	}
}

func TestWorkDocumentShiftReplayAndSharedEmptyGraph(t *testing.T) {
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 2})
	p.Gen = 4
	p.Edits = []core.DocEdit{{Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}}
	n := value.Ptr(value.Data{Start: 10, End: 11, Gen: 1})
	if err := catchFatal(t, func() { p.ShiftOf(n) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("shift interruption = %v", err)
	}
	if p.Shifts != nil || value.D(n).Start != 10 {
		t.Fatal("interrupted shift replay published state")
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 3})
	if got := p.ShiftOf(n); got != 3 || p.Shifts[1] != 3 {
		t.Fatalf("shift = %d, cache=%v", got, p.Shifts)
	}
	empty := value.Ptr(value.Data{Start: 0, End: 0})
	leaf := value.Ptr(value.Data{Start: 3, End: 4})
	root := value.Ptr(value.Data{Start: 0, End: 0, Children: []*value.Node{empty, leaf}, Fields: value.Fields{{Name: "empty", Value: empty}, {Name: "leaf", Value: leaf}}})
	clone := root.Clone()
	history := resultJSON(clone, nil)
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 32})
	p.Gen = 1
	p.Moved = map[*value.Node]*value.Node{}
	got := p.MoveEmpty(root, 2)
	if got == root || value.D(root).Start != 0 || value.D(empty).Start != 0 || value.D(got).Children[0] != value.D(got).Fields[0].Value.(*value.Node) {
		t.Fatal("empty shared nodes were mutated or duplicated")
	}
	if value.D(leaf).Start != 5 || value.D(got).Children[1] != value.D(got).Fields[1].Value.(*value.Node) {
		t.Fatal("shared non-empty descendant was moved more than once")
	}
	if resultJSON(clone, nil) != history {
		t.Fatal("movement changed historical Clone")
	}
}

func TestWorkDocumentStaleMemoCallReplayIsBounded(t *testing.T) {
	p := &core.Parser{Memo: core.NewMemoTable(), Pos: 19, Gen: 3, Edits: []core.DocEdit{{Start: 0, End: 1, Delta: 2}, {Start: 0, End: 1, Delta: 3}, {Start: 0, End: 1, Delta: 4}}}
	r := &core.Rule{ID: 1}
	e := &core.MemoEntry{Ok: true, End: 12, From: 10, Examined: 13, Far: 12}
	p.Memo.Put(core.MemoKey{Rule: 1, Pos: 19}, e)
	e.Pos = 10 // A shifted slot can retain older value coordinates until replay.
	before := *e
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 4}) // Lookup costs two; only two of three edits fit.
	if err := catchFatal(t, func() { p.CallBegin(r, 0) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("stale hit interruption = %v", err)
	}
	if !reflect.DeepEqual(*e, before) {
		t.Fatal("stale hit published partial replay")
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 5})
	_, _, ok, hit := p.CallBegin(r, 0)
	if !ok || !hit || p.Pos != 21 || e.Vgen != 3 || e.Gen != 3 {
		t.Fatalf("completed stale hit: ok=%v hit=%v pos=%d entry=%+v", ok, hit, p.Pos, e)
	}
}

func TestWorkDocumentGrowthRejectsNegativeCapacity(t *testing.T) {
	panicOf := func(run func()) (got any) { defer func() { got = recover() }(); run(); return nil }
	values := []int{1}
	want := panicOf(func() { _ = slices.Grow(values, -1) })
	for _, p := range []*core.Parser{{}, stateWorkParser(limits.WorkLimits{MaxSteps: 1})} {
		got := panicOf(func() { core.GrowWork(p, values, -1) })
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("negative capacity panic=%v, want=%v", got, want)
		}
		if p.Control != nil && p.Control.StepsUsed != 0 {
			t.Fatal("invalid growth consumed work")
		}
	}
}
