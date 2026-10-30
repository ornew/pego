package core

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// SharedProgram is the part of a program that the parses of every backend read: the static
// program, the environment the declared variables start from and the buffers of finished parses.
// Program embeds it, and a parser points to it.
type SharedProgram struct {
	*staticProgram
	// scratch holds the buffers of finished parses for later ones (*scratch).
	scratch sync.Pool
	// initEnv is the environment of the declared variables that every parse starts from
	// (Parser.SeedVars). Environments are persistent, so every parse shares it.
	initEnv *Env
	// envLevels is the depth of the environment trie (EnvLevels of the variable count).
	envLevels int
}

// InitSharedProgram makes s the shared part of a program that runs the static program sp, with
// the declared variables of sp at their initial values.
func InitSharedProgram(s *SharedProgram, sp *staticProgram) {
	s.staticProgram = sp
	vars := sp.Vars.All()
	vals := make([]any, len(vars))
	for i, d := range vars {
		vals[i] = d.Init
	}
	s.envLevels = EnvLevels(len(vals))
	s.initEnv = NewEnv(vals, s.envLevels)
}

// StaticProgramOf returns the static program that s runs.
func StaticProgramOf(s *SharedProgram) *staticProgram { return s.staticProgram }

// Parser is the execution state of a single parse.
type Parser struct {
	Prog *SharedProgram
	Input
	Pos int
	// Hw and lw are the input range [lw, hw) examined by the current rule call. They are used by
	// incremental parsing.
	Hw int
	lw int
	// Stats counts evaluations and memo uses.
	Stats Stats
	// Aborted reports an interrupted execution, whose partial caches cannot be
	// reused by a Document. Normal syntax errors are completed executions.
	Aborted        bool
	EmitCall       bool // a caller-owned stream callback is currently executing
	Diagnostic     func(*SyntaxError) error
	DiagnosticTail StreamDiagnosticTail

	Env     *Env            // predicate variables
	envLevels int          // the depth of Env (Prog.envLevels)
	initEnv *Env            // the initial environment of the variables (Parser.SeedVars), or nil
	Frame   *Frame          // current capture frame
	Trail   []Undo          // capture writes to undo on backtracking
	Cut     bool            // whether a cut has been passed in the current alternative
	Memo    *MemoTable

	// While silent is positive (inside a lookahead), expectations are not recorded.
	Silent   int
	Farthest int
	// Expectations makes Expect record expectations. Without it only Farthest advances, and a
	// record holds at most the placeholder ExpFar, which stands for a failure at its position.
	Expectations bool
	// Exp is the stack of expectation records. The current record is Exp[ExpBase:], the
	// expectations at Farthest. An expectation is an index into the expectation descriptions; an
	// @error message is an index with MsgBit set (ExpID).
	Exp     []ExpID
	ExpBase int
	// ExpBits cheaply excludes IDs absent from the current record. Collisions still scan.
	ExpBits uint64
	// Arena is an append-only area for expectations kept in memo entries and recovery (referenced by
	// slices).
	Arena    []ExpID
	LastKept []ExpID
	// Descs maps expectation indices to strings (it differs per backend).
	Descs []string
	// Recovered holds the syntax errors recovered by #recover. Backtracking undoes them.
	Recovered []*SyntaxError
	Missing   *MissingTokenState // invocation-local opt-in insertion accounting
	// MissingLen is len(missing.path), or 0 without missing. It lives here so that mark and reset
	// need no pointer chase or call on strict parses.
	MissingLen int
	// Diagnosed is the prefix of recovered already delivered to the stream diagnostic callback.
	Diagnosed int

	// Emit receives the elements of the #stream repetition when parsing a stream.
	Emit func(*value.Node) error
	// StreamID is the ID of the start rule of a stream parse, the one rule whose #stream repetition
	// streams.
	StreamID int
	// Depth is the depth of nested recursive rule calls (0 outside them, so a start rule outside every
	// call cycle has depth 0). The parse is aborted when it exceeds MaxDepth.
	Depth    int
	MaxDepth int
	// Pruned is the position at which the memo was last pruned.
	Pruned int
	// Created holds the struct nodes built by the action being evaluated (from EvalCtx.Cbase).
	Created []*value.Node
	// MemoAll requests memoizing transient rules (rule.transient) too (used by Document).
	MemoAll bool
	// NoPlain keeps call sites from calling InvokePlain directly for plain rules (Rule.Plain), so
	// that every rule call goes through Call. It is set when MemoAll is (plain rules are then
	// memoized) and when tracing (Call reports the calls).
	NoPlain bool
	// Tr is the tracing state, or nil when the parse is not traced.
	Tr *Tracer
	// DeferMemo defers memoizing a rule at a position to its second call there (FirstCall). It is
	// set for whole-input parses; Document and streams memoize on the first call.
	DeferMemo bool

	// Backend is the state that the backend running the parse keeps beside the parser, or nil
	// when it keeps none.
	Backend BackendState
	// Estack is the operand stack of expression code (vm.Program.eval), also used for the
	// arguments of built-ins and struct fields by the closure backend's evaluator.
	Estack []any

	// Chunks of nodes and child slices (alloc.go), and the stack that collects repetition children
	NodeSlab  []value.Node
	PtrSlab   []*value.Node
	KidStack  []*value.Node
	FrameSlab []Frame
	fieldSlab []value.NodeField
	// Document edits: gen is the number of edits made before this parse, and edits records them
	// (see moveResult). moved holds the copies made by MoveEmpty.
	Gen    uint32
	Edits  []DocEdit
	Moved  map[*value.Node]*value.Node
	Shifts map[uint32]int // ShiftOf of each generation in the current moveResult
	// moveBase and baseShift are the generation and the shift of the memo entry being moved.
	moveBase  uint32
	baseShift int
	// Runs and LastRuns are the repetition runs recorded in this Document parse and the previous
	// one (resume.go). Runs is nil outside Document parses.
	Runs, LastRuns map[RunKey]*RunRecord
	// Provisional counts the uses of provisional results of growing left recursions.
	Provisional int
	// MemoSeedUses tracks unresolved seed dependencies of completed memo entries.
	// Each growth discharges its own dependencies; provisional remains monotonic
	// because repetition recording must still notice every use of a growing seed.
	MemoSeedUses int
	// Completed dependent entries can come from an earlier, already closed head.
	// Their dependencies are never discharged merely by finishing a new head.
	MemoProvisionalUses int
	ActiveSeed          *MemoEntry
	Resumed             int // elements reused by ResumeRepeat (for tests)
	// Scratch holds the pooled buffers the parser uses (AcquirePooledParser), or nil.
	Scratch *scratch
	// NodeChunks counts the node chunks allocated since the last splitChunks.
	// A negative count tracks the exact-size prefix budget before #stream.
	NodeChunks int
	ectx       EvalCtx        // area for UseCtx
	item       [1]*value.Node // the element list of an action whose body is not a sequence (one)
	oplocals   [3]Local       // $lhs, $rhs and $op of a OperatorsRule operator action
	Saved      []*value.Node  // captures saved across a reset in longest
}

