package engine

import (
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// sharedProgram is the part of a program that the parses of every backend read: the static
// program, the environment the declared variables start from and the buffers of finished parses.
// Program embeds it, and a parser points to it.
type sharedProgram struct {
	*staticProgram
	// scratch holds the buffers of finished parses for later ones (*scratch).
	scratch sync.Pool
	// initEnv is the environment of the declared variables that every parse starts from
	// (parser.seedVars). Environments are persistent, so every parse shares it.
	initEnv *env
}

// parser is the execution state of a single parse.
type parser struct {
	prog *sharedProgram
	input
	pos int
	// hw and lw are the input range [lw, hw) examined by the current rule call. They are used by
	// incremental parsing.
	hw int
	lw int
	// stats counts evaluations and memo uses.
	stats Stats
	// aborted reports an interrupted execution, whose partial caches cannot be
	// reused by a Document. Normal syntax errors are completed executions.
	aborted        bool
	emitCall       bool // a caller-owned stream callback is currently executing
	diagnostic     func(*SyntaxError) error
	diagnosticTail streamDiagnosticTail

	env     *env            // predicate variables
	bound   map[string]bool // names bound anywhere in this parse (a lookup hint)
	initEnv *env            // the initial environment of the variables (parser.seedVars), or nil
	frame   *frame          // current capture frame
	trail   []undo          // capture writes to undo on backtracking
	cut     bool            // whether a cut has been passed in the current alternative
	memo    *memoTable

	// While silent is positive (inside a lookahead), expectations are not recorded.
	silent   int
	farthest int
	// exp is the stack of expectation records. The current record is exp[expBase:], the
	// expectations at farthest. An expectation is an index into descs; an @error message is an index
	// with msgBit set (expID).
	exp     []expID
	expBase int
	// expBits cheaply excludes IDs absent from the current record. Collisions still scan.
	expBits uint64
	// arena is an append-only area for expectations kept in memo entries and recovery (referenced by
	// slices).
	arena    []expID
	lastKept []expID
	// descs maps expectation indices to strings (it differs per backend).
	descs []string
	// recovered holds the syntax errors recovered by #recover. Backtracking undoes them.
	recovered []*SyntaxError
	missing   *missingTokenState // invocation-local opt-in insertion accounting
	// missingLen is len(missing.path), or 0 without missing. It lives here so that mark and reset
	// need no pointer chase or call on strict parses.
	missingLen int
	// diagnosed is the prefix of recovered already delivered to the stream diagnostic callback.
	diagnosed int

	// emit receives the elements of the #stream repetition when parsing a stream.
	emit func(*value.Node) error
	// depth is the rule call depth (1 for the start rule). The parse is aborted when it exceeds
	// maxDepth.
	depth    int
	maxDepth int
	// pruned is the position at which the memo was last pruned.
	pruned int
	// created holds the struct nodes built by the action being evaluated (from evalCtx.cbase).
	created []*value.Node
	// memoAll requests memoizing transient rules (rule.transient) too (used by Document).
	memoAll bool
	// noPlain keeps call sites from calling invokePlain directly for plain rules (rule.plain), so
	// that every rule call goes through call. It is set when memoAll is (plain rules are then
	// memoized) and when tracing (call reports the calls).
	noPlain bool
	// tr is the tracing state, or nil when the parse is not traced.
	tr *tracer
	// deferMemo defers memoizing a rule at a position to its second call there (firstCall). It is
	// set for whole-input parses; Document and streams memoize on the first call.
	deferMemo bool

	// backend is the state that the backend running the parse keeps beside the parser, or nil
	// when it keeps none.
	backend backendState
	// estack is the operand stack of expression code (vmProgram.eval), also used for the
	// arguments of built-ins and struct fields by the closure backend's evaluator.
	estack []any
	names  []string // field names for newStruct in the closure backend's evaluator

	// Chunks of nodes and child slices (alloc.go), and the stack that collects repetition children
	nodeSlab  []value.Node
	ptrSlab   []*value.Node
	kidStack  []*value.Node
	frameSlab []frame
	fieldSlab []value.NodeField
	// Document edits: gen is the number of edits made before this parse, and edits records them
	// (see moveResult). moved holds the copies made by moveEmpty.
	gen    uint32
	edits  []docEdit
	moved  map[*value.Node]*value.Node
	shifts map[uint32]int // shiftOf of each generation in the current moveResult
	// moveBase and baseShift are the generation and the shift of the memo entry being moved.
	moveBase  uint32
	baseShift int
	// runs and lastRuns are the repetition runs recorded in this Document parse and the previous
	// one (resume.go). runs is nil outside Document parses.
	runs, lastRuns map[runKey]*runRecord
	// provisional counts the uses of provisional results of growing left recursions.
	provisional int
	// memoSeedUses tracks unresolved seed dependencies of completed memo entries.
	// Each growth discharges its own dependencies; provisional remains monotonic
	// because repetition recording must still notice every use of a growing seed.
	memoSeedUses int
	// Completed dependent entries can come from an earlier, already closed head.
	// Their dependencies are never discharged merely by finishing a new head.
	memoProvisionalUses int
	activeSeed          *memoEntry
	resumed             int // elements reused by resumeRepeat (for tests)
	// scratch holds the pooled buffers the parser uses (acquirePooledParser), or nil.
	scratch *scratch
	// nodeChunks counts the node chunks allocated since the last splitChunks.
	// A negative count tracks the exact-size prefix budget before #stream.
	nodeChunks int
	ectx       evalCtx // area for useCtx
	// Areas for lambdas of the closure backend: closures live until the next evaluation
	// (useCtx), and the contexts and parameters of a call until it returns.
	closures arena[closure]
	lctxs    arena[evalCtx]
	locals   arena[local]
	item     [1]*value.Node // the element list of an action whose body is not a sequence (one)
	oplocals [3]local       // $lhs, $rhs and $op of a OperatorsRule operator action
	saved    []*value.Node  // captures saved across a reset in longest
}

// backendState is the state that a backend keeps beside the parser for the parse it runs, such
// as the stacks of a VM. The parser holds it so that a rule body, which receives only the parser,
// can find it. The shared code calls it where it lets go of parse state; a Document keeps it from
// one parse to the next.
type backendState interface {
	// splitChunks lets go of the chunks the backend allocates values from, when the parser
	// splits its own chunks (splitChunks, startStreamChunks).
	splitChunks()
	// clearInactive clears the unused capacity of the backend's stacks when a stream commits.
	clearInactive()
	// attach makes the state serve the parser p, for a Document that kept it from a parse with
	// another parser.
	attach(p *parser)
	// keep prepares the state to be kept for a later parse of an input of n units. It lets go
	// of the parser, which attach replaces, and drops buffers much larger than such a parse
	// needs; clear also clears the used capacity.
	keep(n int, clear bool)
	// release lets go of the parser and clears and drops the buffers of the state.
	release()
	// retainedBytes estimates the memory of the buffers the state keeps.
	retainedBytes() uint64
}

type memoKey struct {
	rule, pos, min int
	missingSilent  bool // insertion-disabled lookahead has its own LR seeds
	// env holds the values of the variables the rule reads (rule.vars) when it is called, or nil.
	// Results of such rules are memoized per combination of these values.
	env []any
}

type memoEntry struct {
	node     *value.Node
	end      int
	examined int
	// from is the start of the examined input (detecting a line start may examine the preceding
	// character).
	from int
	// gen is the number of document edits when the node was stored or last brought up to date
	// through this entry, and shift is how far edits have shifted the entry since; shifted reports
	// that some edit shifted it (the shifts may cancel out, and nodes may have been moved through
	// another result that shares them, so shift alone does not tell whether to move the node).
	gen   uint32
	shift int32
	// errs holds the syntax errors recovered in this call. They are recorded again when the memo
	// entry is used.
	errs []*SyntaxError
	// far and expected are the farthest failure position and the expectations recorded within this
	// call. They are recorded again when the memo entry is used.
	far      int
	expected []expID

	// memoTable key, and the next entry at the same position
	env       []any
	pos       int
	next      *memoEntry
	rule, min int32
	// vgen is the number of document edits that the positions (pos, end, examined, from, far) and
	// shift account for. A Document applies an edit to most entries only when they are looked up
	// (advanceEntry).
	vgen uint32

	ok bool
	// silent reports that the result was evaluated inside a lookahead. No expectations were
	// recorded, so it is re-evaluated when referenced outside a lookahead.
	silent        bool
	missingSilent bool // insertion-disabled lookahead seed key
	// growing reports that this is a provisional result while a left recursion is growing.
	growing bool
	// provisional reports that this result depends on a growing LR seed. It may be reused
	// during this parse generation, but its input range alone cannot validate an edit.
	provisional bool
	// positional reports that the result depends on position values (.Start, .End). It cannot
	// be shifted past an edit.
	positional bool
	shifted    bool
}

// Stats counts rule evaluations and memo uses during a parse.
type Stats struct {
	Evaluated       int // number of rule body evaluations
	Reused          int // combined memo hits and resumed repetition elements
	MemoHits        int // memo-result reuse
	ResumedElements int // repetition elements reused
}

// reportedStats derives the public reuse breakdown from counters already
// maintained by the parser, avoiding another increment for every reused item.
func (p *parser) reportedStats() Stats {
	s := p.stats
	s.ResumedElements = p.resumed
	s.MemoHits = s.Reused - p.resumed
	return s
}

// frame holds the capture values for each rule body or repetition element.
type frame struct {
	vals []*value.Node
}

// emptyFrame is the frame for scopes without captures (shared because it is never written to).
var emptyFrame = &frame{}

type undo struct {
	f    *frame
	slot int
	old  *value.Node
}

// env is a persistent list with one current binding per predicate variable.
// Older environments saved for rollback are never modified.
type env struct {
	name string
	val  any
	next *env
}

func (e *env) lookup(name string) (any, bool) {
	for ; e != nil; e = e.next {
		if e.name == name {
			return e.val, true
		}
	}
	return nil, false
}

// bind keeps one current binding per name. Replacing a non-head binding copies
// only the prefix before it, so environments saved for rollback stay immutable.
// bound is monotonic: a name never bound before cannot occur in the current
// environment. A name bound only on an abandoned branch may be absent now;
// that false positive only causes a search, not a different binding.
func (p *parser) bind(name string, val any) {
	if workControlsEnabled && p.control != nil {
		p.bindControlled(name, val)
		return
	}
	e := p.env
	if p.bound == nil {
		p.bound = make(map[string]bool)
	}
	if e == nil {
		p.bound[name] = true
		p.env = &env{name: name, val: val}
		return
	}
	if e.name == name {
		if e.val != val {
			p.env = &env{name: name, val: val, next: e.next}
		}
		return
	}
	if !p.bound[name] {
		p.bound[name] = true
		p.env = &env{name: name, val: val, next: e}
		return
	}
	old := e
	for old != nil && old.name != name {
		old = old.next
	}
	if old == nil {
		p.env = &env{name: name, val: val, next: e}
		return
	}
	// Variables are checked scalar values; an equal binding has no effect.
	if old.val == val {
		return
	}
	head := &env{name: name, val: val}
	tail := head
	for cur := e; cur != old; cur = cur.next {
		tail.next = &env{name: cur.name, val: cur.val}
		tail = tail.next
	}
	tail.next = old.next
	p.env = head
}

// mark is the state restored on backtracking.
type mark struct {
	pos       int
	env       *env
	trail     int
	recovered int
	missing   int
}

func (p *parser) mark() mark {
	return mark{pos: p.pos, env: p.env, trail: len(p.trail), recovered: len(p.recovered), missing: p.missingLen}
}

func (p *parser) reset(m mark) {
	if p.missingLen > m.missing {
		p.resetMissing(m.missing)
	}
	p.pos = m.pos
	p.env = m.env
	p.recovered = p.recovered[:m.recovered]
	p.undoCaptures(m.trail)
	p.trail = p.trail[:m.trail]
}

func (p *parser) setCapture(slot int, v *value.Node) {
	p.trail = append(p.trail, undo{f: p.frame, slot: slot, old: p.frame.vals[slot]})
	p.frame.vals[slot] = v
}

// touch records that the input up to just before position n has been examined.
func (p *parser) touch(n int) {
	if n > p.hw {
		p.hw = n
	}
}

// expect records that id was expected at position pos.
func (p *parser) expect(pos int, id expID) {
	if p.silent > 0 || pos < p.farthest {
		return
	}
	if pos > p.farthest {
		p.farthest = pos
		p.exp = p.exp[:p.expBase]
		p.expBits = 0
	}
	bit := uint64(1) << (uint32(id) * 0x9e3779b1 >> 26)
	if p.expBits&bit != 0 {
		for _, e := range p.exp[p.expBase:] {
			p.stepWork()
			if e == id {
				return
			}
		}
	}
	p.expBits |= bit
	p.exp = append(p.exp, id)
}

// expMark is the expectation record before isolate.
type expMark struct {
	far, base int
	bits      uint64
}

// isolate starts a separate expectation record whose farthest position is pos.
func (p *parser) isolate(pos int) expMark {
	m := expMark{p.farthest, p.expBase, p.expBits}
	p.expBase, p.farthest, p.expBits = len(p.exp), pos, 0
	return m
}

// unisolate ends the separate record and returns its farthest position and expectations. The
// expectations alias the stack and are valid only until the next record (passing them to
// mergeExpected is fine; keep copies them).
func (p *parser) unisolate(m expMark) (far int, inner []expID) {
	far, inner = p.farthest, p.exp[p.expBase:]
	p.exp = p.exp[:p.expBase]
	p.expBase, p.farthest, p.expBits = m.base, m.far, m.bits
	return far, inner
}

// keep copies expectations into the append-only area.
func (p *parser) keep(ids []expID) []expID {
	if len(ids) == 0 {
		return nil
	}
	if p.equalExpected(ids, p.lastKept) { // the same sequence of expectations often repeats
		return p.lastKept
	}
	if cap(p.arena)-len(p.arena) < len(ids) {
		// Allocate per chunk (an older chunk stays alive only while slices into it remain).
		p.arena = make([]expID, 0, max(1024, len(ids)))
	}
	off := len(p.arena)
	p.arena = appendWork(p, p.arena, ids)
	p.lastKept = p.arena[off:len(p.arena):len(p.arena)]
	return p.lastKept
}

// mergeExpected records the expectations ids at position far. ids may point into the record
// stack beyond the current record (writes only happen before the position already read).
func (p *parser) mergeExpected(far int, ids []expID) {
	for _, x := range ids {
		p.stepWork()
		p.expect(far, x)
	}
}

// rule is a rule as the parser runs it: the facts of the compiled rule and the executable parts that
// a backend built for it (closure_builder.go, vm.go).
type rule struct {
	staticRule
	// kinds are the node kinds the rule makes (value.NewRuleKinds).
	kinds value.RuleKinds
	body  func(p *parser, min int) (*value.Node, bool)
	// nodeKinds and opKinds are the kinds of nodeType and opNodeType.
	nodeKinds, opKinds *value.NodeTypeKinds
	operatorsRule      *operatorsRule
	// act evaluates the action (nil if there is no action).
	act evaluator
	// value makes the rule's value from the value of its body, for a rule without an action.
	value func(p *parser, v *value.Node, start int) *value.Node
	// entry is the start of the body in bytecode (only for rules of the bytecode backend).
	entry int
	// initVals are the initial values of vars, in the same order (nil when vars is empty).
	initVals []any
}

// call calls rule r at binding level min.
// The work is split into callBegin, growBegin/growStep/growEnd, invoke, and callEnd, and the
// iterative-model VM (ivm.go) calls the same functions from its own stack.
func (p *parser) call(r *rule, min int) (*value.Node, bool) {
	p.stepWork()
	if p.tr != nil {
		// A traced parse: report the call (tracedCall calls back here with direct set).
		if !p.tr.direct {
			return p.tracedCall(r, min)
		}
		p.tr.direct = false
	}
	if !p.memoizes(r) || p.firstCall(r) {
		// A call without memoization. The examined range (hw, lw) only grows, so it need not be saved
		// and restored, and expectations need not be recorded separately.
		if len(r.Scope.Names) == 0 {
			return p.invokePlain(r, min)
		}
		start, rec, missing := p.pos, len(p.recovered), p.missingMark()
		v, ok := p.invoke(r, min)
		if !ok {
			p.pos = start
			p.recovered = p.recovered[:rec]
			if p.missing != nil {
				p.resetMissing(missing)
			}
		}
		return v, ok
	}
	st, v, ok, hit := p.callBegin(r, min)
	if hit {
		return v, ok
	}
	if r.Leader {
		g := p.growBegin(st.key)
		for {
			v, ok = p.invoke(r, min)
			if !p.growStep(&g, v, ok) {
				break
			}
		}
		v, ok = p.growEnd(r, &g)
	} else {
		v, ok = p.invoke(r, min)
	}
	return p.callEnd(r, &st, v, ok)
}

// noVals are the values of variables where no variable is defined (all nil): the memo key of
// most calls of a rule that reads variables, shared instead of allocated for each call. Memo
// entries only compare their keys, so nothing writes to it.
var noVals [16]any

// envValues returns the current values of the variables names (nil for an undefined variable).
func (p *parser) envValues(names []string) []any {
	if p.env == nil && len(names) <= len(noVals) {
		return noVals[:len(names):len(names)]
	}
	vals := make([]any, len(names))
	if !workControlsEnabled || p.control == nil {
		for i, name := range names {
			vals[i], _ = p.env.lookup(name)
		}
		return vals
	}
	for i, name := range names {
		p.stepWork()
		vals[i], _ = p.lookupValue(name)
	}
	return vals
}

// memoEnv returns the values of the variables the memoized rule r reads, for its memo key. While
// nothing has been bound since the parse began they are the initial values, computed once: the
// key only compares them, so the slice is shared instead of allocated for each call.
func (p *parser) memoEnv(r *rule) []any {
	if p.env == p.initEnv && r.initVals != nil && (!workControlsEnabled || p.control == nil) {
		return r.initVals
	}
	return p.envValues(r.Vars)
}

// memoizes reports whether the results of rule r are memoized.
func (p *parser) memoizes(r *rule) bool {
	return r.Leader || r.Memo && (!r.Transient || p.memoAll) && !(p.missing != nil && r.Missing)
}

// firstCall reports whether this is the first call of the memoized rule r at the current
// position, in which case the call is not memoized, and records the call. A result is reused only
// when the rule is called again at the same position, which in practice is rare: memoizing every
// first call cost a memo entry per call for nothing. Deferring memoization to the second call
// evaluates a rule at most twice per position, so parse time stays linear.
// Left-recursion leaders are always memoized (the memo drives the growing of the seed).
func (p *parser) firstCall(r *rule) bool {
	if !p.deferMemo || r.Seen < 0 {
		return false
	}
	return p.memoFirstCall(p.pos, r.Seen)
}

// callState is the state saved across a memoized rule call.
type callState struct {
	key                 memoKey
	start               int
	savedHW, savedLW    int
	rec                 int
	missing             int
	exp                 expMark
	memoSeedUses        int
	memoProvisionalUses int
}

// callBegin starts a memoized call (the caller has decided to memoize it: memoizes and not
// firstCall). It looks up the memo; if a usable result exists, it sets hit and returns that
// result. Otherwise it prepares for the call.
func (p *parser) callBegin(r *rule, min int) (st callState, v *value.Node, ok, hit bool) {
	key := memoKey{rule: r.ID, pos: p.pos, min: min, missingSilent: p.missing != nil && r.Missing && p.silent > 0}
	if len(r.Vars) > 0 {
		key.env = p.memoEnv(r)
	}
	// A result computed inside a lookahead recorded no expectations, so it is not reused outside
	// one; and errors recovered inside a lookahead carry no expectations either, so a result with
	// recovered errors is reused only in the same kind of context it was computed in.
	var e *memoEntry
	found := false
	if r.Leader && key.env != nil {
		// The left-recursion guard is keyed by rule and position only: while the rule grows
		// here, a call reads the growing result whatever its variable values.
		e, found = p.memoGrowing(key)
	}
	if !found {
		e, found = p.memoGet(key)
		if found && e.vgen != p.gen && !p.advanceEntry(e, p.edits[:p.gen]) {
			found = false // an edit since invalidated it
		}
	}
	if found && (p.missing == nil || !r.Missing || e.growing) && (e.growing || (!e.silent || p.silent > 0) && (len(e.errs) == 0 || e.silent == (p.silent > 0))) {
		p.touch(e.examined)
		p.lw = min2(p.lw, e.from)
		p.stats.Reused++
		if !e.growing {
			p.mergeExpected(e.far, e.expected)
		}
		if e.growing || e.provisional {
			p.provisional++
			if e.growing && e == p.activeSeed {
				p.memoSeedUses++
			} else {
				p.memoProvisionalUses++
			}
		}
		if e.shifted {
			p.moveResult(e)
		}
		if !e.ok {
			return st, nil, false, true
		}
		p.pos = e.end
		if p.missing == nil {
			p.recovered = appendWork(p, p.recovered, e.errs)
		} else {
			p.replayMissing(p.missingSeedEvents(e), e.errs)
		}
		return st, e.node, true, true
	}
	st = callState{key: key, start: p.pos, savedHW: p.hw, savedLW: p.lw, rec: len(p.recovered), missing: p.missingMark(), memoSeedUses: p.memoSeedUses, memoProvisionalUses: p.memoProvisionalUses}
	p.hw, p.lw = st.start, st.start
	// A result stored in the memo also includes the expectations recorded in this call.
	st.exp = p.isolate(st.start)
	return st, nil, false, false
}

// callEnd stores the result of the body (or of growing, for a leader) in the memo and restores
// the state from before the call.
func (p *parser) callEnd(r *rule, st *callState, v *value.Node, ok bool) (*value.Node, bool) {
	var e *memoEntry
	if r.Leader {
		e, _ = p.memoGet(st.key)
	} else {
		if !ok {
			p.recovered = p.recovered[:st.rec]
			if p.missing != nil {
				p.resetMissing(st.missing)
			}
		}
		e = p.memo.alloc()
		*e = memoEntry{node: v, ok: ok, end: p.pos, examined: p.hw, from: p.lw, silent: p.silent > 0, gen: p.gen, vgen: p.gen,
			positional: r.Positional, errs: appendWork(p, []*SyntaxError(nil), p.recovered[st.rec:])}
		p.memoPut(st.key, e)
	}
	far, inner := p.unisolate(st.exp)
	if e != nil {
		e.far, e.expected = far, p.keep(inner)
		e.provisional = p.memoAll && (p.memoSeedUses != st.memoSeedUses || p.memoProvisionalUses != st.memoProvisionalUses)
		if e.provisional {
			// The completed intermediate result can outlive the seed it saw.
			// Propagate that dependency even before a later memo hit imports it.
			p.memoProvisionalUses++
		}
	}
	p.mergeExpected(far, inner)
	p.hw = max(st.savedHW, p.hw)
	p.lw = min2(st.savedLW, p.lw)
	if !ok {
		p.pos = st.start
	}
	return v, ok
}

// growState is the grow-the-seed state of a left-recursion leader.
type growState struct {
	key          memoKey
	start        int
	rec          int
	missing      int
	best         *memoEntry
	memoSeedUses int
	prevSeed     *memoEntry
}

// growBegin places a failure in the memo as the growing result and prepares the first body
// evaluation. After seeding the memo with a failure, the body is evaluated repeatedly, and the
// result at the point where the match stops growing is adopted.
func (p *parser) growBegin(key memoKey) growState {
	g := growState{key: key, start: p.pos, rec: len(p.recovered), missing: p.missingMark(), memoSeedUses: p.memoSeedUses, prevSeed: p.activeSeed}
	g.best = p.memo.alloc()
	*g.best = memoEntry{ok: false, end: g.start, examined: g.start, from: g.start, growing: true, gen: p.gen, vgen: p.gen}
	if p.missing != nil {
		p.recordMissingSeed(g.best, g.missing)
	}
	p.activeSeed = g.best
	p.memoPut(key, g.best)
	return g
}

// growStep receives the body's result; if the match grew, it stores the result in the memo,
// prepares the next evaluation, and returns true.
func (p *parser) growStep(g *growState, v *value.Node, ok bool) bool {
	p.stepWork()
	if !ok || (g.best.ok && p.pos <= g.best.end) {
		return false
	}
	g.best = p.memo.alloc()
	*g.best = memoEntry{node: v, ok: true, end: p.pos, examined: p.hw, from: p.lw, growing: true, gen: p.gen, vgen: p.gen, errs: appendWork(p, []*SyntaxError(nil), p.recovered[g.rec:])}
	if p.missing != nil {
		p.recordMissingSeed(g.best, g.missing)
	}
	p.activeSeed = g.best
	p.memoPut(g.key, g.best)
	p.pos = g.start
	p.recovered = p.recovered[:g.rec]
	if p.missing != nil {
		p.resetMissing(g.missing)
	}
	return true
}

// growEnd finalizes and returns the last result.
func (p *parser) growEnd(r *rule, g *growState) (*value.Node, bool) {
	// This head has finished examining its whole growth, so its own seed
	// reads are resolved. Reads of another growing head or completed
	// dependent calls remain in memoProvisionalUses and cannot be discharged.
	p.memoSeedUses = g.memoSeedUses
	p.activeSeed = g.prevSeed
	best := g.best
	best.growing = false
	best.silent = p.silent > 0
	best.examined = p.hw
	best.from = p.lw
	best.positional = r.Positional
	p.pos = best.end
	p.recovered = p.recovered[:g.rec]
	if p.missing != nil {
		p.resetMissing(g.missing)
	}
	if p.missing == nil {
		p.recovered = appendWork(p, p.recovered, best.errs)
	} else {
		p.replayMissing(p.missingSeedEvents(best), best.errs)
	}
	return best.node, best.ok
}

// invoke evaluates the rule body and builds the value with the action or the CST rules.
func (p *parser) invoke(r *rule, min int) (*value.Node, bool) {
	st := p.invokeBegin(r)
	v, ok := r.body(p, min)
	return p.invokeEnd(r, &st, v, ok)
}

// invokePlain is an unmemoized call of a rule without captures: invoke with the steps of
// invokeBegin and invokeEnd inlined, minus the capture frame (the body never writes one, so the
// caller's stays current), and with the failure handling of call.
func (p *parser) invokePlain(r *rule, min int) (*value.Node, bool) {
	p.stepWork()
	start, rec, trail, missing := p.pos, len(p.recovered), len(p.trail), p.missingMark()
	prevEnv, prevCut := p.env, p.cut
	p.cut = false
	p.depth++
	if p.depth > p.maxDepth {
		p.fail("nesting too deep: more than %d rule calls", p.maxDepth)
	}
	p.stats.Evaluated++
	v, ok := r.body(p, min)
	p.depth--
	p.cut = prevCut
	p.trail = p.trail[:min2(trail, len(p.trail))]
	if ok {
		v = p.finish(r, emptyFrame, v, start)
	} else {
		p.pos = start
		p.recovered = p.recovered[:rec]
		if p.missing != nil {
			p.resetMissing(missing)
		}
	}
	p.env = prevEnv
	return v, ok
}

// invokeState is the state saved during a body evaluation.
type invokeState struct {
	f         *frame
	prevFrame *frame
	prevEnv   *env
	prevCut   bool
	trail     int
	start     int
}

func (p *parser) invokeBegin(r *rule) invokeState {
	p.stepWork()
	st := invokeState{f: p.newFrame(len(r.Scope.Names)), prevFrame: p.frame, prevEnv: p.env, prevCut: p.cut, trail: len(p.trail), start: p.pos}
	p.frame, p.cut = st.f, false
	p.depth++
	if p.depth > p.maxDepth {
		p.fail("nesting too deep: more than %d rule calls", p.maxDepth)
	}
	p.stats.Evaluated++
	return st
}

func (p *parser) invokeEnd(r *rule, st *invokeState, v *value.Node, ok bool) (*value.Node, bool) {
	p.depth--
	p.frame, p.cut = st.prevFrame, st.prevCut
	p.trail = p.trail[:min2(st.trail, len(p.trail))]
	if ok {
		v = p.finish(r, st.f, v, st.start)
	}
	p.env = st.prevEnv
	return v, ok
}

// finish builds the rule's value from the value of the rule body, and converts it to the rule's named node type, if any. A value-free
// rule is never checked.
func (p *parser) finish(r *rule, f *frame, v *value.Node, start int) *value.Node {
	if r.Novalue {
		return nil
	}
	if r.act != nil {
		v = p.runAction(r, f, nil, start, p.pos)
		if r.nodeKinds != nil {
			v = p.convertResult(r, v)
		}
		return v
	}
	v = r.value(p, v, start)
	if r.Splice {
		// No node of its own, and its captures end here: the caller takes the elements.
		if v != nil {
			value.D(v).Fresh = false
		}
		return v
	}
	if len(r.Scope.Names) > 0 {
		v = p.attachCaptures(v, r.Scope, f, start, p.pos)
	}
	owned := v != nil && value.D(v).Fresh
	if owned {
		if p.prog.RecordsSourceInfo && value.D(v).Kind.Rule == "" {
			value.D(v).Kind = r.kinds.Named(value.D(v).Kind, r.Name)
		}
		value.D(v).Fresh = false
	}
	if r.nodeKinds != nil {
		if owned && r.nodeKinds.ConvertOwned(v) {
			return v
		}
		v = p.convertResult(r, v)
	}
	return v
}

// convertResult converts v, the value of rule r, to the rule's named node type.
func (p *parser) convertResult(r *rule, v *value.Node) *value.Node {
	n, ok := r.nodeKinds.Convert(value.UnboxNode(v))
	if !ok {
		p.fail("cannot convert %s to %s in result of rule %s", typeName(value.UnboxNode(v)), r.NodeType, r.WhereName())
	}
	return n
}

// attachCaptures attaches captures to the node as fields. Captures that did not match (nil) are
// not attached. If the node belongs to another rule, it is wrapped in a tuple node first.
func (p *parser) attachCaptures(v *value.Node, s *compile.Scope, f *frame, start, end int) *value.Node {
	found := false
	wrap := v == nil || !value.D(v).Fresh
	for _, c := range f.vals {
		p.stepWork()
		if c != nil {
			found = true
		}
		if c != nil && c == v {
			// Also wrap when a capture refers to the node itself (a capture of the whole body).
			wrap = true
		}
	}
	if !found {
		return v
	}
	if wrap {
		v = p.newNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(end), Children: append(p.nodes(1)[:0], v), Fresh: true})
	}
	if value.D(v).Fields == nil {
		value.D(v).Fields = p.fields(len(s.Names))
	}
	value.D(v).Labeled = p.prog.RecordsSourceInfo
	for i, name := range s.Names {
		p.stepWork()
		if f.vals[i] != nil {
			p.setValueField(&value.D(v).Fields, name, f.vals[i])
		}
	}
	return v
}

