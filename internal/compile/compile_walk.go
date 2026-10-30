package compile

import (
	"github.com/ornew/pego/grammar"
)

// ExprBuilder builds the executable form M of a program's rule bodies while the compiler walks them
// (walker). The walk decides every static fact of the program — expectation IDs, value-free twins,
// capture slots, resumable repetitions, missing-token sites — and reports the errors; the builder is
// given those facts and only builds. Each method builds the form of one construct from the forms of
// its parts.
type ExprBuilder[M any] interface {
	// Literal matches the literal e, recording the expectation desc where it fails.
	Literal(e *grammar.Literal, desc ExpID, build bool) M
	// Class matches one character of the class e.
	Class(e *grammar.CharClass, desc ExpID, build bool) M
	// Any matches one character.
	Any(build bool) M
	// Call calls the rule r, at the binding level min of an Operators Rule.
	Call(r *Rule, min int) M
	// Seq matches the items in order. flat marks the items whose value is the elements of a
	// sequence, valued those with one value; n is the number of values of the sequence.
	Seq(items []M, flat, valued []bool, n int, build bool) M
	// Choice matches the first alternative that matches; firsts tells which of them the next
	// character can skip.
	Choice(e *grammar.Choice, alts []M, firsts []First, build bool) M
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
	Anchor(e grammar.Expr, desc ExpID) M
	// Predicate runs the predicate e on the captures of scope s.
	Predicate(e *grammar.Predicate, s *Scope) M
	// ScanRepeat matches the repetition e of a value-free character class or ., whose
	// expectation is desc.
	ScanRepeat(e *grammar.Repeat, desc ExpID) M
	// Repeat matches the repetition e of elem, whose captures are in elemScope (its own scope when
	// ownScope). stream passes the elements of a #stream on; site, if not 0, is the number of a
	// repetition that a Document can resume, whose elements' values move past an edit when
	// shiftable.
	Repeat(e *grammar.Repeat, elem M, elemScope *Scope, ownScope, build, stream bool, site int, shiftable bool, in *Rule) M
	// ProjectRepeat gathers the values of the capture slot of the elements of e (project.go),
	// which share one frame of n slots.
	ProjectRepeat(e *grammar.Repeat, elem M, slot, n int) M
	// Missing inserts the literal expected where m fails (#missing), at the stable site.
	Missing(m M, site int, expected string, build bool) M
	// ErrorMessage replaces the expectations of m with msg (#error); missingScope labels the
	// missing tokens inserted inside m with it.
	ErrorMessage(m M, msg ExpID, missingScope bool) M
	// Diagnostic names the ordinary expectations of m (#diagnostic).
	Diagnostic(m M, name ExpID) M
	// Recover recovers a failure of m, the form of e, by skipping the input skip matches.
	Recover(e grammar.Expr, m, skip M, build bool) M
	// RuleBody completes the rule r with its body.
	RuleBody(r *Rule, body M)
	// OperatorsRule completes the Operators Rule r with its trivia skip (if hasSkip), operand
	// lines and operator lines.
	OperatorsRule(r *Rule, skip M, hasSkip bool, operands, operators []OpLine[M])
}

// First is the terminal that an alternative of a choice must begin with (firstTerminal), or none.
type First struct {
	Term  grammar.Expr // a *grammar.Literal or *grammar.CharClass; nil if there is none
	Desc  ExpID
	Depth int
}

// OpLine is a line of an Operators Rule, with the form of its expression.
type OpLine[M any] struct {
	Expr grammar.Expr
	// Action is the action the line evaluates, nil if it has none or its rule is value-free.
	Action grammar.Term
	Scope  *Scope
	Form   M
	// Key is the line's *grammar.OperatorsRuleOperand or *grammar.OperatorsRuleOperator.
	Key any
	// The operator of an operator line: its number in declaration order, kind, associativity and
	// binding level.
	ID, Level   int
	Kind, Assoc string
}

// Walker walks the rule bodies of a program for the compiler, deciding their static facts, and has
// b build their executable form.
type Walker[M any] struct {
	*Compiler
	Builder ExprBuilder[M]
}

