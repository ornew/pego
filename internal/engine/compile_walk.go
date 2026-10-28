package engine

import (
	"github.com/ornew/pego/grammar"
)

// exprBuilder builds the executable form M of a program's rule bodies while the compiler walks them
// (walker). The walk decides every static fact of the program — expectation IDs, value-free twins,
// capture slots, resumable repetitions, missing-token sites — and reports the errors; the builder is
// given those facts and only builds. Each method builds the form of one construct from the forms of
// its parts.
type exprBuilder[M any] interface {
	// Literal matches the literal e, recording the expectation desc where it fails.
	Literal(e *grammar.Literal, desc expID, build bool) M
	// Class matches one character of the class e.
	Class(e *grammar.CharClass, desc expID, build bool) M
	// Any matches one character.
	Any(build bool) M
	// Call calls the rule r, at the binding level min of an Operators Rule.
	Call(r *staticRule, min int) M
	// Seq matches the items in order. flat marks the items whose value is the elements of a
	// sequence, valued those with one value; n is the number of values of the sequence.
	Seq(items []M, flat, valued []bool, n int, build bool) M
	// Choice matches the first alternative that matches; firsts tells which of them the next
	// character can skip.
	Choice(e *grammar.Choice, alts []M, firsts []first, build bool) M
	Optional(e *grammar.Optional, m M, build bool) M
	And(m M) M
	Not(m M) M
	Atomic(m M, build bool) M
	Discard(m M) M
	// Capture records the value of m in slot. projected reports that m is a ProjectRepeat.
	Capture(e *grammar.Capture, slot int, m M, projected, build bool) M
	Cut() M
	// Empty matches the empty input (_), Fail nothing.
	Empty() M
	Fail() M
	// Anchor matches the position e (^^, $$, ^ or $), recording desc where it does not hold.
	Anchor(e grammar.Expr, desc expID) M
	// Predicate runs the predicate e on the captures of scope s.
	Predicate(e *grammar.Predicate, s *scope) M
	// ScanRepeat matches the repetition e of a value-free character class or ., whose
	// expectation is desc.
	ScanRepeat(e *grammar.Repeat, desc expID) M
	// Repeat matches the repetition e of elem, whose captures are in elemScope (its own scope when
	// ownScope). stream passes the elements of a #stream on; site, if not 0, is the number of a
	// repetition that a Document can resume, whose elements' values move past an edit when
	// shiftable.
	Repeat(e *grammar.Repeat, elem M, elemScope *scope, ownScope, build, stream bool, site int, shiftable bool) M
	// ProjectRepeat gathers the values of the capture slot of the elements of e (project.go),
	// which share one frame of n slots.
	ProjectRepeat(e *grammar.Repeat, elem M, slot, n int) M
	// Missing inserts the literal expected where m fails (#missing), at the stable site.
	Missing(m M, site int, expected string, build bool) M
	// ErrorMessage replaces the expectations of m with msg (#error); missingScope labels the
	// missing tokens inserted inside m with it.
	ErrorMessage(m M, msg expID, missingScope bool) M
	// Diagnostic names the ordinary expectations of m (#diagnostic).
	Diagnostic(m M, name expID) M
	// Recover recovers a failure of m, the form of e, by skipping the input skip matches.
	Recover(e grammar.Expr, m, skip M, build bool) M
	// Step charges the work of running m once, if the forms charge it per expression.
	Step(m M) M
	// RuleBody completes the rule r with its body.
	RuleBody(r *staticRule, body M)
	// OperatorsRule completes the Operators Rule r with its trivia skip (if hasSkip), operand
	// lines and operator lines.
	OperatorsRule(r *staticRule, skip M, hasSkip bool, operands, operators []opLine[M])
}

// first is the terminal that an alternative of a choice must begin with (firstTerminal), or none.
type first struct {
	term  grammar.Expr // a *grammar.Literal or *grammar.CharClass; nil if there is none
	desc  expID
	depth int
}

// opLine is a line of an Operators Rule, with the form of its expression.
type opLine[M any] struct {
	expr grammar.Expr
	// action is the action the line evaluates, nil if it has none or its rule is value-free.
	action grammar.Term
	scope  *scope
	m      M
	// key is the line's *grammar.OperatorsRuleOperand or *grammar.OperatorsRuleOperator.
	key any
	// The operator of an operator line: its number in declaration order, kind, associativity and
	// binding level.
	id, level   int
	kind, assoc string
}

