//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func diagnosticParser(limits limits.WorkLimits) *core.Parser {
	return &core.Parser{Input: core.Input{Control: core.NewWorkControl(nil, limits)}}
}

func diagnosticHash(id core.ExpID) uint32 {
	return uint32(id) * 0x9e3779b1 >> 26
}

func collidingDiagnosticIDs(n int) []core.ExpID {
	buckets := make(map[uint32][]core.ExpID)
	for id := core.ExpID(0); id < 1<<20; id++ {
		b := diagnosticHash(id)
		buckets[b] = append(buckets[b], id)
		if len(buckets[b]) == n {
			return buckets[b]
		}
	}
	panic("could not find deterministic expectation hash collision")
}

func uniqueDiagnosticIDs(n int) []core.ExpID {
	seen := make(map[uint32]bool)
	ids := make([]core.ExpID, 0, n)
	for id := core.ExpID(0); id < 1<<20 && len(ids) < n; id++ {
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
	t.Parallel()
	ids := collidingDiagnosticIDs(16)
	bit := uint64(1) << diagnosticHash(ids[0])
	prefix := append([]core.ExpID(nil), ids[:len(ids)-1]...)
	p := diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(prefix))})
	p.Farthest, p.ExpBits, p.Exp = 7, bit, append([]core.ExpID(nil), prefix...)
	p.Expect(7, ids[len(ids)-1])
	if len(p.Exp) != len(ids) || p.Exp[len(p.Exp)-1] != ids[len(ids)-1] || p.Control.StepsUsed != uint64(len(prefix)) {
		t.Fatalf("late colliding expectation result=%v steps=%d", p.Exp, p.Control.StepsUsed)
	}

	p = diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(ids))})
	p.Farthest, p.ExpBits, p.Exp = 7, bit, append([]core.ExpID(nil), ids...)
	p.Expect(7, ids[len(ids)-1])
	if len(p.Exp) != len(ids) || p.Control.StepsUsed != uint64(len(ids)) {
		t.Fatalf("late duplicate scan changed record or steps: len=%d steps=%d", len(p.Exp), p.Control.StepsUsed)
	}

	p = diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(prefix) - 1)})
	p.Farthest, p.ExpBits, p.Exp = 7, bit, append([]core.ExpID(nil), prefix...)
	before := append([]core.ExpID(nil), p.Exp...)
	if err := catchFatal(t, func() { p.Expect(7, ids[len(ids)-1]) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("one-short collision scan error = %v, want ErrWorkLimit", err)
	}
	if !reflect.DeepEqual(p.Exp, before) || p.Control.StepsUsed != uint64(len(prefix)-1) {
		t.Fatalf("failed expectation scan mutated record or used %d steps", p.Control.StepsUsed)
	}

	p = diagnosticParser(limits.WorkLimits{MaxSteps: 4})
	p.Farthest, p.Exp, p.Silent = 12, []core.ExpID{1, 2, 3}, 1
	p.Expect(12, 4)
	p.Silent = 0
	p.Expect(11, 5)
	if p.Control.StepsUsed != 0 || len(p.Exp) != 3 {
		t.Fatalf("silent/older expectation was not a no-op: steps=%d exp=%v", p.Control.StepsUsed, p.Exp)
	}
}

