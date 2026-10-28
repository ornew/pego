//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func diagnosticParser(limits limits.WorkLimits) *parser {
	return &parser{input: input{control: newWorkControl(nil, limits)}}
}

func diagnosticHash(id expID) uint32 {
	return uint32(id) * 0x9e3779b1 >> 26
}

func collidingDiagnosticIDs(n int) []expID {
	buckets := make(map[uint32][]expID)
	for id := expID(0); id < 1<<20; id++ {
		b := diagnosticHash(id)
		buckets[b] = append(buckets[b], id)
		if len(buckets[b]) == n {
			return buckets[b]
		}
	}
	panic("could not find deterministic expectation hash collision")
}

func uniqueDiagnosticIDs(n int) []expID {
	seen := make(map[uint32]bool)
	ids := make([]expID, 0, n)
	for id := expID(0); id < 1<<20 && len(ids) < n; id++ {
		b := diagnosticHash(id)
		if !seen[b] {
			seen[b] = true
			ids = append(ids, id)
		}
	}
	if len(ids) != n {
		panic("could not find unique expectation hash buckets")
	}
	return ids
}

func TestWorkExpectationCollisionScanBudgetsAndShortCircuits(t *testing.T) {
	ids := collidingDiagnosticIDs(16)
	bit := uint64(1) << diagnosticHash(ids[0])
	prefix := append([]expID(nil), ids[:len(ids)-1]...)
	p := diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(prefix))})
	p.farthest, p.expBits, p.exp = 7, bit, append([]expID(nil), prefix...)
	p.expect(7, ids[len(ids)-1])
	if len(p.exp) != len(ids) || p.exp[len(p.exp)-1] != ids[len(ids)-1] || p.control.stepsUsed != uint64(len(prefix)) {
		t.Fatalf("late colliding expectation result=%v steps=%d", p.exp, p.control.stepsUsed)
	}

	p = diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(ids))})
	p.farthest, p.expBits, p.exp = 7, bit, append([]expID(nil), ids...)
	p.expect(7, ids[len(ids)-1])
	if len(p.exp) != len(ids) || p.control.stepsUsed != uint64(len(ids)) {
		t.Fatalf("late duplicate scan changed record or steps: len=%d steps=%d", len(p.exp), p.control.stepsUsed)
	}

	p = diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(prefix) - 1)})
	p.farthest, p.expBits, p.exp = 7, bit, append([]expID(nil), prefix...)
	before := append([]expID(nil), p.exp...)
	if err := catchFatal(t, func() { p.expect(7, ids[len(ids)-1]) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("one-short collision scan error = %v, want ErrWorkLimit", err)
	}
	if !reflect.DeepEqual(p.exp, before) || p.control.stepsUsed != uint64(len(prefix)-1) {
		t.Fatalf("failed expectation scan mutated record or used %d steps", p.control.stepsUsed)
	}

	p = diagnosticParser(limits.WorkLimits{MaxSteps: 4})
	p.farthest, p.exp, p.silent = 12, []expID{1, 2, 3}, 1
	p.expect(12, 4)
	p.silent = 0
	p.expect(11, 5)
	if p.control.stepsUsed != 0 || len(p.exp) != 3 {
		t.Fatalf("silent/older expectation was not a no-op: steps=%d exp=%v", p.control.stepsUsed, p.exp)
	}
}

