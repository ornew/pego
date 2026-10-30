package vm

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/engine/core"

	"github.com/ornew/pego/internal/value"
)

// NewIterative prepares a module for execution with the iterative model.
func NewIterative(m *bytecode.Module) *Program { return buildVMProgram(m, iterateBody) }

// iterateBody runs a rule body with the iterative model: calls push frames instead of recursing.
func iterateBody(vm *Program, r *core.Rule, entry int) func(p *core.Parser, min int) (*value.Node, bool) {
	return func(p *core.Parser, min int) (*value.Node, bool) {
		mc := ivmMachineOf(p)
		res := mc.iterate(vm, mc.bodyFrame(entry, r, min, false))
		return res.v, res.ok
	}
}

// The iterative-model VM. It runs rule calls, left-recursion growth, Operators Rule expression loops, and
// longest matching as state machines pushed on the VM stack (a slice of iframe) instead of using
// host recursion. Each state machine calls the shared runtime used by the recursive model (the
// CallBegin family, OperatorsRuleBuild, and so on) in the same order, so the results match the recursive
// model. Action and predicate expressions are evaluated with host calls, because their depth is
// determined by the grammar (not by the input).

// ivmMachine is the state of the iterative-model VM for one parse: the VM state it runs code
// with, the pool of its frames and the result of the last longest match. It is the driver state
// of the VM (Machine.driver): the parser's backend state is the embedded Machine, whose
// driver points back here.
type ivmMachine struct {
	Machine
	pool    framePool                 // frames of the iterative-model VM
	attempt core.OperatorsRuleAttempt // result of the longestFrame that just finished
}

// ivmMachineOf returns the iterative-model VM state of the parse that p runs, making it on first
// use.
func ivmMachineOf(p *core.Parser) *ivmMachine {
	if mc, ok := p.Backend.(*Machine); ok {
		if im, ok := mc.driver.(*ivmMachine); ok {
			return im
		}
	}
	return newIVMMachine(p)
}

// newIVMMachine makes the iterative-model VM state of the parse that p runs. It takes over the
// stacks of the VM state that p has, such as the value stack a Document kept. The first rule body
// of a parse makes it, before any code holds the VM state that it replaces.
func newIVMMachine(p *core.Parser) *ivmMachine {
	im := &ivmMachine{}
	if mc, ok := p.Backend.(*Machine); ok {
		im.Machine = *mc
	}
	im.p, im.driver = p, im
	p.Backend = &im.Machine
	return im
}

func (im *ivmMachine) reset() {
	im.pool, im.attempt = framePool{}, core.OperatorsRuleAttempt{}
}

// iresult is the result of running a child.
type iresult struct {
	v         *value.Node
	ok        bool
	cutFailed bool // result of longestFrame, whose attempt is in ivmMachine.attempt
}

// iframe is an element of the VM stack. next receives the child's result res (empty on the first
// call) and either returns the next child to run, or sets done and returns its own result.
type iframe interface {
	next(mc *ivmMachine, vm *Program, res iresult) (child iframe, out iresult, done bool)
}

