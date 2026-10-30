package ref

import (
	"errors"
	"testing"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/ops"
)

// The model is the executable contract; these tests pin the behaviors its contract text states.

var pilot = ops.Pilot()

func call(m *Model, name string, args ...any) []any {
	return m.Call(pilot.Op(name), args)
}

func TestMemoModel(t *testing.T) {
	m := New(Config{})
	k := []any{int64(1), int64(5), int64(0), int64(7)}
	if r := call(m, "memo.lookup", k...); r[1].(bool) {
		t.Fatal("empty memo found an entry")
	}
	e := call(m, "memo.store", append(k, true, int64(9))...)[0]
	if r := call(m, "memo.lookup", k...); r[0] != e || !r[1].(bool) {
		t.Fatal("lookup after store")
	}
	other := []any{int64(1), int64(5), int64(0), int64(8)}
	if r := call(m, "memo.lookup", other...); r[1].(bool) {
		t.Fatal("other values found the entry")
	}
	// Growth under other values: the guard finds the seed whatever the values.
	s := call(m, "memo.seed", other...)[0]
	if r := call(m, "memo.lookupGrowing", other[:3]...); r[0] != s {
		t.Fatal("lookupGrowing missed the seed")
	}
	if m.Allows(pilot.Op("memo.lookup"), k) {
		t.Fatal("lookup of another key of a growing slot is allowed")
	}
	s2 := call(m, "memo.reseed", append(other, int64(6))...)[0]
	call(m, "memo.finishSeed", s2)
	if r := call(m, "memo.lookupGrowing", other[:3]...); r[1].(bool) {
		t.Fatal("finished seed still growing")
	}
	if r := call(m, "memo.lookup", other...); r[0] != s2 || !call(m, "memo.ok", s2)[0].(bool) || call(m, "memo.end", s2)[0] != int64(6) {
		t.Fatal("finished seed is not the complete entry")
	}
	if got := m.Quantity(ops.QKeyScan, nil, k); got != 4 {
		t.Fatalf("key scan at 5 = %d, want 4 (two entries of one value)", got)
	}
}

func TestSlotGrowth(t *testing.T) {
	m := New(Config{})
	store := func(pos int64) int64 {
		args := []any{int64(0), pos, int64(0), int64(0), true, pos}
		q := m.Quantity(ops.QSlotGrowth, nil, args)
		call(m, "memo.store", args...)
		return q
	}
	for _, c := range []struct{ pos, want int64 }{{0, 0}, {1, 1}, {2, 0}, {3, 3}, {7, 4}, {7, 0}, {8, 0}, {15, 9}} {
		if got := store(c.pos); got != c.want {
			t.Errorf("growth storing at %d = %d, want %d", c.pos, got, c.want)
		}
	}
}

func TestSeenModel(t *testing.T) {
	m := New(Config{Rules: 2})
	if !call(m, "seen.first", int64(0), int64(3))[0].(bool) || call(m, "seen.first", int64(0), int64(3))[0].(bool) {
		t.Fatal("first and repeated call")
	}
	// One repeat in two calls exceeds 2/16: the rule is eager and records nothing more.
	if call(m, "seen.first", int64(0), int64(4))[0].(bool) {
		t.Fatal("eager rule deferred a call")
	}
	if !call(m, "seen.first", int64(1), int64(4))[0].(bool) {
		t.Fatal("rules share counts")
	}
}

func TestBudgetAndDepthModels(t *testing.T) {
	m := New(Config{Limit: 5, MaxDepth: 1})
	call(m, "budget.charge", uint64(5))
	func() {
		defer func() {
			if x := recover(); !errors.As(asErr(x), new(exec.Interrupt)) {
				t.Fatalf("charge beyond the limit: %v", x)
			}
		}()
		call(m, "budget.charge", uint64(1))
	}()
	if call(m, "budget.used")[0] != uint64(5) {
		t.Fatal("an interrupted charge changed the units used")
	}
	call(m, "depth.enter")
	func() {
		defer func() {
			if _, ok := recover().(exec.Interrupt); !ok {
				t.Fatal("depth beyond the limit did not interrupt")
			}
		}()
		call(m, "depth.enter")
	}()
}

func asErr(x any) error {
	err, _ := x.(error)
	return err
}
