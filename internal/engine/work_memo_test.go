//go:build !pego_work_controls_reference

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

func memoTestEnv(n int, prefix string) (*core.Env, map[string]bool) {
	var head *core.Env
	bound := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s%d", prefix, i)
		head = &core.Env{Name: name, Val: i, Next: head}
		bound[name] = true
	}
	return head, bound
}

func TestWorkMemoEnvValuesDeepLookup(t *testing.T) {
	const count = 320
	const prefix = "shared_environment_name_with_long_prefix_"
	root, _ := memoTestEnv(count, prefix)
	p := &core.Parser{Env: root, Input: core.Input{Control: core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 1_000_000})}}
	last := fmt.Sprintf("%s%d", prefix, 0) // The first inserted binding is deepest.
	vals := p.EnvValues([]string{last, "missing"})
	if len(vals) != 2 || vals[0] != 0 || vals[1] != nil {
		t.Fatalf("envValues = %#v, want [0 nil]", vals)
	}
	if p.Control.StepsUsed <= count {
		t.Fatalf("deep lookup used %d steps, want key and lookup work beyond %d", p.Control.StepsUsed, count)
	}

	limited := &core.Parser{Env: root, Input: core.Input{Control: core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 80})}}
	if err := catchFatal(t, func() { limited.EnvValues([]string{last}) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("deep envValues error = %v, want ErrWorkLimit", err)
	}

	// The nil-environment key remains a shared all-nil slice and resolves missing variables.
	nilParser := &core.Parser{Input: core.Input{Control: core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 8})}}
	got := nilParser.EnvValues([]string{"undefined", "also_undefined"})
	if len(got) != 2 || got[0] != nil || got[1] != nil {
		t.Fatalf("nil envValues = %#v, want two nil values", got)
	}
}

func TestWorkMemoBindPreservesSnapshotsOnInterruption(t *testing.T) {
	root, bound := memoTestEnv(48, "binding_")
	// Build a saved snapshot with a target at the tail and force the replacement path.
	p := &core.Parser{Env: root, Bound: bound, Input: core.Input{Control: core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 56})}}
	before := p.Env
	oldValues := make([]any, 0, 48)
	for e := before; e != nil; e = e.Next {
		oldValues = append(oldValues, e.Val)
	}
	target := "binding_0"
	if err := catchFatal(t, func() { p.Bind(target, "replacement") }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("tail replacement error = %v, want ErrWorkLimit", err)
	}
	if p.Env != before {
		t.Fatal("interrupted bind published a partial environment")
	}
	for i, e := 0, before; e != nil; i, e = i+1, e.Next {
		if i >= len(oldValues) || e.Val != oldValues[i] {
			t.Fatalf("saved environment changed at index %d", i)
		}
	}

	// Equal head and non-head bindings keep the persistent snapshot identity.
	stable := &core.Parser{Env: before, Bound: bound, Input: core.Input{Control: core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 1_000_000})}}
	head := before
	stable.Bind(head.Name, head.Val)
	if stable.Env != head {
		t.Fatal("equal head bind changed environment identity")
	}
	stable.Bind(target, 0) // binding_0 is the tail and has value zero.
	if stable.Env != head {
		t.Fatal("equal non-head bind changed environment identity")
	}

	// A completed tail update copies only the prefix and leaves the old snapshot intact.
	stable.Bind(target, "replacement")
	if stable.Env == before {
		t.Fatal("changed tail bind reused the old environment")
	}
	if got, ok := stable.Env.Lookup(target); !ok || got != "replacement" {
		t.Fatalf("replacement lookup = %v, %t", got, ok)
	}
	if got, ok := before.Lookup(target); !ok || got != 0 {
		t.Fatalf("saved tail lookup = %v, %t; want 0", got, ok)
	}
}

