package engine

import (
	"github.com/ornew/pego/grammar"
)

// pratt is a compiled Pratt expression. Binding levels are numbered from 1 in declaration
// order (higher binds tighter).
type pratt struct {
	skip      matcher
	skipEntry int // start of the skip bytecode (bytecode backend only; -1 if none)
	operands  []*prattLine
	prefix    []*prattOp
	led       []*prattOp // infix and postfix
}

// prattLine is an operand line or an operator line. Each has its own capture scope.
type prattLine struct {
	scope *scope
	m     matcher
	act   evaluator // action (nil if none)
	isSeq bool
	entry int // start of the bytecode (bytecode backend only)
}

type prattOp struct {
	id    int
	kind  string
	assoc string
	level int
	line  *prattLine
}

func (c *compiler) compilePratt(r *rule, e *grammar.Pratt) {
	pr := &pratt{}
	r.pratt = pr
	if e.Skip != nil {
		c.noCaptures(e.Skip)
		pr.skip = c.expr(e.Skip, r.scope, false)
	}
	if len(e.Operands) == 0 {
		c.errorf(e.Pos, "pratt needs at least one operand")
	}
	for _, o := range e.Operands {
		// A loaded compiled grammar (c.an is nil) has already been checked.
		if c.an != nil && contains(c.an.leftCalls(o.Expr, nil), r.name) {
			c.errorf(e.Pos, "operand of %s calls %s at its start (left recursion); use infix or postfix instead", r.name, r.name)
		}
		pr.operands = append(pr.operands, c.prattLine(o.Expr, o.Action, nil))
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
					c.errorf(op.Pos, "invalid associativity %q", op.Assoc)
				}
			default:
				c.errorf(op.Pos, "invalid operator kind %q", op.Kind)
				continue
			}
			line := c.prattLine(op.Expr, op.Action, locals)
			for _, name := range line.scope.names {
				if name == "lhs" || name == "rhs" || name == "op" {
					c.errorf(op.Pos, "capture name %s is reserved in pratt operators", name)
				}
			}
			o := &prattOp{id: id, kind: op.Kind, assoc: op.Assoc, level: i + 1, line: line}
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
				c.errorf(e.Pos, "duplicate level name %s", l.Name)
			}
			seen[l.Name] = true
		}
	}
	r.body = func(p *parser, min int) (*Node, bool) { return p.prattParse(r, min) }
}

// leanLine reports whether the value of a Pratt line is unused. The line's value is not used by
// operator lines with an action ($op is built from the range and $n is unavailable) or by operand
// lines whose action does not use $n. The bytecode compiler uses the same test.
func leanLine(action grammar.Term, locals []string) bool {
	if action == nil {
		return false
	}
	if locals != nil {
		return true
	}
	uses := false
	walkTerm(action, func(t grammar.Term) {
		if _, ok := t.(*grammar.IndexRef); ok {
			uses = true
		}
	})
	return !uses
}

func (c *compiler) prattLine(e grammar.Expr, action grammar.Term, locals []string) *prattLine {
	s := newScope()
	novalue := c.rule.novalue || leanLine(action, locals)
	l := &prattLine{scope: s, m: c.expr(e, s, !novalue)}
	_, l.isSeq = e.(*grammar.Seq)
	if action != nil && !c.rule.novalue {
		l.act = termEvaluator(action)
		if locals != nil {
			c.noIndex = "pratt operator actions; use captures, $lhs, $rhs, or $op"
		}
		c.checkTerm(action, s, locals)
		c.noIndex = ""
	}
	return l
}

// prattAttempt is the result of trying an operator part.
type prattAttempt struct {
	op         *prattOp
	frame      *frame
	v          *Node
	start, end int
	env        *env
	cut        bool
	errs       []*SyntaxError // syntax errors recovered in this candidate
}

func (p *parser) skipTrivia(pr *pratt) {
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
func (p *parser) longest(ops []*prattOp) (best prattAttempt, cutFailed bool) {
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
			best = prattAttempt{op: op, frame: f, v: v, start: m0.pos, end: p.pos, env: p.env, cut: cut,
				errs: append([]*SyntaxError(nil), p.recovered[m0.recovered:]...)}
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
		p.saved = append(p.saved[:0], f.vals...)
		p.reset(m0)
		copy(f.vals, p.saved)
	}
	return best, cutFailed
}

