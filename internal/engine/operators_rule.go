package engine

import (
	"github.com/ornew/pego/grammar"
)

// operatorsRule is a compiled Operators Rule expression. Binding levels are numbered from 1 in declaration
// order (higher binds tighter).
type operatorsRule struct {
	skip      matcher
	skipEntry int // start of the skip bytecode (bytecode backend only; -1 if none)
	operands  []*operatorsRuleLine
	prefix    []*operatorsRuleOp
	led       []*operatorsRuleOp // infix and postfix
}

// operatorsRuleLine is an operand line or an operator line. Each has its own capture scope.
type operatorsRuleLine struct {
	scope *scope
	m     matcher
	act   evaluator // action (nil if none)
	// value makes the value of an operand line without an action, and single that of an operator
	// line's operator, from the value of the line's expression.
	value  func(p *parser, v *Node, start int) *Node
	single singleMode
	shape  shaper
	entry  int // start of the bytecode (bytecode backend only)
	// key is the line's *grammar.OperatorsRuleOperand or *grammar.OperatorsRuleOperator.
	key any
}

type operatorsRuleOp struct {
	id    int
	kind  string
	assoc string
	level int
	line  *operatorsRuleLine
}

func (c *compiler) compileOperatorsRule(r *rule, e *grammar.OperatorsRule) {
	pr := &operatorsRule{}
	r.operatorsRule = pr
	if e.Skip != nil {
		c.noCaptures(e.Skip)
		pr.skip = c.expr(e.Skip, r.scope, false)
	}
	if len(e.Operands) == 0 {
		c.errorf(e.Diagnostic(), "operatorsRule needs at least one operand")
	}
	for _, o := range e.Operands {
		// A loaded compiled grammar (c.an is nil) has already been checked.
		if c.an != nil && contains(c.an.leftCalls(o.Expr, nil), r.name) {
			c.errorf(e.Diagnostic(), "operand of %s calls %s at its start (left recursion); use infix or postfix instead", r.name, r.name)
		}
		pr.operands = append(pr.operands, c.operatorsRuleLine(o.Expr, o.Action, nil, o))
	}
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			var locals []string
			switch op.Kind {
			case grammar.Prefix:
				locals = []string{"rhs", "op"}
			case grammar.Postfix:
				locals = []string{"lhs", "op"}
			case grammar.Infix:
				locals = []string{"lhs", "rhs", "op"}
				if op.Assoc != grammar.AssocLeft && op.Assoc != grammar.AssocRight && op.Assoc != grammar.AssocNone {
					c.errorf(op.Diagnostic(), "invalid associativity %q", op.Assoc)
				}
			default:
				c.errorf(op.Diagnostic(), "invalid operator kind %q", op.Kind)
				continue
			}
			line := c.operatorsRuleLine(op.Expr, op.Action, locals, op)
			for _, name := range line.scope.names {
				if name == "lhs" || name == "rhs" || name == "op" {
					c.errorf(op.Diagnostic(), "capture name %s is reserved in operatorsRule operators", name)
				}
			}
			o := &operatorsRuleOp{id: id, kind: op.Kind, assoc: op.Assoc, level: i + 1, line: line}
			id++
			if op.Kind == grammar.Prefix {
				pr.prefix = append(pr.prefix, o)
			} else {
				pr.led = append(pr.led, o)
			}
		}
	}
	seen := map[string]bool{}
	for _, l := range e.Levels {
		if l.Name != "" {
			if seen[l.Name] {
				c.errorf(e.Diagnostic(), "duplicate level name %s", l.Name)
			}
			seen[l.Name] = true
		}
	}
	r.body = func(p *parser, min int) (*Node, bool) { return p.operatorsRuleParse(r, min) }
}

// leanLine reports whether the value of an Operators Rule line is unused: the line is an operand
// line with an action, which reads captures only. An operator line's value is $op or its element.
// The bytecode compiler uses the same test.
func leanLine(action grammar.Term, locals []string) bool {
	return action != nil && locals == nil
}

func (c *compiler) operatorsRuleLine(e grammar.Expr, action grammar.Term, locals []string, key any) *operatorsRuleLine {
	s := newScope()
	novalue := c.rule.novalue || leanLine(action, locals)
	l := &operatorsRuleLine{scope: s, m: c.expr(e, s, !novalue), key: key}
	l.value, l.single, l.shape = scopeValueOf(c.prog, e), singleModeOf(c.prog, e), shaperOf(c.prog, e)
	if action != nil && !c.rule.novalue {
		l.act = termEvaluator(action)
		c.checkTerm(action, s, locals)
	}
	return l
}

// operatorsRuleAttempt is the result of trying an operator part.
type operatorsRuleAttempt struct {
	op         *operatorsRuleOp
	frame      *frame
	v          *Node
	start, end int
	env        *env
	cut        bool
	errs       []*SyntaxError // syntax errors recovered in this candidate
	missing    []missingTokenEvent
}

func (p *parser) skipTrivia(pr *operatorsRule) {
	if pr.skip == nil {
		return
	}
	m := p.mark()
	p.silent++
	_, ok := pr.skip(p)
	p.silent--
	if !ok {
		p.reset(m)
	}
}