func (c *Walker[M]) compileRule(r *Rule) {
	control := c.Control
	c.Control.poll()
	c.rule = r
	if p, ok := r.Def.Expr.(*grammar.OperatorsRule); ok {
		c.compileOperatorsRule(r, p)
		return
	}
	body := r.BodyExpr()
	r.Stream = c.checkStream(body)
	r.Lean = r.Novalue || leanBody(r)
	c.proj = ruleProjectionsControlled(c.Prog, r, control)
	m := c.Expr(body, r.Scope, !r.Lean)
	if c.proj != nil {
		if c.Prog.Projected == nil {
			c.Prog.Projected = map[*grammar.Call]bool{}
		}
		markProjectedMaps(r.Action, c.proj, c.Prog.Projected, control)
	}
	c.proj = nil
	if r.Action != nil {
		c.CheckTerm(r.Action, r.Scope, nil)
	}
	c.Builder.RuleBody(r, m)
}

// Expr walks the expression. If build is false, its form only matches without building values.
func (c *Walker[M]) Expr(e grammar.Expr, s *Scope, build bool) M {
	c.Control.poll()
	control := c.Control
	switch e := e.(type) {
	case *grammar.Literal:
		return c.Builder.Literal(e, c.desc(LiteralDesc(e)), build)

	case *grammar.CharClass:
		return c.Builder.Class(e, c.desc(CharClassStringControlled(e, c.Control)), build)

	case *grammar.Any:
		return c.Builder.Any(build)

	case *grammar.Ref:
		r := c.Prog.ByName[e.Name]
		if r == nil {
			c.errorf(e.Diagnostic(), "undefined rule %s", e.Name)
			var none M
			return none
		}
		min := 0
		if e.Level != "" {
			min = c.levelMin(e, r)
		}
		if !build && r.TwinOK {
			r = c.twinOf(r)
		}
		return c.Builder.Call(r, min)

	case *grammar.Seq:
		ms := make([]M, len(e.Items))
		flat := make([]bool, len(e.Items))
		valued := make([]bool, len(e.Items))
		for i, it := range e.Items {
			ms[i] = c.Expr(it, s, build)
			flat[i] = IsElems(c.Prog, it)
			valued[i] = !flat[i] && ValuedCount(c.Prog, it) == 1
		}
		return c.Builder.Seq(ms, flat, valued, ValuedCount(c.Prog, e), build)

	case *grammar.Choice:
		ms := make([]M, len(e.Alts))
		firsts := make([]First, len(e.Alts))
		for i, alt := range e.Alts {
			ms[i] = c.Expr(alt, s, build)
			firsts[i] = c.first(alt)
		}
		return c.Builder.Choice(e, ms, firsts, build)

	case *grammar.Repeat:
		return c.repeat(e, s, build, false)

	case *grammar.Optional:
		return c.Builder.Optional(e, c.Expr(e.Expr, s, build), build)

	case *grammar.And:
		// Captures and variable definitions inside a positive lookahead remain valid after it.
		return c.Builder.And(c.Expr(e.Expr, s, build && hasCapturesControlled(e.Expr, control)))

	case *grammar.Not:
		c.noCaptures(e.Expr)
		return c.Builder.Not(c.Expr(e.Expr, s, false))

	case *grammar.Atomic:
		c.noCaptures(e.Expr)
		return c.Builder.Atomic(c.Expr(e.Expr, s, false), build)

	case *grammar.Discard:
		c.noCaptures(e.Expr)
		return c.Builder.Discard(c.Expr(e.Expr, s, false))

	case *grammar.Capture:
		if !build && c.rule != nil && c.rule.PredCaps != nil && !c.rule.PredCaps[e.Name] {
			// In a program that builds no tree, captures not referenced by predicates are not recorded.
			return c.Expr(e.Expr, s, false)
		}
		// Even inside a value-free body (leanBody), captures build and record their values.
		slot := s.Slot(e.Name)
		if field := c.proj[e.Name]; field != "" {
			return c.Builder.Capture(e, slot, c.ProjectRepeat(e.Expr.(*grammar.Repeat), field), true, build)
		}
		return c.Builder.Capture(e, slot, c.Expr(e.Expr, s, true), false, build)

	case *grammar.Cut:
		return c.Builder.Cut()

	case *grammar.Top:
		return c.Builder.Empty()

	case *grammar.Bottom:
		return c.Builder.Fail()

	case *grammar.BeginInput:
		return c.Builder.Anchor(e, IDBeginInput)
	case *grammar.EndInput:
		return c.Builder.Anchor(e, IDEndInput)
	case *grammar.BeginLine:
		return c.Builder.Anchor(e, IDBeginLine)
	case *grammar.EndLine:
		return c.Builder.Anchor(e, IDEndLine)

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
func (c *Compiler) first(e grammar.Expr) First {
	switch t, depth := FirstTerminalControlled(c.Prog, e, 0, map[string]bool{}, c.Control); t := t.(type) {
	case *grammar.Literal:
		return First{t, c.desc(LiteralDesc(t)), depth}
	case *grammar.CharClass:
		return First{t, c.desc(CharClassStringControlled(t, c.Control)), depth}
	}
	return First{}
}

// scanRepeat walks a repetition of a value-free single-character expression (a character class or
// .), which its form advances over without saving and restoring state per element. It reports false
// if e is not one.
func (c *Walker[M]) scanRepeat(e *grammar.Repeat, build bool) (M, bool) {
	var none M
	if build {
		return none, false
	}
	switch x := e.Expr.(type) {
	case *grammar.CharClass:
		return c.Builder.ScanRepeat(e, c.desc(CharClassStringControlled(x, c.Control))), true
	case *grammar.Any:
		return c.Builder.ScanRepeat(e, IDAny), true
	}
	return none, false
}

// repeat walks a repetition. If stream is true, its form passes the elements on when parsing a
// stream.
func (c *Walker[M]) repeat(e *grammar.Repeat, s *Scope, build, stream bool) M {
	control := c.Control
	if !stream {
		if m, ok := c.scanRepeat(e, build); ok {
			return m
		}
	}
	elemScope := s
	if elementScopedControlled(e.Expr, control) {
		elemScope = NewScope()
		elemScope.outer = s
	}
	m := c.Expr(e.Expr, elemScope, build)
	site, shiftable := 0, false
	if c.resumable(e.Expr) { // a Document parse is not a stream: #stream has no effect there
		c.runSites++
		site, shiftable = c.runSites, !c.positional(e.Expr)
	}
	return c.Builder.Repeat(e, m, elemScope, elemScope != s, build, stream, site, shiftable, c.rule)
}

// ProjectRepeat walks a repetition that gathers the values of the capture field of its elements
// (project.go). The elements build no values of their own.
func (c *Walker[M]) ProjectRepeat(e *grammar.Repeat, field string) M {
	elemScope := NewScope()
	m := c.Expr(e.Expr, elemScope, false)
	return c.Builder.ProjectRepeat(e, m, elemScope.Slots[field], len(elemScope.Names))
}

// predicate checks a Predicate Action { ... } and walks it.
func (c *Walker[M]) predicate(e *grammar.Predicate, s *Scope) M {
	if a, ok := e.Term.(*grammar.Bind); ok {
		if c.Prog.Vars.lookup(a.Name) == nil {
			c.errorf(a.Diagnostic(), "undefined variable %s; declare it with var %s at the top level", a.Name, a.Name)
		}
		c.CheckTerm(a.Value, s, nil)
	} else {
		c.CheckTerm(e.Term, s, nil)
	}
	return c.Builder.Predicate(e, s)
}

// attributed walks an attributed expression. Attributes are applied from the inside out, in the
// order they are written.
func (c *Walker[M]) attributed(e *grammar.Attributed, s *Scope, build bool) M {
	var m M
	if rep, ok := e.Expr.(*grammar.Repeat); ok && IsStream(e) {
		m = c.repeat(rep, s, build, true)
	} else {
		m = c.Expr(e.Expr, s, build)
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
			m = c.Builder.Missing(m, site, lit.Value, build)
		case "error":
			msg, ok := c.stringArg(a, "message")
			if ok {
				m = c.Builder.ErrorMessage(m, MsgBit|c.desc(msg), c.Prog.HasMissing)
			}
		case "diagnostic":
			name, ok := c.stringArg(a, "name")
			if ok {
				m = c.Builder.Diagnostic(m, c.desc(name))
			}
		case "recover":
			skipExpr, ok := a.Arg("skip")
			if !ok {
				c.errorf(a.Diagnostic(), "#recover requires a skip argument")
				continue
			}
			c.noCaptures(skipExpr)
			m = c.Builder.Recover(e.Expr, m, c.Expr(skipExpr, s, false), build)
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
func (c *Walker[M]) compileOperatorsRule(r *Rule, e *grammar.OperatorsRule) {
	var skip M
	if e.Skip != nil {
		c.noCaptures(e.Skip)
		skip = c.Expr(e.Skip, r.Scope, false)
	}
	if len(e.Operands) == 0 {
		c.errorf(e.Diagnostic(), "operatorsRule needs at least one operand")
	}
	var operands, operators []OpLine[M]
	for _, o := range e.Operands {
		// A loaded compiled grammar (c.an is nil) has already been checked.
		if c.an != nil && contains(c.an.leftCalls(o.Expr, nil), r.Name) {
			c.errorf(e.Diagnostic(), "operand of %s calls %s at its start (left recursion); use infix or postfix instead", r.Name, r.Name)
		}
		line := c.operatorsRuleLine(o.Expr, o.Action, nil, o)
		c.checkValueCaptures(o.Diagnostic(), line)
		operands = append(operands, line)
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
			for _, name := range line.Scope.Names {
				if name == "lhs" || name == "rhs" || name == "op" {
					c.errorf(op.Diagnostic(), "capture name %s is reserved in operatorsRule operators", name)
				}
			}
			c.checkValueCaptures(op.Diagnostic(), line)
			line.ID, line.Kind, line.Assoc, line.Level = id, op.Kind, op.Assoc, i+1
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
	if r.Action != nil {
		// The whole-rule action sees only the final folded value, as $value.
		c.CheckTerm(r.Action, r.Scope, OperatorsRuleActionLocals)
	}
	c.Builder.OperatorsRule(r, skip, e.Skip != nil, operands, operators)
}

// checkValueCaptures reports a capture named value in the operand or operator line at d: the name
// is reserved for the folded value, which the whole-rule action reads as $value.
func (c *Walker[M]) checkValueCaptures(d grammar.Pos, line OpLine[M]) {
	for _, name := range line.Scope.Names {
		if name == OperatorsRuleValue {
			c.errorf(d, "capture name %s is reserved for the whole-rule action of an Operators Rule, which reads the folded value as $%s", name, name)
		}
	}
}

// OperatorsRuleValue is the local name of the final folded value of an Operators Rule in its
// whole-rule action, and OperatorsRuleActionLocals the locals of that action.
const OperatorsRuleValue = "value"

var OperatorsRuleActionLocals = []string{OperatorsRuleValue}

func (c *Walker[M]) operatorsRuleLine(e grammar.Expr, action grammar.Term, locals []string, key any) OpLine[M] {
	s := NewScope()
	novalue := c.rule.Novalue || LeanLine(action, locals)
	l := OpLine[M]{Expr: e, Scope: s, Form: c.Expr(e, s, !novalue), Key: key}
	if action != nil && !c.rule.Novalue {
		l.Action = action
		c.CheckTerm(action, s, locals)
	}
	return l
}

// LeanLine reports whether the value of an Operators Rule line is unused: the line is an operand
// line with an action, which reads captures only. An operator line's value is $op or its element.
// The bytecode compiler uses the same test.
func LeanLine(action grammar.Term, locals []string) bool {
	return action != nil && locals == nil
}
