package engine

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// parser is the execution state of a single parse.
type parser struct {
	prog *Program
	input
	pos int
	// hw and lw are the input range [lw, hw) examined by the current rule call. They are used by
	// incremental parsing.
	hw int
	lw int
	// stats counts evaluations and memo uses.
	stats Stats

	env   *env   // predicate variables
	frame *frame // current capture frame
	trail []undo // capture writes to undo on backtracking
	cut   bool   // whether a cut has been passed in the current alternative
	memo  *memoTable

	// While silent is positive (inside a lookahead), expectations are not recorded.
	silent   int
	farthest int
	// exp is the stack of expectation records. The current record is exp[expBase:], the
	// expectations at farthest. An expectation is an index into descs; an #error message is an index
	// with msgBit set (expID).
	exp     []expID
	expBase int
	// arena is an append-only area for expectations kept in memo entries and recovery (referenced by
	// slices).
	arena    []expID
	lastKept []expID
	// descs maps expectation indices to strings (it differs per backend).
	descs []string
	// recovered holds the syntax errors recovered by #recover. Backtracking undoes them.
	recovered []*SyntaxError

	// emit receives the elements of the #stream repetition when parsing a stream.
	emit func(*Node) error
	// depth is the rule call depth (1 for the start rule). The parse is aborted when it exceeds
	// maxDepth.
	depth    int
	maxDepth int
	// pruned is the position at which the memo was last pruned.
	pruned int
	// created holds the struct nodes built by the action being evaluated (from evalCtx.cbase).
	created []*Node
	// memoAll requests memoizing transient rules (rule.transient) too (used by Document).
	memoAll bool
	// deferMemo defers memoizing a rule at a position to its second call there (firstCall). It is
	// set for whole-input parses; Document and streams memoize on the first call.
	deferMemo bool

	// Bytecode VM state
	vals []any     // value stack
	ents []vmEntry // entry stack
	reps []repState
	// estack is the operand stack of expression code (vmProgram.eval).
	estack []any
	pool   framePool // frames of the iterative-model VM

	// Chunks of nodes and child slices (alloc.go), and the stack that collects repetition children
	nodeSlab  []Node
	ptrSlab   []*Node
	kidStack  []*Node
	frameSlab []frame
	fieldSlab []NodeField
	posSlab   []int
	// nodeChunks counts the node chunks allocated since the last splitChunks.
	nodeChunks int
	funcSlab   []vmFunc
	ectx       evalCtx      // area for useCtx
	item       [1]*Node     // the element list of an action whose body is not a sequence (one)
	oplocals   [3]local     // $lhs, $rhs and $op of a Pratt operator action
	saved      []*Node      // captures saved across a reset in longest
	attempt    prattAttempt // result of the longestFrame that just finished (iterative VM)
}

// expID is an expectation (an index into descs). If msgBit is set, it is an #error message.
type expID int32

const msgBit expID = 1 << 30

// Expectations placed at the start of descs regardless of backend
const (
	idAny expID = iota
	idBeginInput
	idEndInput
	idBeginLine
	idEndLine
	numFixedDescs
)

var fixedDescs = []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line"}

type memoKey struct {
	rule, pos, min int
	// env holds the values of the variables the rule reads (rule.vars) when it is called, or nil.
	// Results of such rules are memoized per combination of these values.
	env []any
}

type memoEntry struct {
	node     *Node
	end      int
	examined int
	// from is the start of the examined input (detecting a line start may examine the preceding
	// character).
	from int
	// shift is the amount of position shift from edits not yet applied to the node.
	shift int
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

	ok bool
	// silent reports that the result was evaluated inside a lookahead. No expectations were
	// recorded, so it is re-evaluated when referenced outside a lookahead.
	silent bool
	// growing reports that this is a provisional result while a left recursion is growing.
	growing bool
	// positional reports that the result depends on position values (startPos, endPos). It cannot
	// be shifted past an edit.
	positional bool
}

// Stats counts rule evaluations and memo uses during a parse.
type Stats struct {
	Evaluated int // number of rule body evaluations
	Reused    int // number of memo results reused
}

// frame holds the capture values for each rule body or repetition element.
type frame struct {
	vals []*Node
}

type undo struct {
	f    *frame
	slot int
	old  *Node
}

// env is a persistent list of predicate variables. A definition is represented by prepending a
// new element; older environments are never modified.
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

// mark is the state restored on backtracking.
type mark struct {
	pos       int
	env       *env
	trail     int
	recovered int
}

