// Package ref holds the reference models of the parser VM's state components (design 065): naive
// Go implementations of the contracts in internal/ir/exec/ops, written for clarity, never printed
// or optimized. The conformance suite runs operation sequences on a model and on an implementation
// and compares them. A model also checks the precondition of each call, so that randomized
// sequences stay within what compositions guarantee, and computes the abstract quantities that
// cost bounds are written in.
package ref

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/ops"
)

// Config sets up the components of a model.
type Config struct {
	// Rules is the number of rules whose memoization is deferred (component seen).
	Rules int
	// Limit is the budget limit, 0 for none.
	Limit uint64
	// MaxDepth is the nesting limit.
	MaxDepth int64
}

// Model is the reference model of the pilot components. It implements exec.Machine.
type Model struct {
	cfg Config

	// memo
	entries map[memoKey]*Entry
	growing map[slotKey]*Entry
	at      map[int64]int // live entries per position
	covered int64         // positions the memo covers: one more than the largest position stored at

	// seen
	seen  map[[2]int64]bool
	pages map[[2]int64]bool
	rules []seenCounts

	// budget
	used uint64

	// depth
	depth int64
}

type slotKey struct{ rule, pos, level int64 }

type memoKey struct {
	slotKey
	vals int64
}

// Entry is a memo entry of the model.
type Entry struct {
	key     memoKey
	ok      bool
	end     int64
	growing bool
}

type seenCounts struct {
	calls, repeats int32
	eager          bool
}

// New returns a model in the state at the start of a parse.
func New(cfg Config) *Model {
	return &Model{cfg: cfg, entries: map[memoKey]*Entry{}, growing: map[slotKey]*Entry{}, at: map[int64]int{},
		seen: map[[2]int64]bool{}, pages: map[[2]int64]bool{}, rules: make([]seenCounts, cfg.Rules)}
}

// Values returns the model's Values handle for the conformance identifier id: the identifier itself.
// Two Values are equal when their identifiers are; each stands for a single value.
func (m *Model) Values(id int64) any { return id }

func key(args []any) memoKey {
	return memoKey{slotKey{args[0].(int64), args[1].(int64), args[2].(int64)}, args[3].(int64)}
}

func slot(args []any) slotKey { return slotKey{args[0].(int64), args[1].(int64), args[2].(int64)} }

// Allows reports whether a call of o with args meets its precondition in the current state.
func (m *Model) Allows(o *exec.Op, args []any) bool {
	switch o.Name {
	case "memo.store":
		return m.growing[slot(args)] == nil && args[5].(int64) >= args[1].(int64) && args[1].(int64) >= 0
	case "memo.seed":
		return m.growing[slot(args)] == nil && args[1].(int64) >= 0
	case "memo.reseed":
		k := key(args)
		g := m.growing[k.slotKey]
		return g != nil && g.key == k && args[4].(int64) > k.pos
	case "memo.lookup":
		k := key(args)
		g := m.growing[k.slotKey]
		return g == nil || g.key == k
	case "memo.finishSeed":
		e := args[0].(*Entry)
		return e.growing && m.growing[e.key.slotKey] == e
	case "seen.first":
		r, pos := args[0].(int64), args[1].(int64)
		return r >= 0 && r < int64(m.cfg.Rules) && pos >= 0
	case "depth.exit":
		return m.depth > 0
	}
	return true
}

// Quantity returns the cost quantity name for a call of o with args, in the state before the call.
func (m *Model) Quantity(name string, o *exec.Op, args []any) int64 {
	switch name {
	case ops.QKeyScan:
		// Every Values of a conformance case is one value.
		return 2 * int64(m.at[args[1].(int64)])
	case ops.QSlotGrowth:
		pos := args[1].(int64)
		if m.covered > 0 && pos >= m.covered && bits.Len64(uint64(pos+1)) > bits.Len64(uint64(m.covered)) {
			return m.covered
		}
		return 0
	case ops.QPageIndex:
		return args[1].(int64) >> 10
	case ops.QPages:
		return int64(len(m.pages))
	case ops.QSeenIndex:
		return (args[1].(int64)*int64(m.cfg.Rules) + args[0].(int64)) / 64
	case ops.QN:
		return int64(min(args[0].(uint64), math.MaxInt64))
	}
	panic("ref: unknown quantity " + name)
}

