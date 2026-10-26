//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func memoStorageParser(t *memoTable, limits WorkLimits) *parser {
	return &parser{input: input{control: newWorkControl(nil, limits)}, memo: t}
}

func slotPointers(slots []*memoEntry) []*memoEntry {
	return append([]*memoEntry(nil), slots...)
}

func assertSlotPointers(t *testing.T, got, want []*memoEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slot length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d pointer changed: got %p, want %p", i, got[i], want[i])
		}
	}
}

func TestWorkMemoPutSlotGrowthIsAtomic(t *testing.T) {
	table := &memoTable{slots: make([]*memoEntry, 511)}
	for i := range table.slots {
		table.slots[i] = &memoEntry{pos: i, rule: int32(i)}
	}
	oldHeader := table.slots
	oldPointers := slotPointers(oldHeader)
	newEntry := &memoEntry{node: &Node{text: "published after retry"}}
	p := memoStorageParser(table, WorkLimits{MaxSteps: 130})
	key := memoKey{rule: 7, pos: 511}

	err := catchFatal(t, func() { p.memoPut(key, newEntry) })
	if !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("slot growth error = %v, want ErrWorkLimit", err)
	}
	if len(table.slots) != 511 || cap(table.slots) != cap(oldHeader) {
		t.Fatalf("failed growth published slot header len/cap %d/%d, want 511/%d", len(table.slots), cap(table.slots), cap(oldHeader))
	}
	assertSlotPointers(t, table.slots, oldPointers)
	if newEntry.pos != 0 || newEntry.rule != 0 || newEntry.env != nil {
		t.Fatalf("failed growth mutated candidate entry: %#v", newEntry)
	}

	p.control = newWorkControl(nil, WorkLimits{MaxSteps: 10_000})
	p.memoPut(key, newEntry)
	if len(table.slots) != 512 {
		t.Fatalf("retry slot length = %d, want 512", len(table.slots))
	}
	if table.slots[511] != newEntry {
		t.Fatalf("retry slot = %p, want candidate at index 511", table.slots[511])
	}
	assertSlotPointers(t, table.slots[:511], oldPointers)
}

func seedSeenTable(words, capacity int) *memoTable {
	seen := make([]uint64, words, capacity)
	for i := range seen {
		seen[i] = uint64(i+1) * 0x9e3779b97f4a7c15
	}
	return &memoTable{stride: 1, seen: seen}
}

func TestWorkMemoSeenGrowthCopyAndRetry(t *testing.T) {
	table := seedSeenTable(512, 512)
	old := append([]uint64(nil), table.seen...)
	oldSlice := table.seen
	p := memoStorageParser(table, WorkLimits{MaxSteps: 130})
	err := catchFatal(t, func() { p.memoFirstCall(512*64, 0) })
	if !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("seen allocation growth error = %v, want ErrWorkLimit", err)
	}
	if len(table.seen) != 512 || cap(table.seen) != 512 {
		t.Fatalf("failed seen growth published len/cap %d/%d, want 512/512", len(table.seen), cap(table.seen))
	}
	if !reflect.DeepEqual(table.seen, old) || &table.seen[0] != &oldSlice[0] {
		t.Fatal("failed seen growth changed published words or backing array")
	}
	if table.calls != nil {
		t.Fatalf("failed seen growth initialized adaptive counters: %#v", table.calls)
	}

	p.control = newWorkControl(nil, WorkLimits{MaxSteps: 10_000})
	if deferred := p.memoFirstCall(512*64, 0); !deferred {
		t.Fatal("first call after seen growth was not deferred")
	}
	if len(table.seen) <= 512 || table.seen[512]&(1<<0) == 0 {
		t.Fatalf("retry did not publish requested seen bit: len=%d word=%#x", len(table.seen), table.seen[512])
	}
}