func TestWorkMemoKeyEqualityAndLongChains(t *testing.T) {
	table := &core.MemoTable{Slots: make([]*core.MemoEntry, 1)}
	p := &core.Parser{Memo: table}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
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

func workMemoVariableGrammar() string {
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

func TestWorkMemoParseBudgetPoolRetryAndRetainedResult(t *testing.T) {
	prog := mustCompile(t, workMemoVariableGrammar())
	input := "a"
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			opts := ParseOptions{Backend: backend, Unit: unit}
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
			limited.WorkLimits = limits.WorkLimits{MaxSteps: 1 << 50}
			limited.Trace = func(e TraceEvent) {
				if e.Kind == TraceEnter && e.Rule == "marker" {
					markerCalls++
				}
				if !armed && e.Kind == TraceEnter && e.Rule == "r" {
					armed = true
					core.TraceParser(e).DeferMemo = false
					core.TraceParser(e).Control.Limits.MaxSteps = core.TraceParser(e).Control.StepsUsed + 10_000
				}
			}
			if node, parseErr := prog.ParseWith("main", input, limited); node != nil || !errors.Is(parseErr, limits.ErrWorkLimit) {
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

func TestWorkMemoBindCopyAndCancellation(t *testing.T) {
	root, bound := memoTestEnv(320, "v")
	// An equal tail binding performs the same search but does not copy a prefix.
	probe := &core.Parser{Env: root, Bound: bound}
	probe.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
	probe.Bind("v0", 0)
	searchSteps := probe.Control.StepsUsed
	p := &core.Parser{Env: root, Bound: bound}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: searchSteps + 5})
	if err := catchFatal(t, func() { p.Bind("v0", 999) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("prefix-copy error=%v", err)
	}
	if p.Env != root || p.Control.StepsUsed != searchSteps+5 {
		t.Fatal("interrupted prefix was published or search did not complete")
	}
	p.Control = nil
	p.Bind("v0", 999)
	if got, _ := p.Env.Lookup("v0"); got != 999 {
		t.Fatal("retry lost changed binding")
	}
	if got, _ := root.Lookup("v0"); got != 0 {
		t.Fatal("prefix copy changed saved snapshot")
	}
	for _, run := range []func(*core.Parser){func(p *core.Parser) { p.EnvValues([]string{"absent"}) }, func(p *core.Parser) { p.Bind("v0", 999) }} {
		p := &core.Parser{Env: root, Bound: bound}
		p.Control = core.NewWorkControl(newPollingContext(1), limits.WorkLimits{})
		if err := catchFatal(t, func() { run(p) }); !errors.Is(err, context.Canceled) {
			t.Fatalf("environment walk cancellation=%v", err)
		}
		if p.Env != root {
			t.Fatal("cancellation changed current environment")
		}
	}
}

func TestWorkMemoChainAndStringLimits(t *testing.T) {
	table := &core.MemoTable{Slots: make([]*core.MemoEntry, 1)}
	for i := 0; i < 320; i++ {
		table.Slots[0] = &core.MemoEntry{Rule: int32(i), Next: table.Slots[0]}
	}
	oldHead := table.Slots[0]
	replacement := &core.MemoEntry{Node: value.Ptr(value.Data{Text: "replacement"})}
	p := &core.Parser{Memo: table}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 80})
	if err := catchFatal(t, func() { p.MemoGet(core.MemoKey{Rule: 0}) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("chain get=%v", err)
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 80})
	if err := catchFatal(t, func() { p.MemoPut(core.MemoKey{Rule: 0}, replacement) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("chain put=%v", err)
	}
	if table.Slots[0] != oldHead || replacement.Rule != 0 || replacement.Env != nil || replacement.Next != nil {
		t.Fatal("interrupted put published an entry")
	}
	p.Control = core.NewWorkControl(newPollingContext(1), limits.WorkLimits{})
	if err := catchFatal(t, func() { p.MemoGet(core.MemoKey{Rule: 0}) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("chain cancellation=%v", err)
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
	p.MemoPut(core.MemoKey{Rule: 0}, replacement)
	if got, ok := p.MemoGet(core.MemoKey{Rule: 0}); !ok || got != replacement {
		t.Fatal("replacement failed")
	}
	// Retiring bookkeeping must not clear the old public node or its descendants.
	retained := value.Ptr(value.Data{Text: "retained", Children: []*value.Node{value.Ptr(value.Data{Text: "child"})}, Fields: value.Fields{{Name: "value", Value: "unchanged"}}})
	table.Slots[0] = &core.MemoEntry{Rule: 1, Node: retained, Env: []any{strings.Repeat("x", 1024)}}
	before, _ := json.Marshal(retained)
	key := core.MemoKey{Rule: 1, Env: []any{strings.Repeat("x", 1024)}}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 80})
	if err := catchFatal(t, func() { p.MemoGet(key) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("string key comparison=%v", err)
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
	p.MemoPut(key, &core.MemoEntry{})
	after, _ := json.Marshal(retained)
	if string(before) != string(after) {
		t.Fatal("entry retirement changed public graph")
	}
	// Unequal string lengths and key lengths remain constant-time rejections.
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 2})
	if got, ok := p.MemoGet(core.MemoKey{Rule: 1, Env: nil}); ok || got != nil {
		t.Fatal("key length mismatch matched")
	}
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 3})
	if got, ok := p.MemoGet(core.MemoKey{Rule: 1, Env: []any{"short"}}); ok || got != nil {
		t.Fatal("string length mismatch matched")
	}
}

func workMemoPrefix(table *core.MemoTable, retained *value.Node) {
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

func TestWorkMemoGrowthFailureCleanup(t *testing.T) {
	prog := mustCompile(t, `rule main = expr $$

rule expr = expr "a" / "a"`)
	p := core.AcquirePooledParser(&prog.sharedProgram)
	retained := value.Ptr(value.Data{Text: "retained", Children: []*value.Node{value.Ptr(value.Data{Text: "child"})}})
	before, _ := json.Marshal(retained)
	key := core.MemoKey{Rule: prog.ByName["expr"].ID}
	g := p.GrowBegin(key)
	oldSeed := g.Best
	workMemoPrefix(p.Memo, retained)
	oldHead := p.Memo.Slots[0]
	p.Pos, p.Hw = 1, 1
	p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 64})
	if err := catchFatal(t, func() { p.GrowStep(&g, retained, true) }); !errors.Is(err, limits.ErrWorkLimit) {
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

func TestWorkMemoBatchGrowthInterruption(t *testing.T) {
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
					opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 40}, Trace: func(e TraceEvent) {
						if e.Kind == TraceExit && e.Rule == "expr" {
							exprExits++
						}
						if !armed && e.Kind == TraceExit && e.Rule == "atom" {
							armed = true
							observed = core.TraceParser(e)
							workMemoPrefix(core.TraceParser(e).Memo, retained)
							chunks = append([][]core.MemoEntry(nil), core.TraceParser(e).Memo.Chunks...)
							if cancel {
								core.TraceParser(e).Control.Ctx = newPollingContext(2)
							} else {
								core.TraceParser(e).Control.Limits.MaxSteps = core.TraceParser(e).Control.StepsUsed + 64
							}
						}
					}}
					n, err := prog.ParseWith("main", "a", opts)
					want := limits.ErrWorkLimit
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