// BackendState is the state that a backend keeps beside the parser for the parse it runs, such
// as the stacks of a VM. The parser holds it so that a rule body, which receives only the parser,
// can find it. The shared code calls it where it lets go of parse state; a Document keeps it from
// one parse to the next.
type BackendState interface {
	// SplitChunks lets go of the chunks the backend allocates values from, when the parser
	// splits its own chunks (splitChunks, StartStreamChunks).
	SplitChunks()
	// ClearInactive clears the unused capacity of the backend's stacks when a stream commits.
	ClearInactive()
	// Attach makes the state serve the parser p, for a Document that kept it from a parse with
	// another parser.
	Attach(p *Parser)
	// Keep prepares the state to be kept for a later parse of an input of n units. It lets go
	// of the parser, which Attach replaces, and drops buffers much larger than such a parse
	// needs; clear also clears the used capacity.
	Keep(n int, clear bool)
	// Release lets go of the parser and clears and drops the buffers of the state.
	Release()
	// RetainedBytes estimates the memory of the buffers the state keeps.
	RetainedBytes() uint64
}

type MemoKey struct {
	Rule, Pos, Min int
	MissingSilent  bool // insertion-disabled lookahead has its own LR seeds
	// Env holds the values of the variables the rule reads (rule.vars) when it is called, or nil.
	// Results of such rules are memoized per combination of these values.
	Env []any
}

type MemoEntry struct {
	Node     *value.Node
	End      int
	Examined int
	// From is the start of the examined input (detecting a line start may examine the preceding
	// character).
	From int
	// Gen is the number of document edits when the node was stored or last brought up to date
	// through this entry, and shift is how far edits have shifted the entry since; shifted reports
	// that some edit shifted it (the shifts may cancel out, and nodes may have been moved through
	// another result that shares them, so shift alone does not tell whether to move the node).
	Gen   uint32
	shift int32
	// Errs holds the syntax errors recovered in this call. They are recorded again when the memo
	// entry is used.
	Errs []*SyntaxError
	// Far and expected are the farthest failure position and the expectations recorded within this
	// call. They are recorded again when the memo entry is used.
	Far      int
	Expected []ExpID

	// MemoTable key, and the next entry at the same position
	Env       []any
	Pos       int
	Next      *MemoEntry
	Rule, Min int32
	// Vgen is the number of document edits that the positions (pos, end, examined, from, far) and
	// shift account for. A Document applies an edit to most entries only when they are looked up
	// (AdvanceEntry).
	Vgen uint32

	Ok bool
	// silent reports that the result was evaluated inside a lookahead. No expectations were
	// recorded, so it is re-evaluated when referenced outside a lookahead.
	silent        bool
	missingSilent bool // insertion-disabled lookahead seed key
	// Growing reports that this is a provisional result while a left recursion is growing.
	Growing bool
	// Provisional reports that this result depends on a growing LR seed. It may be reused
	// during this parse generation, but its input range alone cannot validate an edit.
	Provisional bool
	// Positional reports that the result depends on position values (.Start, .End). It cannot
	// be shifted past an edit.
	Positional bool
	shifted    bool
}

// Stats counts rule evaluations and memo uses during a parse.
type Stats struct {
	Evaluated       int // number of rule body evaluations
	Reused          int // combined memo hits and resumed repetition elements
	MemoHits        int // memo-result reuse
	ResumedElements int // repetition elements reused
}

