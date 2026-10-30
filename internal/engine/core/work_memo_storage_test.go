//go:build !pego_work_controls_reference

package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func memoStorageParser(t *MemoTable, limits limits.Limits) *Parser {
	return &Parser{Input: Input{Control: NewWorkControl(nil, limits)}, Memo: t}
}

func slotPointers(slots []*MemoEntry) []*MemoEntry {
	return append([]*MemoEntry(nil), slots...)
}

func assertSlotPointers(t *testing.T, got, want []*MemoEntry) {
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
	table := &MemoTable{Slots: make([]*MemoEntry, 511)}
	for i := range table.Slots {
		table.Slots[i] = &MemoEntry{Pos: i, Rule: int32(i)}
	}
	oldHeader := table.Slots
	oldPointers := slotPointers(oldHeader)
	newEntry := &MemoEntry{Node: value.Ptr(value.Data{Text: "published after retry"})}
	p := memoStorageParser(table, limits.Limits{MaxWork: 130})
	key := MemoKey{Rule: 7, Pos: 511}

	err := catchFatal(t, func() { p.MemoPut(key, newEntry) })
	if !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("slot growth error = %v, want ErrLimit", err)
	}
	if len(table.Slots) != 511 || cap(table.Slots) != cap(oldHeader) {
		t.Fatalf("failed growth published slot header len/cap %d/%d, want 511/%d", len(table.Slots), cap(table.Slots), cap(oldHeader))
	}
	assertSlotPointers(t, table.Slots, oldPointers)
	if newEntry.Pos != 0 || newEntry.Rule != 0 || newEntry.Env != nil {
		t.Fatalf("failed growth mutated candidate entry: %#v", newEntry)
	}

	p.Control = NewWorkControl(nil, limits.Limits{MaxWork: 10_000})
	p.MemoPut(key, newEntry)
	if len(table.Slots) != 512 {
		t.Fatalf("retry slot length = %d, want 512", len(table.Slots))
	}
	if table.Slots[511] != newEntry {
		t.Fatalf("retry slot = %p, want candidate at index 511", table.Slots[511])
	}
	assertSlotPointers(t, table.Slots[:511], oldPointers)
}

func seedSeenTable(words, capacity int) *MemoTable {
	seen := make([]uint64, words, capacity)
	for i := range seen {
		seen[i] = uint64(i+1) * 0x9e3779b97f4a7c15
	}
	return &MemoTable{Stride: 1, Seen: seen}
}

func TestWorkMemoSeenGrowthCopyAndRetry(t *testing.T) {
	table := seedSeenTable(512, 512)
	old := append([]uint64(nil), table.Seen...)
	oldSlice := table.Seen
	p := memoStorageParser(table, limits.Limits{MaxWork: 130})
	err := catchFatal(t, func() { p.memoFirstCall(512*64, 0) })
	if !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("seen allocation growth error = %v, want ErrLimit", err)
	}
	if len(table.Seen) != 512 || cap(table.Seen) != 512 {
		t.Fatalf("failed seen growth published len/cap %d/%d, want 512/512", len(table.Seen), cap(table.Seen))
	}
	if !reflect.DeepEqual(table.Seen, old) || &table.Seen[0] != &oldSlice[0] {
		t.Fatal("failed seen growth changed published words or backing array")
	}
	if table.Calls != nil {
		t.Fatalf("failed seen growth initialized adaptive counters: %#v", table.Calls)
	}

	p.Control = NewWorkControl(nil, limits.Limits{MaxWork: 10_000})
	if deferred := p.memoFirstCall(512*64, 0); !deferred {
		t.Fatal("first call after seen growth was not deferred")
	}
	if len(table.Seen) <= 512 || table.Seen[512]&(1<<0) == 0 {
		t.Fatalf("retry did not publish requested seen bit: len=%d word=%#x", len(table.Seen), table.Seen[512])
	}
}

func TestWorkMemoSeenSpareCapacityClearInterruption(t *testing.T) {
	table := seedSeenTable(512, 1024)
	backing := table.Seen[:cap(table.Seen)]
	for i := 512; i < len(backing); i++ {
		backing[i] = ^uint64(0)
	}
	old := append([]uint64(nil), table.Seen...)
	oldHeader := table.Seen
	p := memoStorageParser(table, limits.Limits{MaxWork: 130})
	err := catchFatal(t, func() { p.memoFirstCall(512*64, 0) })
	if !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("seen spare-capacity clear error = %v, want ErrLimit", err)
	}
	if len(table.Seen) != 512 || cap(table.Seen) != 1024 || &table.Seen[0] != &oldHeader[0] {
		t.Fatalf("failed clear changed published header: len/cap=%d/%d", len(table.Seen), cap(table.Seen))
	}
	if !reflect.DeepEqual(table.Seen, old) {
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
	if table.Calls != nil {
		t.Fatalf("failed clear initialized adaptive counters: %#v", table.Calls)
	}

	p.Control = NewWorkControl(nil, limits.Limits{MaxWork: 10_000})
	if deferred := p.memoFirstCall(512*64, 0); !deferred {
		t.Fatal("first call after spare-capacity retry was not deferred")
	}
	if table.Seen[512] != 1 {
		t.Fatalf("retried seen bit word = %#x, want 1", table.Seen[512])
	}
}

func TestWorkMemoSeenGrowthCancellationIsAtomic(t *testing.T) {
	table := seedSeenTable(512, 512)
	old := append([]uint64(nil), table.Seen...)
	ctx := newPollingContext(1)
	p := memoStorageParser(table, limits.Limits{})
	p.Control = NewWorkControl(ctx, limits.Limits{})
	err := catchFatal(t, func() {
		p.Control.SinceCheck = 0
		p.memoFirstCall(512*64, 0)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("seen growth cancellation error = %v, want context.Canceled", err)
	}
	if len(table.Seen) != 512 || !reflect.DeepEqual(table.Seen, old) || table.Calls != nil {
		t.Fatal("canceled seen growth published partial state")
	}
}

func cloneSeenTable(t *MemoTable) *MemoTable {
	return &MemoTable{
		Seen:   append([]uint64(nil), t.Seen...),
		Stride: t.Stride,
		Calls:  append([]SeenCalls(nil), t.Calls...),
	}
}

func compareFirstCallParity(t *testing.T, seed *MemoTable, calls [][2]int) {
	t.Helper()
	want := cloneSeenTable(seed)
	got := cloneSeenTable(seed)
	p := memoStorageParser(got, limits.Limits{MaxWork: 100_000})
	for i, call := range calls {
		expected := want.firstCall(call[0], call[1])
		actual := p.memoFirstCall(call[0], call[1])
		if actual != expected || !reflect.DeepEqual(got.Seen, want.Seen) || !reflect.DeepEqual(got.Calls, want.Calls) {
			t.Fatalf("call %d (%d,%d): controlled=%t state=%#v/%#v, raw=%t state=%#v/%#v", i, call[0], call[1], actual, got.Calls, got.Seen, expected, want.Calls, want.Seen)
		}
	}
}

func TestWorkMemoFirstCallMatchesAdaptiveEstimator(t *testing.T) {
	seed := &MemoTable{Stride: 4}
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
	nearCeiling := &MemoTable{
		Stride: 4,
		Calls:  []SeenCalls{{}, {calls: int32(1<<31 - 1)}},
	}
	nearCeiling.MarkSeen(3, 1)
	compareFirstCallParity(t, nearCeiling, [][2]int{{3, 1}, {3, 1}})
}