func TestWorkExpectationKeepSnapshotsAndAtomicCopy(t *testing.T) {
	t.Parallel()
	ids := make([]core.ExpID, 300)
	for i := range ids {
		ids[i] = core.ExpID(i)
	}
	p := diagnosticParser(limits.WorkLimits{MaxSteps: 100_000})
	first := p.Keep(ids)
	if len(first) != len(ids) || cap(first) != len(first) {
		t.Fatalf("kept snapshot len/cap=%d/%d", len(first), cap(first))
	}
	if again := p.Keep(ids); len(again) == 0 || &again[0] != &first[0] {
		t.Fatal("equal expectation sequence did not preserve snapshot identity")
	}
	if got := p.Keep(nil); got != nil {
		t.Fatalf("empty keep = %#v, want nil", got)
	}

	equalParser := diagnosticParser(limits.WorkLimits{MaxSteps: 299})
	equalParser.Arena = append([]core.ExpID(nil), first...)
	equalParser.LastKept = equalParser.Arena[:len(ids):len(ids)]
	oldLast := &equalParser.LastKept[0]
	if err := catchFatal(t, func() { equalParser.Keep(ids) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("one-short keep equality error = %v, want ErrWorkLimit", err)
	}
	if &equalParser.LastKept[0] != oldLast || len(equalParser.LastKept) != len(ids) {
		t.Fatal("interrupted equality changed the published lastKept view")
	}

	changed := append([]core.ExpID(nil), ids...)
	for i := range changed {
		changed[i] += 1000
	}
	copyParser := diagnosticParser(limits.WorkLimits{MaxSteps: 129})
	copyParser.Arena = make([]core.ExpID, len(first), 1024)
	copy(copyParser.Arena, first)
	copyParser.LastKept = copyParser.Arena[:len(first):len(first)]
	priorAlias := copyParser.LastKept
	oldSnapshot := append([]core.ExpID(nil), copyParser.LastKept...)
	if err := catchFatal(t, func() { copyParser.Keep(changed) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("partial expectation copy error = %v, want ErrWorkLimit", err)
	}
	if len(copyParser.Arena) != len(first) || !reflect.DeepEqual(copyParser.LastKept, oldSnapshot) {
		t.Fatal("interrupted copy published partial arena or changed old snapshot")
	}
	copyParser.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 301})
	complete := copyParser.Keep(changed)
	if len(complete) != len(changed) || cap(complete) != len(complete) || !reflect.DeepEqual(copyParser.LastKept, changed) || copyParser.Control.StepsUsed != 301 {
		t.Fatal("exact-budget retry did not publish exact-capacity changed snapshot")
	}
	if len(priorAlias) != len(oldSnapshot) || cap(priorAlias) != len(oldSnapshot) || !reflect.DeepEqual(priorAlias, oldSnapshot) {
		t.Fatal("successful retry changed the retained prior snapshot alias")
	}

	rollover := make([]core.ExpID, 500)
	for i := range rollover {
		rollover[i] = core.ExpID(5000 + i)
	}
	copyParser.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 100_000})
	rolled := copyParser.Keep(rollover)
	if len(rolled) != len(rollover) || cap(rolled) != len(rolled) || !reflect.DeepEqual(priorAlias, oldSnapshot) {
		t.Fatal("arena rollover changed the retained exact-capacity snapshot")
	}
}

