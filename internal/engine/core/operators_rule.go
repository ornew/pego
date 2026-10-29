package core

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// OperatorsRule is a compiled Operators Rule expression. Binding levels are numbered from 1 in declaration
// order (higher binds tighter).
type OperatorsRule struct {
	Skip      Matcher
	SkipEntry int // start of the skip bytecode (bytecode backend only; -1 if none)
	Operands  []*OperatorsRuleLine
	Prefix    []*OperatorsRuleOp
	Led       []*OperatorsRuleOp // infix and postfix
}

// OperatorsRuleLine is an operand line or an operator line. Each has its own capture scope.
type OperatorsRuleLine struct {
	Scope *compile.Scope
	M     Matcher
	Act   Evaluator // action (nil if none)
	// Value makes the value of an operand line without an action, and single that of an operator
	// line's operator, from the value of the line's expression.
	Value  func(p *Parser, v *value.Node, start int) *value.Node
	Single compile.SingleMode
	Shape  compile.Shaper
	Entry  int // start of the bytecode (bytecode backend only)
	// Key is the line's *grammar.OperatorsRuleOperand or *grammar.OperatorsRuleOperator.
	Key any
}

type OperatorsRuleOp struct {
	Id    int
	Kind  string
	Assoc string
	Level int
	Line  *OperatorsRuleLine
}

// OperatorsRuleAttempt is the result of trying an operator part.
type OperatorsRuleAttempt struct {
	Op         *OperatorsRuleOp
	Frame      *Frame
	V          *value.Node
	Start, End int
	Env        *Env
	Cut        bool
	Errs       []*SyntaxError // syntax errors recovered in this candidate
	missing    []MissingTokenEvent
}

func (p *Parser) skipTrivia(pr *OperatorsRule) {
	if pr.Skip == nil {
		return
	}
	m := p.Mark()
	p.Silent++
	_, ok := pr.Skip(p)
	p.Silent--
	if !ok {
		p.Reset(m)
	}
}

// Longest tries every operator part and returns the longest match (the one declared first on a
// tie); best.op is nil when none matched. cutFailed reports that some candidate failed after passing
// a cut. The attempt is returned by value and captures are saved in one buffer per parser, so trying
// operators allocates nothing for lines without captures.
func (p *Parser) Longest(ops []*OperatorsRuleOp) (best OperatorsRuleAttempt, cutFailed bool) {
	m0 := p.Mark()
	for _, op := range ops {
		prevFrame, prevCut := p.Frame, p.Cut
		f := p.NewFrame(len(op.Line.Scope.Names))
		p.Frame, p.Cut = f, false
		v, ok := op.Line.M(p)
		cut := p.Cut
		p.Frame, p.Cut = prevFrame, prevCut
		if ok && p.Pos == m0.Pos && op.Kind != grammar.Infix {
			// A prefix or postfix operator that consumes nothing could be applied forever,
			// so an empty match does not count as a candidate.
			ok = false
		}
		if ok && (best.Op == nil || p.Pos > best.End) {
			best = OperatorsRuleAttempt{Op: op, Frame: f, V: v, Start: m0.Pos, End: p.Pos, Env: p.Env, Cut: cut,
				Errs: AppendWork(p, []*SyntaxError(nil), p.Recovered[m0.Recovered:]), missing: p.MissingEvents(m0.missing)}
		}
		if !ok && cut {
			cutFailed = true
		}
		if len(f.Vals) == 0 {
			p.Reset(m0)
			continue
		}
		// reset undoes this candidate's captures; restore them. Nothing is parsed between saving
		// and restoring, so one buffer per parser suffices.
		p.Saved = AppendWork(p, p.Saved[:0], f.Vals)
		p.Reset(m0)
		CopyWork(p, f.Vals, p.Saved)
	}
	return best, cutFailed
}

func (p *Parser) Apply(a *OperatorsRuleAttempt) {
	p.Pos, p.Env = a.End, a.Env
	if p.Missing != nil || len(a.Errs) != 0 {
		p.applyRecovered(a)
	}
}

// applyRecovered replays the recovery errors, and under missing-token support the missing-token
// events, of an applied operator. It is separate so that apply stays inlinable: the common strict
// parse has neither.
func (p *Parser) applyRecovered(a *OperatorsRuleAttempt) {
	if p.Missing == nil {
		p.Recovered = AppendWork(p, p.Recovered, a.Errs)
	} else {
		p.restoreMissing(a.missing, a.Errs, false)
	}
}

