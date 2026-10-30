// Package ops declares the operations of the PEGO parser VM in Exec IR (design 065): their
// signatures, behavior contracts, preconditions, effects and cost bounds. The declarations are
// shared by every target; each target implements them, and the conformance suite
// (internal/ir/exec/conform) checks an implementation against a reference model
// (internal/ir/exec/ref).
//
// The module holds the operations of the pilot set: the memo table, deferred memoization, the
// budget and the nesting depth. Cost quantities are named by the constants Q…, which the reference
// models compute.
package ops

import "github.com/ornew/pego/internal/ir/exec"

// Features of the parser VM.
const (
	Budget   = "budget"
	Document = "document"
	Observe  = "observe"
	Missing  = "missing"
	Stream   = "stream"
	Typed    = "typed"
)

// State components.
const (
	Memo  = "memo"
	Seen  = "seen"
	Bud   = "budget"
	Depth = "depth"
	Env   = "env"
)

// Cost quantities: abstract measures of the state before a call, computed by the reference model.
const (
	// QEntriesAt is the number of entries stored at the call's position, for any rule.
	QEntriesAt = "entries_at_pos"
	// QSlotGrowth is the number of positions the memo covers before the call when storing at the
	// call's position makes that number cross a power of two, and 0 otherwise: the copy that doubling
	// a position-indexed table stands for.
	QSlotGrowth = "slot_growth"
	// QPageIndex is the call's position divided by 1,024.
	QPageIndex = "page_index"
	// QPages is the number of 1,024-position regions of all rules that deferred memoization has
	// recorded calls in.
	QPages = "pages"
	// QN is the argument n of budget.charge.
	QN = "n"
)

// Names of handle types.
const (
	Entry  = "Entry"
	Values = "Values"
)

