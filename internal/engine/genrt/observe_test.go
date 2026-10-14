package genrt

import (
	"reflect"
	"testing"
)

func TestObservationSeparatesSkippedMemoAndEvaluations(t *testing.T) {
	var events []TraceEvent
	var p Profile
	o := observation{profile: &p, trace: func(e TraceEvent) { events = append(events, e) }}
	o.enter("main", 0, 0)
	o.evaluate()
	o.skip("rejected", 0, 0, "first-character")
	o.enter("item", 0, 0)
	o.evaluate()
	o.evaluate() // LR growth is owned by item, not main.
	o.exit(true, false, 2, 3, true)
	o.enter("item", 0, 0)
	o.exit(true, true, 2, 3, true)
	o.exit(true, false, 2, 3, true)
	if p.Calls != 3 || p.Evals != 3 || p.MemoHits != 1 || p.Skipped != 1 {
		t.Fatalf("profile = %+v", p)
	}
	if events[1].Kind != TraceSkipped || events[1].Evals != 0 || events[1].Depth != 2 {
		t.Fatalf("skip = %+v", events[1])
	}
	if events[len(events)-1].Evals != 1 {
		t.Fatal("nested evaluations leaked to main")
	}
	rows := p.Snapshot()
	if rows[0].Rule != "item" || rows[1].Rule != "main" || rows[2].Rule != "rejected" {
		t.Fatalf("snapshot order = %+v", rows)
	}
	rows[0].Calls = 100
	if p.row("item").Calls != 2 {
		t.Fatal("snapshot aliases live profile")
	}
}

func TestObservationFilterIncludesDescendantsWithoutChangingDepth(t *testing.T) {
	var got []TraceEvent
	o := observation{rules: map[string]bool{"selected": true}, maxDepth: 3,
		trace: func(e TraceEvent) { got = append(got, e) }}
	o.enter("main", 0, 0)
	o.skip("other", 0, 0, "first-character")
	o.enter("selected", 0, 0)
	o.enter("child", 0, 0)
	o.enter("too-deep", 0, 0)
	o.exit(false, false, 0, 0, false)
	o.exit(true, false, 0, 0, false)
	o.skip("skipped-child", 0, 0, "first-character")
	o.exit(true, false, 0, 0, false)
	o.exit(true, false, 0, 0, false)
	var names []string
	for _, e := range got {
		names = append(names, e.Rule)
	}
	want := []string{"selected", "child", "child", "skipped-child", "selected"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("rules = %v, want %v", names, want)
	}
	if got[0].Depth != 2 || got[1].Depth != 3 {
		t.Fatalf("depths = %+v", got)
	}
}

func TestObservationUnknownExaminedAndExplicitMemo(t *testing.T) {
	var p Profile
	o := observation{profile: &p}
	o.enter("item", 0, 0)
	o.evaluate()
	o.exit(false, false, 0, 999, false)
	o.enter("item", 0, 0)
	o.evaluate()
	o.exit(false, false, 0, 4, true)
	o.enter("item", 1, 0)
	o.exit(false, false, 0, 0, false) // Zero evaluations is not automatically a memo hit.
	r := p.row("item")
	if r.Wasted != 4 || r.UnknownExamined != 2 || r.Repeats != 1 || r.MemoHits != 0 {
		t.Fatalf("row = %+v", r)
	}
}

func TestObservationPanicIdentityReentrancyAndClear(t *testing.T) {
	marker := &struct{}{}
	var inner Profile
	o := observation{trace: func(TraceEvent) {
		x := observation{profile: &inner}
		x.enter("inner", 0, 0)
		x.evaluate()
		x.exit(true, false, 1, 1, true)
		panic(marker)
	}}
	func() {
		defer o.clear()
		defer func() {
			if got := recover(); got != marker {
				t.Fatalf("panic = %v", got)
			}
		}()
		o.enter("outer", 0, 0)
	}()
	if inner.Calls != 1 || len(o.stack) != 0 || o.trace != nil {
		t.Fatal("invocation cleanup or reentrancy failed")
	}
}

func TestObservationProfilesInvocationsIndependently(t *testing.T) {
	var p Profile
	for range 2 {
		o := observation{profile: &p}
		o.enter("main", 0, 0)
		o.evaluate()
		o.exit(true, false, 1, 1, true)
		o.clear()
	}
	if p.Parses != 2 || p.Entered != 2 || p.Calls != 2 || p.row("main").Repeats != 0 {
		t.Fatalf("accumulated profile = %+v", p)
	}
}

func TestObservationSkipReasonsAndSnapshotOwnership(t *testing.T) {
	var p Profile
	o := observation{profile: &p}
	o.enter("main", 0, 0)
	o.skip("item", 0, 0, "first-character")
	o.skip("item", 0, 0, "first-character")
	o.exit(false, false, 0, 0, false)
	rows := p.Snapshot()
	rows[0].SkipReasons["first-character"] = 99
	if p.SkipReasons["first-character"] != 2 || p.row("item").SkipReasons["first-character"] != 2 {
		t.Fatal("snapshot aliases reason counts")
	}
	if p.row("item").Entered != 0 || p.row("item").Failed != 0 {
		t.Fatal("skip fabricated an entered failure")
	}
}