// iterate puts root at the bottom of the stack and runs until it finishes.
func (mc *ivmMachine) iterate(vm *Program, root iframe) iresult {
	stack := []iframe{root}
	var res iresult
	for {
		child, out, done := stack[len(stack)-1].next(mc, vm, res)
		if !done {
			stack = append(stack, child)
			res = iresult{}
			continue
		}
		mc.releaseFrame(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if len(stack) == 0 {
			return out
		}
		res = out
	}
}

// bodyFrame runs code (a rule body or an Operators Rule section). For an unmemoized call of a rule without
// captures, the body frame also does the steps of the call (InvokePlain), so that such calls,
// the most common, push one frame instead of two.
type bodyFrame struct {
	b       vmBody
	block   bool // OperatorsRule section (reports a cut to the caller through p.cut)
	started bool
	call    bool             // a plain call of b.r: run the end of InvokePlain when the body finishes
	inv     core.InvokeState // call: the state saved by the call
	rec     int              // call: recovered errors before the call
}

func (f *bodyFrame) next(mc *ivmMachine, vm *Program, res iresult) (iframe, iresult, bool) {
	p := mc.p
	var ev int
	var v *value.Node
	var ok bool
	if !f.started {
		f.started = true
		ev, v, ok = mc.step(vm, &f.b, false, false, nil)
	} else {
		ev, v, ok = mc.step(vm, &f.b, true, res.ok, res.v)
	}
	switch ev {
	case evCall:
		in := &vm.m.Code[f.b.ip]
		r, min := vm.rules[in.A], int(in.B)
		if r.Plain && !p.NoPlain {
			return mc.plainCallFrame(r, min), iresult{}, false
		}
		if p.Tr != nil {
			return &traceFrame{r: r, min: min}, iresult{}, false
		}
		// What callChild does, written out on this hot path
		if !p.Memoizes(r) || p.FirstCall(r) {
			if len(r.Scope.Names) == 0 {
				return mc.plainCallFrame(r, min), iresult{}, false
			}
			return mc.callFrame(r, min, false), iresult{}, false
		}
		return mc.callFrame(r, min, true), iresult{}, false
	case evOperatorsRule:
		return mc.operatorsRuleFrame(f.b.r, f.b.min), iresult{}, false
	}
	if f.block && f.b.cut {
		p.Cut = true
	}
	if f.call { // the end of InvokePlain
		r := f.b.r
		p.Depth--
		p.Cut = f.inv.PrevCut
		p.Trail = p.Trail[:core.Min2(f.inv.Trail, len(p.Trail))]
		if ok {
			v = p.Finish(r, core.EmptyFrame, v, f.inv.Start)
		} else {
			p.Pos = f.inv.Start
			p.Recovered = p.Recovered[:f.rec]
		}
		p.Env = f.inv.PrevEnv
	}
	return nil, iresult{v: v, ok: ok}, true
}

// callChild returns the frame that runs a call of r (the same steps as call).
func (mc *ivmMachine) callChild(r *core.Rule, min int) iframe {
	p := mc.p
	if !p.Memoizes(r) || p.FirstCall(r) {
		if len(r.Scope.Names) == 0 {
			return mc.plainCallFrame(r, min)
		}
		return mc.callFrame(r, min, false)
	}
	return mc.callFrame(r, min, true)
}

// plainCallFrame starts an unmemoized call of a rule without captures (the start of InvokePlain)
// and returns the frame that runs its body.
func (mc *ivmMachine) plainCallFrame(r *core.Rule, min int) *bodyFrame {
	p := mc.p
	p.Spend()
	f := mc.bodyFrame(r.Entry, r, min, false)
	f.call = true
	f.inv = core.InvokeState{PrevEnv: p.Env, PrevCut: p.Cut, Trail: len(p.Trail), Start: p.Pos}
	f.rec = len(p.Recovered)
	p.Cut = false
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.Fail("nesting too deep: more than %d rule calls", p.MaxDepth)
	}
	p.Stats.Evaluated++
	return f
}

func blockFrame(mc *ivmMachine, entry int) *bodyFrame {
	return mc.bodyFrame(entry, nil, 0, true)
}

// Frames are reused after being popped, so pushing a frame does not allocate in steady state.
type framePool struct {
	bodies         []*bodyFrame
	calls          []*callFrame
	skips          []*skipFrame
	longests       []*longestFrame
	nuds           []*nudFrame
	operatorsRules []*operatorsRuleFrame
}

// reuse pops a frame from a free list, or allocates one.
func reuse[T any](list *[]*T) *T {
	if n := len(*list); n > 0 {
		f := (*list)[n-1]
		*list = (*list)[:n-1]
		return f
	}
	return new(T)
}

func (mc *ivmMachine) skipFrame(pr *core.OperatorsRule) *skipFrame {
	f := reuse(&mc.pool.skips)
	*f = skipFrame{pr: pr}
	return f
}

func (mc *ivmMachine) longestFrame(ops []*core.OperatorsRuleOp) *longestFrame {
	f := reuse(&mc.pool.longests)
	*f = longestFrame{ops: ops}
	return f
}

func (mc *ivmMachine) nudFrame(r *core.Rule) *nudFrame {
	f := reuse(&mc.pool.nuds)
	*f = nudFrame{r: r}
	return f
}

func (mc *ivmMachine) operatorsRuleFrame(r *core.Rule, min int) *operatorsRuleFrame {
	p := mc.p
	p.Spend()
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.Fail("nesting too deep: more than %d rule calls", p.MaxDepth)
	}
	f := reuse(&mc.pool.operatorsRules)
	*f = operatorsRuleFrame{r: r, min: min}
	return f
}