// ReportedStats derives the public reuse breakdown from counters already
// maintained by the parser, avoiding another increment for every reused item.
func (p *Parser) ReportedStats() Stats {
	s := p.Stats
	s.ResumedElements = p.Resumed
	s.MemoHits = s.Reused - p.Resumed
	return s
}

// Frame holds the capture values for each rule body or repetition element.
type Frame struct {
	Vals []*value.Node
}

// EmptyFrame is the frame for scopes without captures (shared because it is never written to).
var EmptyFrame = &Frame{}

type Undo struct {
	F    *Frame
	Slot int
	Old  *value.Node
}

// Bind rebinds the variable in slot i. Binding the value it has already is a no-op. Saved
// environments are persistent, so a rebind copies the path to the slot (see Env).
func (p *Parser) Bind(slot int, val any) {
	if BudgetControlsEnabled && p.Control != nil {
		p.bindControlled(slot, val)
		return
	}
	// Variables are checked scalar values; an equal binding has no effect.
	if p.Env.Get(slot, p.envLevels) == val {
		return
	}
	p.Env = p.Env.Set(slot, p.envLevels, val)
}

// Mark is the state restored on backtracking.
type Mark struct {
	Pos       int
	Env       *Env
	Trail     int
	Recovered int
	missing   int
}

func (p *Parser) Mark() Mark {
	return Mark{Pos: p.Pos, Env: p.Env, Trail: len(p.Trail), Recovered: len(p.Recovered), missing: p.MissingLen}
}

func (p *Parser) Reset(m Mark) {
	if p.MissingLen > m.missing {
		p.ResetMissing(m.missing)
	}
	p.Pos = m.Pos
	p.Env = m.Env
	p.Recovered = p.Recovered[:m.Recovered]
	p.UndoCaptures(m.Trail)
	p.Trail = p.Trail[:m.Trail]
}

func (p *Parser) SetCapture(slot int, v *value.Node) {
	p.Trail = append(p.Trail, Undo{F: p.Frame, Slot: slot, Old: p.Frame.Vals[slot]})
	p.Frame.Vals[slot] = v
}

// touch records that the input up to just before position n has been examined.
func (p *Parser) touch(n int) {
	if n > p.Hw {
		p.Hw = n
	}
}

// Expect records that id was expected at position pos.
func (p *Parser) Expect(pos int, id ExpID) {
	if p.Silent > 0 || pos < p.Farthest {
		return
	}
	if pos > p.Farthest {
		p.Farthest = pos
		p.Exp = p.Exp[:p.ExpBase]
		p.ExpBits = 0
	}
	if !p.Expectations {
		// The record keeps one placeholder, so that merging it still carries the position.
		if len(p.Exp) == p.ExpBase {
			p.Exp = append(p.Exp, ExpFar)
		}
		return
	}
	bit := uint64(1) << (uint32(id) * 0x9e3779b1 >> 26)
	if p.ExpBits&bit != 0 {
		for _, e := range p.Exp[p.ExpBase:] {
			p.Spend()
			if e == id {
				return
			}
		}
	}
	p.ExpBits |= bit
	p.Exp = append(p.Exp, id)
}

// ExpMark is the expectation record before isolate.
type ExpMark struct {
	Far, Base int
	Bits      uint64
}

// Isolate starts a separate expectation record whose farthest position is pos.
func (p *Parser) Isolate(pos int) ExpMark {
	m := ExpMark{p.Farthest, p.ExpBase, p.ExpBits}
	p.ExpBase, p.Farthest, p.ExpBits = len(p.Exp), pos, 0
	return m
}

// Unisolate ends the separate record and returns its farthest position and expectations. The
// expectations alias the stack and are valid only until the next record (passing them to
// MergeExpected is fine; keep copies them).
func (p *Parser) Unisolate(m ExpMark) (far int, inner []ExpID) {
	far, inner = p.Farthest, p.Exp[p.ExpBase:]
	p.Exp = p.Exp[:p.ExpBase]
	p.ExpBase, p.Farthest, p.ExpBits = m.Base, m.Far, m.Bits
	return far, inner
}

// Keep copies expectations into the append-only area.
func (p *Parser) Keep(ids []ExpID) []ExpID {
	if len(ids) == 0 {
		return nil
	}
	if p.equalExpected(ids, p.LastKept) { // the same sequence of expectations often repeats
		return p.LastKept
	}
	if cap(p.Arena)-len(p.Arena) < len(ids) {
		// Allocate per chunk (an older chunk stays alive only while slices into it remain).
		p.Arena = make([]ExpID, 0, max(1024, len(ids)))
	}
	off := len(p.Arena)
	p.Arena = AppendSpend(p, p.Arena, ids)
	p.LastKept = p.Arena[off:len(p.Arena):len(p.Arena)]
	return p.LastKept
}

// MergeExpected records the expectations ids at position far. ids may point into the record
// stack beyond the current record (writes only happen before the position already read).
func (p *Parser) MergeExpected(far int, ids []ExpID) {
	for _, x := range ids {
		p.Spend()
		p.Expect(far, x)
	}
}

