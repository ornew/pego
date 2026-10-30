package runtime

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/conform"
	"github.com/ornew/pego/internal/ir/exec/ref"
)

// The generated Go runtime is the first implementation of the parser VM's pilot operations under
// conformance (design 065). The binding calls the runtime's own functions; where an operation is
// inline code of a composition, it does what that code does: the seed operations allocate an entry
// and store it with memoPut as grow does, and finishSeed clears the flag as grow does.

// growingProbe is a key value no stored key has: a lookup with it finds only a growing entry,
// which memoTable.get matches whatever the values.
var growingProbe = []any{new(byte)}

type rtMachine struct {
	p     *Parser
	vals  map[int64][]any
	rules []*Rule
	// metered reports a parse with budget controls, whose charges the suite checks.
	metered bool
}

func newRTMachine(cfg ref.Config, controlled, sparse bool) conform.Impl {
	p := &Parser{MaxDepth: int(cfg.MaxDepth)}
	p.memo.stride, p.memo.sparse = cfg.Rules, sparse
	m := &rtMachine{p: p, vals: map[int64][]any{}, metered: controlled}
	for i := range cfg.Rules {
		m.rules = append(m.rules, &Rule{Seen: i})
	}
	if controlled {
		limit := cfg.Limit
		if limit == 0 {
			limit = Unlimited
		}
		p.Control = newBudgetControl(nil, Limits{Budget: limit})
	}
	if m.metered {
		return meteredRT{m}
	}
	return m
}

type meteredRT struct{ *rtMachine }

func (m meteredRT) Used() uint64 { return m.p.Control.budgetUsed() }

func (m *rtMachine) Values(id int64) any {
	if m.vals[id] == nil {
		m.vals[id] = []any{id}
	}
	return id
}

func (m *rtMachine) Call(o *exec.Op, args []any) (res []any) {
	defer func() {
		if x := recover(); x != nil {
			f, ok := x.(fatal)
			if !ok {
				panic(x)
			}
			if _, limit := f.err.(*LimitError); limit {
				panic(exec.Interrupt{Reason: "limit"})
			}
			panic(exec.Interrupt{Reason: "nesting"})
		}
	}()
	p := m.p
	i := func(k int) int { return int(args[k].(int64)) }
	key := func() (int, int, int, []any) { return i(0), i(1), i(2), m.vals[args[3].(int64)] }
	switch o.Name {
	case "memo.lookup":
		rule, pos, level, env := key()
		e := p.memoGet(rule, pos, level, env)
		return []any{e, e != nil}
	case "memo.lookupGrowing":
		e := p.memoGet(i(0), i(1), i(2), growingProbe)
		return []any{e, e != nil}
	case "memo.store":
		rule, pos, level, env := key()
		e := p.memo.alloc()
		*e = memoEntry{ok: args[4].(bool), end: i(5)}
		p.memoPut(rule, pos, level, env, e)
		return []any{e}
	case "memo.seed":
		rule, pos, level, env := key()
		e := p.memo.alloc()
		*e = memoEntry{end: pos, growing: true}
		p.memoPut(rule, pos, level, env, e)
		return []any{e}
	case "memo.reseed":
		rule, pos, level, env := key()
		e := p.memo.alloc()
		*e = memoEntry{ok: true, end: i(4), growing: true}
		p.memoPut(rule, pos, level, env, e)
		return []any{e}
	case "memo.finishSeed":
		args[0].(*memoEntry).growing = false
		return nil
	case "memo.ok":
		return []any{args[0].(*memoEntry).ok}
	case "memo.end":
		return []any{int64(args[0].(*memoEntry).end)}
	case "memo.growing":
		return []any{args[0].(*memoEntry).growing}
	case "seen.first":
		p.Pos = i(1)
		return []any{p.FirstCall(m.rules[i(0)])}
	case "budget.charge":
		p.Control.Charge(args[0].(uint64))
		return nil
	case "budget.used":
		return []any{p.Control.budgetUsed()}
	}
	panic("runtime binding: no operation " + o.Name)
}

var (
	rtMemoOps   = []string{"memo.lookup", "memo.lookupGrowing", "memo.store", "memo.seed", "memo.reseed", "memo.finishSeed", "memo.ok", "memo.end", "memo.growing", "seen.first"}
	rtBudgetOps = []string{"budget.charge", "budget.used"}
)

func rtSuites() []conform.Suite {
	var suites []conform.Suite
	for _, controlled := range []bool{false, true} {
		for _, sparse := range []bool{false, true} {
			ops := rtMemoOps
			if controlled {
				ops = append(append([]string(nil), rtMemoOps...), rtBudgetOps...)
			}
			suites = append(suites, conform.Suite{
				Name: fmt.Sprintf("generated Go runtime (controlled=%v, sparse seen=%v)", controlled, sparse),
				Ops:  ops,
				New:  func(c ref.Config) conform.Impl { return newRTMachine(c, controlled, sparse) },
			})
		}
	}
	return suites
}