// nodeOrNil converts a nil *Node to an interface nil.
func nodeOrNil(n *value.Node) any {
	if n == nil {
		return nil
	}
	return n
}

// fatal is an error after which the parse cannot continue, such as a runtime error in an action.
type fatal struct{ err error }

func (p *parser) fail(format string, args ...any) {
	panic(fatal{fmt.Errorf(format, args...)})
}

// SyntaxError reports that the input did not match the grammar.
type SyntaxError struct {
	Pos      int      // position, in the parse's position unit
	Line     int      // 1-based line
	Col      int      // 1-based column, in the position unit of the parse
	Expected []string // what was expected at that position
	Messages []string // messages given by @error
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Message())
}

// Message returns the error description without the position.
func (e *SyntaxError) Message() string {
	if len(e.Messages) > 0 {
		return strings.Join(e.Messages, "; ")
	}
	if len(e.Expected) > 0 {
		return "syntax error: expected " + strings.Join(e.Expected, ", ")
	}
	return "syntax error"
}

// SyntaxErrors is a list of syntax errors, including those recovered by error recovery.
type SyntaxErrors []*SyntaxError

func (l SyntaxErrors) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *parser) syntaxError() *SyntaxError {
	return p.makeError(p.farthest, p.exp[p.expBase:])
}