// Rule is a rule as the parser runs it: the facts of the compiled rule and the executable parts that
// the backend built for it.
type Rule struct {
	staticRule
	// Kinds are the node kinds the rule makes (value.NewRuleKinds).
	Kinds value.RuleKinds
	// Body matches the body of the rule at the current position and returns its node; min is the
	// binding level of the call (see Call).
	Body func(p *Parser, min int) (*value.Node, bool)
	// NodeKinds and OpKinds are the kinds of nodeType and opNodeType.
	NodeKinds, OpKinds *value.NodeTypeKinds
	OperatorsRule      *OperatorsRule
	// Act evaluates the action (nil if there is no action).
	Act Evaluator
	// Value makes the rule's value from the value of its body, for a rule without an action.
	Value func(p *Parser, v *value.Node, start int) *value.Node
	// Entry is the start of the body in bytecode (only for rules of the bytecode backend).
	Entry int
	// InitVals are the initial values of vars, in the same order (nil when vars is empty).
	InitVals []any
	// VarSlots are the slots of vars, in the same order (-1 for a name that no variable declares).
	VarSlots []int
}

// StaticRuleOf returns the compiled rule that r runs.
func StaticRuleOf(r *Rule) *staticRule { return &r.staticRule }

// Call calls rule r at binding level min.
// The work is split into CallBegin, GrowBegin/GrowStep/GrowEnd, invoke, and CallEnd, and the
// iterative VM calls the same functions from its own stack.
func (p *Parser) Call(r *Rule, min int) (*value.Node, bool) {
	p.Spend()
	if p.Tr != nil {
		// A traced parse: report the call (tracedCall calls back here with direct set).
		if !p.Tr.direct {
			return p.tracedCall(r, min)
		}
		p.Tr.direct = false
	}
	if !p.Memoizes(r) || p.FirstCall(r) {
		// A call without memoization. The examined range (hw, lw) only grows, so it need not be saved
		// and restored, and expectations need not be recorded separately.
		if len(r.Scope.Names) == 0 {
			return p.InvokePlain(r, min)
		}
		start, rec, missing := p.Pos, len(p.Recovered), p.missingMark()
		v, ok := p.invoke(r, min)
		if !ok {
			p.Pos = start
			p.Recovered = p.Recovered[:rec]
			if p.Missing != nil {
				p.ResetMissing(missing)
			}
		}
		return v, ok
	}
	st, v, ok, hit := p.CallBegin(r, min)
	if hit {
		return v, ok
	}
	if r.Leader {
		g := p.GrowBegin(st.Key)
		for {
			v, ok = p.invoke(r, min)
			if !p.GrowStep(&g, v, ok) {
				break
			}
		}
		v, ok = p.GrowEnd(r, &g)
	} else {
		v, ok = p.invoke(r, min)
	}
	return p.CallEnd(r, &st, v, ok)
}

// noVals are the values of variables where no variable is defined (all nil): the memo key of
// most calls of a rule that reads variables, shared instead of allocated for each call. Memo
// entries only compare their keys, so nothing writes to it.
var noVals [16]any

// EnvValues returns the current values of the variables in slots (nil for a slot below zero, an
// undeclared variable).
func (p *Parser) EnvValues(slots []int) []any {
	if p.Env == nil && len(slots) <= len(noVals) {
		return noVals[:len(slots):len(slots)]
	}
	vals := make([]any, len(slots))
	controlled := BudgetControlsEnabled && p.Control != nil
	for i, slot := range slots {
		if controlled {
			p.Spend()
		}
		if slot >= 0 {
			vals[i] = p.VarValue(slot)
		}
	}
	return vals
}

// memoEnv returns the values of the variables the memoized rule r reads, for its memo key. While
// nothing has been bound since the parse began they are the initial values, computed once: the
// key only compares them, so the slice is shared instead of allocated for each call.
func (p *Parser) memoEnv(r *Rule) []any {
	if p.Env == p.initEnv && r.InitVals != nil && (!BudgetControlsEnabled || p.Control == nil) {
		return r.InitVals
	}
	return p.EnvValues(r.VarSlots)
}

// Memoizes reports whether the results of rule r are memoized.
func (p *Parser) Memoizes(r *Rule) bool {
	return r.Leader || r.Memo && (!r.Transient || p.MemoAll) && !(p.Missing != nil && r.Missing)
}

// FirstCall reports whether this is the first call of the memoized rule r at the current
// position, in which case the call is not memoized, and records the call. A result is reused only
// when the rule is called again at the same position, which in practice is rare: memoizing every
// first call cost a memo entry per call for nothing. Deferring memoization to the second call
// evaluates the body of a rule at most twice per position, binding level and combination of variable
// values. That bounds how often a rule body runs, not the time of a parse: loops inside a body,
// left-recursion growth and variable environments do work that the memo does not share (see "Time and
// Memory" in spec/runtime/parsing.md).
// Left-recursion leaders are always memoized (the memo drives the growing of the seed).
func (p *Parser) FirstCall(r *Rule) bool {
	if !p.DeferMemo || r.Seen < 0 {
		return false
	}
	return p.memoFirstCall(p.Pos, r.Seen)
}