// conformanceFailures runs the handwritten cases and seeds randomized cases of n calls on s, and
// returns the failures and the number of cases that ran.
func conformanceFailures(s conform.Suite, seeds, n int) (fails []error, ran int) {
	run := func(c conform.Case) {
		skipped, err := conform.Run(s, c)
		if !skipped {
			ran++
		}
		if err != nil {
			fails = append(fails, err)
		}
	}
	for _, c := range conform.Cases() {
		run(c)
	}
	for seed := range seeds {
		// One case in four has a limit that the case may reach; the others run to their end.
		var limit uint64
		if seed%4 == 0 {
			limit = uint64(200 + seed)
		}
		rng := rand.New(rand.NewPCG(uint64(seed), 65))
		run(conform.Random(fmt.Sprint("random_", seed), rng, s.Ops, n, ref.Config{Rules: 3 + seed%2*130, Limit: limit, MaxDepth: 4}))
	}
	return fails, ran
}

func TestExecConformance(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, s := range rtSuites() {
		t.Run(s.Name, func(t *testing.T) {
			fails, ran := conformanceFailures(s, seeds, 150)
			if ran < seeds {
				t.Fatalf("only %d cases ran", ran)
			}
			for _, err := range fails {
				t.Error(err)
			}
		})
	}
}

// TestExecConformanceDetectsBrokenRuntime is the negative control: deliberately broken variants of
// the runtime's operations must fail conformance.
func TestExecConformanceDetectsBrokenRuntime(t *testing.T) {
	type broken struct {
		op         string
		call       func(m *rtMachine, args []any) []any
		controlled bool
	}
	variants := map[string]broken{
		// memoTable.put that links a new entry behind the chain instead of replacing the entry under
		// the same key: lookups keep finding the old entry.
		"store appends without replacing": {op: "memo.store", call: func(m *rtMachine, args []any) []any {
			t := &m.p.memo
			pos := int(args[1].(int64))
			e := t.alloc()
			*e = memoEntry{ok: args[4].(bool), end: int(args[5].(int64)), rule: int32(args[0].(int64)), min: int32(args[2].(int64)), env: m.vals[args[3].(int64)]}
			for pos >= len(t.slots) {
				t.slots = append(t.slots, nil)
			}
			link := &t.slots[pos]
			for *link != nil {
				link = &(*link).next
			}
			*link = e
			return []any{e}
		}},
		// A first-call record that drops the rule from the bit index (every rule shares one bit).
		"seen ignores the rule": {op: "seen.first", call: func(m *rtMachine, args []any) []any {
			m.p.Pos = int(args[1].(int64))
			return []any{m.p.FirstCall(m.rules[0])}
		}},
		// A lookup that forgets the level, as a key without Min would.
		"lookup ignores the level": {op: "memo.lookup", call: func(m *rtMachine, args []any) []any {
			e := m.p.memoGet(int(args[0].(int64)), int(args[1].(int64)), 0, m.vals[args[3].(int64)])
			return []any{e, e != nil}
		}},
		// A lookup that charges each entry it passes twice: within the results, beyond the bound.
		"lookup charges twice": {op: "memo.lookup", controlled: true, call: func(m *rtMachine, args []any) []any {
			if pos := int(args[1].(int64)); pos < len(m.p.memo.slots) {
				for e := m.p.memo.slots[pos]; e != nil; e = e.next {
					m.p.Spend()
					m.p.Spend()
				}
			}
			e := m.p.memoGet(int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64)), m.vals[args[3].(int64)])
			return []any{e, e != nil}
		}},
	}
	for name, b := range variants {
		t.Run(name, func(t *testing.T) {
			s := conform.Suite{Name: "broken: " + name, Ops: rtMemoOps, New: func(c ref.Config) conform.Impl {
				if b.controlled {
					return meteredBrokenRT{brokenRT{newRTMachine(c, true, false).(meteredRT).rtMachine, b.op, b.call}}
				}
				return brokenRT{newRTMachine(c, false, false).(*rtMachine), b.op, b.call}
			}}
			fails, _ := conformanceFailures(s, 30, 150)
			if len(fails) == 0 {
				t.Fatal("conformance did not detect the broken operation")
			}
			t.Logf("detected:\n%v", fails[0])
		})
	}
}

type brokenRT struct {
	*rtMachine
	op   string
	call func(m *rtMachine, args []any) []any
}

func (b brokenRT) Call(o *exec.Op, args []any) []any {
	if o.Name == b.op {
		return b.call(b.rtMachine, args)
	}
	return b.rtMachine.Call(o, args)
}

type meteredBrokenRT struct{ brokenRT }

func (m meteredBrokenRT) Used() uint64 { return m.p.Control.budgetUsed() }