func (p *parser) makeError(pos int, expected []expID) *SyntaxError {
	e := &SyntaxError{Pos: pos}
	for _, x := range expected {
		p.stepWork()
		if x&msgBit != 0 {
			e.Messages = append(e.Messages, p.descs[x&^msgBit])
		} else {
			e.Expected = append(e.Expected, p.descs[x])
		}
	}
	p.sortExpected(e.Expected)
	e.Line, e.Col = p.lineCol(pos)
	return e
}

// docEdit is a Document edit: the positions [start, end) were replaced by end-start+delta
// positions.
type docEdit struct{ start, end, delta int }

// moveResult brings the positions of the result of the memo entry e, which edits have shifted, up
// to date.
//
// Nodes are moved in place rather than copied, so that reusing a result after an edit costs no
// allocation: a node's positions are valid after its first gen edits, and the later ones are
// replayed. No edit falls inside the input a reused result examined, so all the nodes of its tree
// move together, and for a non-empty node each edit either lies before it (the node moves by the
// edit's delta) or not (it stays), which its positions tell wherever the node is shared. An empty
// node at the point of an insertion is ambiguous: it may belong to a result before the insertion
// and to one after it at the same time. Empty nodes are therefore never modified; they are
// copied, moving with the node that contains them (or, at the root, with the memo entry).
func (p *parser) moveResult(e *memoEntry) {
	n := e.node
	p.moveBase, p.baseShift = e.gen, int(e.shift)
	e.gen, e.shift, e.shifted = p.gen, 0, false
	if n == nil {
		return
	}
	if p.moved == nil {
		p.moved = map[*value.Node]*value.Node{}
	}
	if value.D(n).Start == value.D(n).End {
		e.node = p.moveEmpty(n, p.baseShift)
	} else if value.D(n).Gen < p.gen {
		p.moveTree(n, p.shiftOf(n))
	}
	p.clearMovementWork()
}