func (p *parser) mark() mark {
	return mark{pos: p.pos, env: p.env, trail: len(p.trail), recovered: len(p.recovered)}
}

func (p *parser) reset(m mark) {
	p.pos = m.pos
	p.env = m.env
	p.recovered = p.recovered[:m.recovered]
	for i := len(p.trail) - 1; i >= m.trail; i-- {
		u := p.trail[i]
		u.f.vals[u.slot] = u.old
	}
	p.trail = p.trail[:m.trail]
}

func (p *parser) setCapture(slot int, v *Node) {
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
	}
	for _, e := range p.exp[p.expBase:] {
		if e == id {
			return
		}
	}
	p.exp = append(p.exp, id)
}

// expMark is the expectation record from before a separate record was started.
type expMark struct{ far, base int }

// isolate starts recording subsequent expectations separately from the existing record, with pos
// as the farthest position.
func (p *parser) isolate(pos int) expMark {
	m := expMark{p.farthest, p.expBase}
	p.expBase, p.farthest = len(p.exp), pos
	return m
}

// unisolate ends the separate record and returns its farthest position and expectations. The
// expectations point into the stack area, so they are valid only until the next recording
// (passing them to mergeExpected is fine; to retain them, copy them with keep).
func (p *parser) unisolate(m expMark) (far int, inner []expID) {
	far, inner = p.farthest, p.exp[p.expBase:]
	p.exp = p.exp[:p.expBase]
	p.expBase, p.farthest = m.base, m.far
	return far, inner
}

// keep copies expectations into the append-only area.
func (p *parser) keep(ids []expID) []expID {
	if len(ids) == 0 {
		return nil
	}
	if slices.Equal(ids, p.lastKept) { // the same sequence of expectations often repeats
		return p.lastKept
	}
	if cap(p.arena)-len(p.arena) < len(ids) {
		// Allocate per chunk (an older chunk stays alive only while slices into it remain).
		p.arena = make([]expID, 0, max(1024, len(ids)))
	}
	off := len(p.arena)
	p.arena = append(p.arena, ids...)
	p.lastKept = p.arena[off:len(p.arena):len(p.arena)]
	return p.lastKept
}

// mergeExpected records the expectations ids at position far. ids may point into the record
// stack beyond the current record (writes only happen before the position already read).
func (p *parser) mergeExpected(far int, ids []expID) {
	for _, x := range ids {
		p.expect(far, x)
	}
}

// call calls rule r at binding level min.
// The work is split into callBegin, growBegin/growStep/growEnd, invoke, and callEnd, and the
// iterative-model VM (ivm.go) calls the same functions from its own stack.
func (p *parser) call(r *rule, min int) (*Node, bool) {
	if !p.memoizes(r) || p.firstCall(r) {
		// A call without memoization. The examined range (hw, lw) only grows, so it need not be saved
		// and restored, and expectations need not be recorded separately.
		if len(r.scope.names) == 0 {
			return p.invokePlain(r, min)
		}
		start, rec := p.pos, len(p.recovered)
		v, ok := p.invoke(r, min)
		if !ok {
			p.pos = start
			p.recovered = p.recovered[:rec]
		}
		return v, ok
	}
	st, v, ok, hit := p.callBegin(r, min)
	if hit {
		return v, ok
	}
	if r.leader {
		g := p.growBegin(st.key)
		for {
			v, ok = p.invoke(r, min)
			if !p.growStep(g, v, ok) {
				break
			}
		}
		v, ok = p.growEnd(r, g)
	} else {
		v, ok = p.invoke(r, min)
	}
	return p.callEnd(r, &st, v, ok)
}

// envValues returns the current values of the variables names (nil for an undefined variable).
func (p *parser) envValues(names []string) []any {
	vals := make([]any, len(names))
	for i, name := range names {
		vals[i], _ = p.env.lookup(name)
	}
	return vals
}

// memoizes reports whether the results of rule r are memoized.
func (p *parser) memoizes(r *rule) bool {
	return r.leader || r.memo && (!r.transient || p.memoAll)
}

// firstCall reports whether this is the first call of the memoized rule r at the current
// position, in which case the call is not memoized, and records the call. A result is reused only
// when the rule is called again at the same position, which in practice is rare: memoizing every
// first call cost a memo entry per call for nothing. Deferring memoization to the second call
// evaluates a rule at most twice per position, so parse time stays linear.
// Left-recursion leaders are always memoized (the memo drives the growing of the seed).
func (p *parser) firstCall(r *rule) bool {
	if !p.deferMemo || r.seen < 0 {
		return false
	}
	return p.memo.firstCall(p.pos, r.seen)
}