// CallState is the state saved across a memoized rule call.
type CallState struct {
	Key                 MemoKey
	start               int
	savedHW, savedLW    int
	Rec                 int
	missing             int
	exp                 ExpMark
	memoSeedUses        int
	memoProvisionalUses int
}

// CallBegin starts a memoized call (the caller has decided to memoize it: memoizes and not
// FirstCall). It looks up the memo; if a usable result exists, it sets hit and returns that
// result. Otherwise it prepares for the call.
func (p *Parser) CallBegin(r *Rule, min int) (st CallState, v *value.Node, ok, hit bool) {
	key := MemoKey{Rule: r.ID, Pos: p.Pos, Min: min, MissingSilent: p.Missing != nil && r.Missing && p.Silent > 0}
	if len(r.Vars) > 0 {
		key.Env = p.memoEnv(r)
	}
	// A result computed inside a lookahead recorded no expectations, so it is not reused outside
	// one; and errors recovered inside a lookahead carry no expectations either, so a result with
	// recovered errors is reused only in the same kind of context it was computed in.
	var e *MemoEntry
	found := false
	if r.Leader && key.Env != nil {
		// The left-recursion guard is keyed by rule and position only: while the rule grows
		// here, a call reads the growing result whatever its variable values.
		e, found = p.memoGrowing(key)
	}
	if !found {
		e, found = p.MemoGet(key)
		if found && e.Vgen != p.Gen && !p.AdvanceEntry(e, p.Edits[:p.Gen]) {
			found = false // an edit since invalidated it
		}
	}
	if found && (p.Missing == nil || !r.Missing || e.Growing) && (e.Growing || (!e.silent || p.Silent > 0) && (len(e.Errs) == 0 || e.silent == (p.Silent > 0))) {
		p.touch(e.Examined)
		p.lw = Min2(p.lw, e.From)
		p.Stats.Reused++
		if !e.Growing {
			p.MergeExpected(e.Far, e.Expected)
		}
		if e.Growing || e.Provisional {
			p.Provisional++
			if e.Growing && e == p.ActiveSeed {
				p.MemoSeedUses++
			} else {
				p.MemoProvisionalUses++
			}
		}
		if e.shifted {
			p.moveResult(e)
		}
		if !e.Ok {
			return st, nil, false, true
		}
		p.Pos = e.End
		if p.Missing == nil {
			p.Recovered = AppendSpend(p, p.Recovered, e.Errs)
		} else {
			p.ReplayMissing(p.MissingSeedEvents(e), e.Errs)
		}
		return st, e.Node, true, true
	}
	st = CallState{Key: key, start: p.Pos, savedHW: p.Hw, savedLW: p.lw, Rec: len(p.Recovered), missing: p.missingMark(), memoSeedUses: p.MemoSeedUses, memoProvisionalUses: p.MemoProvisionalUses}
	p.Hw, p.lw = st.start, st.start
	// A result stored in the memo also includes the expectations recorded in this call.
	st.exp = p.Isolate(st.start)
	return st, nil, false, false
}

// CallEnd stores the result of the body (or of growing, for a leader) in the memo and restores
// the state from before the call.
func (p *Parser) CallEnd(r *Rule, st *CallState, v *value.Node, ok bool) (*value.Node, bool) {
	var e *MemoEntry
	if r.Leader {
		e, _ = p.MemoGet(st.Key)
	} else {
		if !ok {
			p.Recovered = p.Recovered[:st.Rec]
			if p.Missing != nil {
				p.ResetMissing(st.missing)
			}
		}
		e = p.Memo.Alloc()
		*e = MemoEntry{Node: v, Ok: ok, End: p.Pos, Examined: p.Hw, From: p.lw, silent: p.Silent > 0, Gen: p.Gen, Vgen: p.Gen,
			Positional: r.Positional, Errs: AppendSpend(p, []*SyntaxError(nil), p.Recovered[st.Rec:])}
		p.MemoPut(st.Key, e)
	}
	far, inner := p.Unisolate(st.exp)
	if e != nil {
		e.Far, e.Expected = far, p.Keep(inner)
		e.Provisional = p.MemoAll && (p.MemoSeedUses != st.memoSeedUses || p.MemoProvisionalUses != st.memoProvisionalUses)
		if e.Provisional {
			// The completed intermediate result can outlive the seed it saw.
			// Propagate that dependency even before a later memo hit imports it.
			p.MemoProvisionalUses++
		}
	}
	p.MergeExpected(far, inner)
	p.Hw = max(st.savedHW, p.Hw)
	p.lw = Min2(st.savedLW, p.lw)
	if !ok {
		p.Pos = st.start
	}
	return v, ok
}

// GrowState is the grow-the-seed state of a left-recursion leader.
type GrowState struct {
	Key          MemoKey
	start        int
	Rec          int
	missing      int
	Best         *MemoEntry
	memoSeedUses int
	prevSeed     *MemoEntry
}