func TestWorkMergeExpectedForwardAliasAndBudgets(t *testing.T) {
	t.Parallel()
	ids := uniqueDiagnosticIDs(12)
	stack := make([]core.ExpID, 2+2+len(ids), 2+2+2*len(ids))
	stack[0], stack[1], stack[2], stack[3] = 90, 91, 92, 93
	copy(stack[4:], ids)
	p := diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(ids))})
	p.Exp, p.ExpBase, p.Farthest = stack, 2, 1
	aliased := p.Exp[4:]
	p.MergeExpected(5, aliased)
	if p.Farthest != 5 || len(p.Exp) != 2+len(ids) || !reflect.DeepEqual(p.Exp[:2], []core.ExpID{90, 91}) || !reflect.DeepEqual(p.Exp[2:], ids) {
		t.Fatalf("forward aliased merge state: far=%d exp=%v", p.Farthest, p.Exp)
	}
	if p.Control.StepsUsed != uint64(len(ids)) {
		t.Fatalf("merge charged %d steps, want one per visited ID", p.Control.StepsUsed)
	}

	ignored := diagnosticParser(limits.WorkLimits{MaxSteps: uint64(len(ids))})
	ignored.Exp, ignored.ExpBase, ignored.Farthest, ignored.Silent = []core.ExpID{7, 8}, 1, 5, 1
	ignored.MergeExpected(5, ids)
	if ignored.Control.StepsUsed != uint64(len(ids)) || !reflect.DeepEqual(ignored.Exp, []core.ExpID{7, 8}) {
		t.Fatalf("silent merge steps/state = %d/%v", ignored.Control.StepsUsed, ignored.Exp)
	}

	canceled := diagnosticParser(limits.WorkLimits{})
	canceled.Control = core.NewWorkControl(newPollingContext(1), limits.WorkLimits{})
	canceled.Exp, canceled.ExpBase, canceled.Farthest, canceled.Silent = []core.ExpID{7, 8}, 1, 5, 1
	longIDs := make([]core.ExpID, 300)
	for i := range longIDs {
		longIDs[i] = core.ExpID(i)
	}
	if err := catchFatal(t, func() { canceled.MergeExpected(5, longIDs) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("polling cancellation in ignored merge = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(canceled.Exp, []core.ExpID{7, 8}) {
		t.Fatal("silent interrupted merge changed the active record")
	}
}

func diagnosticDescriptions() []string {
	return []string{"zeta", "\xff", "alpha", "\xc3\xff", "é", "alpha", "message-0", "message-1"}
}

func TestWorkMakeErrorByteSortAndMessageOrder(t *testing.T) {
	t.Parallel()
	p := diagnosticParser(limits.WorkLimits{MaxSteps: 100_000})
	p.Descs = diagnosticDescriptions()
	ids := []core.ExpID{0, core.MsgBit | 7, 1, 2, core.MsgBit | 6, 3, 4, 5}
	first := p.MakeError(0, ids)
	wantExpected := []string{"alpha", "alpha", "zeta", "é", "\xc3\xff", "\xff"}
	wantMessages := []string{"message-1", "message-0"}
	if !reflect.DeepEqual(first.Expected, wantExpected) || !reflect.DeepEqual(first.Messages, wantMessages) {
		t.Fatalf("diagnostic order Expected=%q Messages=%q", first.Expected, first.Messages)
	}
	firstExpected := append([]string(nil), first.Expected...)
	firstMessages := append([]string(nil), first.Messages...)

	long := strings.Repeat("prefix-", 512)
	p.Descs = []string{long + "z", long + "a", long + "m", "later-message"}
	second := p.MakeError(0, []core.ExpID{0, 1, core.MsgBit | 3, 2})
	if !reflect.DeepEqual(second.Expected, []string{long + "a", long + "m", long + "z"}) || !reflect.DeepEqual(second.Messages, []string{"later-message"}) {
		t.Fatal("long-prefix diagnostic sort or message order changed")
	}
	if !reflect.DeepEqual(first.Expected, firstExpected) || !reflect.DeepEqual(first.Messages, firstMessages) {
		t.Fatal("sorting a later diagnostic mutated the retained earlier error")
	}
}

func TestWorkMakeErrorInterruptionDuringConstructionAndSort(t *testing.T) {
	t.Parallel()
	const n = 300
	descs := make([]string, n)
	ids := make([]core.ExpID, n)
	for i := range descs {
		descs[i] = strings.Repeat("common-prefix-", 64) + string(rune('a'+i%26)) + strings.Repeat("x", i%13)
		ids[i] = core.ExpID(i)
	}
	p := diagnosticParser(limits.WorkLimits{MaxSteps: n - 1})
	p.Descs = descs
	if err := catchFatal(t, func() { _ = p.MakeError(0, ids) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("description construction error = %v, want ErrWorkLimit", err)
	}
	if p.Control.StepsUsed != n-1 {
		t.Fatalf("description interruption used %d steps, want %d", p.Control.StepsUsed, n-1)
	}

	// Permit every description visit, then stop within the controlled sort of long-prefix strings.
	p = diagnosticParser(limits.WorkLimits{MaxSteps: n + 512})
	p.Descs = descs
	if err := catchFatal(t, func() { _ = p.MakeError(0, ids) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("sort interruption error = %v, want ErrWorkLimit", err)
	}
	if p.Control.StepsUsed <= n || p.Control.StepsUsed > n+512 {
		t.Fatalf("sort interruption steps = %d", p.Control.StepsUsed)
	}
}
