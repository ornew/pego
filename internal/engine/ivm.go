package engine

import "github.com/ornew/pego/grammar"

// The iterative-model VM. It runs rule calls, left-recursion growth, Pratt expression loops, and
// longest matching as state machines pushed on the VM stack (a slice of iframe) instead of using
// host recursion. Each state machine calls the shared runtime used by the recursive model (the
// callBegin family, prattBuild, and so on) in the same order, so the results match the recursive
// model. Action and predicate expressions are evaluated with host calls, because their depth is
// determined by the grammar (not by the input).

// iresult is the result of running a child.
type iresult struct {
	v         *Node
	ok        bool
	cutFailed bool // result of longestFrame, whose attempt is in parser.attempt
}

// iframe is an element of the VM stack. next receives the child's result res (empty on the first
// call) and either returns the next child to run, or sets done and returns its own result.
type iframe interface {
	next(p *parser, vm *vmProgram, res iresult) (child iframe, out iresult, done bool)
}

// iterate puts root at the bottom of the stack and runs until it finishes.
func (p *parser) iterate(vm *vmProgram, root iframe) iresult {
	stack := []iframe{root}
	var res iresult
	for {
		child, out, done := stack[len(stack)-1].next(p, vm, res)
		if !done {
			stack = append(stack, child)
			res = iresult{}
			continue
		}
		p.release(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if len(stack) == 0 {
			return out
		}
		res = out
	}
}

// bodyFrame runs code (a rule body or a Pratt section). For an unmemoized call of a rule without
// captures, the body frame also does the steps of the call (invokePlain), so that such calls,
// the most common, push one frame instead of two.
type bodyFrame struct {
	b       vmBody
	block   bool // Pratt section (reports a cut to the caller through p.cut)
	started bool
	call    bool        // a plain call of b.r: run the end of invokePlain when the body finishes
	inv     invokeState // call: the state saved by the call
	rec     int         // call: recovered errors before the call
}

func (f *bodyFrame) next(p *parser, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	var ev int
	var v *Node
	var ok bool
	if !f.started {
		f.started = true
		ev, v, ok = p.step(vm, &f.b, false, false, nil)
	} else {
		ev, v, ok = p.step(vm, &f.b, true, res.ok, res.v)
	}
	switch ev {
	case evCall:
		in := &vm.m.Code[f.b.ip]
		r, min := vm.rules[in.A], int(in.B)
		if !p.memoizes(r) || p.firstCall(r) {
			if len(r.scope.names) == 0 {
				return p.plainCallFrame(r, min), iresult{}, false
			}
			return p.callFrame(r, min, false), iresult{}, false
		}
		return p.callFrame(r, min, true), iresult{}, false
	case evPratt:
		return p.prattFrame(f.b.r, f.b.min), iresult{}, false
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

// plainCallFrame starts an unmemoized call of a rule without captures (the start of invokePlain)
// and returns the frame that runs its body.
func (p *parser) plainCallFrame(r *rule, min int) *bodyFrame {
	f := p.bodyFrame(r.entry, r, min, false)
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

func blockFrame(p *parser, entry int) *bodyFrame {
	return p.bodyFrame(entry, nil, 0, true)
}

// Frames are reused after being popped, so pushing a frame does not allocate in steady state.
type framePool struct {
	bodies   []*bodyFrame
	calls    []*callFrame
	skips    []*skipFrame
	longests []*longestFrame
	nuds     []*nudFrame
	pratts   []*prattFrame
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

func (p *parser) skipFrame(pr *pratt) *skipFrame {
	f := reuse(&p.pool.skips)
	*f = skipFrame{pr: pr}
	return f
}

func (p *parser) longestFrame(ops []*prattOp) *longestFrame {
	f := reuse(&p.pool.longests)
	*f = longestFrame{ops: ops}
	return f
}

func (p *parser) nudFrame(r *rule) *nudFrame {
	f := reuse(&p.pool.nuds)
	*f = nudFrame{r: r}
	return f
}

func (p *parser) prattFrame(r *rule, min int) *prattFrame {
	f := reuse(&p.pool.pratts)
	*f = prattFrame{r: r, min: min}
	return f
}

func (p *parser) bodyFrame(entry int, r *rule, min int, block bool) *bodyFrame {
	var f *bodyFrame
	if n := len(p.pool.bodies); n > 0 {
		f = p.pool.bodies[n-1]
		p.pool.bodies = p.pool.bodies[:n-1]
	} else {
		f = &bodyFrame{}
	}
	*f = bodyFrame{b: p.newBody(entry, r, min), block: block}
	return f
}

// callFrame returns the frame of a call of r, memoized or not (the caller has decided, calling
// firstCall once).
func (p *parser) callFrame(r *rule, min int, memoized bool) *callFrame {
	var f *callFrame
	if n := len(p.pool.calls); n > 0 {
		f = p.pool.calls[n-1]
		p.pool.calls = p.pool.calls[:n-1]
	} else {
		f = &callFrame{}
	}
	*f = callFrame{r: r, min: min, memoized: memoized}
	return f
}

// release makes a finished frame available for reuse.
func (p *parser) release(f iframe) {
	switch f := f.(type) {
	case *bodyFrame:
		p.pool.bodies = append(p.pool.bodies, f)
	case *callFrame:
		f.g = growState{}
		p.pool.calls = append(p.pool.calls, f)
	case *skipFrame:
		p.pool.skips = append(p.pool.skips, f)
	case *longestFrame:
		p.pool.longests = append(p.pool.longests, f)
	case *nudFrame:
		p.pool.nuds = append(p.pool.nuds, f)
	case *prattFrame:
		p.pool.pratts = append(p.pool.pratts, f)
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

func (f *callFrame) next(p *parser, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	r := f.r
	switch f.state {
	case cBegin:
		if !f.memoized {
			f.start, f.rec = p.pos, len(p.recovered)
			return f.invoke(p), iresult{}, false
		}
		st, v, ok, hit := p.callBegin(r, f.min)
		if hit {
			return nil, iresult{v: v, ok: ok}, true
		}
		f.st = st
		if r.leader {
			f.g = p.growBegin(st.key)
		}
		return f.invoke(p), iresult{}, false
	default: // cInvoked
		v, ok := p.invokeEnd(r, &f.inv, res.v, res.ok)
		if !f.memoized {
			if !ok {
				p.pos = f.start
				p.recovered = p.recovered[:f.rec]
			}
			return nil, iresult{v: v, ok: ok}, true
		}
		if r.leader {
			if p.growStep(&f.g, v, ok) {
				return f.invoke(p), iresult{}, false
			}
			v, ok = p.growEnd(r, &f.g)
		}
		v, ok = p.callEnd(r, &f.st, v, ok)
		return nil, iresult{v: v, ok: ok}, true
	}
}

// invoke starts evaluating the body (same steps as parser.invoke).
func (f *callFrame) invoke(p *parser) iframe {
	f.state = cInvoked
	f.inv = p.invokeBegin(f.r)
	return p.bodyFrame(f.r.entry, f.r, f.min, false)
}

// skipFrame is the skip of a Pratt expression (same steps as parser.skipTrivia).
type skipFrame struct {
	pr      *pratt
	m       mark
	started bool
}

func (f *skipFrame) next(p *parser, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	if f.pr.skipEntry < 0 {
		return nil, iresult{ok: true}, true
	}
	if !f.started {
		f.started = true
		f.m = p.mark()
		p.silent++
		return blockFrame(p, f.pr.skipEntry), iresult{}, false
	}
	p.silent--
	if !res.ok {
		p.reset(f.m)
	}
	return nil, iresult{ok: true}, true
}

// longestFrame is the longest match over operator parts (same steps as parser.longest).
type longestFrame struct {
	ops       []*prattOp
	i         int
	started   bool
	m0        mark
	best      prattAttempt
	cutFailed bool
	prevFrame *frame
	prevCut   bool
	f         *frame
}

func (f *longestFrame) next(p *parser, vm *vmProgram, res iresult) (iframe, iresult, bool) {
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
			f.best = prattAttempt{op: op, frame: f.f, v: res.v, start: f.m0.pos, end: p.pos, env: p.env, cut: cut,
				errs: append([]*SyntaxError(nil), p.recovered[f.m0.recovered:]...)}
		}
		if !res.ok && cut {
			f.cutFailed = true
		}
		if len(f.f.vals) == 0 {
			p.reset(f.m0)
		} else {
			p.saved = append(p.saved[:0], f.f.vals...) // see parser.longest
			p.reset(f.m0)
			copy(f.f.vals, p.saved)
		}
		f.i++
	}
	if f.i == len(f.ops) {
		p.attempt = f.best // passed through the parser to keep iresult small
		return nil, iresult{cutFailed: f.cutFailed}, true
	}
	f.prevFrame, f.prevCut = p.frame, p.cut
	f.f = p.newFrame(len(f.ops[f.i].line.scope.names))
	p.frame, p.cut = f.f, false
	return blockFrame(p, f.ops[f.i].line.entry), iresult{}, false
}

// nudFrame is the beginning of an expression (same steps as parser.prattNud).
type nudFrame struct {
	r         *rule
	state     int
	m0        mark
	a         prattAttempt
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

func (f *nudFrame) next(p *parser, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	pr := f.r.pratt
	for {
		switch f.state {
		case nBegin:
			f.state = nSkipped
			if pr.skipEntry >= 0 {
				return p.skipFrame(pr), iresult{}, false
			}
		case nSkipped:
			f.m0 = p.mark()
			f.state = nPrefix
			return p.longestFrame(pr.prefix), iresult{}, false
		case nPrefix:
			f.a = p.attempt
			if f.a.op == nil {
				f.state = nOperand
				continue
			}
			p.apply(&f.a)
			f.state = nRHS
			return p.prattFrame(f.r, f.a.op.level), iresult{}, false
		case nRHS:
			if res.ok {
				return nil, iresult{v: p.prattBuild(f.r, &f.a, nil, res.v), ok: true}, true
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
			f.f = p.newFrame(len(o.scope.names))
			p.frame, p.cut = f.f, false
			f.state = nOperandDone
			return blockFrame(p, o.entry), iresult{}, false
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

// prattFrame parses an expression, absorbing only operators that bind tighter than binding level
// min (same steps as parser.prattParse).
type prattFrame struct {
	r        *rule
	min      int
	state    int
	lhs      *Node
	lastNone int
	m0       mark
	a        prattAttempt
}

const (
	pBegin = iota
	pNud
	pLoop
	pSkipped
	pLed
	pRHS
)

func (f *prattFrame) next(p *parser, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	pr := f.r.pratt
	for {
		switch f.state {
		case pBegin:
			f.state = pNud
			return p.nudFrame(f.r), iresult{}, false
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
				return p.skipFrame(pr), iresult{}, false
			}
		case pSkipped:
			f.state = pLed
			return p.longestFrame(pr.led), iresult{}, false
		case pLed:
			f.a = p.attempt
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
				f.lhs = p.prattBuild(f.r, a, f.lhs, nil)
				f.lastNone = -1
				f.state = pLoop
				continue
			}
			rmin := a.op.level
			if a.op.assoc == grammar.AssocRight {
				rmin--
			}
			f.state = pRHS
			return p.prattFrame(f.r, rmin), iresult{}, false
		case pRHS:
			a := &f.a
			if !res.ok {
				if a.cut {
					return nil, iresult{}, true
				}
				p.reset(f.m0)
				return nil, iresult{v: f.lhs, ok: true}, true
			}
			f.lhs = p.prattBuild(f.r, a, f.lhs, res.v)
			f.lastNone = -1
			if a.op.assoc == grammar.AssocNone {
				f.lastNone = a.op.level
			}
			f.state = pLoop
		}
	}
}