func (mc *ivmMachine) bodyFrame(entry int, r *core.Rule, min int, block bool) *bodyFrame {
	var f *bodyFrame
	if n := len(mc.pool.bodies); n > 0 {
		f = mc.pool.bodies[n-1]
		mc.pool.bodies = mc.pool.bodies[:n-1]
	} else {
		f = &bodyFrame{}
	}
	*f = bodyFrame{b: mc.newBody(entry, r, min), block: block}
	return f
}

// callFrame returns the frame of a call of r, memoized or not (the caller has decided, calling
// firstCall once).
func (mc *ivmMachine) callFrame(r *core.Rule, min int, memoized bool) *callFrame {
	var f *callFrame
	if n := len(mc.pool.calls); n > 0 {
		f = mc.pool.calls[n-1]
		mc.pool.calls = mc.pool.calls[:n-1]
	} else {
		f = &callFrame{}
	}
	*f = callFrame{r: r, min: min, memoized: memoized}
	return f
}

// releaseFrame makes a finished frame available for reuse.
func (mc *ivmMachine) releaseFrame(f iframe) {
	switch f := f.(type) {
	case *bodyFrame:
		mc.pool.bodies = append(mc.pool.bodies, f)
	case *callFrame:
		f.g = core.GrowState{}
		mc.pool.calls = append(mc.pool.calls, f)
	case *skipFrame:
		mc.pool.skips = append(mc.pool.skips, f)
	case *longestFrame:
		mc.pool.longests = append(mc.pool.longests, f)
	case *nudFrame:
		mc.pool.nuds = append(mc.pool.nuds, f)
	case *operatorsRuleFrame:
		mc.p.Depth--
		mc.pool.operatorsRules = append(mc.pool.operatorsRules, f)
	}
}

// callFrame is a rule call (same steps as parser.call).
type callFrame struct {
	r     *core.Rule
	min   int
	state int
	// memoized is set for a memoized call (CallBegin and CallEnd); otherwise the call is not
	// memoized (unmemoized calls of rules without captures use plainCallFrame instead).
	memoized   bool
	st         core.CallState
	g          core.GrowState
	inv        core.InvokeState
	start, rec int // unmemoized calls: state to restore on failure
}

const (
	cBegin = iota
	cInvoked
)

func (f *callFrame) next(mc *ivmMachine, vm *Program, res iresult) (iframe, iresult, bool) {
	p := mc.p
	r := f.r
	switch f.state {
	case cBegin:
		p.Spend() // the charge of Parser.Call
		if !f.memoized {
			f.start, f.rec = p.Pos, len(p.Recovered)
			return f.invoke(mc), iresult{}, false
		}
		st, v, ok, hit := p.CallBegin(r, f.min)
		if hit {
			return nil, iresult{v: v, ok: ok}, true
		}
		f.st = st
		if r.Leader {
			f.g = p.GrowBegin(st.Key)
		}
		return f.invoke(mc), iresult{}, false
	default: // cInvoked
		v, ok := p.InvokeEnd(r, &f.inv, res.v, res.ok)
		if !f.memoized {
			if !ok {
				p.Pos = f.start
				p.Recovered = p.Recovered[:f.rec]
			}
			return nil, iresult{v: v, ok: ok}, true
		}
		if r.Leader {
			if p.GrowStep(&f.g, v, ok) {
				return f.invoke(mc), iresult{}, false
			}
			v, ok = p.GrowEnd(r, &f.g)
		}
		v, ok = p.CallEnd(r, &f.st, v, ok)
		return nil, iresult{v: v, ok: ok}, true
	}
}

// invoke starts evaluating the body (same steps as parser.invoke).
func (f *callFrame) invoke(mc *ivmMachine) iframe {
	p := mc.p
	f.state = cInvoked
	f.inv = p.InvokeBegin(f.r)
	return mc.bodyFrame(f.r.Entry, f.r, f.min, false)
}

// skipFrame is the skip of an Operators Rule expression (same steps as parser.skipTrivia).
type skipFrame struct {
	pr      *core.OperatorsRule
	m       core.Mark
	started bool
}

