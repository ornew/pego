//go:build !pego_budget_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

// memoTestVars returns n variable values: slot i holds i.
func memoTestVars(n int) []any {
	vals := make([]any, n)
	for i := range vals {
		vals[i] = i
	}
	return vals
}

// memoTestParser returns a parser whose variables are memoTestVars(n).
func memoTestParser(n int) *core.Parser {
	p := &core.Parser{}
	p.SetVars(memoTestVars(n))
	return p
}

func TestBudgetMemoEnvValuesDeepLookup(t *testing.T) {
	t.Parallel()
	const count = 320 // three levels: two inner nodes and a leaf on every path
	p := memoTestParser(count)
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1_000_000})
	vals := p.EnvValues([]int{count - 1, -1})
	if len(vals) != 2 || vals[0] != count-1 || vals[1] != nil {
		t.Fatalf("envValues = %#v, want [%d nil]", vals, count-1)
	}
	// Each variable costs a key unit; a declared one also costs a unit for every node on its path.
	if want := uint64(1 + 3 + 1); p.Control.Used != want {
		t.Fatalf("lookups used %d steps, want %d", p.Control.Used, want)
	}

	limited := memoTestParser(count)
	limited.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 3})
	if err := catchFatal(t, func() { limited.EnvValues([]int{0}) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("deep envValues error = %v, want ErrLimit", err)
	}

	// The nil-environment key remains a shared all-nil slice and resolves missing variables.
	nilParser := &core.Parser{Input: core.Input{Control: core.NewBudgetControl(nil, limits.Limits{Budget: 8})}}
	got := nilParser.EnvValues([]int{-1, -1})
	if len(got) != 2 || got[0] != nil || got[1] != nil {
		t.Fatalf("nil envValues = %#v, want two nil values", got)
	}
}

func TestBudgetMemoBindPreservesSnapshotsOnInterruption(t *testing.T) {
	t.Parallel()
	const count = 48 // two levels: an inner node and a leaf on every path
	// A rebind costs a unit for the old value and one for each node it copies.
	p := memoTestParser(count)
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 2})
	before := p.Env
	if err := catchFatal(t, func() { p.Bind(0, "replacement") }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("replacement error = %v, want ErrLimit", err)
	}
	if p.Env != before {
		t.Fatal("interrupted bind published a partial environment")
	}
	for i := 0; i < count; i++ {
		if got := before.Get(i, p.EnvDepth()); got != i {
			t.Fatalf("saved environment changed at slot %d: %v", i, got)
		}
	}

	// Equal bindings keep the persistent snapshot identity.
	stable := memoTestParser(count)
	stable.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1_000_000})
	head := stable.Env
	stable.Bind(count-1, count-1)
	stable.Bind(0, 0)
	if stable.Env != head {
		t.Fatal("equal bind changed environment identity")
	}

	// A completed update copies only the path and leaves the old snapshot intact.
	stable.Bind(0, "replacement")
	if stable.Env == head {
		t.Fatal("changed bind reused the old environment")
	}
	if got := stable.Env.Get(0, stable.EnvDepth()); got != "replacement" {
		t.Fatalf("replacement lookup = %v", got)
	}
	if got := head.Get(0, stable.EnvDepth()); got != 0 {
		t.Fatalf("saved lookup = %v; want 0", got)
	}
	if got := stable.Env.Get(count-1, stable.EnvDepth()); got != count-1 {
		t.Fatalf("replacement lost slot %d: %v", count-1, got)
	}
}