func TestWorkExpectationKeepSnapshotsAndAtomicCopy(t *testing.T) {
	ids := make([]expID, 300)
	for i := range ids {
		ids[i] = expID(i)
	}
	p := diagnosticParser(limits.WorkLimits{MaxSteps: 100_000})
	first := p.keep(ids)
	if len(first) != len(ids) || cap(first) != len(first) {
		t.Fatalf("kept snapshot len/cap=%d/%d", len(first), cap(first))
	}
	if again := p.keep(ids); len(again) == 0 || &again[0] != &first[0] {
		t.Fatal("equal expectation sequence did not preserve snapshot identity")
	}
	if got := p.keep(nil); got != nil {
		t.Fatalf("empty keep = %#v, want nil", got)
	}

	equalParser := diagnosticParser(limits.WorkLimits{MaxSteps: 299})
	equalParser.arena = append([]expID(nil), first...)
	equalParser.lastKept = equalParser.arena[:len(ids):len(ids)]
	oldLast := &equalParser.lastKept[0]
	if err := catchFatal(t, func() { equalParser.keep(ids) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("one-short keep equality error = %v, want ErrWorkLimit", err)
	}
	if &equalParser.lastKept[0] != oldLast || len(equalParser.lastKept) != len(ids) {
		t.Fatal("interrupted equality changed the published lastKept view")
	}

	changed := append([]expID(nil), ids...)
	for i := range changed {
		changed[i] += 1000
	}
	copyParser := diagnosticParser(limits.WorkLimits{MaxSteps: 129})
	copyParser.arena = make([]expID, len(first), 1024)
	copy(copyParser.arena, first)
	copyParser.lastKept = copyParser.arena[:len(first):len(first)]
	priorAlias := copyParser.lastKept
	oldSnapshot := append([]expID(nil), copyParser.lastKept...)
	if err := catchFatal(t, func() { copyParser.keep(changed) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("partial expectation copy error = %v, want ErrWorkLimit", err)
	}
	if len(copyParser.arena) != len(first) || !reflect.DeepEqual(copyParser.lastKept, oldSnapshot) {
		t.Fatal("interrupted copy published partial arena or changed old snapshot")
	}
	copyParser.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 301})
	complete := copyParser.keep(changed)
	if len(complete) != len(changed) || cap(complete) != len(complete) || !reflect.DeepEqual(copyParser.lastKept, changed) || copyParser.control.stepsUsed != 301 {
		t.Fatal("exact-budget retry did not publish exact-capacity changed snapshot")
	}
	if len(priorAlias) != len(oldSnapshot) || cap(priorAlias) != len(oldSnapshot) || !reflect.DeepEqual(priorAlias, oldSnapshot) {
		t.Fatal("successful retry changed the retained prior snapshot alias")
	}

	rollover := make([]expID, 500)
	for i := range rollover {
		rollover[i] = expID(5000 + i)
	}
	copyParser.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 100_000})
	rolled := copyParser.keep(rollover)
	if len(rolled) != len(rollover) || cap(rolled) != len(rolled) || !reflect.DeepEqual(priorAlias, oldSnapshot) {
		t.Fatal("arena rollover changed the retained exact-capacity snapshot")
	}
}