// GrowBegin places a failure in the memo as the growing result and prepares the first body
// evaluation. After seeding the memo with a failure, the body is evaluated repeatedly, and the
// result at the point where the match stops growing is adopted.
func (p *Parser) GrowBegin(key MemoKey) GrowState {
	g := GrowState{Key: key, start: p.Pos, Rec: len(p.Recovered), missing: p.missingMark(), memoSeedUses: p.MemoSeedUses, prevSeed: p.ActiveSeed}
	g.Best = p.Memo.Alloc()
	*g.Best = MemoEntry{Ok: false, End: g.start, Examined: g.start, From: g.start, Growing: true, Gen: p.Gen, Vgen: p.Gen}
	if p.Missing != nil {
		p.RecordMissingSeed(g.Best, g.missing)
	}
	p.ActiveSeed = g.Best
	p.MemoPut(key, g.Best)
	return g
}

// GrowStep receives the body's result; if the match grew, it stores the result in the memo,
// prepares the next evaluation, and returns true.
func (p *Parser) GrowStep(g *GrowState, v *value.Node, ok bool) bool {
	p.Spend()
	if !ok || (g.Best.Ok && p.Pos <= g.Best.End) {
		return false
	}
	g.Best = p.Memo.Alloc()
	*g.Best = MemoEntry{Node: v, Ok: true, End: p.Pos, Examined: p.Hw, From: p.lw, Growing: true, Gen: p.Gen, Vgen: p.Gen, Errs: AppendSpend(p, []*SyntaxError(nil), p.Recovered[g.Rec:])}
	if p.Missing != nil {
		p.RecordMissingSeed(g.Best, g.missing)
	}
	p.ActiveSeed = g.Best
	p.MemoPut(g.Key, g.Best)
	p.Pos = g.start
	p.Recovered = p.Recovered[:g.Rec]
	if p.Missing != nil {
		p.ResetMissing(g.missing)
	}
	return true
}

// GrowEnd finalizes and returns the last result.
func (p *Parser) GrowEnd(r *Rule, g *GrowState) (*value.Node, bool) {
	// This head has finished examining its whole growth, so its own seed
	// reads are resolved. Reads of another growing head or completed
	// dependent calls remain in memoProvisionalUses and cannot be discharged.
	p.MemoSeedUses = g.memoSeedUses
	p.ActiveSeed = g.prevSeed
	best := g.Best
	best.Growing = false
	best.silent = p.Silent > 0
	best.Examined = p.Hw
	best.From = p.lw
	best.Positional = r.Positional
	p.Pos = best.End
	p.Recovered = p.Recovered[:g.Rec]
	if p.Missing != nil {
		p.ResetMissing(g.missing)
	}
	if p.Missing == nil {
		p.Recovered = AppendSpend(p, p.Recovered, best.Errs)
	} else {
		p.ReplayMissing(p.MissingSeedEvents(best), best.Errs)
	}
	return best.Node, best.Ok
}

// invoke evaluates the rule body and builds the value with the action or the CST rules.
func (p *Parser) invoke(r *Rule, min int) (*value.Node, bool) {
	st := p.InvokeBegin(r)
	v, ok := r.Body(p, min)
	return p.InvokeEnd(r, &st, v, ok)
}

// InvokePlain is an unmemoized call of a rule without captures: invoke with the steps of
// InvokeBegin and InvokeEnd inlined, minus the capture frame (the body never writes one, so the
// caller's stays current), and with the failure handling of call.
func (p *Parser) InvokePlain(r *Rule, min int) (*value.Node, bool) {
	p.Spend()
	start, rec, trail, missing := p.Pos, len(p.Recovered), len(p.Trail), p.missingMark()
	prevEnv, prevCut := p.Env, p.Cut
	p.Cut = false
	if r.Rec {
		p.Enter()
	}
	p.Stats.Evaluated++
	v, ok := r.Body(p, min)
	if r.Rec {
		p.Depth--
	}
	p.Cut = prevCut
	p.Trail = p.Trail[:Min2(trail, len(p.Trail))]
	if ok {
		v = p.Finish(r, EmptyFrame, v, start)
	} else {
		p.Pos = start
		p.Recovered = p.Recovered[:rec]
		if p.Missing != nil {
			p.ResetMissing(missing)
		}
	}
	p.Env = prevEnv
	return v, ok
}

// InvokeState is the state saved during a body evaluation.
type InvokeState struct {
	f         *Frame
	prevFrame *Frame
	PrevEnv   *Env
	PrevCut   bool
	Trail     int
	Start     int
}

func (p *Parser) InvokeBegin(r *Rule) InvokeState {
	p.Spend()
	st := InvokeState{f: p.NewFrame(len(r.Scope.Names)), prevFrame: p.Frame, PrevEnv: p.Env, PrevCut: p.Cut, Trail: len(p.Trail), Start: p.Pos}
	p.Frame, p.Cut = st.f, false
	if r.Rec {
		p.Enter()
	}
	p.Stats.Evaluated++
	return st
}

func (p *Parser) InvokeEnd(r *Rule, st *InvokeState, v *value.Node, ok bool) (*value.Node, bool) {
	if r.Rec {
		p.Depth--
	}
	p.Frame, p.Cut = st.prevFrame, st.PrevCut
	p.Trail = p.Trail[:Min2(st.Trail, len(p.Trail))]
	if ok {
		v = p.Finish(r, st.f, v, st.Start)
	}
	p.Env = st.PrevEnv
	return v, ok
}