func (f *skipFrame) next(mc *ivmMachine, vm *Program, res iresult) (iframe, iresult, bool) {
	p := mc.p
	if f.pr.SkipEntry < 0 {
		return nil, iresult{ok: true}, true
	}
	if !f.started {
		f.started = true
		f.m = p.Mark()
		p.Silent++
		return blockFrame(mc, f.pr.SkipEntry), iresult{}, false
	}
	p.Silent--
	if !res.ok {
		p.Reset(f.m)
	}
	return nil, iresult{ok: true}, true
}

// longestFrame is the longest match over operator parts (same steps as Parser.Longest).
type longestFrame struct {
	ops       []*core.OperatorsRuleOp
	i         int
	started   bool
	m0        core.Mark
	best      core.OperatorsRuleAttempt
	cutFailed bool
	prevFrame *core.Frame
	prevCut   bool
	f         *core.Frame
}

func (f *longestFrame) next(mc *ivmMachine, vm *Program, res iresult) (iframe, iresult, bool) {
	p := mc.p
	if !f.started {
		f.started = true
		f.m0 = p.Mark()
	} else {
		op := f.ops[f.i]
		cut := p.Cut
		p.Frame, p.Cut = f.prevFrame, f.prevCut
		if res.ok && p.Pos == f.m0.Pos && op.Kind != grammar.Infix {
			res.ok = false // see Parser.Longest: empty prefix and postfix matches are not candidates
		}
		if res.ok && (f.best.Op == nil || p.Pos > f.best.End) {
			f.best = core.OperatorsRuleAttempt{Op: op, Frame: f.f, V: res.v, Start: f.m0.Pos, End: p.Pos, Env: p.Env, Cut: cut,
				Errs: core.AppendSpend(p, []*core.SyntaxError(nil), p.Recovered[f.m0.Recovered:])}
		}
		if !res.ok && cut {
			f.cutFailed = true
		}
		if len(f.f.Vals) == 0 {
			p.Reset(f.m0)
		} else {
			p.Saved = core.AppendSpend(p, p.Saved[:0], f.f.Vals) // see Parser.Longest
			p.Reset(f.m0)
			core.CopySpend(p, f.f.Vals, p.Saved)
		}
		f.i++
	}
	if f.i == len(f.ops) {
		mc.attempt = f.best // passed through the parser to keep iresult small
		return nil, iresult{cutFailed: f.cutFailed}, true
	}
	f.prevFrame, f.prevCut = p.Frame, p.Cut
	f.f = p.NewFrame(len(f.ops[f.i].Line.Scope.Names))
	p.Frame, p.Cut = f.f, false
	return blockFrame(mc, f.ops[f.i].Line.Entry), iresult{}, false
}

// nudFrame is the beginning of an expression (same steps as parser.operatorsRuleNud).
type nudFrame struct {
	r         *core.Rule
	state     int
	m0        core.Mark
	a         core.OperatorsRuleAttempt
	i         int
	prevFrame *core.Frame
	prevCut   bool
	f         *core.Frame
}

const (
	nBegin = iota
	nSkipped
	nPrefix
	nRHS
	nOperand
	nOperandDone
)

func (f *nudFrame) next(mc *ivmMachine, vm *Program, res iresult) (iframe, iresult, bool) {
	p := mc.p
	pr := f.r.OperatorsRule
	for {
		switch f.state {
		case nBegin:
			f.state = nSkipped
			if pr.SkipEntry >= 0 {
				return mc.skipFrame(pr), iresult{}, false
			}
		case nSkipped:
			f.m0 = p.Mark()
			f.state = nPrefix
			return mc.longestFrame(pr.Prefix), iresult{}, false
		case nPrefix:
			f.a = mc.attempt
			if f.a.Op == nil {
				if res.cutFailed {
					return nil, iresult{}, true
				}
				f.state = nOperand
				continue
			}
			p.Apply(&f.a)
			f.state = nRHS
			return mc.operatorsRuleFrame(f.r, f.a.Op.Level), iresult{}, false
		case nRHS:
			if res.ok {
				return nil, iresult{v: p.OperatorsRuleBuild(f.r, &f.a, nil, res.v), ok: true}, true
			}
			if f.a.Cut {
				return nil, iresult{}, true
			}
			p.Reset(f.m0)
			f.state = nOperand
		case nOperand:
			if f.i == len(pr.Operands) {
				return nil, iresult{}, true
			}
			o := pr.Operands[f.i]
			f.prevFrame, f.prevCut = p.Frame, p.Cut
			f.f = p.NewFrame(len(o.Scope.Names))
			p.Frame, p.Cut = f.f, false
			f.state = nOperandDone
			return blockFrame(mc, o.Entry), iresult{}, false
		case nOperandDone:
			o := pr.Operands[f.i]
			cut := p.Cut
			p.Frame, p.Cut = f.prevFrame, f.prevCut
			if res.ok {
				return nil, iresult{v: p.LineResult(f.r, o, f.f, res.v, f.m0.Pos, nil), ok: true}, true
			}
			p.Reset(f.m0)
			if cut {
				return nil, iresult{}, true
			}
			f.i++
			f.state = nOperand
		}
	}
}

