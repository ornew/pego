package sample

import "github.com/ornew/zeroparse/grammar"

// A Operators Rule expression is emitted as a flat chain while immutable scopes track the parser's recursive
// prefix and infix RHS frames. Returning to a parent restores its own nonassociative restriction.
// Longest operator selection and full input validity remain checked by the matcher and real parser.

// operatorsRuleOp is an operator together with its level.
type operatorsRuleOp struct {
	op    *grammar.OperatorsRuleOperator
	level int
}

// operatorsRuleStop caches operator parts in declaration order and the RHS grammar per entry level.
// Longest selection must precede level and nonassociative eligibility checks.
type operatorsRuleStop struct {
	ops, prefixes []operatorsRuleOp
	pr            *grammar.OperatorsRule
	base          *operatorsRuleCheck
	uncommitted   bool // every led part is known unable to commit a failed application
}

// operatorsRuleScope is an immutable frame. minimum is the weakest bound in the live frame chain.
type operatorsRuleScope struct {
	minLevel, lastNone, minimum int
	parent                      *operatorsRuleScope
}

func newOperatorsRuleScope(minLevel, lastNone int, parent *operatorsRuleScope) *operatorsRuleScope {
	minimum := minLevel
	if parent != nil {
		minimum = min(minimum, parent.minimum)
	}
	return &operatorsRuleScope{minLevel: minLevel, lastNone: lastNone, minimum: minimum, parent: parent}
}

// owner returns the innermost frame that can consume the selected operator. Disabled or repeated
// none operators return from inner frames before the parent considers that same winning part.
// Every visited frame costs work; incomplete means the caller must not infer an owner or rejection.
func (s *operatorsRuleScope) owner(o operatorsRuleOp, budget int) (owner *operatorsRuleScope, work int, complete bool) {
	for f := s; f != nil; f = f.parent {
		work++
		if work > budget {
			return nil, work, false
		}
		if o.level >= f.minLevel && !(o.op.Kind == grammar.Infix && o.op.Assoc == grammar.AssocNone && f.lastNone == o.level) {
			return f, work, true
		}
	}
	return nil, work, true
}

// operatorsRuleCheck retains immutable scopes independently of later choices and backtracking.
type operatorsRuleCheck struct {
	stop  *operatorsRuleStop
	scope *operatorsRuleScope
}

func (g *gen) operatorsRuleOwner(scope *operatorsRuleScope, o operatorsRuleOp) *operatorsRuleScope {
	owner, work, _ := scope.owner(o, g.cfg.budget-g.steps)
	g.steps += work
	return owner
}

// operatorsRuleOps returns operators of the kind from minLevel on. Prefix selection always starts at zero;
// only the infix/postfix tail is restricted by a named entry or a prefix's right-hand binding level.
func operatorsRuleOps(pr *grammar.OperatorsRule, minLevel int, kind string) []operatorsRuleOp {
	var ops []operatorsRuleOp
	for i := minLevel; i < len(pr.Levels); i++ {
		for _, op := range pr.Levels[i].Operators {
			if op.Kind == kind {
				ops = append(ops, operatorsRuleOp{op, i})
			}
		}
	}
	return ops
}

// thunk is a continuation without a value.
type thunk func() bool

func (g *gen) genOperatorsRule(ri *ruleInfo, minLevel int, k cont) bool {
	start := len(g.out)
	scope := newOperatorsRuleScope(minLevel, -1, nil)
	return g.operatorsRulePrimary(ri, scope, 0, func(inner *operatorsRuleScope) bool {
		return g.operatorsRuleTail(ri, 0, inner, func() bool {
			return k(val{kind: vUnknown, start: start, end: len(g.out)})
		})
	})
}

// operatorsRuleSkip generates the skip expression, which the parser inserts before each operand and operator.
func (g *gen) operatorsRuleSkip(pr *grammar.OperatorsRule, k thunk) bool {
	if pr.Skip == nil {
		return k()
	}
	s0 := g.steps
	if g.gen(pr.Skip, func(val) bool { return k() }) {
		return true
	}
	// If skip does not match, nothing is skipped.
	return g.retry(s0) && g.in.length(pr.Skip) > 0 && k()
}

// operatorsRulePart generates an operator part in its own capture scope. Prefix and postfix parts must
// consume input: the parser does not consider an empty match of them.
func (g *gen) operatorsRulePart(op *grammar.OperatorsRuleOperator, k thunk) bool {
	pos := len(g.out)
	caps := g.caps
	g.caps = nil
	m := g.mark(g.in.ops[op])
	ok := g.gen(op.Expr, func(val) bool {
		if op.Kind != grammar.Infix && len(g.out) == pos {
			return false
		}
		inner := g.caps
		g.caps = caps
		ok := k()
		g.caps = inner
		return ok
	})
	g.unmark(m)
	g.caps = caps
	return ok
}

