package engine

import (
	"github.com/ornew/pego/grammar"

	"github.com/ornew/pego/internal/value"
)

// The iterative-model VM. It runs rule calls, left-recursion growth, Operators Rule expression loops, and
// longest matching as state machines pushed on the VM stack (a slice of iframe) instead of using
// host recursion. Each state machine calls the shared runtime used by the recursive model (the
// callBegin family, operatorsRuleBuild, and so on) in the same order, so the results match the recursive
// model. Action and predicate expressions are evaluated with host calls, because their depth is
// determined by the grammar (not by the input).

// ivmMachine is the state of the iterative-model VM for one parse: the VM state it runs code
// with, the pool of its frames and the result of the last longest match. It is the driver state
// of the VM (vmMachine.driver): the parser's backend state is the embedded vmMachine, whose
// driver points back here.
type ivmMachine struct {
	vmMachine
	pool    framePool            // frames of the iterative-model VM
	attempt operatorsRuleAttempt // result of the longestFrame that just finished
}

// ivmMachineOf returns the iterative-model VM state of the parse that p runs, making it on first
// use.
func ivmMachineOf(p *parser) *ivmMachine {
	if mc, ok := p.backend.(*vmMachine); ok {
		if im, ok := mc.driver.(*ivmMachine); ok {
			return im
		}
	}
	return newIVMMachine(p)
}

// newIVMMachine makes the iterative-model VM state of the parse that p runs. It takes over the
// stacks of the VM state that p has, such as the value stack a Document kept. The first rule body
// of a parse makes it, before any code holds the VM state that it replaces.
func newIVMMachine(p *parser) *ivmMachine {
	im := &ivmMachine{}
	if mc, ok := p.backend.(*vmMachine); ok {
		im.vmMachine = *mc
	}
	im.p, im.driver = p, im
	p.backend = &im.vmMachine
	return im
}

func (im *ivmMachine) reset() {
	im.pool, im.attempt = framePool{}, operatorsRuleAttempt{}
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
	next(mc *ivmMachine, vm *vmProgram, res iresult) (child iframe, out iresult, done bool)
}

