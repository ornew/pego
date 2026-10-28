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

	"github.com/ornew/pego/internal/limits"
)

func memoTestEnv(n int, prefix string) (*env, map[string]bool) {
	var head *env
	bound := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s%d", prefix, i)
		head = &env{name: name, val: i, next: head}
		bound[name] = true
	}
	return head, bound
}

func TestWorkMemoEnvValuesDeepLookup(t *testing.T) {
	const count = 320
	const prefix = "shared_environment_name_with_long_prefix_"
	root, _ := memoTestEnv(count, prefix)
	p := &parser{env: root, input: input{control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 1_000_000})}}
	last := fmt.Sprintf("%s%d", prefix, 0) // The first inserted binding is deepest.
	vals := p.envValues([]string{last, "missing"})
	if len(vals) != 2 || vals[0] != 0 || vals[1] != nil {
		t.Fatalf("envValues = %#v, want [0 nil]", vals)
	}
	if p.control.stepsUsed <= count {
		t.Fatalf("deep lookup used %d steps, want key and lookup work beyond %d", p.control.stepsUsed, count)
	}

	limited := &parser{env: root, input: input{control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 80})}}
	if err := catchFatal(t, func() { limited.envValues([]string{last}) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("deep envValues error = %v, want ErrWorkLimit", err)
	}

	// The nil-environment key remains a shared all-nil slice and resolves missing variables.
	nilParser := &parser{input: input{control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 8})}}
	got := nilParser.envValues([]string{"undefined", "also_undefined"})
	if len(got) != 2 || got[0] != nil || got[1] != nil {
		t.Fatalf("nil envValues = %#v, want two nil values", got)
	}
}

func TestWorkMemoBindPreservesSnapshotsOnInterruption(t *testing.T) {
	root, bound := memoTestEnv(48, "binding_")
	// Build a saved snapshot with a target at the tail and force the replacement path.
	p := &parser{env: root, bound: bound, input: input{control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 56})}}
	before := p.env
	oldValues := make([]any, 0, 48)
	for e := before; e != nil; e = e.next {
		oldValues = append(oldValues, e.val)
	}
	target := "binding_0"
	if err := catchFatal(t, func() { p.bind(target, "replacement") }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("tail replacement error = %v, want ErrWorkLimit", err)
	}
	if p.env != before {
		t.Fatal("interrupted bind published a partial environment")
	}
	for i, e := 0, before; e != nil; i, e = i+1, e.next {
		if i >= len(oldValues) || e.val != oldValues[i] {
			t.Fatalf("saved environment changed at index %d", i)
		}
	}

	// Equal head and non-head bindings keep the persistent snapshot identity.
	stable := &parser{env: before, bound: bound, input: input{control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 1_000_000})}}
	head := before
	stable.bind(head.name, head.val)
	if stable.env != head {
		t.Fatal("equal head bind changed environment identity")
	}
	stable.bind(target, 0) // binding_0 is the tail and has value zero.
	if stable.env != head {
		t.Fatal("equal non-head bind changed environment identity")
	}

	// A completed tail update copies only the prefix and leaves the old snapshot intact.
	stable.bind(target, "replacement")
	if stable.env == before {
		t.Fatal("changed tail bind reused the old environment")
	}
	if got, ok := stable.env.lookup(target); !ok || got != "replacement" {
		t.Fatalf("replacement lookup = %v, %t", got, ok)
	}
	if got, ok := before.lookup(target); !ok || got != 0 {
		t.Fatalf("saved tail lookup = %v, %t; want 0", got, ok)
	}
}