// Call implements exec.Machine.
func (m *Model) Call(o *exec.Op, args []any) []any {
	switch o.Name {
	case "memo.lookup":
		e := m.entries[key(args)]
		return []any{e, e != nil}
	case "memo.lookupGrowing":
		e := m.growing[slot(args)]
		return []any{e, e != nil}
	case "memo.store":
		return []any{m.put(&Entry{key: key(args), ok: args[4].(bool), end: args[5].(int64)})}
	case "memo.seed":
		k := key(args)
		e := m.put(&Entry{key: k, end: k.pos, growing: true})
		m.growing[k.slotKey] = e
		return []any{e}
	case "memo.reseed":
		k := key(args)
		e := m.put(&Entry{key: k, ok: true, end: args[4].(int64), growing: true})
		m.growing[k.slotKey] = e
		return []any{e}
	case "memo.finishSeed":
		e := args[0].(*Entry)
		e.growing = false
		delete(m.growing, e.key.slotKey)
		return nil
	case "memo.ok":
		return []any{args[0].(*Entry).ok}
	case "memo.end":
		return []any{args[0].(*Entry).end}
	case "memo.growing":
		return []any{args[0].(*Entry).growing}
	case "seen.first":
		return []any{m.first(args[0].(int64), args[1].(int64))}
	case "budget.charge":
		n := args[0].(uint64)
		if m.Exceeds(n) {
			panic(exec.Interrupt{Reason: "limit"})
		}
		m.charge(n)
		return nil
	case "budget.used":
		return []any{m.used}
	case "depth.enter":
		m.depth++
		if m.depth > m.cfg.MaxDepth {
			panic(exec.Interrupt{Reason: "nesting"})
		}
		return nil
	case "depth.exit":
		m.depth--
		return nil
	case "depth.current":
		return []any{m.depth}
	}
	panic(fmt.Sprintf("ref: no model of %s", o.Name))
}

// Exceeds reports whether charging n units would exceed the budget limit.
func (m *Model) Exceeds(n uint64) bool {
	l := m.cfg.Limit
	return l != 0 && (m.used > l || n > l-m.used)
}

// Absorb adds the n units that an implementation charged inside another operation to the budget
// and reports whether they exceed the limit (then the units used are unchanged). The conformance
// suite calls it, because the charges inside operations are the implementation's: within the
// operation's cost bound, they join the budget that budget.charge and budget.used see.
func (m *Model) Absorb(n uint64) (exceeded bool) {
	if m.Exceeds(n) {
		return true
	}
	m.charge(n)
	return false
}

func (m *Model) charge(n uint64) {
	if math.MaxUint64-m.used < n {
		m.used = math.MaxUint64
	} else {
		m.used += n
	}
}

// put stores e under its key, replacing the entry there.
func (m *Model) put(e *Entry) *Entry {
	if m.entries[e.key] == nil {
		m.at[e.key.pos]++
	}
	m.entries[e.key] = e
	m.covered = max(m.covered, e.key.pos+1)
	return e
}

func (m *Model) first(rule, pos int64) bool {
	c := &m.rules[rule]
	if c.eager {
		return false
	}
	calls := uint32(c.calls) + 1
	if calls == 1<<31 {
		calls >>= 1
		c.repeats >>= 1
	}
	c.calls = int32(calls)
	k := [2]int64{rule, pos}
	m.pages[[2]int64{rule, pos >> 10}] = true
	if !m.seen[k] {
		m.seen[k] = true
		return true
	}
	c.repeats++
	if c.repeats > c.calls>>4 {
		c.eager = true
	}
	return false
}