// OperatorsRuleParse parses an expression, absorbing only operators that bind tighter than binding level
// min. The operand of a prefix operator and the right operand of an infix operator are parsed by
// recursion, so each level counts as a rule call against the nesting limit: a chain of prefix or
// right-associative operators would otherwise nest without bound on the goroutine stack.
func (p *Parser) OperatorsRuleParse(r *Rule, min int) (*value.Node, bool) {
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.Fail("nesting too deep: more than %d rule calls", p.MaxDepth)
	}
	v, ok := p.operatorsRuleExpr(r, min)
	p.Depth--
	return v, ok
}

// operatorsRuleExpr is the body of OperatorsRuleParse.
func (p *Parser) operatorsRuleExpr(r *Rule, min int) (*value.Node, bool) {
	pr := r.OperatorsRule
	lhs, ok := p.operatorsRuleNud(r)
	if !ok {
		return nil, false
	}
	lastNone := -1
	for {
		m0 := p.Mark()
		p.skipTrivia(pr)
		at, cutFailed := p.Longest(pr.Led)
		a := &at
		if a.Op == nil {
			p.Reset(m0)
			if cutFailed {
				return nil, false
			}
			break
		}
		if a.Op.Level <= min || a.Op.Assoc == grammar.AssocNone && a.Op.Level == lastNone {
			p.Reset(m0)
			break
		}
		p.Apply(a)
		if a.Op.Kind == grammar.Postfix {
			lhs = p.OperatorsRuleBuild(r, a, lhs, nil)
			lastNone = -1
			if p.Pos == m0.Pos {
				break
			}
			continue
		}
		rmin := a.Op.Level
		if a.Op.Assoc == grammar.AssocRight {
			rmin--
		}
		rhs, ok := p.OperatorsRuleParse(r, rmin)
		if !ok {
			if a.Cut {
				return nil, false
			}
			p.Reset(m0)
			break
		}
		lhs = p.OperatorsRuleBuild(r, a, lhs, rhs)
		lastNone = -1
		if a.Op.Assoc == grammar.AssocNone {
			lastNone = a.Op.Level
		}
		// An application that consumed no input ends the loop, as an empty iteration ends a repetition:
		// otherwise an operator that can match empty after an operand that can loops forever.
		if p.Pos == m0.Pos {
			break
		}
	}
	return lhs, true
}

// operatorsRuleNud parses the beginning of an expression (a prefix operator or an operand).
func (p *Parser) operatorsRuleNud(r *Rule) (*value.Node, bool) {
	pr := r.OperatorsRule
	p.skipTrivia(pr)
	m0 := p.Mark()
	if at, cutFailed := p.Longest(pr.Prefix); at.Op != nil {
		a := &at
		p.Apply(a)
		rhs, ok := p.OperatorsRuleParse(r, a.Op.Level)
		if ok {
			return p.OperatorsRuleBuild(r, a, nil, rhs), true
		}
		if a.Cut {
			return nil, false
		}
		p.Reset(m0)
	} else if cutFailed {
		return nil, false
	}
	for _, o := range pr.Operands {
		prevFrame, prevCut := p.Frame, p.Cut
		f := p.NewFrame(len(o.Scope.Names))
		p.Frame, p.Cut = f, false
		v, ok := o.M(p)
		cut := p.Cut
		p.Frame, p.Cut = prevFrame, prevCut
		if ok {
			return p.LineResult(r, o, f, v, m0.Pos, nil), true
		}
		p.Reset(m0)
		if cut {
			return nil, false
		}
	}
	return nil, false
}

// LineResult builds the value of an operand line, and checks it against the rule's declared type
// if needed.
func (p *Parser) LineResult(r *Rule, l *OperatorsRuleLine, f *Frame, v *value.Node, start int, locals *Local) *value.Node {
	if r.Novalue {
		return nil
	}
	if l.Act != nil {
		ctx := p.UseCtx(EvalCtx{P: p, Scope: l.Scope, Frame: f, Start: start, End: p.Pos, Locals: locals, Cbase: len(p.Created)})
		v = p.ActionResult(ctx, l.Act, r.WhereName())
	} else {
		v = l.Value(p, v, start)
		if len(l.Scope.Names) > 0 {
			v = p.AttachCaptures(v, l.Scope, f, start, p.Pos)
		}
		if v != nil && value.D(v).Fresh {
			if p.Prog.RecordsSourceInfo && value.D(v).Kind.Rule == "" {
				value.D(v).Kind = r.Kinds.Named(value.D(v).Kind, r.Name)
			}
			value.D(v).Fresh = false
		}
	}
	return v
}