// longest tries every operator part and returns the longest match (the one declared first on a
// tie); best.op is nil when none matched. cutFailed reports that some candidate failed after passing
// a cut. The attempt is returned by value and captures are saved in one buffer per parser, so trying
// operators allocates nothing for lines without captures.
func (p *parser) longest(ops []*operatorsRuleOp) (best operatorsRuleAttempt, cutFailed bool) {
	m0 := p.mark()
	for _, op := range ops {
		prevFrame, prevCut := p.frame, p.cut
		f := p.newFrame(len(op.line.scope.names))
		p.frame, p.cut = f, false
		v, ok := op.line.m(p)
		cut := p.cut
		p.frame, p.cut = prevFrame, prevCut
		if ok && p.pos == m0.pos && op.kind != grammar.Infix {
			// A prefix or postfix operator that consumes nothing could be applied forever,
			// so an empty match does not count as a candidate.
			ok = false
		}
		if ok && (best.op == nil || p.pos > best.end) {
			best = operatorsRuleAttempt{op: op, frame: f, v: v, start: m0.pos, end: p.pos, env: p.env, cut: cut,
				errs: appendWork(p, []*SyntaxError(nil), p.recovered[m0.recovered:]), missing: p.missingEvents(m0.missing)}
		}
		if !ok && cut {
			cutFailed = true
		}
		if len(f.vals) == 0 {
			p.reset(m0)
			continue
		}
		// reset undoes this candidate's captures; restore them. Nothing is parsed between saving
		// and restoring, so one buffer per parser suffices.
		p.saved = appendWork(p, p.saved[:0], f.vals)
		p.reset(m0)
		copyWork(p, f.vals, p.saved)
	}
	return best, cutFailed
}

func (p *parser) apply(a *operatorsRuleAttempt) {
	p.pos, p.env = a.end, a.env
	if p.missing != nil || len(a.errs) != 0 {
		p.applyRecovered(a)
	}
}

// applyRecovered replays the recovery errors, and under missing-token support the missing-token
// events, of an applied operator. It is separate so that apply stays inlinable: the common strict
// parse has neither.
func (p *parser) applyRecovered(a *operatorsRuleAttempt) {
	if p.missing == nil {
		p.recovered = appendWork(p, p.recovered, a.errs)
	} else {
		p.restoreMissing(a.missing, a.errs, false)
	}
}

// operatorsRuleParse parses an expression, absorbing only operators that bind tighter than binding level
// min. The operand of a prefix operator and the right operand of an infix operator are parsed by
// recursion, so each level counts as a rule call against the nesting limit: a chain of prefix or
// right-associative operators would otherwise nest without bound on the goroutine stack.
func (p *parser) operatorsRuleParse(r *rule, min int) (*Node, bool) {
	p.depth++
	if p.depth > p.maxDepth {
		p.fail("nesting too deep: more than %d rule calls", p.maxDepth)
	}
	v, ok := p.operatorsRuleExpr(r, min)
	p.depth--
	return v, ok
}

// operatorsRuleExpr is the body of operatorsRuleParse.
func (p *parser) operatorsRuleExpr(r *rule, min int) (*Node, bool) {
	pr := r.operatorsRule
	lhs, ok := p.operatorsRuleNud(r)
	if !ok {
		return nil, false
	}
	lastNone := -1
	for {
		m0 := p.mark()
		p.skipTrivia(pr)
		at, cutFailed := p.longest(pr.led)
		a := &at
		if a.op == nil {
			p.reset(m0)
			if cutFailed {
				return nil, false
			}
			break
		}
		if a.op.level <= min || a.op.assoc == grammar.AssocNone && a.op.level == lastNone {
			p.reset(m0)
			break
		}
		p.apply(a)
		if a.op.kind == grammar.Postfix {
			lhs = p.operatorsRuleBuild(r, a, lhs, nil)
			lastNone = -1
			if p.pos == m0.pos {
				break
			}
			continue
		}
		rmin := a.op.level
		if a.op.assoc == grammar.AssocRight {
			rmin--
		}
		rhs, ok := p.operatorsRuleParse(r, rmin)
		if !ok {
			if a.cut {
				return nil, false
			}
			p.reset(m0)
			break
		}
		lhs = p.operatorsRuleBuild(r, a, lhs, rhs)
		lastNone = -1
		if a.op.assoc == grammar.AssocNone {
			lastNone = a.op.level
		}
		// An application that consumed no input ends the loop, as an empty iteration ends a repetition:
		// otherwise an operator that can match empty after an operand that can loops forever.
		if p.pos == m0.pos {
			break
		}
	}
	return lhs, true
}