// Finish builds the rule's value from the value of the rule body, and converts it to the rule's named node type, if any. A value-free
// rule is never checked.
func (p *Parser) Finish(r *Rule, f *Frame, v *value.Node, start int) *value.Node {
	if r.Novalue {
		return nil
	}
	if r.Act != nil {
		var locals *Local
		if r.OperatorsRule != nil {
			// The whole-rule action of an Operators Rule sees the folded value as $value.
			p.oplocals[0] = Local{Name: compile.OperatorsRuleValue, Val: NodeOrNil(v)}
			locals = &p.oplocals[0]
		}
		v = p.runAction(r, f, locals, start, p.Pos)
		if r.NodeKinds != nil {
			v = p.convertResult(r, v)
		}
		return v
	}
	v = r.Value(p, v, start)
	if r.Splice {
		// No node of its own, and its captures end here: the caller takes the elements.
		if v != nil {
			value.D(v).Fresh = false
		}
		return v
	}
	if len(r.Scope.Names) > 0 {
		v = p.AttachCaptures(v, r.Scope, f, start, p.Pos)
	}
	owned := v != nil && value.D(v).Fresh
	if owned {
		if p.Prog.RecordsSourceInfo && value.D(v).Kind.Rule == "" {
			value.D(v).Kind = r.Kinds.Named(value.D(v).Kind, r.Name)
		}
		value.D(v).Fresh = false
	}
	if r.NodeKinds != nil {
		if owned && r.NodeKinds.ConvertOwned(v) {
			return v
		}
		v = p.convertResult(r, v)
	}
	return v
}

// convertResult converts v, the value of rule r, to the rule's named node type.
func (p *Parser) convertResult(r *Rule, v *value.Node) *value.Node {
	n, ok := r.NodeKinds.Convert(value.UnboxNode(v))
	if !ok {
		p.Fail("cannot convert %s to %s in result of rule %s", TypeName(value.UnboxNode(v)), r.NodeType, r.WhereName())
	}
	return n
}

// AttachCaptures attaches captures to the node as fields. Captures that did not match (nil) are
// not attached. If the node belongs to another rule, it is wrapped in a tuple node first.
func (p *Parser) AttachCaptures(v *value.Node, s *compile.Scope, f *Frame, start, end int) *value.Node {
	found := false
	wrap := v == nil || !value.D(v).Fresh
	for _, c := range f.Vals {
		p.Spend()
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
		v = p.NewNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(end), Children: append(p.Nodes(1)[:0], v), Fresh: true})
	}
	if value.D(v).Fields == nil {
		value.D(v).Fields = p.fields(len(s.Names))
	}
	value.D(v).Labeled = p.Prog.RecordsSourceInfo
	for i, name := range s.Names {
		p.Spend()
		if f.Vals[i] != nil {
			p.SetValueField(&value.D(v).Fields, name, f.Vals[i])
		}
	}
	return v
}

// NodeOrNil converts a nil *Node to an interface nil.
func NodeOrNil(n *value.Node) any {
	if n == nil {
		return nil
	}
	return n
}

// Fatal is an error after which the parse cannot continue, such as a runtime error in an action.
type Fatal struct{ Err error }

func (p *Parser) Fail(format string, args ...any) {
	panic(Fatal{fmt.Errorf(format, args...)})
}

// Enter counts one more nested entry of a recursive rule and fails the parse beyond the nesting
// limit.
func (p *Parser) Enter() {
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.tooDeep()
	}
}

// StreamsIn reports whether the #stream repetition of the rule id streams: the parse is a stream
// parse, the rule is its start rule, and the rule runs at the top (a recursive rule counts its
// entries, so its outermost entry is the first; any other rule is entered once).
func (p *Parser) StreamsIn(id int, rec bool) bool {
	return p.Emit != nil && p.StreamID == id && (!rec || p.Depth == 1)
}

//go:noinline
func (p *Parser) tooDeep() {
	p.Fail("nesting too deep: more than %d nested recursive rule calls", p.MaxDepth)
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

func Min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *Parser) SyntaxError() *SyntaxError {
	return p.MakeError(p.Farthest, p.Exp[p.ExpBase:])
}

func (p *Parser) MakeError(pos int, expected []ExpID) *SyntaxError {
	e := &SyntaxError{Pos: pos}
	for _, x := range expected {
		p.Spend()
		if x == ExpFar {
			continue
		}
		if x&MsgBit != 0 {
			e.Messages = append(e.Messages, p.Descs[x&^MsgBit])
		} else {
			e.Expected = append(e.Expected, p.Descs[x])
		}
	}
	p.sortExpected(e.Expected)
	e.Line, e.Col = p.lineCol(pos)
	return e
}

// DocEdit is a Document edit: the positions [start, end) were replaced by end-start+delta
// positions.
type DocEdit struct{ Start, End, Delta int }

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
func (p *Parser) moveResult(e *MemoEntry) {
	n := e.Node
	p.moveBase, p.baseShift = e.Gen, int(e.shift)
	e.Gen, e.shift, e.shifted = p.Gen, 0, false
	if n == nil {
		return
	}
	if p.Moved == nil {
		p.Moved = map[*value.Node]*value.Node{}
	}
	if value.D(n).Start == value.D(n).End {
		e.Node = p.MoveEmpty(n, p.baseShift)
	} else if value.D(n).Gen < p.Gen {
		p.MoveTree(n, p.ShiftOf(n))
	}
	p.ClearMovementSpend()
}