// walker walks the rule bodies of a program for the compiler, deciding their static facts, and has
// b build their executable form.
type walker[M any] struct {
	*compiler
	b exprBuilder[M]
}

func (c *walker[M]) compileRule(r *staticRule) {
	control := c.control
	c.control.poll()
	c.rule = r
	if p, ok := r.def.Expr.(*grammar.OperatorsRule); ok {
		c.compileOperatorsRule(r, p)
		return
	}
	body := r.bodyExpr()
	r.stream = c.checkStream(body)
	r.lean = r.novalue || leanBody(r)
	c.proj = ruleProjectionsControlled(c.prog, r, control)
	m := c.expr(body, r.scope, !r.lean)
	if c.proj != nil {
		if c.prog.projected == nil {
			c.prog.projected = map[*grammar.Call]bool{}
		}
		markProjectedMaps(r.action, c.proj, c.prog.projected, control)
	}
	c.proj = nil
	if r.action != nil {
		c.checkTerm(r.action, r.scope, nil)
	}
	c.b.RuleBody(r, m)
}

// expr walks the expression. If build is false, its form only matches without building values.
func (c *walker[M]) expr(e grammar.Expr, s *scope, build bool) M {
	c.control.poll()
	return c.b.Step(c.exprForm(e, s, build))
}

func (c *walker[M]) exprForm(e grammar.Expr, s *scope, build bool) M {
	control := c.control
	switch e := e.(type) {
	case *grammar.Literal:
		return c.b.Literal(e, c.desc(literalDesc(e)), build)

	case *grammar.CharClass:
		return c.b.Class(e, c.desc(charClassStringControlled(e, c.control)), build)

	case *grammar.Any:
		return c.b.Any(build)

	case *grammar.Ref:
		r := c.prog.byName[e.Name]
		if r == nil {
			c.errorf(e.Diagnostic(), "undefined rule %s", e.Name)
			var none M
			return none
		}
		min := 0
		if e.Level != "" {
			min = c.levelMin(e, r)
		}
		if !build && r.twinOK {
			r = c.twinOf(r)
		}
		return c.b.Call(r, min)

	case *grammar.Seq:
		ms := make([]M, len(e.Items))
		flat := make([]bool, len(e.Items))
		valued := make([]bool, len(e.Items))
		for i, it := range e.Items {
			ms[i] = c.expr(it, s, build)
			flat[i] = isElems(c.prog, it)
			valued[i] = !flat[i] && valuedCount(c.prog, it) == 1
		}
		return c.b.Seq(ms, flat, valued, valuedCount(c.prog, e), build)

	case *grammar.Choice:
		ms := make([]M, len(e.Alts))
		firsts := make([]first, len(e.Alts))
		for i, alt := range e.Alts {
			ms[i] = c.expr(alt, s, build)
			firsts[i] = c.first(alt)
		}
		return c.b.Choice(e, ms, firsts, build)

	case *grammar.Repeat:
		return c.repeat(e, s, build, false)

	case *grammar.Optional:
		return c.b.Optional(e, c.expr(e.Expr, s, build), build)

	case *grammar.And:
		// Captures and variable definitions inside a positive lookahead remain valid after it.
		return c.b.And(c.expr(e.Expr, s, build && hasCapturesControlled(e.Expr, control)))

	case *grammar.Not:
		c.noCaptures(e.Expr)
		return c.b.Not(c.expr(e.Expr, s, false))

	case *grammar.Atomic:
		c.noCaptures(e.Expr)
		return c.b.Atomic(c.expr(e.Expr, s, false), build)

	case *grammar.Discard:
		c.noCaptures(e.Expr)
		return c.b.Discard(c.expr(e.Expr, s, false))

	case *grammar.Capture:
		if !build && c.rule != nil && c.rule.predCaps != nil && !c.rule.predCaps[e.Name] {
			// In a program that builds no tree, captures not referenced by predicates are not recorded.
			return c.expr(e.Expr, s, false)
		}
		// Even inside a value-free body (leanBody), captures build and record their values.
		slot := s.slot(e.Name)
		if field := c.proj[e.Name]; field != "" {
			return c.b.Capture(e, slot, c.projectRepeat(e.Expr.(*grammar.Repeat), field), true, build)
		}
		return c.b.Capture(e, slot, c.expr(e.Expr, s, true), false, build)

	case *grammar.Cut:
		return c.b.Cut()

	case *grammar.Top:
		return c.b.Empty()

	case *grammar.Bottom:
		return c.b.Fail()

	case *grammar.BeginInput:
		return c.b.Anchor(e, idBeginInput)
	case *grammar.EndInput:
		return c.b.Anchor(e, idEndInput)
	case *grammar.BeginLine:
		return c.b.Anchor(e, idBeginLine)
	case *grammar.EndLine:
		return c.b.Anchor(e, idEndLine)

	case *grammar.Predicate:
		return c.predicate(e, s)

	case *grammar.Attributed:
		return c.attributed(e, s, build)

	case *grammar.OperatorsRule:
		c.errorf(e.Diagnostic(), "operatorsRule is only allowed as the whole body of a rule")
		var none M
		return none
	}
	c.errorf(grammar.Pos{}, "unsupported expression %T", e)
	var none M
	return none
}