// shiftOf returns how far the non-empty node n of the result being moved moves with the edits
// since the generation of n (Data.Gen). The nodes of a result move together. Those that were current when the entry's
// result was last current (gen at most moveBase) move by the entry's shift; others were moved through
// another result since, and the edits after that are replayed once per generation (shifts).
func (p *parser) shiftOf(n *value.Node) int {
	if value.D(n).Gen <= p.moveBase {
		return p.baseShift
	}
	if d, ok := p.shifts[value.D(n).Gen]; ok {
		return d
	}
	s := int(value.D(n).Start)
	for _, ed := range p.edits[value.D(n).Gen:p.gen] {
		p.stepWork()
		if s >= ed.end {
			s += ed.delta
		}
	}
	if p.shifts == nil {
		p.shifts = map[uint32]int{}
	}
	p.shifts[value.D(n).Gen] = s - int(value.D(n).Start)
	return s - int(value.D(n).Start)
}

// moveTree moves the non-empty node n by shift (shiftOf(n)), and its descendants, in place.
func (p *parser) moveTree(n *value.Node, shift int) {
	p.stepWork()
	gen := value.D(n).Gen
	value.D(n).Start += int32(shift)
	value.D(n).End += int32(shift)
	value.D(n).Gen = p.gen
	for i, c := range value.D(n).Children {
		p.stepWork()
		if c != nil && value.D(c).Gen < p.gen {
			value.D(n).Children[i] = p.moveChild(c, gen, shift)
		}
	}
	for i := range value.D(n).Fields {
		p.stepWork()
		if c, ok := value.D(n).Fields[i].Value.(*value.Node); ok && c != nil && value.D(c).Gen < p.gen {
			value.D(n).Fields[i].Value = p.moveChild(c, gen, shift)
		}
	}
}