func (p *parser) apply(a *prattAttempt) {
	p.pos, p.env = a.end, a.env
	p.recovered = append(p.recovered, a.errs...)
}

// prattParse parses an expression, absorbing only operators that bind tighter than binding level
// min.
func (p *parser) prattParse(r *rule, min int) (*Node, bool) {
	pr := r.pratt
	lhs, ok := p.prattNud(r)
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
			lhs = p.prattBuild(r, a, lhs, nil)
			lastNone = -1
			continue
		}
		rmin := a.op.level
		if a.op.assoc == grammar.AssocRight {
			rmin--
		}
		rhs, ok := p.prattParse(r, rmin)
		if !ok {
			if a.cut {
				return nil, false
			}
			p.reset(m0)
			break
		}
		lhs = p.prattBuild(r, a, lhs, rhs)
		lastNone = -1
		if a.op.assoc == grammar.AssocNone {
			lastNone = a.op.level
		}
	}
	return lhs, true
}

// prattNud parses the beginning of an expression (a prefix operator or an operand).
func (p *parser) prattNud(r *rule) (*Node, bool) {
	pr := r.pratt
	p.skipTrivia(pr)
	m0 := p.mark()
	if at, _ := p.longest(pr.prefix); at.op != nil {
		a := &at
		p.apply(a)
		rhs, ok := p.prattParse(r, a.op.level)
		if ok {
			return p.prattBuild(r, a, nil, rhs), true
		}
		if a.cut {
			return nil, false
		}
		p.reset(m0)
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

// lineResult builds the value of an operand line.
func (p *parser) lineResult(r *rule, l *prattLine, f *frame, v *Node, start int, locals *local) *Node {
	if r.novalue {
		return nil
	}
	if l.act != nil {
		items := p.one(v)
		if l.isSeq && v != nil {
			items = v.Children
		}
		ctx := p.useCtx(evalCtx{p: p, scope: l.scope, frame: f, items: items, start: start, end: p.pos, locals: locals, cbase: len(p.created)})
		return p.actionResult(ctx, l.act, r.name)
	}
	if len(l.scope.names) > 0 {
		v = p.attachCaptures(v, l.scope, f, start, p.pos)
	}
	if v != nil && v.fresh {
		if v.Rule == "" {
			v.Rule = r.name
		}
		v.fresh = false
	}
	return v
}

// prattBuild builds the value of an operator application.
func (p *parser) prattBuild(r *rule, a *prattAttempt, lhs, rhs *Node) *Node {
	if r.novalue {
		return nil
	}
	start, end := a.start, p.pos
	if lhs != nil {
		start = lhs.Start
	}
	if a.op.kind == grammar.Postfix {
		end = a.end
	}
	l := a.op.line
	if l.act != nil {
		op := p.newNode(Node{Type: TypeMatch, Start: a.start, End: a.end, Text: p.text(a.start, a.end), terminal: true})
		// The locals live in the parser, like the evaluation context (see useCtx).
		ls := &p.oplocals
		ls[0] = local{name: "op", val: op}
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
		return p.actionResult(ctx, l.act, r.name)
	}
	opv := a.v
	if len(l.scope.names) > 0 {
		opv = p.attachCaptures(opv, l.scope, a.frame, a.start, a.end)
	}
	if opv != nil {
		opv.fresh = false
	}
	var kids []*Node
	switch a.op.kind {
	case grammar.Prefix:
		kids = p.nodes(2)
		kids[0], kids[1] = opv, rhs
	case grammar.Postfix:
		kids = p.nodes(2)
		kids[0], kids[1] = lhs, opv
	default:
		kids = p.nodes(3)
		kids[0], kids[1], kids[2] = lhs, opv, rhs
	}
	return p.newNode(Node{Type: TypeOperator, Rule: r.name, Start: start, End: end, Children: kids, Fields: Fields{{"operator", a.op.id}}})
}