func TestWorkMemoKeyEqualityAndLongChains(t *testing.T) {
	table := &memoTable{slots: make([]*memoEntry, 1)}
	p := &parser{memo: table}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
	long := strings.Repeat("common-prefix-", 96)
	var head *memoEntry
	for i := 0; i < 320; i++ {
		env := []any{i, long + fmt.Sprint(i)}
		e := &memoEntry{rule: int32(i), min: int32(i % 3), pos: 0, env: env, next: head}
		head = e
	}
	table.slots[0] = head

	key := memoKey{rule: 0, pos: 0, min: 0, env: []any{0, long + "0"}}
	if got, ok := p.memoGet(key); !ok || got.rule != 0 {
		t.Fatalf("late memo hit = %v, %t", got, ok)
	}
	key.env = []any{0, long + "different"}
	if got, ok := p.memoGet(key); ok || got != nil {
		t.Fatalf("memo miss = %v, %t; want nil, false", got, ok)
	}

	// slices.Equal semantics distinguish a typed nil from a nil interface and equate nil/empty slices.
	var typedNil *Node
	table.slots[0] = &memoEntry{rule: 9, pos: 0, env: []any{typedNil}}
	if got, ok := p.memoGet(memoKey{rule: 9, pos: 0, env: []any{nil}}); ok || got != nil {
		t.Fatalf("typed-nil key matched nil interface: %v, %t", got, ok)
	}
	table.slots[0] = &memoEntry{rule: 9, pos: 0}
	if got, ok := p.memoGet(memoKey{rule: 9, pos: 0, env: []any{}}); !ok || got == nil {
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
	prog := compile(t, workMemoVariableGrammar())
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
					e.p.deferMemo = false
					e.p.control.limits.MaxSteps = e.p.control.stepsUsed + 10_000
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
	probe := &parser{env: root, bound: bound}
	probe.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
	probe.bind("v0", 0)
	searchSteps := probe.control.stepsUsed
	p := &parser{env: root, bound: bound}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: searchSteps + 5})
	if err := catchFatal(t, func() { p.bind("v0", 999) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("prefix-copy error=%v", err)
	}
	if p.env != root || p.control.stepsUsed != searchSteps+5 {
		t.Fatal("interrupted prefix was published or search did not complete")
	}
	p.control = nil
	p.bind("v0", 999)
	if got, _ := p.env.lookup("v0"); got != 999 {
		t.Fatal("retry lost changed binding")
	}
	if got, _ := root.lookup("v0"); got != 0 {
		t.Fatal("prefix copy changed saved snapshot")
	}
	for _, run := range []func(*parser){func(p *parser) { p.envValues([]string{"absent"}) }, func(p *parser) { p.bind("v0", 999) }} {
		p := &parser{env: root, bound: bound}
		p.control = newWorkControl(newPollingContext(1), limits.WorkLimits{})
		if err := catchFatal(t, func() { run(p) }); !errors.Is(err, context.Canceled) {
			t.Fatalf("environment walk cancellation=%v", err)
		}
		if p.env != root {
			t.Fatal("cancellation changed current environment")
		}
	}
}

func TestWorkMemoChainAndStringLimits(t *testing.T) {
	table := &memoTable{slots: make([]*memoEntry, 1)}
	for i := 0; i < 320; i++ {
		table.slots[0] = &memoEntry{rule: int32(i), next: table.slots[0]}
	}
	oldHead := table.slots[0]
	replacement := &memoEntry{node: &Node{text: "replacement"}}
	p := &parser{memo: table}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 80})
	if err := catchFatal(t, func() { p.memoGet(memoKey{rule: 0}) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("chain get=%v", err)
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 80})
	if err := catchFatal(t, func() { p.memoPut(memoKey{rule: 0}, replacement) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("chain put=%v", err)
	}
	if table.slots[0] != oldHead || replacement.rule != 0 || replacement.env != nil || replacement.next != nil {
		t.Fatal("interrupted put published an entry")
	}
	p.control = newWorkControl(newPollingContext(1), limits.WorkLimits{})
	if err := catchFatal(t, func() { p.memoGet(memoKey{rule: 0}) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("chain cancellation=%v", err)
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
	p.memoPut(memoKey{rule: 0}, replacement)
	if got, ok := p.memoGet(memoKey{rule: 0}); !ok || got != replacement {
		t.Fatal("replacement failed")
	}
	// Retiring bookkeeping must not clear the old public node or its descendants.
	retained := &Node{text: "retained", children: []*Node{{text: "child"}}, fields: Fields{{Name: "value", Value: "unchanged"}}}
	table.slots[0] = &memoEntry{rule: 1, node: retained, env: []any{strings.Repeat("x", 1024)}}
	before, _ := json.Marshal(retained)
	key := memoKey{rule: 1, env: []any{strings.Repeat("x", 1024)}}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 80})
	if err := catchFatal(t, func() { p.memoGet(key) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("string key comparison=%v", err)
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
	p.memoPut(key, &memoEntry{})
	after, _ := json.Marshal(retained)
	if string(before) != string(after) {
		t.Fatal("entry retirement changed public graph")
	}
	// Unequal string lengths and key lengths remain constant-time rejections.
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 2})
	if got, ok := p.memoGet(memoKey{rule: 1, env: nil}); ok || got != nil {
		t.Fatal("key length mismatch matched")
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 3})
	if got, ok := p.memoGet(memoKey{rule: 1, env: []any{"short"}}); ok || got != nil {
		t.Fatal("string length mismatch matched")
	}
}