// operatorsRuleFrame parses an expression, absorbing only operators that bind tighter than binding level
// min (same steps as Parser.OperatorsRuleParse).
type operatorsRuleFrame struct {
	r        *core.Rule
	min      int
	state    int
	lhs      *value.Node
	lastNone int
	m0       core.Mark
	a        core.OperatorsRuleAttempt
}

const (
	pBegin = iota
	pNud
	pLoop
	pSkipped
	pLed
	pRHS
)

func (f *operatorsRuleFrame) next(mc *ivmMachine, vm *Program, res iresult) (iframe, iresult, bool) {
	p := mc.p
	pr := f.r.OperatorsRule
	for {
		switch f.state {
		case pBegin:
			f.state = pNud
			return mc.nudFrame(f.r), iresult{}, false
		case pNud:
			if !res.ok {
				return nil, iresult{}, true
			}
			f.lhs, f.lastNone = res.v, -1
			f.state = pLoop
		case pLoop:
			p.Spend()
			f.m0 = p.Mark()
			f.state = pSkipped
			if pr.SkipEntry >= 0 {
				return mc.skipFrame(pr), iresult{}, false
			}
		case pSkipped:
			f.state = pLed
			return mc.longestFrame(pr.Led), iresult{}, false
		case pLed:
			f.a = mc.attempt
			a := &f.a
			if a.Op == nil {
				p.Reset(f.m0)
				if res.cutFailed {
					return nil, iresult{}, true
				}
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			if a.Op.Level <= f.min || a.Op.Assoc == grammar.AssocNone && a.Op.Level == f.lastNone {
				p.Reset(f.m0)
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			p.Apply(a)
			if a.Op.Kind == grammar.Postfix {
				f.lhs = p.OperatorsRuleBuild(f.r, a, f.lhs, nil)
				f.lastNone = -1
				if p.Pos == f.m0.Pos { // as in Parser.OperatorsRuleParse
					return nil, iresult{v: f.lhs, ok: true}, true
				}
				f.state = pLoop
				continue
			}
			rmin := a.Op.Level
			if a.Op.Assoc == grammar.AssocRight {
				rmin--
			}
			f.state = pRHS
			return mc.operatorsRuleFrame(f.r, rmin), iresult{}, false
		case pRHS:
			a := &f.a
			if !res.ok {
				if a.Cut {
					return nil, iresult{}, true
				}
				p.Reset(f.m0)
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			f.lhs = p.OperatorsRuleBuild(f.r, a, f.lhs, res.v)
			f.lastNone = -1
			if a.Op.Assoc == grammar.AssocNone {
				f.lastNone = a.Op.Level
			}
			if p.Pos == f.m0.Pos { // as in Parser.OperatorsRuleParse
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			f.state = pLoop
		}
	}
}

// traceFrame is a traced rule call in the iterative-model VM: it reports the call, runs the frame
// that bodyFrame would have pushed for it, and reports the result.
type traceFrame struct {
	r       *core.Rule
	min     int
	started bool
}

func (f *traceFrame) next(mc *ivmMachine, vm *Program, res iresult) (iframe, iresult, bool) {
	p := mc.p
	if !f.started {
		f.started = true
		p.TraceEnter(f.r, f.min)
		return mc.callChild(f.r, f.min), iresult{}, false
	}
	p.TraceExit(res.ok)
	return nil, res, true
}