func TestBudgetMemoKeyEqualityAndLongChains(t *testing.T) {
	t.Parallel()
	table := &core.MemoTable{Slots: make([]*core.MemoEntry, 1)}
	p := &core.Parser{Memo: table}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 30})
	long := strings.Repeat("common-prefix-", 96)
	var head *core.MemoEntry
	for i := 0; i < 320; i++ {
		env := []any{i, long + fmt.Sprint(i)}
		e := &core.MemoEntry{Rule: int32(i), Min: int32(i % 3), Pos: 0, Env: env, Next: head}
		head = e
	}
	table.Slots[0] = head

	key := core.MemoKey{Rule: 0, Pos: 0, Min: 0, Env: []any{0, long + "0"}}
	if got, ok := p.MemoGet(key); !ok || got.Rule != 0 {
		t.Fatalf("late memo hit = %v, %t", got, ok)
	}
	key.Env = []any{0, long + "different"}
	if got, ok := p.MemoGet(key); ok || got != nil {
		t.Fatalf("memo miss = %v, %t; want nil, false", got, ok)
	}

	// slices.Equal semantics distinguish a typed nil from a nil interface and equate nil/empty slices.
	var typedNil *value.Node
	table.Slots[0] = &core.MemoEntry{Rule: 9, Pos: 0, Env: []any{typedNil}}
	if got, ok := p.MemoGet(core.MemoKey{Rule: 9, Pos: 0, Env: []any{nil}}); ok || got != nil {
		t.Fatalf("typed-nil key matched nil interface: %v, %t", got, ok)
	}
	table.Slots[0] = &core.MemoEntry{Rule: 9, Pos: 0}
	if got, ok := p.MemoGet(core.MemoKey{Rule: 9, Pos: 0, Env: []any{}}); !ok || got == nil {
		t.Fatalf("nil/empty key equality = %v, %t; want a hit", got, ok)
	}
}

func budgetMemoVariableGrammar() string {
	const count = 280
	var src strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&src, "var memo_key_with_common_prefix_%d int\n", i)
	}
	src.WriteString("rule main = ")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&src, "{memo_key_with_common_prefix_%d := %d} ", i, i)
	}
	src.WriteString("(r \"x\" / r) $$\nrule marker = ^^\nrule r = marker ")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&src, "{memo_key_with_common_prefix_%d == %d} ", i, i)
	}
	src.WriteString("\"a\"")
	return src.String()
}

func TestBudgetMemoParseBudgetPoolRetryAndRetainedResult(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, budgetMemoVariableGrammar())
	input := "a"
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			// The memo keys of 280 variables cost more than the default budget of a one-character
			// input: the fixture's cost comes from the grammar, so the setup parse disables the
			// budget. The limit below falls inside the key of the first call of r, which reads
			// every variable (four units each).
			opts := ParseOptions{Backend: backend, Unit: unit, Limits: limits.Limits{Budget: limits.Unlimited}}
			retained, err := prog.ParseWith("main", input, opts)
			if err != nil || retained == nil {
				t.Fatalf("%v/%v setup parse: node=%v err=%v", backend, unit, retained, err)
			}
			before, jsonErr := json.Marshal(retained)
			if jsonErr != nil {
				t.Fatal(jsonErr)
			}

			armed := false
			markerCalls := 0
			limited := opts
			limited.Limits = limits.Limits{Budget: 1 << 50}
			limited.Trace = func(e TraceEvent) {
				if e.Kind == TraceEnter && e.Rule == "marker" {
					markerCalls++
				}
				if !armed && e.Kind == TraceEnter && e.Rule == "r" {
					armed = true
					core.TraceParser(e).DeferMemo = false
					core.TraceParser(e).Control.Limits.Budget = core.TraceParser(e).Control.Used + 500
				}
			}
			if node, parseErr := prog.ParseWith("main", input, limited); node != nil || !errors.Is(parseErr, limits.ErrLimit) {
				t.Fatalf("%v/%v limited parse: node=%v err=%v", backend, unit, node, parseErr)
			}
			if markerCalls != 0 {
				t.Fatalf("memo-key interruption entered rule body %d times", markerCalls)
			}
			if !armed {
				t.Fatalf("%v/%v limited parse never reached the memoized rule", backend, unit)
			}
			retry, err := prog.ParseWith("main", input, opts)
			if err != nil || retry == nil {
				t.Fatalf("%v/%v retry parse: node=%v err=%v", backend, unit, retry, err)
			}
			if after, jsonErr := json.Marshal(retained); jsonErr != nil || string(after) != string(before) {
				t.Fatalf("%v/%v retained result changed after pool reuse", backend, unit)
			}
		}
	}
}