// convertOperator converts v, the value of an operator of rule r, to the named node type of the
// rule's operators.
func (p *Parser) convertOperator(r *Rule, v *value.Node) *value.Node {
	n, ok := r.OpKinds.Convert(value.UnboxNode(v))
	if !ok {
		p.Fail("cannot convert %s to %s in result of rule %s", TypeName(value.UnboxNode(v)), r.OpNodeType, r.WhereName())
	}
	return n
}

// OperatorsRuleBuild builds the value of an operator application.
func (p *Parser) OperatorsRuleBuild(r *Rule, a *OperatorsRuleAttempt, lhs, rhs *value.Node) *value.Node {
	if r.Novalue {
		return nil
	}
	start, end := a.Start, p.Pos
	if lhs != nil {
		start = int(value.D(lhs).Start)
	}
	if a.Op.Kind == grammar.Postfix {
		end = a.End
	}
	l := a.Op.Line
	// The operator's value is its one element, or the node of its tuple, which has the line's
	// captures.
	opv, ok := ShapeSingle(p, l.Shape, l.Single, a.V, a.Start)
	if !ok {
		opv = p.NewNode(value.Data{Kind: value.SeqKind, Start: int32(a.Start), End: int32(a.End)})
	}
	if l.Single == compile.SingleTuple && len(l.Scope.Names) > 0 {
		opv = p.AttachCaptures(opv, l.Scope, a.Frame, a.Start, a.End)
	}
	if opv != nil {
		value.D(opv).Fresh = false
	}
	if l.Act != nil {
		// The locals live in the parser, like the evaluation context (see UseCtx).
		ls := &p.oplocals
		ls[0] = Local{Name: "op", Val: NodeOrNil(opv)}
		locals := &ls[0]
		if a.Op.Kind != grammar.Prefix {
			ls[1] = Local{Name: "lhs", Val: NodeOrNil(lhs), Next: locals}
			locals = &ls[1]
		}
		if a.Op.Kind != grammar.Postfix {
			ls[2] = Local{Name: "rhs", Val: NodeOrNil(rhs), Next: locals}
			locals = &ls[2]
		}
		ctx := p.UseCtx(EvalCtx{P: p, Scope: l.Scope, Frame: a.Frame, Start: start, End: end, Locals: locals, Cbase: len(p.Created)})
		v := p.ActionResult(ctx, l.Act, r.WhereName())
		if r.OpKinds != nil {
			v = p.convertOperator(r, v)
		}
		return v
	}
	// Without an action, the application is the node of the tuple (lhs, op, rhs) of the line's
	// kind, with those captures.
	var kids []*value.Node
	var fields value.Fields
	switch a.Op.Kind {
	case grammar.Prefix:
		kids = p.Nodes(2)
		kids[0], kids[1] = opv, rhs
		fields = value.Fields{{Name: "op", Value: NodeOrNil(opv)}, {Name: "rhs", Value: NodeOrNil(rhs)}}
	case grammar.Postfix:
		kids = p.Nodes(2)
		kids[0], kids[1] = lhs, opv
		fields = value.Fields{{Name: "lhs", Value: NodeOrNil(lhs)}, {Name: "op", Value: NodeOrNil(opv)}}
	default:
		kids = p.Nodes(3)
		kids[0], kids[1], kids[2] = lhs, opv, rhs
		fields = value.Fields{{Name: "lhs", Value: NodeOrNil(lhs)}, {Name: "op", Value: NodeOrNil(opv)}, {Name: "rhs", Value: NodeOrNil(rhs)}}
	}
	kind := value.SeqKind
	if r.OpKinds != nil {
		kind = r.OpKinds[value.BaseTuple] // made with the named node type of the operators
	} else if p.Prog.RecordsSourceInfo {
		kind = r.Kinds.Named(value.SeqKind, r.Name)
	}
	n := p.NewNode(value.Data{Kind: kind, Start: int32(start), End: int32(end), Children: kids, Fields: fields, Labeled: p.Prog.RecordsSourceInfo})
	return n
}