func TestWorkMemoSeenSpareCapacityClearInterruption(t *testing.T) {
	table := seedSeenTable(512, 1024)
	backing := table.seen[:cap(table.seen)]
	for i := 512; i < len(backing); i++ {
		backing[i] = ^uint64(0)
	}
	old := append([]uint64(nil), table.seen...)
	oldHeader := table.seen
	p := memoStorageParser(table, WorkLimits{MaxSteps: 130})
	err := catchFatal(t, func() { p.memoFirstCall(512*64, 0) })
	if !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("seen spare-capacity clear error = %v, want ErrWorkLimit", err)
	}
	if len(table.seen) != 512 || cap(table.seen) != 1024 || &table.seen[0] != &oldHeader[0] {
		t.Fatalf("failed clear changed published header: len/cap=%d/%d", len(table.seen), cap(table.seen))
	}
	if !reflect.DeepEqual(table.seen, old) {
		t.Fatal("failed clear changed words in the published seen slice")
	}
	// The growth is charged in chunks: the limit stops it at the first chunk that does not fit.
	if !strings.Contains(err.Error(), "requested 128") {
		t.Fatalf("seen growth charge was not chunked: %v", err)
	}
	// The charge precedes any write: an interrupted growth touches no storage.
	for i := 512; i < len(backing); i++ {
		if backing[i] != ^uint64(0) {
			t.Fatalf("interrupted growth wrote word %d", i)
		}
	}
	if table.calls != nil {
		t.Fatalf("failed clear initialized adaptive counters: %#v", table.calls)
	}

	p.control = newWorkControl(nil, WorkLimits{MaxSteps: 10_000})
	if deferred := p.memoFirstCall(512*64, 0); !deferred {
		t.Fatal("first call after spare-capacity retry was not deferred")
	}
	if table.seen[512] != 1 {
		t.Fatalf("retried seen bit word = %#x, want 1", table.seen[512])
	}
}

func TestWorkMemoSeenGrowthCancellationIsAtomic(t *testing.T) {
	table := seedSeenTable(512, 512)
	old := append([]uint64(nil), table.seen...)
	ctx := newPollingContext(1)
	p := memoStorageParser(table, WorkLimits{})
	p.control = newWorkControl(ctx, WorkLimits{})
	err := catchFatal(t, func() {
		p.control.stepsSinceCheck = 0
		p.memoFirstCall(512*64, 0)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("seen growth cancellation error = %v, want context.Canceled", err)
	}
	if len(table.seen) != 512 || !reflect.DeepEqual(table.seen, old) || table.calls != nil {
		t.Fatal("canceled seen growth published partial state")
	}
}

func cloneSeenTable(t *memoTable) *memoTable {
	return &memoTable{
		seen:   append([]uint64(nil), t.seen...),
		stride: t.stride,
		calls:  append([]seenCalls(nil), t.calls...),
	}
}

func compareFirstCallParity(t *testing.T, seed *memoTable, calls [][2]int) {
	t.Helper()
	want := cloneSeenTable(seed)
	got := cloneSeenTable(seed)
	p := memoStorageParser(got, WorkLimits{MaxSteps: 100_000})
	for i, call := range calls {
		expected := want.firstCall(call[0], call[1])
		actual := p.memoFirstCall(call[0], call[1])
		if actual != expected || !reflect.DeepEqual(got.seen, want.seen) || !reflect.DeepEqual(got.calls, want.calls) {
			t.Fatalf("call %d (%d,%d): controlled=%t state=%#v/%#v, raw=%t state=%#v/%#v", i, call[0], call[1], actual, got.calls, got.seen, expected, want.calls, want.seen)
		}
	}
}

func TestWorkMemoFirstCallMatchesAdaptiveEstimator(t *testing.T) {
	seed := &memoTable{stride: 4}
	var calls [][2]int
	for i := 0; i < 48; i++ {
		calls = append(calls, [2]int{0, 1})
	}
	for i := 0; i < 80; i++ {
		calls = append(calls, [2]int{i % 7, i % 4})
	}
	compareFirstCallParity(t, seed, calls)

	// Seed a repeated key just below the signed counter ceiling. The next increment
	// crosses the high bit, so the raw estimator must discount both counters.
	nearCeiling := &memoTable{
		stride: 4,
		calls:  []seenCalls{{}, {calls: int32(1<<31 - 1)}},
	}
	nearCeiling.markSeen(3, 1)
	compareFirstCallParity(t, nearCeiling, [][2]int{{3, 1}, {3, 1}})
}
