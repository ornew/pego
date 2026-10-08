package sample

import "github.com/ornew/pego/grammar"

// A Pratt expression is generated as a flat chain: prefix operators, an operand, and then postfix
// operators and infix operators each followed by another such operand. The parser determines the tree
// from the binding levels; any chain is accepted except where an operator part is read differently
// (the longest match wins) and where an infix none operator would chain. The generator avoids the
// latter and leaves the rest to the checks and the parser.

// prattOp is an operator together with its level.
type prattOp struct {
	op    *grammar.PrattOperator
	level int
}

// prattOps returns the operators of the kind in levels from minLevel on (a level-restricted call parses
// only those).
func prattOps(pr *grammar.Pratt, minLevel int, kind string) []prattOp {
	var ops []prattOp
	for i := minLevel; i < len(pr.Levels); i++ {
		for _, op := range pr.Levels[i].Operators {
			if op.Kind == kind {
				ops = append(ops, prattOp{op, i})
			}
		}
	}
	return ops
}

// thunk is a continuation without a value.
type thunk func() bool

func (g *gen) genPratt(ri *ruleInfo, minLevel int, k cont) bool {
	start := len(g.out)
	open := make([]bool, len(ri.pratt.Levels))
	return g.prattPrimary(ri, minLevel, 0, func() bool {
		return g.prattTail(ri, minLevel, 0, open, func() bool {
			return k(val{kind: vUnknown, start: start, end: len(g.out)})
		})
	})
}

// prattSkip generates the skip expression, which the parser inserts before each operand and operator.
func (g *gen) prattSkip(pr *grammar.Pratt, k thunk) bool {
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

// prattPart generates an operator part in its own capture scope. Prefix and postfix parts must
// consume input: the parser does not consider an empty match of them.
func (g *gen) prattPart(op *grammar.PrattOperator, k thunk) bool {
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
func (g *gen) opOptions(ops []prattOp) []int {
	opts := make([]option, len(ops))
	for i, o := range ops {
		opts[i] = option{height: g.in.height(o.op.Expr), own: g.in.ops[o.op], reach: g.in.reach(o.op.Expr)}
	}
	return g.order(opts)
}

// prattPrimary generates prefix operators followed by an operand.
func (g *gen) prattPrimary(ri *ruleInfo, minLevel, prefixes int, k thunk) bool {
	pr := ri.pratt
	s0 := g.steps
	operand := func() bool {
		return g.prattSkip(pr, func() bool { return g.prattOperand(pr, k) })
	}
	prefix := func() bool {
		ops := prattOps(pr, minLevel, grammar.Prefix)
		if len(ops) == 0 || prefixes >= g.cfg.maxRepeat {
			return false
		}
		return g.prattSkip(pr, func() bool {
			for _, i := range g.opOptions(ops) {
				if g.prattPart(ops[i].op, func() bool { return g.prattPrimary(ri, minLevel, prefixes+1, k) }) {
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
		for _, o := range prattOps(pr, minLevel, grammar.Prefix) {
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

// prattOperand generates one of the operands, which form an ordered choice.
func (g *gen) prattOperand(pr *grammar.Pratt, k thunk) bool {
	opts := make([]option, len(pr.Operands))
	for i, o := range pr.Operands {
		opts[i] = option{height: g.in.height(o.Expr), own: g.in.operands[o], reach: g.in.reach(o.Expr)}
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

// prattTail generates postfix and infix operators after an operand, or ends the chain. open[l] is true if
// an infix none operator of level l was applied and no looser operator came after it: another one of
// that level would not chain.
func (g *gen) prattTail(ri *ruleInfo, minLevel, count int, open []bool, k thunk) bool {
	pr := ri.pratt
	s0 := g.steps
	// after returns open updated for an operator of level l.
	after := func(l int, none bool) []bool {
		o := append([]bool(nil), open...)
		for i := l + 1; i < len(o); i++ {
			o[i] = false
		}
		o[l] = none
		return o
	}
	stop := func() bool {
		pending := g.pending
		ok := true
		if c := g.prattStopCheck(ri, minLevel); c != nil {
			ok = g.addCheck(c, true, true)
		}
		ok = ok && k()
		g.pending = pending
		return ok
	}
	postfix := func() bool {
		ops := prattOps(pr, minLevel, grammar.Postfix)
		if len(ops) == 0 || count >= g.cfg.maxRepeat {
			return false
		}
		return g.prattSkip(pr, func() bool {
			for _, i := range g.opOptions(ops) {
				o := ops[i]
				if g.prattPart(o.op, func() bool { return g.prattTail(ri, minLevel, count+1, after(o.level, false), k) }) {
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
		var ops []prattOp
		for _, o := range prattOps(pr, minLevel, grammar.Infix) {
			if !(o.op.Assoc == grammar.AssocNone && open[o.level]) {
				ops = append(ops, o)
			}
		}
		if len(ops) == 0 || count >= g.cfg.maxRepeat {
			return false
		}
		return g.prattSkip(pr, func() bool {
			for _, i := range g.opOptions(ops) {
				o := ops[i]
				next := after(o.level, o.op.Assoc == grammar.AssocNone)
				if g.prattPart(o.op, func() bool {
					return g.prattPrimary(ri, minLevel, 0, func() bool { return g.prattTail(ri, minLevel, count+1, next, k) })
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
			for _, o := range prattOps(pr, minLevel, grammar.Infix) {
				if g.wanted(g.in.ops[o.op]) {
					options = []thunk{infix, postfix, stop}
				}
			}
			for _, o := range prattOps(pr, minLevel, grammar.Postfix) {
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

// prattStopCheck returns the expression that must not match where a chain ends: the parser would
// otherwise continue the chain with a postfix or infix operator. Infix operators whose part can match
// the empty string are left out, since whether they apply depends on the operand after them.
func (g *gen) prattStopCheck(ri *ruleInfo, minLevel int) grammar.Expr {
	if c, ok := ri.stops[minLevel]; ok {
		return c
	}
	var alts []grammar.Expr
	for _, kind := range []string{grammar.Postfix, grammar.Infix} {
		for _, o := range prattOps(ri.pratt, minLevel, kind) {
			if g.in.length(o.op.Expr) > 0 {
				alts = append(alts, o.op.Expr)
			}
		}
	}
	var c grammar.Expr
	if len(alts) > 0 {
		items := []grammar.Expr{&grammar.Choice{Alts: alts}}
		if ri.pratt.Skip != nil {
			items = []grammar.Expr{&grammar.Optional{Expr: ri.pratt.Skip}, items[0]}
		}
		c = &grammar.Seq{Items: items}
	}
	ri.stops[minLevel] = c
	return c
}