// moveChild moves the descendant c of a node of generation gen that moved by shift, and returns
// it or its copy.
func (p *parser) moveChild(c *value.Node, gen uint32, shift int) *value.Node {
	if value.D(c).Start != value.D(c).End {
		if value.D(c).Gen != gen {
			shift = p.shiftOf(c)
		}
		p.moveTree(c, shift)
		return c
	}
	// An empty node is not modified after it is made, so its positions are valid where its
	// parent's were, and it moves with its parent.
	if shift == 0 {
		return c
	}
	return p.moveEmpty(c, shift)
}

// moveEmpty returns a copy of the empty node n moved by shift. Its descendants move with it: empty
// ones are copied the same way, and non-empty ones (captured in a lookahead) are moved in place.
// Copies made in the same moveResult are shared.
func (p *parser) moveEmpty(n *value.Node, shift int) *value.Node {
	p.stepWork()
	if c, ok := p.moved[n]; ok {
		return c
	}
	c := p.newNode(*value.D(n))
	value.D(c).Start += int32(shift)
	value.D(c).End += int32(shift)
	p.moved[n] = c
	if value.D(n).Children != nil {
		value.D(c).Children = p.nodes(len(value.D(n).Children))
		for i, ch := range value.D(n).Children {
			p.stepWork()
			value.D(c).Children[i] = p.moveDescendant(ch, value.D(n).Gen, shift)
		}
	}
	if value.D(n).Fields != nil {
		value.D(c).Fields = p.fields(len(value.D(n).Fields))[:len(value.D(n).Fields)]
		for i, f := range value.D(n).Fields {
			p.stepWork()
			if vn, ok := f.Value.(*value.Node); ok {
				f.Value = p.moveDescendant(vn, value.D(n).Gen, shift)
			}
			value.D(c).Fields[i] = f
		}
	}
	return c
}

