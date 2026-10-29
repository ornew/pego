//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkDocumentMemoReplayAtomicReservation(t *testing.T) {
	edits := []docEdit{{start: 0, end: 1, delta: 2}, {start: 0, end: 1, delta: 3}, {start: 0, end: 1, delta: 4}}
	before := memoEntry{pos: 10, end: 12, from: 10, examined: 13, far: 12}
	e := before
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 2})
	if err := catchFatal(t, func() { p.advanceEntry(&e, edits) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("interruption = %v", err)
	}
	if !reflect.DeepEqual(e, before) {
		t.Fatal("interrupted replay published partial memo state")
	}
	if p.control.inputUsed != 0 || p.control.stepsUsed != 2 {
		t.Fatal("edit replay charged input or failed reservation")
	}
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 3})
	want := before
	if !advanceEntry(&want, edits) || !p.advanceEntry(&e, edits) || !reflect.DeepEqual(e, want) {
		t.Fatal("completed replay differs from ordinary edit replay")
	}
}

func TestWorkDocumentResyncSearchAndTailAccounting(t *testing.T) {
	elems := make([]runElem, 513)
	for i := range elems {
		elems[i] = runElem{start: i * 2, from: 100}
	}
	for _, at := range []int{-1, 0, 1, 510, 1024, 1025} {
		p := stateWorkParser(limits.WorkLimits{MaxSteps: 100})
		want := (&parser{}).runSearch(elems, 3, at)
		if got := p.runSearch(elems, 3, at); got != want {
			t.Fatalf("search(%d) = %d, want %d", at, got, want)
		}
	}
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 1})
	if err := catchFatal(t, func() { p.runSearch(elems, 0, 510) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("search interruption = %v", err)
	}
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 2})
	if err := catchFatal(t, func() { p.runAfter(elems[:3], 100) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("tail interruption = %v", err)
	}
	if p.control.inputUsed != 0 {
		t.Fatal("run traversal charged input")
	}
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 3})
	if !p.runAfter(elems[:3], 100) || p.control.stepsUsed != 3 {
		t.Fatal("tail traversal did not account for each element")
	}
}

func TestWorkDocumentRunPublicationCopyIsProvisional(t *testing.T) {
	old := make([]runElem, 128)
	mid := make([]runElem, 1)
	for i := range old {
		old[i].start = i
	}
	mid[0].start = 900
	r := runState{key: runKey{site: 1}, old: old, k: 128, j: -1, mid: mid, count: 129}
	retained := &runRecord{elems: old}
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 128})
	p.runs = map[runKey]*runRecord{r.key: retained}
	if err := catchFatal(t, func() { p.finishRun(&r) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("copy interruption = %v", err)
	}
	if p.runs[r.key] != retained || len(retained.elems) != 128 || retained.elems[127].start != 127 {
		t.Fatal("interrupted assembly replaced retained run")
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 129})
	p.finishRun(&r)
	if len(p.runs[r.key].elems) != 129 || p.runs[r.key].elems[128].start != 900 {
		t.Fatal("completed run assembly differs")
	}
}

func TestWorkDocumentMovementChargesEdges(t *testing.T) {
	child := value.Ptr(value.Data{Start: 2, End: 3})
	field := value.Ptr(value.Data{Start: 4, End: 5})
	root := value.Ptr(value.Data{Start: 1, End: 6, Children: []*value.Node{child}, Fields: value.Fields{{Name: "field", Value: field}}})
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 3})
	p.gen = 1
	if err := catchFatal(t, func() { p.moveTree(root, 10) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("movement interruption = %v", err)
	}
	// Legacy movement is non-transactional, but later unvisited fields remain.
	if value.D(root).Start != 11 || value.D(child).Start != 12 || value.D(field).Start != 4 {
		t.Fatal("movement checkpoints do not precede individual edges")
	}
	if p.control.inputUsed != 0 {
		t.Fatal("graph traversal charged input")
	}
}