// numberSeen numbers the rules whose memoization firstCall may defer (rule.seen) and returns
// their count.
func numberSeen(lists ...[]*rule) int {
	n := 0
	for _, rules := range lists {
		for _, r := range rules {
			r.seen = -1
			if r.memo && !r.transient && !r.leader {
				r.seen = n
				n++
			}
		}
	}
	return n
}

// callState is the state saved across a memoized rule call.
type callState struct {
	key              memoKey
	start            int
	savedHW, savedLW int
	rec              int
	exp              expMark
}

// callBegin starts a memoized call (the caller has decided to memoize it: memoizes and not
// firstCall). It looks up the memo; if a usable result exists, it sets hit and returns that
// result. Otherwise it prepares for the call.
func (p *parser) callBegin(r *rule, min int) (st callState, v *Node, ok, hit bool) {
	key := memoKey{rule: r.id, pos: p.pos, min: min}
	if len(r.vars) > 0 {
		key.env = p.envValues(r.vars)
	}
	if e, found := p.memo.get(key); found && (!e.silent || p.silent > 0 || e.growing) {
		p.touch(e.examined)
		p.lw = min2(p.lw, e.from)
		p.stats.Reused++
		if !e.growing {
			p.mergeExpected(e.far, e.expected)
		}
		if e.shift != 0 {
			e.node = shiftNode(e.node, e.shift, map[*Node]*Node{})
			e.shift = 0
		}
		if !e.ok {
			return st, nil, false, true
		}
		p.pos = e.end
		p.recovered = append(p.recovered, e.errs...)
		return st, e.node, true, true
	}
	st = callState{key: key, start: p.pos, savedHW: p.hw, savedLW: p.lw, rec: len(p.recovered)}
	p.hw, p.lw = st.start, st.start
	// A result stored in the memo also includes the expectations recorded in this call.
	st.exp = p.isolate(st.start)
	return st, nil, false, false
}