// iterate puts root at the bottom of the stack and runs until it finishes.
func (mc *ivmMachine) iterate(vm *vmProgram, root iframe) iresult {
	p := mc.p
	stack := []iframe{root}
	var res iresult
	for {
		p.stepWork()
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
// captures, the body frame also does the steps of the call (invokePlain), so that such calls,
// the most common, push one frame instead of two.
type bodyFrame struct {
	b       vmBody
	block   bool // OperatorsRule section (reports a cut to the caller through p.cut)
	started bool
	call    bool        // a plain call of b.r: run the end of invokePlain when the body finishes
	inv     invokeState // call: the state saved by the call
	rec     int         // call: recovered errors before the call
}

func (f *bodyFrame) next(mc *ivmMachine, vm *vmProgram, res iresult) (iframe, iresult, bool) {
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
		if r.Plain && !p.noPlain {
			return mc.plainCallFrame(r, min), iresult{}, false
		}
		if p.tr != nil {
			return &traceFrame{r: r, min: min}, iresult{}, false
		}
		// What callChild does, written out on this hot path
		if !p.memoizes(r) || p.firstCall(r) {
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
		p.cut = true
	}
	if f.call { // the end of invokePlain
		r := f.b.r
		p.depth--
		p.cut = f.inv.prevCut
		p.trail = p.trail[:min2(f.inv.trail, len(p.trail))]
		if ok {
			v = p.finish(r, emptyFrame, v, f.inv.start)
		} else {
			p.pos = f.inv.start
			p.recovered = p.recovered[:f.rec]
		}
		p.env = f.inv.prevEnv
	}
	return nil, iresult{v: v, ok: ok}, true
}

// callChild returns the frame that runs a call of r (the same steps as call).
func (mc *ivmMachine) callChild(r *rule, min int) iframe {
	p := mc.p
	if !p.memoizes(r) || p.firstCall(r) {
		if len(r.Scope.Names) == 0 {
			return mc.plainCallFrame(r, min)
		}
		return mc.callFrame(r, min, false)
	}
	return mc.callFrame(r, min, true)
}

// plainCallFrame starts an unmemoized call of a rule without captures (the start of invokePlain)
// and returns the frame that runs its body.
func (mc *ivmMachine) plainCallFrame(r *rule, min int) *bodyFrame {
	p := mc.p
	f := mc.bodyFrame(r.entry, r, min, false)
	f.call = true
	f.inv = invokeState{prevEnv: p.env, prevCut: p.cut, trail: len(p.trail), start: p.pos}
	f.rec = len(p.recovered)
	p.cut = false
	p.depth++
	if p.depth > p.maxDepth {
		p.fail("nesting too deep: more than %d rule calls", p.maxDepth)
	}
	p.stats.Evaluated++
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

func (mc *ivmMachine) skipFrame(pr *operatorsRule) *skipFrame {
	f := reuse(&mc.pool.skips)
	*f = skipFrame{pr: pr}
	return f
}

func (mc *ivmMachine) longestFrame(ops []*operatorsRuleOp) *longestFrame {
	f := reuse(&mc.pool.longests)
	*f = longestFrame{ops: ops}
	return f
}

func (mc *ivmMachine) nudFrame(r *rule) *nudFrame {
	f := reuse(&mc.pool.nuds)
	*f = nudFrame{r: r}
	return f
}

func (mc *ivmMachine) operatorsRuleFrame(r *rule, min int) *operatorsRuleFrame {
	p := mc.p
	p.depth++
	if p.depth > p.maxDepth {
		p.fail("nesting too deep: more than %d rule calls", p.maxDepth)
	}
	f := reuse(&mc.pool.operatorsRules)
	*f = operatorsRuleFrame{r: r, min: min}
	return f
}

func (mc *ivmMachine) bodyFrame(entry int, r *rule, min int, block bool) *bodyFrame {
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
func (mc *ivmMachine) callFrame(r *rule, min int, memoized bool) *callFrame {
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
		f.g = growState{}
		mc.pool.calls = append(mc.pool.calls, f)
	case *skipFrame:
		mc.pool.skips = append(mc.pool.skips, f)
	case *longestFrame:
		mc.pool.longests = append(mc.pool.longests, f)
	case *nudFrame:
		mc.pool.nuds = append(mc.pool.nuds, f)
	case *operatorsRuleFrame:
		mc.p.depth--
		mc.pool.operatorsRules = append(mc.pool.operatorsRules, f)
	}
}

// callFrame is a rule call (same steps as parser.call).
type callFrame struct {
	r     *rule
	min   int
	state int
	// memoized is set for a memoized call (callBegin and callEnd); otherwise the call is not
	// memoized (unmemoized calls of rules without captures use plainCallFrame instead).
	memoized   bool
	st         callState
	g          growState
	inv        invokeState
	start, rec int // unmemoized calls: state to restore on failure
}

const (
	cBegin = iota
	cInvoked
)

func (f *callFrame) next(mc *ivmMachine, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	p := mc.p
	r := f.r
	switch f.state {
	case cBegin:
		if !f.memoized {
			f.start, f.rec = p.pos, len(p.recovered)
			return f.invoke(mc), iresult{}, false
		}
		st, v, ok, hit := p.callBegin(r, f.min)
		if hit {
			return nil, iresult{v: v, ok: ok}, true
		}
		f.st = st
		if r.Leader {
			f.g = p.growBegin(st.key)
		}
		return f.invoke(mc), iresult{}, false
	default: // cInvoked
		v, ok := p.invokeEnd(r, &f.inv, res.v, res.ok)
		if !f.memoized {
			if !ok {
				p.pos = f.start
				p.recovered = p.recovered[:f.rec]
			}
			return nil, iresult{v: v, ok: ok}, true
		}
		if r.Leader {
			if p.growStep(&f.g, v, ok) {
				return f.invoke(mc), iresult{}, false
			}
			v, ok = p.growEnd(r, &f.g)
		}
		v, ok = p.callEnd(r, &f.st, v, ok)
		return nil, iresult{v: v, ok: ok}, true
	}
}

// invoke starts evaluating the body (same steps as parser.invoke).
func (f *callFrame) invoke(mc *ivmMachine) iframe {
	p := mc.p
	f.state = cInvoked
	f.inv = p.invokeBegin(f.r)
	return mc.bodyFrame(f.r.entry, f.r, f.min, false)
}

// skipFrame is the skip of an Operators Rule expression (same steps as parser.skipTrivia).
type skipFrame struct {
	pr      *operatorsRule
	m       mark
	started bool
}

func (f *skipFrame) next(mc *ivmMachine, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	p := mc.p
	if f.pr.skipEntry < 0 {
		return nil, iresult{ok: true}, true
	}
	if !f.started {
		f.started = true
		f.m = p.mark()
		p.silent++
		return blockFrame(mc, f.pr.skipEntry), iresult{}, false
	}
	p.silent--
	if !res.ok {
		p.reset(f.m)
	}
	return nil, iresult{ok: true}, true
}

// longestFrame is the longest match over operator parts (same steps as parser.longest).
type longestFrame struct {
	ops       []*operatorsRuleOp
	i         int
	started   bool
	m0        mark
	best      operatorsRuleAttempt
	cutFailed bool
	prevFrame *frame
	prevCut   bool
	f         *frame
}

func (f *longestFrame) next(mc *ivmMachine, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	p := mc.p
	if !f.started {
		f.started = true
		f.m0 = p.mark()
	} else {
		op := f.ops[f.i]
		cut := p.cut
		p.frame, p.cut = f.prevFrame, f.prevCut
		if res.ok && p.pos == f.m0.pos && op.kind != grammar.Infix {
			res.ok = false // see parser.longest: empty prefix and postfix matches are not candidates
		}
		if res.ok && (f.best.op == nil || p.pos > f.best.end) {
			f.best = operatorsRuleAttempt{op: op, frame: f.f, v: res.v, start: f.m0.pos, end: p.pos, env: p.env, cut: cut,
				errs: appendWork(p, []*SyntaxError(nil), p.recovered[f.m0.recovered:])}
		}
		if !res.ok && cut {
			f.cutFailed = true
		}
		if len(f.f.vals) == 0 {
			p.reset(f.m0)
		} else {
			p.saved = appendWork(p, p.saved[:0], f.f.vals) // see parser.longest
			p.reset(f.m0)
			copyWork(p, f.f.vals, p.saved)
		}
		f.i++
	}
	if f.i == len(f.ops) {
		mc.attempt = f.best // passed through the parser to keep iresult small
		return nil, iresult{cutFailed: f.cutFailed}, true
	}
	f.prevFrame, f.prevCut = p.frame, p.cut
	f.f = p.newFrame(len(f.ops[f.i].line.scope.Names))
	p.frame, p.cut = f.f, false
	return blockFrame(mc, f.ops[f.i].line.entry), iresult{}, false
}

// nudFrame is the beginning of an expression (same steps as parser.operatorsRuleNud).
type nudFrame struct {
	r         *rule
	state     int
	m0        mark
	a         operatorsRuleAttempt
	i         int
	prevFrame *frame
	prevCut   bool
	f         *frame
}

const (
	nBegin = iota
	nSkipped
	nPrefix
	nRHS
	nOperand
	nOperandDone
)

func (f *nudFrame) next(mc *ivmMachine, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	p := mc.p
	pr := f.r.operatorsRule
	for {
		switch f.state {
		case nBegin:
			f.state = nSkipped
			if pr.skipEntry >= 0 {
				return mc.skipFrame(pr), iresult{}, false
			}
		case nSkipped:
			f.m0 = p.mark()
			f.state = nPrefix
			return mc.longestFrame(pr.prefix), iresult{}, false
		case nPrefix:
			f.a = mc.attempt
			if f.a.op == nil {
				if res.cutFailed {
					return nil, iresult{}, true
				}
				f.state = nOperand
				continue
			}
			p.apply(&f.a)
			f.state = nRHS
			return mc.operatorsRuleFrame(f.r, f.a.op.level), iresult{}, false
		case nRHS:
			if res.ok {
				return nil, iresult{v: p.operatorsRuleBuild(f.r, &f.a, nil, res.v), ok: true}, true
			}
			if f.a.cut {
				return nil, iresult{}, true
			}
			p.reset(f.m0)
			f.state = nOperand
		case nOperand:
			if f.i == len(pr.operands) {
				return nil, iresult{}, true
			}
			o := pr.operands[f.i]
			f.prevFrame, f.prevCut = p.frame, p.cut
			f.f = p.newFrame(len(o.scope.Names))
			p.frame, p.cut = f.f, false
			f.state = nOperandDone
			return blockFrame(mc, o.entry), iresult{}, false
		case nOperandDone:
			o := pr.operands[f.i]
			cut := p.cut
			p.frame, p.cut = f.prevFrame, f.prevCut
			if res.ok {
				return nil, iresult{v: p.lineResult(f.r, o, f.f, res.v, f.m0.pos, nil), ok: true}, true
			}
			p.reset(f.m0)
			if cut {
				return nil, iresult{}, true
			}
			f.i++
			f.state = nOperand
		}
	}
}

// operatorsRuleFrame parses an expression, absorbing only operators that bind tighter than binding level
// min (same steps as parser.operatorsRuleParse).
type operatorsRuleFrame struct {
	r        *rule
	min      int
	state    int
	lhs      *value.Node
	lastNone int
	m0       mark
	a        operatorsRuleAttempt
}

const (
	pBegin = iota
	pNud
	pLoop
	pSkipped
	pLed
	pRHS
)

func (f *operatorsRuleFrame) next(mc *ivmMachine, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	p := mc.p
	pr := f.r.operatorsRule
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
			f.m0 = p.mark()
			f.state = pSkipped
			if pr.skipEntry >= 0 {
				return mc.skipFrame(pr), iresult{}, false
			}
		case pSkipped:
			f.state = pLed
			return mc.longestFrame(pr.led), iresult{}, false
		case pLed:
			f.a = mc.attempt
			a := &f.a
			if a.op == nil {
				p.reset(f.m0)
				if res.cutFailed {
					return nil, iresult{}, true
				}
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			if a.op.level <= f.min || a.op.assoc == grammar.AssocNone && a.op.level == f.lastNone {
				p.reset(f.m0)
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			p.apply(a)
			if a.op.kind == grammar.Postfix {
				f.lhs = p.operatorsRuleBuild(f.r, a, f.lhs, nil)
				f.lastNone = -1
				if p.pos == f.m0.pos { // as in parser.operatorsRuleParse
					return nil, iresult{v: f.lhs, ok: true}, true
				}
				f.state = pLoop
				continue
			}
			rmin := a.op.level
			if a.op.assoc == grammar.AssocRight {
				rmin--
			}
			f.state = pRHS
			return mc.operatorsRuleFrame(f.r, rmin), iresult{}, false
		case pRHS:
			a := &f.a
			if !res.ok {
				if a.cut {
					return nil, iresult{}, true
				}
				p.reset(f.m0)
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			f.lhs = p.operatorsRuleBuild(f.r, a, f.lhs, res.v)
			f.lastNone = -1
			if a.op.assoc == grammar.AssocNone {
				f.lastNone = a.op.level
			}
			if p.pos == f.m0.pos { // as in parser.operatorsRuleParse
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			f.state = pLoop
		}
	}
}

// traceFrame is a traced rule call in the iterative-model VM: it reports the call, runs the frame
// that bodyFrame would have pushed for it, and reports the result.
type traceFrame struct {
	r       *rule
	min     int
	started bool
}

func (f *traceFrame) next(mc *ivmMachine, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	p := mc.p
	if !f.started {
		f.started = true
		p.traceEnter(f.r, f.min)
		return mc.callChild(f.r, f.min), iresult{}, false
	}
	p.traceExit(res.ok)
	return nil, res, true
}