// opOptions orders operators for trying.
func (g *gen) opOptions(ops []operatorsRuleOp) []int {
	opts := make([]option, len(ops))
	for i, o := range ops {
		opts[i] = option{height: g.in.height(o.op.Expr), length: g.in.length(o.op.Expr), own: g.in.ops[o.op], reach: g.in.reach(o.op.Expr)}
	}
	return g.order(opts)
}

// operatorsRulePrimary generates unrestricted prefixes followed by an operand. Each prefix opens a fresh
// RHS frame at its own binding level, even below a tighter named entry. Prefix counts stay per primary.
func (g *gen) operatorsRulePrimary(ri *ruleInfo, scope *operatorsRuleScope, prefixes int, k func(*operatorsRuleScope) bool) bool {
	pr := ri.operatorsRule
	s0 := g.steps
	operand := func() bool {
		return g.operatorsRuleSkip(pr, func() bool { return g.operatorsRuleOperand(pr, func() bool { return k(scope) }) })
	}
	prefix := func() bool {
		ops := operatorsRuleOps(pr, 0, grammar.Prefix)
		if len(ops) == 0 || prefixes >= g.cfg.maxRepeat {
			return false
		}
		return g.operatorsRuleSkip(pr, func() bool {
			for _, i := range g.opOptions(ops) {
				o := ops[i]
				if g.operatorsRulePart(o.op, func() bool {
					return g.operatorsRulePrimary(ri, newOperatorsRuleScope(o.level+1, -1, scope), prefixes+1, k)
				}) {
					return true
				}
				if !g.retry(s0) {
					return false
				}
			}
			return false
		})
	}
	prefixFirst := !g.minimal() && g.rng.IntN(5) == 0
	if g.cfg.coverage && !g.minimal() {
		for _, o := range operatorsRuleOps(pr, 0, grammar.Prefix) {
			if g.wanted(g.in.ops[o.op]) {
				prefixFirst = true
			}
		}
	}
	if prefixFirst {
		return prefix() || (g.retry(s0) && operand())
	}
	return operand() || (g.retry(s0) && prefix())
}

// operatorsRuleOperand generates one of the operands, which form an ordered choice.
func (g *gen) operatorsRuleOperand(pr *grammar.OperatorsRule, k thunk) bool {
	opts := make([]option, len(pr.Operands))
	for i, o := range pr.Operands {
		opts[i] = option{height: g.in.height(o.Expr), length: g.in.length(o.Expr), own: g.in.operands[o], reach: g.in.reach(o.Expr)}
	}
	pending := g.pending
	s0 := g.steps
	for _, i := range g.order(opts) {
		ok := true
		for j := 0; j < i && ok; j++ {
			ok = g.addCheck(pr.Operands[j].Expr, true, false)
		}
		if ok {
			caps := g.caps
			g.caps = nil
			m := g.mark(g.in.operands[pr.Operands[i]])
			ok = g.gen(pr.Operands[i].Expr, func(val) bool {
				inner := g.caps
				g.caps = caps
				ok := k()
				g.caps = inner
				return ok
			})
			g.unmark(m)
			g.caps = caps
		}
		g.pending = pending
		if ok {
			return true
		}
		if !g.retry(s0) {
			return false
		}
	}
	return false
}