func TestWorkDocumentRunAssemblyMatchesOrdinarySplice(t *testing.T) {
	defer func(n int) { minRecorded = n }(minRecorded)
	minRecorded = 1 // Keep the splice fixture small, independent of the recording heuristic.
	for _, controlled := range []bool{false, true} {
		old := make([]runElem, 8)
		for i := range old {
			old[i].start = i
		}
		r := runState{key: runKey{site: 1}, old: old, k: 2, j: 4, t: 3, mid: []runElem{{start: 900}}, post: []runElem{{start: 901}, {start: 902}}, count: 8}
		p := &parser{runs: map[runKey]*runRecord{}}
		if controlled {
			p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 8})
		}
		p.finishRun(&r)
		var got []int
		for _, el := range p.runs[r.key].elems {
			got = append(got, el.start)
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
	if err := catchFatal(t, func() { growWork(p, values, 1) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("growth interruption = %v", err)
	}
	if !reflect.DeepEqual(values, before) {
		t.Fatal("growth changed source references")
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 129})
	got := growWork(p, values, 1)
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
				var running *parser
				stoppedAt := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				observed := &workDocumentReuseContext{Context: ctx, observe: func() {
					if running != nil && running.resumed >= 32 {
						stoppedAt = running.resumed
						cancel()
					}
				}}
				o := base
				o.Context = observed
				o.Trace = func(e TraceEvent) {
					if e.Rule == "main" && e.Kind == TraceEnter {
						running = e.p
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
				if running.control != nil || running.tr != nil || running.prog != nil {
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
	p.moved = map[*value.Node]*value.Node{a: a, b: b}
	p.shifts = map[uint32]int{1: 3, 2: 4}
	if err := catchFatal(t, func() { p.clearMovementWork() }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("cleanup interruption = %v", err)
	}
	if len(p.moved) != 1 || len(p.shifts) != 2 || value.D(a).Text != "a" || value.D(b).Text != "b" {
		t.Fatal("cleanup changed pointees or skipped reservations")
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 3})
	p.clearMovementWork()
	if len(p.moved) != 0 || len(p.shifts) != 0 || p.control.stepsUsed != 3 {
		t.Fatal("cleanup did not finish remaining entries")
	}
}

func TestWorkDocumentShiftReplayAndSharedEmptyGraph(t *testing.T) {
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 2})
	p.gen = 4
	p.edits = []docEdit{{start: 0, end: 1, delta: 1}, {start: 0, end: 1, delta: 1}, {start: 0, end: 1, delta: 1}, {start: 0, end: 1, delta: 1}}
	n := value.Ptr(value.Data{Start: 10, End: 11, Gen: 1})
	if err := catchFatal(t, func() { p.shiftOf(n) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("shift interruption = %v", err)
	}
	if p.shifts != nil || value.D(n).Start != 10 {
		t.Fatal("interrupted shift replay published state")
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 3})
	if got := p.shiftOf(n); got != 3 || p.shifts[1] != 3 {
		t.Fatalf("shift = %d, cache=%v", got, p.shifts)
	}
	empty := value.Ptr(value.Data{Start: 0, End: 0})
	leaf := value.Ptr(value.Data{Start: 3, End: 4})
	root := value.Ptr(value.Data{Start: 0, End: 0, Children: []*value.Node{empty, leaf}, Fields: value.Fields{{Name: "empty", Value: empty}, {Name: "leaf", Value: leaf}}})
	clone := root.Clone()
	history := resultJSON(clone, nil)
	p = stateWorkParser(limits.WorkLimits{MaxSteps: 32})
	p.gen = 1
	p.moved = map[*value.Node]*value.Node{}
	got := p.moveEmpty(root, 2)
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

func TestWorkDocumentRunStateClearReservation(t *testing.T) {
	marker := value.Ptr(value.Data{Text: "retained"})
	r := &runState{old: []runElem{{v: marker}}, mid: make([]runElem, 129), post: []runElem{{v: marker}}}
	for i := range r.mid {
		r.mid[i].v = marker
	}
	p := stateWorkParser(limits.WorkLimits{MaxSteps: 128})
	if err := catchFatal(t, func() { p.freeRunState(r) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("clear interruption = %v", err)
	}
	if len(p.freeRuns) != 0 || r.old != nil || r.mid[128].v != marker || r.post[0].v != marker || value.D(marker).Text != "retained" {
		t.Fatal("incomplete clear was published or changed pointees")
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 130})
	p.freeRunState(r)
	if len(p.freeRuns) != 1 || r.mid[128].v != nil || r.post[0].v != nil {
		t.Fatal("completed clear did not release references")
	}
}

func TestWorkDocumentStaleMemoCallReplayIsBounded(t *testing.T) {
	p := &parser{memo: newMemoTable(), pos: 19, gen: 3, edits: []docEdit{{start: 0, end: 1, delta: 2}, {start: 0, end: 1, delta: 3}, {start: 0, end: 1, delta: 4}}}
	r := &rule{ID: 1}
	e := &memoEntry{ok: true, end: 12, from: 10, examined: 13, far: 12}
	p.memo.put(memoKey{rule: 1, pos: 19}, e)
	e.pos = 10 // A shifted slot can retain older value coordinates until replay.
	before := *e
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 4}) // Lookup costs two; only two of three edits fit.
	if err := catchFatal(t, func() { p.callBegin(r, 0) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("stale hit interruption = %v", err)
	}
	if !reflect.DeepEqual(*e, before) {
		t.Fatal("stale hit published partial replay")
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 5})
	_, _, ok, hit := p.callBegin(r, 0)
	if !ok || !hit || p.pos != 21 || e.vgen != 3 || e.gen != 3 {
		t.Fatalf("completed stale hit: ok=%v hit=%v pos=%d entry=%+v", ok, hit, p.pos, e)
	}
}

func TestWorkDocumentGrowthRejectsNegativeCapacity(t *testing.T) {
	panicOf := func(run func()) (got any) { defer func() { got = recover() }(); run(); return nil }
	values := []int{1}
	want := panicOf(func() { _ = slices.Grow(values, -1) })
	for _, p := range []*parser{{}, stateWorkParser(limits.WorkLimits{MaxSteps: 1})} {
		got := panicOf(func() { growWork(p, values, -1) })
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("negative capacity panic=%v, want=%v", got, want)
		}
		if p.control != nil && p.control.stepsUsed != 0 {
			t.Fatal("invalid growth consumed work")
		}
	}
}