// moveDescendant moves the descendant c of an empty node of generation gen that moved by shift.
func (p *parser) moveDescendant(c *value.Node, gen uint32, shift int) *value.Node {
	switch {
	case c == nil:
		return nil
	case value.D(c).Start == value.D(c).End:
		return p.moveEmpty(c, shift)
	case value.D(c).Gen < p.gen:
		return p.moveChild(c, gen, shift)
	}
	return c
}

// nodeValue returns the Value of the node n: its text for a string node, and a tuple or list
// value sharing its elements otherwise.
func (p *parser) nodeValue(n *value.Node) any {
	switch {
	case value.D(n).Terminal:
		return value.D(n).Text
	case value.D(n).Kind != nil && value.D(n).Kind.Base == value.BaseList:
		return p.newNode(value.Data{Kind: value.ListValueKind, Start: value.D(n).Start, End: value.D(n).End, Children: value.D(n).Children})
	}
	return p.newNode(value.Data{Kind: value.TupleValueKind, Start: value.D(n).Start, End: value.D(n).End, Children: value.D(n).Children})
}

// seedVars gives the parser the initial environment of the program's variables. Every parse
// calls it once, before it enters the start rule.
func (p *parser) seedVars() {
	if t := p.prog.Vars; t != nil {
		p.env, p.initEnv = p.prog.initEnv, p.prog.initEnv
		p.bound = maps.Clone(t.Bound)
	}
}