// operatorsRuleTail emits a tail with a global per-chain operator count. Inner RHS frames are popped only
// when the selected operator belongs to a parent; that parent's lastNone survives the nested RHS.
func (g *gen) operatorsRuleTail(ri *ruleInfo, count int, scope *operatorsRuleScope, k thunk) bool {
	pr := ri.operatorsRule
	s0 := g.steps
	stop := func() bool {
		pending := g.pending
		ok := true
		if c := g.operatorsRuleStopCheck(ri, scope); c != nil {
			ok = g.addOperatorsRuleCheck(c)
		}
		ok = ok && k()
		g.pending = pending
		return ok
	}
	postfix := func() bool {
		if count >= g.cfg.maxRepeat {
			return false
		}
		ops := operatorsRuleOps(pr, scope.minimum, grammar.Postfix)
		if len(ops) == 0 {
			return false
		}
		return g.operatorsRuleSkip(pr, func() bool {
			for _, i := range g.opOptions(ops) {
				o := ops[i]
				owner := g.operatorsRuleOwner(scope, o)
				if g.exhausted() {
					return false
				}
				if owner == nil {
					continue
				}
				if g.operatorsRulePart(o.op, func() bool {
					return g.operatorsRuleTail(ri, count+1, newOperatorsRuleScope(owner.minLevel, -1, owner.parent), k)
				}) {
					return true
				}
				if !g.retry(s0) {
					return false
				}
			}
			return false
		})
	}
	infix := func() bool {
		if count >= g.cfg.maxRepeat {
			return false
		}
		var ops []operatorsRuleOp
		for _, o := range operatorsRuleOps(pr, scope.minimum, grammar.Infix) {
			if g.operatorsRuleOwner(scope, o) != nil {
				ops = append(ops, o)
			}
			if g.exhausted() {
				return false
			}
		}
		if len(ops) == 0 {
			return false
		}
		return g.operatorsRuleSkip(pr, func() bool {
			for _, i := range g.opOptions(ops) {
				o := ops[i]
				owner := g.operatorsRuleOwner(scope, o)
				if g.exhausted() {
					return false
				}
				if owner == nil {
					continue
				}
				lastNone := -1
				if o.op.Assoc == grammar.AssocNone {
					lastNone = o.level
				}
				parent := newOperatorsRuleScope(owner.minLevel, lastNone, owner.parent)
				rhsMin := o.level + 1
				if o.op.Assoc == grammar.AssocRight {
					rhsMin = o.level
				}
				if g.operatorsRulePart(o.op, func() bool {
					return g.operatorsRulePrimary(ri, newOperatorsRuleScope(rhsMin, -1, parent), 0, func(inner *operatorsRuleScope) bool {
						return g.operatorsRuleTail(ri, count+1, inner, k)
					})
				}) {
					return true
				}
				if !g.retry(s0) {
					return false
				}
			}
			return false
		})
	}
	options := []thunk{stop, postfix, infix}
	if !g.minimal() {
		switch n := g.rng.IntN(10); {
		case n < 4:
			options = []thunk{infix, postfix, stop}
		case n < 6:
			options = []thunk{postfix, infix, stop}
		}
		if g.cfg.coverage {
			for _, o := range operatorsRuleOps(pr, scope.minimum, grammar.Infix) {
				if g.wanted(g.in.ops[o.op]) {
					options = []thunk{infix, postfix, stop}
				}
			}
			for _, o := range operatorsRuleOps(pr, scope.minimum, grammar.Postfix) {
				if g.wanted(g.in.ops[o.op]) {
					options = []thunk{postfix, infix, stop}
				}
			}
		}
	}
	for _, o := range options {
		if o() {
			return true
		}
		if !g.retry(s0) {
			return false
		}
	}
	return false
}

// operatorsRuleStopCheck returns a bounded query for a continuation where a chain ends. Compare all led
// parts before filtering the winner by the entry level or active nonassociative scope. An infix
// continuation also needs a complete RHS; unsupported matching leaves the decision to the real parser.
func (g *gen) operatorsRuleStopCheck(ri *ruleInfo, scope *operatorsRuleScope) *operatorsRuleCheck {
	s, ok := ri.stops[scope.minimum]
	if !ok {
		s = g.operatorsRuleStopParts(ri, scope.minimum)
		ri.stops[scope.minimum] = s
	}
	if len(s.ops) == 0 {
		return nil
	}
	if scope.parent == nil && scope.lastNone == -1 {
		return s.base
	}
	return &operatorsRuleCheck{stop: s, scope: scope}
}

func (g *gen) operatorsRuleStopParts(ri *ruleInfo, minLevel int) *operatorsRuleStop {
	pr := ri.operatorsRule
	s := &operatorsRuleStop{pr: pr, prefixes: operatorsRuleOps(pr, 0, grammar.Prefix), uncommitted: true}
	for l, level := range pr.Levels {
		for _, op := range level.Operators {
			if op.Kind == grammar.Infix || op.Kind == grammar.Postfix {
				s.ops = append(s.ops, operatorsRuleOp{op, l})
				s.uncommitted = s.uncommitted && operatorsRuleUncommitted(op.Expr)
			}
		}
	}
	s.base = &operatorsRuleCheck{stop: s, scope: newOperatorsRuleScope(minLevel, -1, nil)}
	return s
}

// operatorsRuleUncommitted proves that a part cannot commit a later failed RHS or fail after a cut.
// References and unsupported constructs remain conservative, even if one current match succeeds.
func operatorsRuleUncommitted(e grammar.Expr) bool {
	switch e := e.(type) {
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Top, *grammar.Bottom,
		*grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine, *grammar.EndLine:
		return true
	case *grammar.Seq:
		for _, it := range e.Items {
			if !operatorsRuleUncommitted(it) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, a := range e.Alts {
			if !operatorsRuleUncommitted(a) {
				return false
			}
		}
		return true
	case *grammar.Repeat:
		return operatorsRuleUncommitted(e.Expr)
	case *grammar.Optional:
		return operatorsRuleUncommitted(e.Expr)
	case *grammar.And:
		return operatorsRuleUncommitted(e.Expr)
	case *grammar.Not:
		return operatorsRuleUncommitted(e.Expr)
	case *grammar.Atomic:
		return operatorsRuleUncommitted(e.Expr)
	case *grammar.Discard:
		return operatorsRuleUncommitted(e.Expr)
	case *grammar.Capture:
		return operatorsRuleUncommitted(e.Expr)
	}
	return false
}