// first returns the terminal that the alternative e must begin with, with its expectation (see
// firstTerminal).
func (c *compiler) first(e grammar.Expr) first {
	switch t, depth := firstTerminalControlled(c.prog, e, 0, map[string]bool{}, c.control); t := t.(type) {
	case *grammar.Literal:
		return first{t, c.desc(literalDesc(t)), depth}
	case *grammar.CharClass:
		return first{t, c.desc(charClassStringControlled(t, c.control)), depth}
	}
	return first{}
}

// scanRepeat walks a repetition of a value-free single-character expression (a character class or
// .), which its form advances over without saving and restoring state per element. It reports false
// if e is not one.
func (c *walker[M]) scanRepeat(e *grammar.Repeat, build bool) (M, bool) {
	var none M
	if build {
		return none, false
	}
	switch x := e.Expr.(type) {
	case *grammar.CharClass:
		return c.b.ScanRepeat(e, c.desc(charClassStringControlled(x, c.control))), true
	case *grammar.Any:
		return c.b.ScanRepeat(e, idAny), true
	}
	return none, false
}

// repeat walks a repetition. If stream is true, its form passes the elements on when parsing a
// stream.
func (c *walker[M]) repeat(e *grammar.Repeat, s *scope, build, stream bool) M {
	control := c.control
	if !stream {
		if m, ok := c.scanRepeat(e, build); ok {
			return m
		}
	}
	elemScope := s
	if elementScopedControlled(e.Expr, control) {
		elemScope = newScope()
		elemScope.outer = s
	}
	m := c.expr(e.Expr, elemScope, build)
	site, shiftable := 0, false
	if c.resumable(e.Expr) { // a Document parse is not a stream: #stream has no effect there
		c.runSites++
		site, shiftable = c.runSites, !c.positional(e.Expr)
	}
	return c.b.Repeat(e, m, elemScope, elemScope != s, build, stream, site, shiftable)
}

// projectRepeat walks a repetition that gathers the values of the capture field of its elements
// (project.go). The elements build no values of their own.
func (c *walker[M]) projectRepeat(e *grammar.Repeat, field string) M {
	elemScope := newScope()
	m := c.expr(e.Expr, elemScope, false)
	return c.b.ProjectRepeat(e, m, elemScope.slots[field], len(elemScope.names))
}

// predicate checks a Predicate Action { ... } and walks it.
func (c *walker[M]) predicate(e *grammar.Predicate, s *scope) M {
	if a, ok := e.Term.(*grammar.Bind); ok {
		if c.prog.vars.lookup(a.Name) == nil {
			c.errorf(a.Diagnostic(), "undefined variable %s; declare it with var %s at the top level", a.Name, a.Name)
		}
		c.checkTerm(a.Value, s, nil)
	} else {
		c.checkTerm(e.Term, s, nil)
	}
	return c.b.Predicate(e, s)
}