// ShiftOf returns how far the non-empty node n of the result being moved moves with the edits
// since the generation of n (Data.Gen). The nodes of a result move together. Those that were current when the entry's
// result was last current (gen at most moveBase) move by the entry's shift; others were moved through
// another result since, and the edits after that are replayed once per generation (shifts).
func (p *Parser) ShiftOf(n *value.Node) int {
	if value.D(n).Gen <= p.moveBase {
		return p.baseShift
	}
	if d, ok := p.Shifts[value.D(n).Gen]; ok {
		return d
	}
	s := int(value.D(n).Start)
	for _, ed := range p.Edits[value.D(n).Gen:p.Gen] {
		p.Spend()
		if s >= ed.End {
			s += ed.Delta
		}
	}
	if p.Shifts == nil {
		p.Shifts = map[uint32]int{}
	}
	p.Shifts[value.D(n).Gen] = s - int(value.D(n).Start)
	return s - int(value.D(n).Start)
}

// MoveTree moves the non-empty node n by shift (ShiftOf(n)), and its descendants, in place.
func (p *Parser) MoveTree(n *value.Node, shift int) {
	p.Spend()
	gen := value.D(n).Gen
	value.D(n).Start += int32(shift)
	value.D(n).End += int32(shift)
	value.D(n).Gen = p.Gen
	for i, c := range value.D(n).Children {
		p.Spend()
		if c != nil && value.D(c).Gen < p.Gen {
			value.D(n).Children[i] = p.moveChild(c, gen, shift)
		}
	}
	for i := range value.D(n).Fields {
		p.Spend()
		if c, ok := value.D(n).Fields[i].Value.(*value.Node); ok && c != nil && value.D(c).Gen < p.Gen {
			value.D(n).Fields[i].Value = p.moveChild(c, gen, shift)
		}
	}
}

// moveChild moves the descendant c of a node of generation gen that moved by shift, and returns
// it or its copy.
func (p *Parser) moveChild(c *value.Node, gen uint32, shift int) *value.Node {
	if value.D(c).Start != value.D(c).End {
		if value.D(c).Gen != gen {
			shift = p.ShiftOf(c)
		}
		p.MoveTree(c, shift)
		return c
	}
	// An empty node is not modified after it is made, so its positions are valid where its
	// parent's were, and it moves with its parent.
	if shift == 0 {
		return c
	}
	return p.MoveEmpty(c, shift)
}

// MoveEmpty returns a copy of the empty node n moved by shift. Its descendants move with it: empty
// ones are copied the same way, and non-empty ones (captured in a lookahead) are moved in place.
// Copies made in the same moveResult are shared.
func (p *Parser) MoveEmpty(n *value.Node, shift int) *value.Node {
	p.Spend()
	if c, ok := p.Moved[n]; ok {
		return c
	}
	c := p.NewNode(*value.D(n))
	value.D(c).Start += int32(shift)
	value.D(c).End += int32(shift)
	p.Moved[n] = c
	if value.D(n).Children != nil {
		value.D(c).Children = p.Nodes(len(value.D(n).Children))
		for i, ch := range value.D(n).Children {
			p.Spend()
			value.D(c).Children[i] = p.moveDescendant(ch, value.D(n).Gen, shift)
		}
	}
	if value.D(n).Fields != nil {
		value.D(c).Fields = p.fields(len(value.D(n).Fields))[:len(value.D(n).Fields)]
		for i, f := range value.D(n).Fields {
			p.Spend()
			if vn, ok := f.Value.(*value.Node); ok {
				f.Value = p.moveDescendant(vn, value.D(n).Gen, shift)
			}
			value.D(c).Fields[i] = f
		}
	}
	return c
}

// moveDescendant moves the descendant c of an empty node of generation gen that moved by shift.
func (p *Parser) moveDescendant(c *value.Node, gen uint32, shift int) *value.Node {
	switch {
	case c == nil:
		return nil
	case value.D(c).Start == value.D(c).End:
		return p.MoveEmpty(c, shift)
	case value.D(c).Gen < p.Gen:
		return p.moveChild(c, gen, shift)
	}
	return c
}

// nodeValue returns the Value of the node n: its text for a string node, and a tuple or list
// value sharing its elements otherwise.
func (p *Parser) nodeValue(n *value.Node) any {
	switch {
	case value.D(n).Terminal:
		return value.D(n).Text
	case value.D(n).Kind != nil && value.D(n).Kind.Base == value.BaseList:
		return p.NewNode(value.Data{Kind: value.ListValueKind, Start: value.D(n).Start, End: value.D(n).End, Children: value.D(n).Children})
	}
	return p.NewNode(value.Data{Kind: value.TupleValueKind, Start: value.D(n).Start, End: value.D(n).End, Children: value.D(n).Children})
}

// SeedVars gives the parser the initial environment of the program's variables. Every parse
// calls it once, before it enters the start rule.
func (p *Parser) SeedVars() {
	if t := p.Prog.Vars; t != nil {
		p.Env, p.initEnv, p.envLevels = p.Prog.initEnv, p.Prog.initEnv, p.Prog.envLevels
	}
}