// Pilot returns the module of the pilot operations, without functions.
func Pilot() *exec.Module {
	m := exec.NewModule("parser", Budget, Document, Observe, Missing, Stream, Typed)
	m.AddComponent(Memo, `The memo table: results of rule calls by key. A key is a rule, a position,
a binding level and the values of the rule's variables.`)
	m.AddComponent(Seen, `The record of deferred memoization: which rules were called at which positions,
and per rule the counts that decide when it is memoized eagerly. It is created with the number of
rules whose memoization is deferred.`)
	m.AddComponent(Bud, `The work budget of the parse: the units charged so far and the limit, zero for
none.`)
	m.AddComponent(Depth, `The nesting depth of rule calls and its limit.`)
	m.AddComponent(Env, `The variable environment. Its operations are not declared yet; Values handles
stand for the values of a rule's variables, compared by the host's value equality.`)

	entry := m.AddHandle(Entry, Memo, "A memo entry: the result of a rule call with its outcome and end.")
	values := m.AddHandle(Values, Env, "The values of a rule's variables, as a memo key.")
	key := []exec.Param{exec.P("rule", exec.TInt), exec.P("pos", exec.TInt), exec.P("level", exec.TInt), exec.P("key", values)}
	scan := exec.Cost{Const: 1, Terms: []exec.CostTerm{{Coef: 1, Quantity: QEntriesAt}}}
	write := exec.Cost{Const: 2, Terms: []exec.CostTerm{{Coef: 1, Quantity: QEntriesAt}, {Coef: 1, Quantity: QSlotGrowth}}}
	with := func(ps []exec.Param, more ...exec.Param) []exec.Param {
		return append(append([]exec.Param(nil), ps...), more...)
	}

	m.AddOp(&exec.Op{Name: "memo.lookup", Component: Memo, Params: key, Results: []exec.Type{entry, exec.TBool},
		Reads: []string{Memo}, Cost: scan,
		Contract: `Returns the entry stored or seeded under the key, if one is there and not replaced, and
true; otherwise false and an entry that must not be used. Two keys are equal when their rules,
positions and levels are equal and the host's value equality holds for their values.`,
		Requires: `If a growing entry exists for the rule, position and level, the key is the one it was
seeded with. Compositions look a left-recursion leader up with memo.lookupGrowing first.`,
		Laws: []string{"two lookups with no write of memo between them return the same result"}})
	m.AddOp(&exec.Op{Name: "memo.lookupGrowing", Component: Memo, Params: key[:3], Results: []exec.Type{entry, exec.TBool},
		Reads: []string{Memo}, Cost: scan,
		Contract: `Returns the growing entry of the rule at the position and level, whatever key values it
was seeded with, and true; otherwise false. This is the left-recursion guard: while a rule grows at a
position, every call of it there reads the growing result.`})
	m.AddOp(&exec.Op{Name: "memo.store", Component: Memo, Params: with(key, exec.P("ok", exec.TBool), exec.P("end", exec.TInt)),
		Results: []exec.Type{entry}, Reads: []string{Memo}, Writes: []string{Memo}, Cost: write,
		Contract: `Stores a complete entry with the outcome ok and the end under the key, replacing the
entry stored under the same key, and returns it. Entries under other keys are unchanged.`,
		Requires: `No growing entry exists for the rule, position and level; end is at least pos.`})
	m.AddOp(&exec.Op{Name: "memo.seed", Component: Memo, Params: key,
		Results: []exec.Type{entry}, Reads: []string{Memo}, Writes: []string{Memo}, Cost: write,
		Contract: `Starts the growth of a left-recursion leader: stores a growing entry that fails and ends
at pos under the key, replacing the entry stored under the same key, and returns it.`,
		Requires: `No growing entry exists for the rule, position and level.`})
	m.AddOp(&exec.Op{Name: "memo.reseed", Component: Memo, Params: with(key, exec.P("end", exec.TInt)),
		Results: []exec.Type{entry}, Reads: []string{Memo}, Writes: []string{Memo}, Cost: write,
		Contract: `Records a longer seed: replaces the growing entry under the key by a growing entry that
succeeds and ends at end, and returns it. The replaced entry is no longer found.`,
		Requires: `The key has a growing entry; end is greater than pos.`})
	m.AddOp(&exec.Op{Name: "memo.finishSeed", Component: Memo, Params: []exec.Param{exec.P("e", entry)},
		Writes: []string{Memo},
		Contract: `Ends a growth: the growing entry e becomes complete, with its outcome and end. Lookups of
its key return it as a complete entry, and memo.lookupGrowing no longer finds it.`,
		Requires: `e is the growing entry most recently seeded or reseeded under its key.`})
	for _, a := range []struct {
		name string
		t    exec.Type
		doc  string
	}{
		{"memo.ok", exec.TBool, "Returns whether the call of entry e succeeded."},
		{"memo.end", exec.TInt, "Returns the end of the match of entry e; for a failure, the position of the call."},
		{"memo.growing", exec.TBool, "Returns whether entry e is the growing entry of a left-recursion leader."},
	} {
		m.AddOp(&exec.Op{Name: a.name, Component: Memo, Params: []exec.Param{exec.P("e", entry)},
			Results: []exec.Type{a.t}, Reads: []string{Memo}, Contract: a.doc})
	}

	m.AddOp(&exec.Op{Name: "seen.first", Component: Seen,
		Params:  []exec.Param{exec.P("rule", exec.TInt), exec.P("pos", exec.TInt)},
		Results: []exec.Type{exec.TBool}, Reads: []string{Seen}, Writes: []string{Seen},
		Cost: exec.Cost{Const: 2, Terms: []exec.CostTerm{{Coef: 1, Quantity: QPageIndex}, {Coef: 1, Quantity: QPages}}},
		Contract: `Records a call of the rule at pos and reports whether its memoization is deferred: true
when the rule is not eager and has not been called at pos before. Each rule counts its calls and its
repeated calls, the calls at a position where it was called before. A call that finds the rule eager
returns false and records nothing. Otherwise the call count grows by one, and when it reaches 2^31
both counts are halved (rounding down); a repeated call then grows the repeat count, and when the
repeats exceed the calls divided by 16 (rounding down), the rule becomes eager for the rest of the
parse. The policy decides how
many rule bodies run, which the statistics report, so every target follows it exactly.`,
		Requires: `0 <= rule < the number of rules the component was created with; pos >= 0.`})

	m.AddOp(&exec.Op{Name: "budget.charge", Component: Bud, Params: []exec.Param{exec.P("n", exec.TU64)},
		Reads: []string{Bud}, Writes: []string{Bud}, Features: []string{Budget}, NoReturn: true,
		Cost: exec.Cost{Terms: []exec.CostTerm{{Coef: 1, Quantity: QN}}},
		Contract: `Charges n units. When the limit is not zero and the units used plus n exceed it, the parse
ends with a limit interruption and the units used are unchanged; otherwise n is added to them.
Charges are never rolled back. A target may also observe cancellation here, at intervals of its
choice, and end the parse with a cancellation interruption.`,
		Laws: []string{"charge(a); charge(b) may merge into charge(a+b) when nothing observable lies between them and no commit separates them"}})
	m.AddOp(&exec.Op{Name: "budget.used", Component: Bud, Results: []exec.Type{exec.TU64},
		Reads: []string{Bud}, Features: []string{Budget},
		Contract: `Returns the units charged so far.`})

	m.AddOp(&exec.Op{Name: "depth.enter", Component: Depth, Reads: []string{Depth}, Writes: []string{Depth}, NoReturn: true,
		Contract: `Enters a rule call: adds one to the depth. When the depth then exceeds the limit, the parse
ends with a nesting interruption.`})
	m.AddOp(&exec.Op{Name: "depth.exit", Component: Depth, Reads: []string{Depth}, Writes: []string{Depth},
		Contract: `Leaves a rule call: subtracts one from the depth.`,
		Requires: `The depth is greater than zero.`})
	m.AddOp(&exec.Op{Name: "depth.current", Component: Depth, Results: []exec.Type{exec.TInt}, Reads: []string{Depth},
		Contract: `Returns the depth.`})
	return m
}