// attributed walks an attributed expression. Attributes are applied from the inside out, in the
// order they are written.
func (c *walker[M]) attributed(e *grammar.Attributed, s *scope, build bool) M {
	var m M
	if rep, ok := e.Expr.(*grammar.Repeat); ok && isStream(e) {
		m = c.repeat(rep, s, build, true)
	} else {
		m = c.expr(e.Expr, s, build)
	}
	for _, a := range e.Attrs {
		switch a.Name {
		case "stream":
			if _, ok := e.Expr.(*grammar.Repeat); !ok {
				c.errorf(a.Diagnostic(), "#stream must be attached to a repetition (*, +, or #repeat(...))")
			}
			if len(a.Args)+len(a.KwArgs) > 0 {
				c.errorf(a.Diagnostic(), "#stream takes no arguments")
			}
		case "missing":
			lit, ok := e.Expr.(*grammar.Literal)
			if !ok || lit.Value == "" {
				c.errorf(a.Diagnostic(), "#missing requires a nonempty literal")
				continue
			}
			if len(a.Args)+len(a.KwArgs) != 0 {
				c.errorf(a.Diagnostic(), "#missing takes no arguments")
			}
			if c.missingSiteIDs == nil {
				c.missingSiteIDs = make(map[*grammar.Attribute]int)
			}
			site := c.missingSiteIDs[a]
			if site == 0 {
				c.missingSites++
				site = c.missingSites
				c.missingSiteIDs[a] = site
			}
			m = c.b.Missing(m, site, lit.Value, build)
		case "error":
			msg, ok := c.stringArg(a, "message")
			if ok {
				m = c.b.ErrorMessage(m, msgBit|c.desc(msg), c.prog.hasMissing)
			}
		case "diagnostic":
			name, ok := c.stringArg(a, "name")
			if ok {
				m = c.b.Diagnostic(m, c.desc(name))
			}
		case "recover":
			skipExpr, ok := a.Arg("skip")
			if !ok {
				c.errorf(a.Diagnostic(), "#recover requires a skip argument")
				continue
			}
			c.noCaptures(skipExpr)
			m = c.b.Recover(e.Expr, m, c.expr(skipExpr, s, false), build)
		default:
			c.errorf(a.Diagnostic(), "unknown attribute @%s", a.Name)
		}
		for name := range a.KwArgs {
			if !validArg(a.Name, name) {
				c.errorf(a.Diagnostic(), "%s%s has no argument %s", annotationMarker(a), a.Name, name)
			}
		}
	}
	return m
}

// compileOperatorsRule walks the Operators Rule e, the body of the rule r.
func (c *walker[M]) compileOperatorsRule(r *staticRule, e *grammar.OperatorsRule) {
	var skip M
	if e.Skip != nil {
		c.noCaptures(e.Skip)
		skip = c.expr(e.Skip, r.scope, false)
	}
	if len(e.Operands) == 0 {
		c.errorf(e.Diagnostic(), "operatorsRule needs at least one operand")
	}
	var operands, operators []opLine[M]
	for _, o := range e.Operands {
		// A loaded compiled grammar (c.an is nil) has already been checked.
		if c.an != nil && contains(c.an.leftCalls(o.Expr, nil), r.name) {
			c.errorf(e.Diagnostic(), "operand of %s calls %s at its start (left recursion); use infix or postfix instead", r.name, r.name)
		}
		operands = append(operands, c.operatorsRuleLine(o.Expr, o.Action, nil, o))
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
			line.id, line.kind, line.assoc, line.level = id, op.Kind, op.Assoc, i+1
			id++
			operators = append(operators, line)
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
	c.b.OperatorsRule(r, skip, e.Skip != nil, operands, operators)
}

func (c *walker[M]) operatorsRuleLine(e grammar.Expr, action grammar.Term, locals []string, key any) opLine[M] {
	s := newScope()
	novalue := c.rule.novalue || leanLine(action, locals)
	l := opLine[M]{expr: e, scope: s, m: c.expr(e, s, !novalue), key: key}
	if action != nil && !c.rule.novalue {
		l.action = action
		c.checkTerm(action, s, locals)
	}
	return l
}

// leanLine reports whether the value of an Operators Rule line is unused: the line is an operand
// line with an action, which reads captures only. An operator line's value is $op or its element.
// The bytecode compiler uses the same test.
func leanLine(action grammar.Term, locals []string) bool {
	return action != nil && locals == nil
}