func workMemoPrefix(table *memoTable, retained *Node) {
	for i := 0; i < 320; i++ {
		e := table.alloc()
		*e = memoEntry{rule: int32(10000 + i), node: retained, next: table.slots[0]}
		table.slots[0] = e
	}
}

func requireMemoCleared(t *testing.T, chunks [][]memoEntry) {
	t.Helper()
	for _, chunk := range chunks {
		for i := range chunk {
			if !reflect.DeepEqual(chunk[i], memoEntry{}) {
				t.Fatalf("used memo cell %d retained state", i)
			}
		}
	}
}

func TestWorkMemoGrowthFailureCleanup(t *testing.T) {
	prog := compile(t, `rule main = expr $$

rule expr = expr "a" / "a"`)
	p := acquirePooledParser(prog)
	retained := &Node{text: "retained", children: []*Node{{text: "child"}}}
	before, _ := json.Marshal(retained)
	key := memoKey{rule: prog.byName["expr"].id}
	g := p.growBegin(key)
	oldSeed := g.best
	workMemoPrefix(p.memo, retained)
	oldHead := p.memo.slots[0]
	p.pos, p.hw = 1, 1
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 64})
	if err := catchFatal(t, func() { p.growStep(&g, retained, true) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("LR replacement interruption=%v", err)
	}
	if p.memo.slots[0] != oldHead {
		t.Fatal("interrupted growth changed memo chain")
	}
	if stored, ok := p.memo.get(key); !ok || stored != oldSeed || !oldSeed.growing {
		t.Fatal("interruption retired the old seed")
	}
	if g.best == oldSeed || g.best.node != retained {
		t.Fatal("test did not reach replacement publication")
	}
	chunks := append([][]memoEntry(nil), p.memo.chunks...)
	p.releaseScratch()
	if p.control != nil || p.scratch != nil {
		t.Fatal("cleanup retained control/scratch")
	}
	requireMemoCleared(t, chunks)
	after, _ := json.Marshal(retained)
	if string(after) != string(before) {
		t.Fatal("cleanup mutated public graph")
	}
}

func TestWorkMemoBatchGrowthInterruption(t *testing.T) {
	prog := compile(t, `rule main = expr #recover(skip=.*) $$

rule expr = expr atom / atom

rule atom = "a"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				for _, cancel := range []bool{false, true} {
					retained := &Node{text: "retained", children: []*Node{{text: "child"}}}
					before, _ := json.Marshal(retained)
					var observed *parser
					var chunks [][]memoEntry
					armed := false
					exprExits := 0
					opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 40}, Trace: func(e TraceEvent) {
						if e.Kind == TraceExit && e.Rule == "expr" {
							exprExits++
						}
						if !armed && e.Kind == TraceExit && e.Rule == "atom" {
							armed = true
							observed = e.p
							workMemoPrefix(e.p.memo, retained)
							chunks = append([][]memoEntry(nil), e.p.memo.chunks...)
							if cancel {
								e.p.control.ctx = newPollingContext(2)
							} else {
								e.p.control.limits.MaxSteps = e.p.control.stepsUsed + 64
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
					if observed.control != nil || observed.scratch != nil {
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