func TestBudgetMemoBindCopyAndCancellation(t *testing.T) {
	t.Parallel()
	// An equal binding performs the lookup but does not copy a path.
	probe := memoTestParser(320)
	root := probe.Env
	probe.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 30})
	probe.Bind(0, 0)
	searchSteps := probe.Control.Used
	p := memoTestParser(320)
	root = p.Env
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: searchSteps + 2})
	if err := catchFatal(t, func() { p.Bind(0, 999) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("path-copy error=%v", err)
	}
	if p.Env != root || p.Control.Used != searchSteps+2 {
		t.Fatal("interrupted path was published or the lookup did not complete")
	}
	p.Control = nil
	p.Bind(0, 999)
	if got := p.Env.Get(0, p.EnvDepth()); got != 999 {
		t.Fatal("retry lost changed binding")
	}
	if got := root.Get(0, p.EnvDepth()); got != 0 {
		t.Fatal("path copy changed saved snapshot")
	}
	for _, run := range []func(*core.Parser){func(p *core.Parser) { p.EnvValues([]int{0}) }, func(p *core.Parser) { p.Bind(0, 999) }} {
		p := memoTestParser(320)
		root := p.Env
		p.Control = core.NewBudgetControl(newPollingContext(1), limits.Limits{})
		p.Control.Charge(core.BudgetCheckInterval - 2) // the next units reach the poll
		if err := catchFatal(t, func() { run(p) }); !errors.Is(err, context.Canceled) {
			t.Fatalf("environment cancellation=%v", err)
		}
		if p.Env != root {
			t.Fatal("cancellation changed current environment")
		}
	}
}

func TestBudgetMemoChainAndStringLimits(t *testing.T) {
	t.Parallel()
	table := &core.MemoTable{Slots: make([]*core.MemoEntry, 1)}
	for i := 0; i < 320; i++ {
		table.Slots[0] = &core.MemoEntry{Rule: int32(i), Next: table.Slots[0]}
	}
	oldHead := table.Slots[0]
	replacement := &core.MemoEntry{Node: value.Ptr(value.Data{Text: "replacement"})}
	p := &core.Parser{Memo: table}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 80})
	if err := catchFatal(t, func() { p.MemoGet(core.MemoKey{Rule: 0}) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("chain get=%v", err)
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 80})
	if err := catchFatal(t, func() { p.MemoPut(core.MemoKey{Rule: 0}, replacement) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("chain put=%v", err)
	}
	if table.Slots[0] != oldHead || replacement.Rule != 0 || replacement.Env != nil || replacement.Next != nil {
		t.Fatal("interrupted put published an entry")
	}
	p.Control = core.NewBudgetControl(newPollingContext(1), limits.Limits{})
	if err := catchFatal(t, func() { p.MemoGet(core.MemoKey{Rule: 0}) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("chain cancellation=%v", err)
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 30})
	p.MemoPut(core.MemoKey{Rule: 0}, replacement)
	if got, ok := p.MemoGet(core.MemoKey{Rule: 0}); !ok || got != replacement {
		t.Fatal("replacement failed")
	}
	// Retiring bookkeeping must not clear the old public node or its descendants.
	retained := value.Ptr(value.Data{Text: "retained", Children: []*value.Node{value.Ptr(value.Data{Text: "child"})}, Fields: value.Fields{{Name: "value", Value: "unchanged"}}})
	table.Slots[0] = &core.MemoEntry{Rule: 1, Node: retained, Env: []any{strings.Repeat("x", 1024)}}
	before, _ := json.Marshal(retained)
	key := core.MemoKey{Rule: 1, Env: []any{strings.Repeat("x", 1024)}}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 80})
	if err := catchFatal(t, func() { p.MemoGet(key) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("string key comparison=%v", err)
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 30})
	p.MemoPut(key, &core.MemoEntry{})
	after, _ := json.Marshal(retained)
	if string(before) != string(after) {
		t.Fatal("entry retirement changed public graph")
	}
	// Unequal string lengths and key lengths remain constant-time rejections.
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 2})
	if got, ok := p.MemoGet(core.MemoKey{Rule: 1, Env: nil}); ok || got != nil {
		t.Fatal("key length mismatch matched")
	}
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 3})
	if got, ok := p.MemoGet(core.MemoKey{Rule: 1, Env: []any{"short"}}); ok || got != nil {
		t.Fatal("string length mismatch matched")
	}
}

func budgetMemoPrefix(table *core.MemoTable, retained *value.Node) {
	for i := 0; i < 320; i++ {
		e := table.Alloc()
		*e = core.MemoEntry{Rule: int32(10000 + i), Node: retained, Next: table.Slots[0]}
		table.Slots[0] = e
	}
}