// operatorsRuleNud parses the beginning of an expression (a prefix operator or an operand).
func (p *parser) operatorsRuleNud(r *rule) (*Node, bool) {
	pr := r.operatorsRule
	p.skipTrivia(pr)
	m0 := p.mark()
	if at, cutFailed := p.longest(pr.prefix); at.op != nil {
		a := &at
		p.apply(a)
		rhs, ok := p.operatorsRuleParse(r, a.op.level)
		if ok {
			return p.operatorsRuleBuild(r, a, nil, rhs), true
		}
		if a.cut {
			return nil, false
		}
		p.reset(m0)
	} else if cutFailed {
		return nil, false
	}
	for _, o := range pr.operands {
		prevFrame, prevCut := p.frame, p.cut
		f := p.newFrame(len(o.scope.names))
		p.frame, p.cut = f, false
		v, ok := o.m(p)
		cut := p.cut
		p.frame, p.cut = prevFrame, prevCut
		if ok {
			return p.lineResult(r, o, f, v, m0.pos, nil), true
		}
		p.reset(m0)
		if cut {
			return nil, false
		}
	}
	return nil, false
}

// lineResult builds the value of an operand line, and checks it against the rule's declared type
// if needed.
func (p *parser) lineResult(r *rule, l *operatorsRuleLine, f *frame, v *Node, start int, locals *local) *Node {
	if r.novalue {
		return nil
	}
	if l.act != nil {
		ctx := p.useCtx(evalCtx{p: p, scope: l.scope, frame: f, start: start, end: p.pos, locals: locals, cbase: len(p.created)})
		v = p.actionResult(ctx, l.act, r.whereName())
	} else {
		v = l.value(p, v, start)
		if len(l.scope.names) > 0 {
			v = p.attachCaptures(v, l.scope, f, start, p.pos)
		}
		if v != nil && v.fresh {
			if p.prog.sourceInfo && v.kind.rule == "" {
				v.kind = r.kinds.named(v.kind, r.name)
			}
			v.fresh = false
		}
	}
	return v
}

// convertOperator converts v, the value of an operator of rule r, to the named node type of the
// rule's operators.
func (p *parser) convertOperator(r *rule, v *Node) *Node {
	n, ok := r.opKinds.convert(unboxNode(v))
	if !ok {
		p.fail("cannot convert %s to %s in result of rule %s", typeName(unboxNode(v)), r.opNodeType, r.whereName())
	}
	return n
}

// operatorsRuleBuild builds the value of an operator application.
func (p *parser) operatorsRuleBuild(r *rule, a *operatorsRuleAttempt, lhs, rhs *Node) *Node {
	if r.novalue {
		return nil
	}
	start, end := a.start, p.pos
	if lhs != nil {
		start = int(lhs.start)
	}
	if a.op.kind == grammar.Postfix {
		end = a.end
	}
	l := a.op.line
	// The operator's value is its one element, or the node of its tuple, which has the line's
	// captures.
	opv, ok := l.shape.single(p, l.single, a.v, a.start)
	if !ok {
		opv = p.newNode(Node{kind: kindSeq, start: int32(a.start), end: int32(a.end)})
	}
	if l.single == singleTuple && len(l.scope.names) > 0 {
		opv = p.attachCaptures(opv, l.scope, a.frame, a.start, a.end)
	}
	if opv != nil {
		opv.fresh = false
	}
	if l.act != nil {
		// The locals live in the parser, like the evaluation context (see useCtx).
		ls := &p.oplocals
		ls[0] = local{name: "op", val: nodeOrNil(opv)}
		locals := &ls[0]
		if a.op.kind != grammar.Prefix {
			ls[1] = local{name: "lhs", val: nodeOrNil(lhs), next: locals}
			locals = &ls[1]
		}
		if a.op.kind != grammar.Postfix {
			ls[2] = local{name: "rhs", val: nodeOrNil(rhs), next: locals}
			locals = &ls[2]
		}
		ctx := p.useCtx(evalCtx{p: p, scope: l.scope, frame: a.frame, start: start, end: end, locals: locals, cbase: len(p.created)})
		v := p.actionResult(ctx, l.act, r.whereName())
		if r.opKinds != nil {
			v = p.convertOperator(r, v)
		}
		return v
	}
	// Without an action, the application is the node of the tuple (lhs, op, rhs) of the line's
	// kind, with those captures.
	var kids []*Node
	var fields Fields
	switch a.op.kind {
	case grammar.Prefix:
		kids = p.nodes(2)
		kids[0], kids[1] = opv, rhs
		fields = Fields{{"op", nodeOrNil(opv)}, {"rhs", nodeOrNil(rhs)}}
	case grammar.Postfix:
		kids = p.nodes(2)
		kids[0], kids[1] = lhs, opv
		fields = Fields{{"lhs", nodeOrNil(lhs)}, {"op", nodeOrNil(opv)}}
	default:
		kids = p.nodes(3)
		kids[0], kids[1], kids[2] = lhs, opv, rhs
		fields = Fields{{"lhs", nodeOrNil(lhs)}, {"op", nodeOrNil(opv)}, {"rhs", nodeOrNil(rhs)}}
	}
	kind := kindSeq
	if r.opKinds != nil {
		kind = r.opKinds[baseTuple] // made with the named node type of the operators
	} else if p.prog.sourceInfo {
		kind = r.kinds.named(kindSeq, r.name)
	}
	n := p.newNode(Node{kind: kind, start: int32(start), end: int32(end), children: kids, fields: fields, labeled: p.prog.sourceInfo})
	return n
}