// callEnd stores the result of the body (or of growing, for a leader) in the memo and restores
// the state from before the call.
func (p *parser) callEnd(r *rule, st *callState, v *Node, ok bool) (*Node, bool) {
	var e *memoEntry
	if r.leader {
		e, _ = p.memo.get(st.key)
	} else {
		if !ok {
			p.recovered = p.recovered[:st.rec]
		}
		e = p.memo.alloc()
		*e = memoEntry{node: v, ok: ok, end: p.pos, examined: p.hw, from: p.lw, silent: p.silent > 0,
			positional: r.positional, errs: append([]*SyntaxError(nil), p.recovered[st.rec:]...)}
		p.memo.put(st.key, e)
	}
	far, inner := p.unisolate(st.exp)
	if e != nil {
		e.far, e.expected = far, p.keep(inner)
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
	key   memoKey
	start int
	rec   int
	best  *memoEntry
}

// growBegin places a failure in the memo as the growing result and prepares the first body
// evaluation. After seeding the memo with a failure, the body is evaluated repeatedly, and the
// result at the point where the match stops growing is adopted.
func (p *parser) growBegin(key memoKey) *growState {
	g := &growState{key: key, start: p.pos, rec: len(p.recovered)}
	g.best = &memoEntry{ok: false, end: g.start, examined: g.start, from: g.start, growing: true}
	p.memo.put(key, g.best)
	return g
}

// growStep receives the body's result; if the match grew, it stores the result in the memo,
// prepares the next evaluation, and returns true.
func (p *parser) growStep(g *growState, v *Node, ok bool) bool {
	if !ok || (g.best.ok && p.pos <= g.best.end) {
		return false
	}
	g.best = &memoEntry{node: v, ok: true, end: p.pos, from: g.start, growing: true, errs: append([]*SyntaxError(nil), p.recovered[g.rec:]...)}
	p.memo.put(g.key, g.best)
	p.pos = g.start
	p.recovered = p.recovered[:g.rec]
	return true
}

// growEnd finalizes and returns the last result.
func (p *parser) growEnd(r *rule, g *growState) (*Node, bool) {
	best := g.best
	best.growing = false
	best.silent = p.silent > 0
	best.examined = p.hw
	best.from = p.lw
	best.positional = r.positional
	p.pos = best.end
	p.recovered = append(p.recovered[:g.rec], best.errs...)
	return best.node, best.ok
}

// invoke evaluates the rule body and builds the value with the action or the CST rules.
func (p *parser) invoke(r *rule, min int) (*Node, bool) {
	st := p.invokeBegin(r)
	v, ok := r.body(p, min)
	return p.invokeEnd(r, &st, v, ok)
}

// invokePlain is an unmemoized call of a rule without captures: invoke with the steps of
// invokeBegin and invokeEnd inlined, minus the capture frame (the body never writes one, so the
// caller's stays current), and with the failure handling of call.
func (p *parser) invokePlain(r *rule, min int) (*Node, bool) {
	start, rec, trail := p.pos, len(p.recovered), len(p.trail)
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
	st := invokeState{f: p.newFrame(len(r.scope.names)), prevFrame: p.frame, prevEnv: p.env, prevCut: p.cut, trail: len(p.trail), start: p.pos}
	p.frame, p.cut = st.f, false
	p.depth++
	if p.depth > p.maxDepth {
		p.fail("nesting too deep: more than %d rule calls", p.maxDepth)
	}
	p.stats.Evaluated++
	return st
}

func (p *parser) invokeEnd(r *rule, st *invokeState, v *Node, ok bool) (*Node, bool) {
	p.depth--
	p.frame, p.cut = st.prevFrame, st.prevCut
	p.trail = p.trail[:min2(st.trail, len(p.trail))]
	if ok {
		v = p.finish(r, st.f, v, st.start)
	}
	p.env = st.prevEnv
	return v, ok
}

// finish builds the rule's value from the value of the rule body.
func (p *parser) finish(r *rule, f *frame, v *Node, start int) *Node {
	if r.novalue {
		return nil
	}
	if r.act != nil {
		var items []*Node
		if r.bodyIsSeq && v != nil {
			items = v.Children
		} else {
			items = p.one(v)
		}
		return p.runAction(r, f, items, start, p.pos)
	}
	if r.terminalType != "" {
		return p.newNode(Node{Type: r.terminalType, Rule: r.name, Start: start, End: p.pos, Text: p.text(start, p.pos), terminal: true})
	}
	if len(r.scope.names) > 0 {
		v = p.attachCaptures(v, r.scope, f, start, p.pos)
	}
	if v == nil {
		return nil
	}
	if v.fresh {
		if v.Rule == "" {
			v.Rule = r.name
		}
		v.fresh = false
	}
	return v
}

// attachCaptures attaches captures to the node as fields. Captures that did not match (nil) are
// not attached. If the node belongs to another rule, it is wrapped in a Seq first.
func (p *parser) attachCaptures(v *Node, s *scope, f *frame, start, end int) *Node {
	found := false
	wrap := v == nil || !v.fresh
	for _, c := range f.vals {
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
		v = p.newNode(Node{Type: TypeSeq, Start: start, End: end, Children: append(p.nodes(1)[:0], v), fresh: true})
	}
	if v.Fields == nil {
		v.Fields = p.fields(len(s.names))
	}
	for i, name := range s.names {
		if f.vals[i] != nil {
			v.Fields.set(name, f.vals[i])
		}
	}
	return v
}

// nodeOrNil converts a nil *Node to an interface nil.
func nodeOrNil(n *Node) any {
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
	Col      int      // 1-based column (in characters)
	Expected []string // what was expected at that position
	Messages []string // messages given by #error
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
		if x&msgBit != 0 {
			e.Messages = append(e.Messages, p.descs[x&^msgBit])
		} else {
			e.Expected = append(e.Expected, p.descs[x])
		}
	}
	sort.Strings(e.Expected)
	e.Line, e.Col = p.lineCol(pos)
	return e
}

// shiftNode returns a copy of the node tree with positions shifted by delta. Shared subtrees
// remain shared in the copy.
func shiftNode(n *Node, delta int, seen map[*Node]*Node) *Node {
	if n == nil {
		return nil
	}
	if c, ok := seen[n]; ok {
		return c
	}
	c := *n
	c.Start += delta
	c.End += delta
	seen[n] = &c
	if n.Children != nil {
		c.Children = make([]*Node, len(n.Children))
		for i, ch := range n.Children {
			c.Children[i] = shiftNode(ch, delta, seen)
		}
	}
	if n.Fields != nil {
		c.Fields = make(Fields, len(n.Fields))
		for i, f := range n.Fields {
			if vn, ok := f.Value.(*Node); ok {
				f.Value = shiftNode(vn, delta, seen)
			}
			c.Fields[i] = f
		}
	}
	return &c
}