func TestWorkMergeExpectedForwardAliasAndBudgets(t *testing.T) {
	ids := uniqueDiagnosticIDs(12)
	stack := make([]expID, 2+2+len(ids), 2+2+2*len(ids))
	stack[0], stack[1], stack[2], stack[3] = 90, 91, 92, 93
	copy(stack[4:], ids)
	p := diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(ids))})
	p.exp, p.expBase, p.farthest = stack, 2, 1
	aliased := p.exp[4:]
	p.mergeExpected(5, aliased)
	if p.farthest != 5 || len(p.exp) != 2+len(ids) || !reflect.DeepEqual(p.exp[:2], []expID{90, 91}) || !reflect.DeepEqual(p.exp[2:], ids) {
		t.Fatalf("forward aliased merge state: far=%d exp=%v", p.farthest, p.exp)
	}
	if p.control.stepsUsed != uint64(len(ids)) {
		t.Fatalf("merge charged %d steps, want one per visited ID", p.control.stepsUsed)
	}

	ignored := diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(ids))})
	ignored.exp, ignored.expBase, ignored.farthest, ignored.silent = []expID{7, 8}, 1, 5, 1
	ignored.mergeExpected(5, ids)
	if ignored.control.stepsUsed != uint64(len(ids)) || !reflect.DeepEqual(ignored.exp, []expID{7, 8}) {
		t.Fatalf("silent merge steps/state = %d/%v", ignored.control.stepsUsed, ignored.exp)
	}

	canceled := diagnosticParser(limits.WorkLimits{})
	canceled.control = newWorkControl(newPollingContext(1), limits.WorkLimits{})
	canceled.exp, canceled.expBase, canceled.farthest, canceled.silent = []expID{7, 8}, 1, 5, 1
	longIDs := make([]expID, 300)
	for i := range longIDs {
		longIDs[i] = expID(i)
	}
	if err := catchFatal(t, func() { canceled.mergeExpected(5, longIDs) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("polling cancellation in ignored merge = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(canceled.exp, []expID{7, 8}) {
		t.Fatal("silent interrupted merge changed the active record")
	}
}

func diagnosticDescriptions() []string {
	return []string{"zeta", "\xff", "alpha", "\xc3\xff", "é", "alpha", "message-0", "message-1"}
}

func TestWorkMakeErrorByteSortAndMessageOrder(t *testing.T) {
	p := diagnosticParser(limits.WorkLimits{MaxSteps: 100_000})
	p.descs = diagnosticDescriptions()
	ids := []expID{0, msgBit | 7, 1, 2, msgBit | 6, 3, 4, 5}
	first := p.makeError(0, ids)
	wantExpected := []string{"alpha", "alpha", "zeta", "é", "\xc3\xff", "\xff"}
	wantMessages := []string{"message-1", "message-0"}
	if !reflect.DeepEqual(first.Expected, wantExpected) || !reflect.DeepEqual(first.Messages, wantMessages) {
		t.Fatalf("diagnostic order Expected=%q Messages=%q", first.Expected, first.Messages)
	}
	firstExpected := append([]string(nil), first.Expected...)
	firstMessages := append([]string(nil), first.Messages...)

	long := strings.Repeat("prefix-", 512)
	p.descs = []string{long + "z", long + "a", long + "m", "later-message"}
	second := p.makeError(0, []expID{0, 1, msgBit | 3, 2})
	if !reflect.DeepEqual(second.Expected, []string{long + "a", long + "m", long + "z"}) || !reflect.DeepEqual(second.Messages, []string{"later-message"}) {
		t.Fatal("long-prefix diagnostic sort or message order changed")
	}
	if !reflect.DeepEqual(first.Expected, firstExpected) || !reflect.DeepEqual(first.Messages, firstMessages) {
		t.Fatal("sorting a later diagnostic mutated the retained earlier error")
	}
}

func TestWorkMakeErrorInterruptionDuringConstructionAndSort(t *testing.T) {
	const n = 300
	descs := make([]string, n)
	ids := make([]expID, n)
	for i := range descs {
		descs[i] = strings.Repeat("common-prefix-", 64) + string(rune('a'+i%26)) + strings.Repeat("x", i%13)
		ids[i] = expID(i)
	}
	p := diagnosticParser(limits.WorkLimits{MaxSteps: n - 1})
	p.descs = descs
	if err := catchFatal(t, func() { _ = p.makeError(0, ids) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("description construction error = %v, want ErrWorkLimit", err)
	}
	if p.control.stepsUsed != n-1 {
		t.Fatalf("description interruption used %d steps, want %d", p.control.stepsUsed, n-1)
	}

	// Permit every description visit, then stop within the controlled sort of long-prefix strings.
	p = diagnosticParser(limits.WorkLimits{MaxSteps: n + 512})
	p.descs = descs
	if err := catchFatal(t, func() { _ = p.makeError(0, ids) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("sort interruption error = %v, want ErrWorkLimit", err)
	}
	if p.control.stepsUsed <= n || p.control.stepsUsed > n+512 {
		t.Fatalf("sort interruption steps = %d", p.control.stepsUsed)
	}
}