func requireMemoCleared(t *testing.T, chunks [][]core.MemoEntry) {
	t.Helper()
	for _, chunk := range chunks {
		for i := range chunk {
			if !reflect.DeepEqual(chunk[i], core.MemoEntry{}) {
				t.Fatalf("used memo cell %d retained state", i)
			}
		}
	}
}

func TestBudgetMemoGrowthFailureCleanup(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = expr $$

rule expr = expr "a" / "a"`)
	p := core.AcquirePooledParser(&prog.sharedProgram)
	retained := value.Ptr(value.Data{Text: "retained", Children: []*value.Node{value.Ptr(value.Data{Text: "child"})}})
	before, _ := json.Marshal(retained)
	key := core.MemoKey{Rule: prog.ByName["expr"].ID}
	g := p.GrowBegin(key)
	oldSeed := g.Best
	budgetMemoPrefix(p.Memo, retained)
	oldHead := p.Memo.Slots[0]
	p.Pos, p.Hw = 1, 1
	p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 64})
	if err := catchFatal(t, func() { p.GrowStep(&g, retained, true) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("LR replacement interruption=%v", err)
	}
	if p.Memo.Slots[0] != oldHead {
		t.Fatal("interrupted growth changed memo chain")
	}
	if stored, ok := p.Memo.Get(key); !ok || stored != oldSeed || !oldSeed.Growing {
		t.Fatal("interruption retired the old seed")
	}
	if g.Best == oldSeed || g.Best.Node != retained {
		t.Fatal("test did not reach replacement publication")
	}
	chunks := append([][]core.MemoEntry(nil), p.Memo.Chunks...)
	p.ReleaseScratch()
	if p.Control != nil || p.Scratch != nil {
		t.Fatal("cleanup retained control/scratch")
	}
	requireMemoCleared(t, chunks)
	after, _ := json.Marshal(retained)
	if string(after) != string(before) {
		t.Fatal("cleanup mutated public graph")
	}
}

func TestBudgetMemoBatchGrowthInterruption(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = expr #recover(skip=.*) $$

rule expr = expr atom / atom

rule atom = "a"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				for _, cancel := range []bool{false, true} {
					retained := value.Ptr(value.Data{Text: "retained", Children: []*value.Node{value.Ptr(value.Data{Text: "child"})}})
					before, _ := json.Marshal(retained)
					var observed *core.Parser
					var chunks [][]core.MemoEntry
					armed := false
					exprExits := 0
					opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, Limits: limits.Limits{Budget: 1 << 40}, Trace: func(e TraceEvent) {
						if e.Kind == TraceExit && e.Rule == "expr" {
							exprExits++
						}
						if !armed && e.Kind == TraceExit && e.Rule == "atom" {
							armed = true
							observed = core.TraceParser(e)
							budgetMemoPrefix(core.TraceParser(e).Memo, retained)
							chunks = append([][]core.MemoEntry(nil), core.TraceParser(e).Memo.Chunks...)
							if cancel {
								core.TraceParser(e).Control.Ctx = newPollingContext(2)
							} else {
								core.TraceParser(e).Control.Limits.Budget = core.TraceParser(e).Control.Used + 64
							}
						}
					}}
					n, err := prog.ParseWith("main", "a", opts)
					want := limits.ErrLimit
					if cancel {
						want = context.Canceled
					}
					if !armed || n != nil || !errors.Is(err, want) {
						t.Fatalf("%v/%v recognize=%v cancel=%v: armed=%v node=%v error=%v", backend, unit, recognize, cancel, armed, n, err)
					}
					if exprExits != 1 { // The initial recursive seed call exits before atom enters.
						t.Fatalf("unexpected completed expr calls: %d", exprExits)
					}
					if observed.Control != nil || observed.Scratch != nil {
						t.Fatal("batch interruption retained invocation state")
					}
					requireMemoCleared(t, chunks)
					after, _ := json.Marshal(retained)
					if string(after) != string(before) {
						t.Fatal("batch cleanup changed retained graph")
					}
					retry, retryErr := prog.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
					if retryErr != nil || (retry == nil) != recognize {
						t.Fatalf("retry=%v/%v", retry, retryErr)
					}
				}
			}
		}
	}
}
