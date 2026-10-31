package compile

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ornew/pego/grammar"
)

// endKind says what an evaluation examines after the last item of a continuation.
type endKind uint8

const (
	// endNone: nothing more, as at the end of a lookahead or of a unit's own extent.
	endNone endKind = iota
	// endRule: what follows the calls of the rule (its FOLLOW).
	endRule
	// endOps: the operator parts of the Operators Rule of the rule, then what follows its calls.
	endOps
)

// cont is a continuation: the expressions that an evaluation evaluates next, in order. After its
// items it continues with next, or, when next is nil, as end says.
type cont struct {
	items []grammar.Expr
	next  *cont
	end   endKind
	rule  string
}

// then returns the continuation that evaluates items and then c.
func (c *cont) then(items ...grammar.Expr) *cont { return &cont{items: items, next: c} }

// rest returns the continuation after the first item of c.
func (c *cont) rest() *cont { return &cont{items: c.items[1:], next: c.next, end: c.end, rule: c.rule} }

// source is a scanning source.
type source struct {
	rule string
	pos  grammar.Pos
	what string
}

func (s *source) String() string { return fmt.Sprintf("%s at %s", s.what, at(s.rule, s.pos)) }

// site is a call of a rule.
type site struct {
	caller, callee string
	ref            *grammar.Ref
	cont           *cont // what follows the call
	// mayStart: the call can run before its caller's body consumes input; mustStart: it always
	// does.
	mayStart, mustStart bool
	repeated            string     // why the call is evaluated repeatedly within its caller, or ""
	seqs                []seqFrame // the sequences around the call
}

// unitKind is the kind of a unit (an expression after whose failure the parser continues where
// it started).
type unitKind uint8

const (
	unitAlt unitKind = iota
	unitIteration
	unitOption
	unitLookahead
	unitRecover
	unitOperand
	unitOperator
)

func (u unitKind) String() string {
	return [...]string{"the alternative", "an iteration of the repetition", "the option", "the lookahead",
		"the base of the #recover", "the operand", "the operator part"}[u]
}

// contSpec is one continuation of a unit, with a description for witnesses.
type contSpec struct {
	paths []*cont
	what  string
	// growth: the continuation follows the growth of a left-recursion cycle, so it reads the
	// FOLLOW sets without the seed reads of that cycle.
	growth bool
}

type unit struct {
	id    int
	kind  unitKind
	rule  string
	expr  grammar.Expr // the expression that holds the unit, for its position
	pos   grammar.Pos
	self  []*cont
	conts []contSpec
	// repeated says why the construct that holds the unit is evaluated more than once in a parse,
	// or is "".
	repeated string
	// growing marks an alternative that begins with a left call; choice and index locate it.
	growing bool
	choice  *grammar.Choice
	index   int
	after   *cont // what follows the choice
	cycle   int
	// unsupported is set for a unit whose shape the classifier does not analyze.
	unsupported string

	src   *source         // the first scanning source inside the unit itself
	calls map[string]bool // the rules that the unit calls
	inner []*unit         // the units inside it, in the same rule

	hasSource bool
	nc        bool // contains a scanning source and is not committing
	witness   string
}

type wctx struct {
	rule                string
	cont                *cont
	mayStart, mustStart bool
	repeated            string
	open                []*unit
	inNot               bool
	seqs                []seqFrame // the sequences around the expression, for bindings
	inRepeat            bool       // inside the element of a repetition of the rule
	ops                 bool       // inside an Operators Rule
	// strictFrom is the first of seqs that the expression is in through groups, options,
	// choices, captures, discards and atomic expressions only, for the growing-variable check.
	strictFrom int
}

type seqFrame struct {
	seq   *grammar.Seq
	index int
}

func (c wctx) with(cn *cont) wctx { c.cont = cn; return c }

// walkRule walks the body and the action of a reachable rule, collecting call sites, scanning
// sources, units, bindings and copying operations.
func (k *classifier) walkRule(r *grammar.RuleDef) {
	c := wctx{rule: r.Name, cont: &cont{end: endRule, rule: r.Name}, mayStart: true, mustStart: true}
	k.collectCaptures(r)
	k.walk(r.Expr, c)
	if r.Action != nil {
		k.walkTerm(r.Action, termCtx{rule: r.Name, c: c, action: true})
	}
}

func (k *classifier) collectCaptures(r *grammar.RuleDef) {
	m := map[string][]grammar.Expr{}
	WalkExpr(r.Expr, func(e grammar.Expr) {
		if cp, ok := e.(*grammar.Capture); ok {
			m[cp.Name] = append(m[cp.Name], cp.Expr)
		}
	})
	k.captures[r.Name] = m
}

// addSource records a scanning source found in the walk.
func (k *classifier) addSource(c wctx, pos grammar.Pos, what string) {
	s := &source{rule: c.rule, pos: pos, what: what}
	if k.local[c.rule] == nil {
		k.local[c.rule] = s
	}
	for _, u := range c.open {
		if u.src == nil {
			u.src = s
		}
	}
}

func (k *classifier) newUnit(kind unitKind, c wctx, e grammar.Expr, self []*cont, conts []contSpec) *unit {
	u := &unit{id: len(k.units), kind: kind, rule: c.rule, expr: e, pos: exprPos(e), self: self, conts: conts,
		repeated: c.repeated, calls: map[string]bool{}}
	for _, o := range c.open {
		o.inner = append(o.inner, u)
	}
	k.units = append(k.units, u)
	return u
}

func (c wctx) push(u *unit) wctx {
	c.open = append(c.open[:len(c.open):len(c.open)], u)
	return c
}

// consumesNothing reports whether e never consumes input.
func (k *classifier) consumesNothing(e grammar.Expr) bool {
	switch e := e.(type) {
	case *grammar.Predicate, *grammar.Cut, *grammar.Top, *grammar.Bottom, *grammar.BeginInput, *grammar.EndInput,
		*grammar.BeginLine, *grammar.EndLine, *grammar.And, *grammar.Not:
		return true
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.Group:
		return k.consumesNothing(e.Expr)
	case *grammar.Seq:
		for _, it := range e.Items {
			if !k.consumesNothing(it) {
				return false
			}
		}
		return true
	}
	return false
}

func (k *classifier) walk(e grammar.Expr, c wctx) {
	k.control.poll()
	switch e.(type) {
	case *grammar.Seq, *grammar.Choice, *grammar.Group, *grammar.Optional, *grammar.Capture, *grammar.Discard,
		*grammar.Atomic, *grammar.Ref, *grammar.Predicate:
	default:
		c.strictFrom = len(c.seqs)
	}
	switch e := e.(type) {
	case *grammar.Ref:
		k.walkRef(e, c)
	case *grammar.Seq:
		may, must := c.mayStart, c.mustStart
		for i, it := range e.Items {
			cc := c.with(c.cont.then(e.Items[i+1:]...))
			cc.mayStart, cc.mustStart = may, must
			cc.seqs = append(c.seqs[:len(c.seqs):len(c.seqs)], seqFrame{e, i})
			k.walk(it, cc)
			may = may && k.nullable(it)
			must = must && k.consumesNothing(it)
		}
	case *grammar.Choice:
		for i, alt := range e.Alts {
			cc := c
			if i < len(e.Alts)-1 {
				var conts []contSpec
				if !k.cutCommits(alt) {
					for _, later := range e.Alts[i+1:] {
						conts = append(conts, contSpec{paths: []*cont{c.cont.then(later)},
							what: fmt.Sprintf("the later alternative %s at %s", k.show(later), at(c.rule, exprPos(later)))})
					}
				}
				u := k.newUnit(unitAlt, c, alt, []*cont{{items: []grammar.Expr{alt}}}, conts)
				u.choice, u.index, u.after = e, i, c.cont
				if k.an.cyclic[c.rule] && c.mayStart {
					if first := k.firstEvaluated(alt); first != nil && k.sameCycle(c.rule, first.Name) && first.Level == "" {
						u.growing, u.cycle = true, k.an.cycle[c.rule]
					}
				}
				cc = c.push(u)
			}
			k.walk(alt, cc)
		}
	case *grammar.Repeat:
		if e.Max == 0 {
			return
		}
		if e.Max < 0 {
			k.addSource(c, exprPos(e), "the repetition "+k.show(e))
			k.srcExprs[e] = true
		}
		u := k.newUnit(unitIteration, c, e, []*cont{{items: []grammar.Expr{e.Expr}}},
			[]contSpec{{paths: []*cont{c.cont}, what: "what follows the repetition " + k.show(e)}})
		k.leftUnit(u, c, e.Expr)
		again := &grammar.Repeat{SourceInfo: e.SourceInfo, Expr: e.Expr, Min: 0, Max: -1}
		cc := c.push(u).with(c.cont.then(again))
		cc.mustStart = false
		cc.inRepeat = true
		if e.Max < 0 || e.Max > 1 {
			cc.repeated = "the repetition " + k.show(e) + " at " + at(c.rule, exprPos(e))
		}
		k.walk(e.Expr, cc)
	case *grammar.Optional:
		u := k.newUnit(unitOption, c, e, []*cont{{items: []grammar.Expr{e.Expr}}},
			[]contSpec{{paths: []*cont{c.cont}, what: "what follows the option " + k.show(e)}})
		k.leftUnit(u, c, e.Expr)
		k.walk(e.Expr, c.push(u))
	case *grammar.And:
		k.walkLookahead(e, e.Expr, c, false)
	case *grammar.Not:
		k.walkLookahead(e, e.Expr, c, true)
	case *grammar.Recovery:
		k.walkRecovery(e, e.Expr, e.Skip, c)
	case *grammar.Attributed:
		if skip := recoverSkip(e); skip != nil {
			k.walkRecovery(e, e.Expr, skip, c)
			return
		}
		k.walk(e.Expr, c)
	case *grammar.Group:
		k.walk(e.Expr, c)
	case *grammar.Capture:
		k.walk(e.Expr, c)
	case *grammar.Discard:
		k.walk(e.Expr, c)
	case *grammar.Atomic:
		k.walk(e.Expr, c)
	case *grammar.Stream:
		k.walk(e.Expr, c)
	case *grammar.InlineAction:
		k.walk(e.Expr, c)
		if e.Action != nil {
			k.walkTerm(e.Action, termCtx{rule: c.rule, c: c, action: true, holder: e})
		}
	case *grammar.Predicate:
		k.walkTerm(e.Term, termCtx{rule: c.rule, c: c, pred: e})
	case *grammar.OperatorsRule:
		k.walkOperators(e, c)
	}
}

// leftUnit marks a unit other than an alternative that begins with a left call: the classifier
// analyzes growing units only as alternatives.
func (k *classifier) leftUnit(u *unit, c wctx, e grammar.Expr) {
	if k.an.cyclic[c.rule] && c.mayStart {
		if first := k.firstEvaluated(e); first != nil && k.sameCycle(c.rule, first.Name) {
			u.unsupported = u.kind.String() + " begins with a left call"
		}
	}
}

func (k *classifier) walkLookahead(e, body grammar.Expr, c wctx, not bool) {
	u := k.newUnit(unitLookahead, c, e, []*cont{{items: []grammar.Expr{body}}},
		[]contSpec{{paths: []*cont{c.cont}, what: "what follows the lookahead " + k.show(e)}})
	k.leftUnit(u, c, body)
	cc := c.push(u).with(&cont{})
	cc.inNot = c.inNot || not
	cc.seqs, cc.strictFrom = nil, 0
	k.walk(body, cc)
}

func (k *classifier) walkRecovery(e, base, skip grammar.Expr, c wctx) {
	k.addSource(c, exprPos(e), "the skip expression "+k.show(skip)+" of #recover")
	k.srcExprs[e] = true
	u := k.newUnit(unitRecover, c, e, []*cont{{items: []grammar.Expr{base}}},
		[]contSpec{{paths: []*cont{c.cont.then(skip)}, what: "the skip expression " + k.show(skip)}})
	k.leftUnit(u, c, base)
	k.walk(base, c.push(u))
	k.walk(skip, c)
}

func (k *classifier) walkRef(e *grammar.Ref, c wctx) {
	if k.byName[e.Name] == nil {
		return
	}
	for _, u := range c.open {
		u.calls[e.Name] = true
	}
	k.sites = append(k.sites, &site{caller: c.rule, callee: e.Name, ref: e, cont: c.cont, mayStart: c.mayStart,
		mustStart: c.mustStart, repeated: c.repeated, seqs: c.seqs})
	if k.an.cyclic[e.Name] && (!k.sameCycle(c.rule, e.Name) || !c.mustStart) {
		k.addSource(c, exprPos(e), "the growth of the left-recursive rule "+e.Name+" at a new position")
		k.srcExprs[e] = true
	}
}

// cutCommits reports whether every way in which the alternative can fail lies after a #cut in it,
// so that the later alternatives are not tried after it fails.
func (k *classifier) cutCommits(alt grammar.Expr) bool {
	s, ok := ungroup(alt).(*grammar.Seq)
	if !ok {
		return false
	}
	for _, it := range s.Items {
		if _, ok := it.(*grammar.Cut); ok {
			return true
		}
		if !k.an.AlwaysSucceeds(it) {
			return false
		}
	}
	return false
}

// firstEvaluated returns the call that e evaluates before anything else, or nil.
func (k *classifier) firstEvaluated(e grammar.Expr) *grammar.Ref {
	for {
		switch x := e.(type) {
		case *grammar.Ref:
			return x
		case *grammar.Group:
			e = x.Expr
		case *grammar.Capture:
			e = x.Expr
		case *grammar.Discard:
			e = x.Expr
		case *grammar.Atomic:
			e = x.Expr
		case *grammar.Stream:
			e = x.Expr
		case *grammar.Seq:
			if len(x.Items) == 0 {
				return nil
			}
			e = x.Items[0]
		default:
			return nil
		}
	}
}

// walkOperators walks an Operators Rule. The rule applies operators, which is a scanning source;
// each operand item other than the last, and each operator part with its right operand, is a
// unit. Its continuations are approximated by the other operand items or operator parts that the
// rule can try at the same position, and by what follows the Operators Rule expression.
func (k *classifier) walkOperators(pr *grammar.OperatorsRule, c wctx) {
	hasOps := false
	for _, l := range pr.Levels {
		hasOps = hasOps || len(l.Operators) > 0
	}
	if hasOps {
		k.addSource(c, exprPos(pr), "the operators that the Operators Rule applies")
		k.srcExprs[pr] = true
	}
	end := &cont{end: endOps, rule: c.rule}
	inside := c.with(end)
	inside.mustStart = false
	inside.ops = true
	inside.seqs, inside.strictFrom = nil, 0
	if hasOps {
		inside.repeated = "the Operators Rule at " + at(c.rule, exprPos(pr))
	}
	withSkip := func(e grammar.Expr, more ...grammar.Expr) *cont {
		items := []grammar.Expr{}
		if pr.Skip != nil {
			items = append(items, k.skipItem(pr))
		}
		return &cont{items: append(append(items, e), more...)}
	}
	// An Operators Rule is a whole rule body: what follows it is what follows the rule. The other
	// operator parts that the rule can try after an operand are continuations of their own.
	follow := contSpec{paths: []*cont{{end: endRule, rule: c.rule}}, what: "what follows the Operators Rule"}
	rhs := &grammar.Ref{Name: c.rule, Level: rhsLevel}
	skipCalls := map[string]bool{}
	if pr.Skip != nil {
		for _, n := range AllCalls(pr.Skip, nil) {
			skipCalls[n] = true
		}
		k.walk(pr.Skip, inside)
	}
	enter := func(u *unit) wctx {
		for n := range skipCalls {
			u.calls[n] = true
		}
		return inside.push(u)
	}
	skipSource := pr.Skip != nil && (k.containsSourceSyntactic(pr.Skip))
	var operandUnits []*unit
	operandCalls := map[string]bool{}
	for i, o := range pr.Operands {
		for _, n := range AllCalls(o.Expr, nil) {
			operandCalls[n] = true
		}
		cc := inside
		if i < len(pr.Operands)-1 {
			var conts []contSpec
			for _, later := range pr.Operands[i+1:] {
				conts = append(conts, contSpec{paths: []*cont{withSkip(later.Expr)},
					what: fmt.Sprintf("the later operand %s at %s", k.show(later.Expr), at(c.rule, exprPos(later.Expr)))})
			}
			u := k.newUnit(unitOperand, c, o.Expr, []*cont{withSkip(o.Expr)}, append(conts, follow))
			if skipSource {
				u.src = &source{rule: c.rule, pos: exprPos(pr.Skip), what: "the skip expression " + k.show(pr.Skip)}
			}
			cc = enter(u)
		}
		cc.mayStart = c.mayStart
		before := len(k.units)
		k.walk(o.Expr, cc)
		for _, v := range k.units[before:] {
			if v.rule == c.rule {
				operandUnits = append(operandUnits, v)
			}
		}
		if i < len(pr.Operands)-1 {
			operandUnits = append(operandUnits, k.units[before-1])
		}
		if o.Action != nil {
			k.walkTerm(o.Action, termCtx{rule: c.rule, c: inside, action: true})
		}
	}
	type part struct {
		level int
		op    *grammar.OperatorsRuleOperator
	}
	var parts []part
	prefixes := false
	for li, l := range pr.Levels {
		for _, op := range l.Operators {
			parts = append(parts, part{li, op})
			prefixes = prefixes || op.Kind == grammar.Prefix
		}
	}
	for _, p := range parts {
		var self *cont
		if p.op.Kind == grammar.Postfix {
			self = withSkip(p.op.Expr)
		} else {
			self = withSkip(p.op.Expr, rhs)
		}
		var conts []contSpec
		for _, q := range parts {
			if q.op == p.op || (q.op.Kind == grammar.Prefix) != (p.op.Kind == grammar.Prefix) {
				continue
			}
			conts = append(conts, contSpec{paths: []*cont{withSkip(q.op.Expr)},
				what: fmt.Sprintf("the operator part %s at %s", k.show(q.op.Expr), at(c.rule, exprPos(q.op.Expr)))})
		}
		if p.op.Kind == grammar.Prefix {
			for _, o := range pr.Operands {
				conts = append(conts, contSpec{paths: []*cont{withSkip(o.Expr)},
					what: fmt.Sprintf("the operand %s at %s", k.show(o.Expr), at(c.rule, exprPos(o.Expr)))})
			}
		}
		u := k.newUnit(unitOperator, c, p.op.Expr, []*cont{self}, append(conts, follow))
		applies := false
		switch p.op.Kind {
		case grammar.Prefix:
			applies = true
		case grammar.Infix:
			applies = prefixes
			for _, q := range parts {
				if q.op.Kind != grammar.Prefix && (q.level > p.level || q.level == p.level && p.op.Assoc == grammar.AssocRight) {
					applies = true
				}
			}
		}
		if p.op.Kind != grammar.Postfix {
			// The right operand parses operands again, and operators too when it can apply them.
			if applies {
				u.calls[c.rule] = true
			} else {
				for n := range operandCalls {
					u.calls[n] = true
				}
				u.inner = append(u.inner, operandUnits...)
			}
		}
		if applies {
			u.src = &source{rule: c.rule, pos: exprPos(p.op.Expr), what: "the right operand of the operator part " + k.show(p.op.Expr) + ", which can apply operators"}
		} else if skipSource {
			u.src = &source{rule: c.rule, pos: exprPos(pr.Skip), what: "the skip expression " + k.show(pr.Skip)}
		}
		cc := enter(u)
		cc.mayStart = c.mayStart && (p.op.Kind == grammar.Prefix || k.operandsNullable(c.rule))
		if p.op.Kind == grammar.Postfix {
			cc.cont = end
		} else {
			cc.cont = end.then(rhs)
		}
		k.walk(p.op.Expr, cc)
		if p.op.Action != nil {
			k.walkTerm(p.op.Action, termCtx{rule: c.rule, c: inside, action: true})
		}
	}
}

// containsSourceSyntactic reports whether e has a repetition without an upper bound, a #recover
// or an Operators Rule in it, without following calls.
func (k *classifier) containsSourceSyntactic(e grammar.Expr) bool {
	found := false
	WalkExpr(e, func(x grammar.Expr) {
		switch x := x.(type) {
		case *grammar.Repeat:
			found = found || x.Max < 0
		case *grammar.Recovery, *grammar.OperatorsRule:
			found = true
		}
	})
	return found
}

// computeRuleSources finds, for each rule, a scanning source that it contains through calls.
func (k *classifier) computeRuleSources() {
	for n, s := range k.local {
		k.ruleSrc[n] = s
	}
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if !k.reach[r.Name] || k.ruleSrc[r.Name] != nil {
				continue
			}
			for _, c := range k.calls[r.Name] {
				if s := k.ruleSrc[c]; s != nil {
					k.ruleSrc[r.Name] = s
					changed = true
					break
				}
			}
		}
	}
	for _, u := range k.units {
		if u.src != nil {
			u.hasSource = true
			continue
		}
		for _, c := range slices.Sorted(maps.Keys(u.calls)) {
			if s := k.ruleSrc[c]; s != nil {
				u.src, u.hasSource = s, true
				break
			}
		}
	}
}

// --- First characters of continuations and FOLLOW ---

// opsAfter is the first characters of the operator parts of the Operators Rule of rule, which
// can follow an operand.
func (k *classifier) opsAfter(rule string) charset {
	pr := k.operators(rule)
	if pr == nil {
		return nil
	}
	var s charset
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			if op.Kind == grammar.Prefix {
				continue
			}
			items := []grammar.Expr{k.skipItem(pr), op.Expr}
			if op.Kind == grammar.Infix {
				items = append(items, &grammar.Ref{Name: rule, Level: rhsLevel})
			}
			t, _ := k.firstItems(items)
			s = s.union(t)
		}
	}
	return s
}

// firstItems returns the first characters of the items in sequence and whether all can match
// without consuming input.
func (k *classifier) firstItems(items []grammar.Expr) (charset, bool) {
	var s charset
	for _, it := range items {
		if it == nil {
			continue
		}
		s = s.union(k.first(it))
		if !k.nullable(it) {
			return s, false
		}
	}
	return s, true
}

// firstCont returns the first characters of a continuation, with follow giving the FOLLOW set of a
// rule. ends receives the rules whose FOLLOW it reaches, if not nil.
func (k *classifier) firstCont(c *cont, follow func(string) charset, ends map[string]bool) charset {
	var s charset
	for c != nil {
		k.control.poll()
		t, null := k.firstItems(c.items)
		s = s.union(t)
		if !null {
			return s
		}
		if c.next != nil {
			c = c.next
			continue
		}
		switch c.end {
		case endOps:
			s = s.union(k.opsAfter(c.rule))
			fallthrough
		case endRule:
			if ends != nil {
				ends[c.rule] = true
			}
			if follow != nil {
				s = s.union(follow(c.rule))
			}
		}
		return s
	}
	return s
}

func (k *classifier) firstPaths(paths []*cont, follow func(string) charset) charset {
	var s charset
	for _, p := range paths {
		s = s.union(k.firstCont(p, follow, nil))
	}
	return s
}

// computeFollow computes FOLLOW(r) for every reachable rule r: the first characters of what the
// parser can evaluate after a call of r, through the ends of the callers' bodies. The start rule
// is followed by the end of the input, which examines nothing.
func (k *classifier) computeFollow() {
	k.follow = k.followSets(func(*site) bool { return true })
}

// followSets computes FOLLOW over the call sites that keep accepts.
func (k *classifier) followSets(keep func(*site) bool) map[string]charset {
	type contrib struct {
		static charset
		ends   map[string]bool
	}
	byCallee := map[string][]contrib{}
	for _, s := range k.sites {
		if !keep(s) {
			continue
		}
		ends := map[string]bool{}
		st := k.firstCont(s.cont, nil, ends)
		byCallee[s.callee] = append(byCallee[s.callee], contrib{st, ends})
	}
	out := map[string]charset{}
	for changed := true; changed; {
		changed = false
		for callee, cs := range byCallee {
			k.control.poll()
			s := out[callee]
			for _, c := range cs {
				s = s.union(c.static)
				for e := range c.ends {
					s = s.union(out[e])
				}
			}
			if !s.equal(out[callee]) {
				out[callee] = s
				changed = true
			}
		}
	}
	return out
}

// keepFor returns the call sites through which a continuation of a unit continues at the end of
// a rule body: every site, or, after a growth, the sites other than the seed reads of the cycle.
// A call of a leader of the cycle at the position where it grows reads the seed and is part of the
// growth; after the growth the parser continues after the call that started it.
func (k *classifier) keepFor(cycle int, growth bool) func(*site) bool {
	if !growth {
		return nil
	}
	return func(s *site) bool {
		return !(k.an.cyclic[s.caller] && k.an.cycle[s.caller] == cycle && k.an.leaders[s.callee] &&
			k.an.cycle[s.callee] == cycle && s.mustStart)
	}
}

// followFor returns the FOLLOW function of a continuation of a unit.
func (k *classifier) followFor(cycle int, growth bool) func(string) charset {
	if !growth {
		return func(r string) charset { return k.follow[r] }
	}
	m := k.growing[cycle]
	if m == nil {
		m = k.followSets(k.keepFor(cycle, true))
		k.growing[cycle] = m
	}
	return func(r string) charset { return m[r] }
}

// --- Shared calls ---

// lead is the first call that a path evaluates before it examines input, with the rest of the path
// after the call. A lead without a call is a path that examines input first or cannot be followed;
// a lead that is done examines nothing more.
type lead struct {
	call *grammar.Ref
	rest *cont
	done bool
}

type endKey struct {
	end  endKind
	rule string
}

const (
	leadBudget = 4096 // steps to find the leads of one set of paths
	leadMax    = 256  // the most leads of one set of paths
)

// leads finds the first calls of the paths. It looks through calls that are not shared, by
// evaluating their bodies, through expressions that examine no input, and through the ends of rule
// bodies, by the call sites that keep accepts. With only set, it looks through every call but one
// of that rule, and records in seen the shareable calls that it looks through.
func (k *classifier) leads(paths []*cont, keep func(*site) bool, only *string, seen map[string]bool) []lead {
	budget := leadBudget
	var out []lead
	// ended are the rule ends already followed: the paths after them are the same each time.
	ended := map[endKey]bool{}
	var visit func(p *cont)
	visit = func(p *cont) {
		for {
			k.control.poll()
			budget--
			if budget < 0 || len(out) > leadMax {
				out = append(out, lead{})
				return
			}
			if p == nil {
				out = append(out, lead{done: true})
				return
			}
			if len(p.items) == 0 {
				if p.next != nil {
					p = p.next
					continue
				}
				if p.end != endNone {
					key := endKey{p.end, p.rule}
					if ended[key] {
						return
					}
					ended[key] = true
				}
				switch p.end {
				case endNone:
					out = append(out, lead{done: true})
				case endOps:
					k.opsLeads(p.rule, visit)
					k.endLeads(p.rule, keep, visit)
				case endRule:
					k.endLeads(p.rule, keep, visit)
				}
				return
			}
			rest := p.rest()
			switch e := p.items[0].(type) {
			case nil:
				p = rest
			case *grammar.Group:
				p = rest.then(e.Expr)
			case *grammar.Capture:
				p = rest.then(e.Expr)
			case *grammar.Discard:
				p = rest.then(e.Expr)
			case *grammar.Atomic:
				p = rest.then(e.Expr)
			case *grammar.Stream:
				p = rest.then(e.Expr)
			case *grammar.InlineAction:
				p = rest.then(e.Expr)
			case *grammar.Seq:
				p = rest.then(e.Items...)
			case *grammar.Attributed:
				if recoverSkip(e) != nil || hasPragma(e, "missing") {
					out = append(out, lead{})
					return
				}
				p = rest.then(e.Expr)
			case *grammar.Predicate, *grammar.Cut, *grammar.Top, *grammar.BeginInput, *grammar.BeginLine:
				p = rest
			case *grammar.EndInput:
				// At the end of the input nothing more is examined.
				out = append(out, lead{done: true})
				return
			case *grammar.Literal:
				if e.Value != "" {
					out = append(out, lead{})
					return
				}
				p = rest
			case *grammar.Choice:
				for _, alt := range e.Alts {
					visit(rest.then(alt))
				}
				return
			case *grammar.Optional:
				visit(rest.then(e.Expr))
				visit(rest)
				return
			case *grammar.Repeat:
				if e.Max == 0 {
					p = rest
					continue
				}
				again := &grammar.Repeat{SourceInfo: e.SourceInfo, Expr: e.Expr, Min: 0, Max: -1}
				visit(rest.then(e.Expr, again))
				if e.Min == 0 {
					visit(rest)
				}
				return
			case *grammar.Ref:
				if k.byName[e.Name] == nil || e.Level == rhsLevel {
					out = append(out, lead{})
					return
				}
				if k.shareable(e) && (only == nil || *only == e.Name) {
					out = append(out, lead{call: e, rest: rest})
					return
				}
				if k.shareable(e) && seen != nil {
					seen[e.Name] = true
				}
				if pr := k.operatorsBody(e.Name); pr != nil {
					k.operatorsLeads(pr, e.Name, visit)
					return
				}
				p = rest.then(k.byName[e.Name].Expr)
			default:
				out = append(out, lead{})
				return
			}
		}
	}
	for _, p := range paths {
		visit(p)
	}
	return out
}

// endLeads continues a path at the end of the body of rule, after each call of the rule; at the
// end of the start rule the parse ends.
func (k *classifier) endLeads(rule string, keep func(*site) bool, visit func(*cont)) {
	if rule == k.start {
		visit(nil)
	}
	for _, s := range k.sites {
		if s.callee == rule && (keep == nil || keep(s)) {
			visit(s.cont)
		}
	}
}

// opsLeads continues a path after an operand of the Operators Rule of rule, with its operator
// parts.
func (k *classifier) opsLeads(rule string, visit func(*cont)) {
	pr := k.operators(rule)
	if pr == nil {
		return
	}
	end := &cont{end: endOps, rule: rule}
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			if op.Kind == grammar.Prefix {
				continue
			}
			items := []grammar.Expr{k.skipItem(pr), op.Expr}
			if op.Kind == grammar.Infix {
				items = append(items, &grammar.Ref{Name: rule, Level: rhsLevel})
			}
			visit(&cont{items: items, next: end})
		}
	}
}

// operatorsBody returns the Operators Rule that is the body of rule, alone or under attributes
// that change neither what it evaluates first nor where the parser continues after it fails, or
// nil.
func (k *classifier) operatorsBody(rule string) *grammar.OperatorsRule {
	body := ungroup(k.byName[rule].Expr)
	if a, ok := body.(*grammar.Attributed); ok {
		if recoverSkip(a) != nil || hasPragma(a, "missing") {
			return nil
		}
		body = ungroup(a.Expr)
	}
	pr, _ := body.(*grammar.OperatorsRule)
	return pr
}

// skipItem is the skip of pr as an item of a path. The rule evaluates its skip before each operand
// and operator part, and continues at the same position when the skip does not match, so a skip
// that can match nothing stands for itself, and the first call that it evaluates is a call that the
// path evaluates first; any other skip is optional.
func (k *classifier) skipItem(pr *grammar.OperatorsRule) grammar.Expr {
	if pr.Skip == nil || k.nullable(pr.Skip) {
		return pr.Skip
	}
	return &grammar.Optional{Expr: pr.Skip}
}

// operatorsLeads continues a path into the body of rule, the Operators Rule pr. The rule begins
// with its skip, then an operand or a prefix operator part, so a skip that is a shareable call is
// what every path through it evaluates first. After its operand the rule tries its operator
// parts, then continues as after its calls: the path continues at the end of the rule body, which
// includes the path that called it.
func (k *classifier) operatorsLeads(pr *grammar.OperatorsRule, rule string, visit func(*cont)) {
	end := &cont{end: endOps, rule: rule}
	start := func(e grammar.Expr, more ...grammar.Expr) {
		items := []grammar.Expr{}
		if pr.Skip != nil {
			items = append(items, k.skipItem(pr))
		}
		visit(&cont{items: append(append(items, e), more...), next: end})
	}
	for _, o := range pr.Operands {
		start(o.Expr)
	}
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			if op.Kind == grammar.Prefix {
				start(op.Expr, &grammar.Ref{Name: rule, Level: rhsLevel})
			}
		}
	}
}

// strip removes the calls that a unit and a continuation share from their starts, one after
// another, and returns what remains of both. Paths that examine nothing more take no part.
//
// A shared call can be the first thing that a call of another shareable rule evaluates, as ws is
// in primary = ws number: when the first calls of the two differ, the classifier looks through
// them, by evaluating their bodies, for a call that both evaluate first.
func (k *classifier) strip(u, c []*cont, keep func(*site) bool) ([]*cont, []*cont) {
	for range 16 {
		lu, lc := k.leads(u, nil, nil, nil), k.leads(c, keep, nil, nil)
		if commonCall(lu, lc) == nil {
			lu, lc = k.deepLeads(u, c, keep)
			if lu == nil {
				return u, c
			}
		}
		u, c = rests(lu), rests(lc)
	}
	return u, c
}

// deepLeads looks for a shareable call that the unit and the continuation both evaluate first,
// through the bodies of the shareable calls that they evaluate first, and returns the leads of
// both at it, or nil.
func (k *classifier) deepLeads(u, c []*cont, keep func(*site) bool) ([]lead, []lead) {
	none := ""
	su, sc := map[string]bool{}, map[string]bool{}
	k.leads(u, nil, &none, su)
	k.leads(c, keep, &none, sc)
	var both []string
	for n := range su {
		if sc[n] {
			both = append(both, n)
		}
	}
	slices.Sort(both)
	for _, n := range both {
		lu, lc := k.leads(u, nil, &n, nil), k.leads(c, keep, &n, nil)
		if commonCall(lu, lc) != nil {
			return lu, lc
		}
	}
	return nil, nil
}

func commonCall(a, b []lead) *grammar.Ref {
	var call *grammar.Ref
	seen := [2]bool{}
	for i, ls := range [2][]lead{a, b} {
		for _, l := range ls {
			if l.done {
				continue
			}
			if l.call == nil || call != nil && (l.call.Name != call.Name || l.call.Level != call.Level) {
				return nil
			}
			call = l.call
			seen[i] = true
		}
	}
	if !seen[0] || !seen[1] {
		return nil
	}
	return call
}

func rests(ls []lead) []*cont {
	var out []*cont
	for _, l := range ls {
		if !l.done {
			out = append(out, l.rest)
		}
	}
	return out
}

// committing reports whether, after the shared calls are removed, the first characters of the
// unit paths and of each continuation are disjoint. It returns the first continuation that is
// not.
func (k *classifier) committing(self []*cont, conts []contSpec, cycle int) (bool, string) {
	for _, cs := range conts {
		keep := k.keepFor(cycle, cs.growth)
		u, c := k.strip(self, cs.paths, keep)
		// A unit whose sources all lie in the shared calls repeats nothing beyond their cost.
		if !k.pathsContainSource(u) {
			continue
		}
		fu := k.firstPaths(u, nil)
		fc := k.firstPaths(c, k.followFor(cycle, cs.growth))
		if fu.intersects(fc) {
			return false, cs.what
		}
	}
	return true, ""
}

// checkUnits checks condition 2: every unit that contains a scanning source is committing.
func (k *classifier) checkUnits() {
	for _, u := range k.units {
		if !u.hasSource {
			continue
		}
		if u.unsupported != "" {
			k.reason(u.rule, u.pos, "%s at %s contains %s, and %s, which the classifier does not analyze",
				u.kind, at(u.rule, u.pos), u.src, u.unsupported)
			continue
		}
		if u.growing {
			k.checkGrowingUnit(u)
			continue
		}
		if !k.unitFailsAfterSource(u) {
			continue
		}
		if ok, what := k.committing(u.self, u.conts, 0); !ok {
			u.nc, u.witness = true, what
		}
	}
}

// --- Growth ---

// leftPaths follows the alternative alt of a rule of the cycle through the calls of rules of the
// cycle to the left calls of leaders, and returns what follows those calls. firstStep reports a
// path on which the alternative evaluates something other than a left call first, so that the
// first step of the growth can continue the unit.
func (k *classifier) leftPaths(start *cont, cycle int) (paths []*cont, firstStep bool, ok bool) {
	ok = true
	budget := leadBudget
	var visit func(p *cont, depth int)
	visit = func(p *cont, depth int) {
		for {
			k.control.poll()
			budget--
			if budget < 0 || p == nil || len(p.items) == 0 {
				ok = false
				return
			}
			rest := p.rest()
			switch e := p.items[0].(type) {
			case *grammar.Group:
				p = rest.then(e.Expr)
			case *grammar.Capture:
				p = rest.then(e.Expr)
			case *grammar.Discard:
				p = rest.then(e.Expr)
			case *grammar.Atomic:
				p = rest.then(e.Expr)
			case *grammar.Stream:
				p = rest.then(e.Expr)
			case *grammar.Seq:
				if len(e.Items) == 0 {
					firstStep = true
					return
				}
				p = rest.then(e.Items...)
			case *grammar.Choice:
				for _, alt := range e.Alts {
					visit(rest.then(alt), depth)
				}
				return
			case *grammar.Ref:
				if !k.an.cyclic[e.Name] || k.an.cycle[e.Name] != cycle || e.Level != "" {
					firstStep = true
					return
				}
				if k.an.leaders[e.Name] {
					paths = append(paths, rest)
					return
				}
				if depth > len(k.rules) {
					ok = false
					return
				}
				p = rest.then(k.byName[e.Name].Expr)
				depth++
			default:
				firstStep = true
				return
			}
		}
	}
	visit(start, 0)
	return paths, firstStep, ok
}

// checkGrowingUnit checks a growing unit. In each step of a growth after the first its left call
// reads the seed, and it examines the input from the end of the seed; its continuations are the
// later growing alternatives and what follows the calls that start the growth.
func (k *classifier) checkGrowingUnit(u *unit) {
	self, firstStep, ok := k.leftPaths(u.self[0], u.cycle)
	if !ok {
		k.reason(u.rule, u.pos, "the growing alternative at %s contains %s, and the classifier cannot follow its left calls",
			at(u.rule, u.pos), u.src)
		return
	}
	var conts []contSpec
	for _, later := range u.choice.Alts[u.index+1:] {
		if f := k.firstEvaluated(later); f == nil || !k.sameCycle(u.rule, f.Name) || f.Level != "" {
			continue
		}
		paths, _, ok := k.leftPaths(u.after.then(later), u.cycle)
		if !ok {
			k.reason(u.rule, u.pos, "the growing alternative at %s contains %s, and the classifier cannot follow its left calls",
				at(u.rule, u.pos), u.src)
			return
		}
		conts = append(conts, contSpec{paths: paths, growth: true,
			what: fmt.Sprintf("the later growing alternative %s at %s", k.show(later), at(u.rule, exprPos(later)))})
	}
	starts, ok := k.growthStarts(u.cycle)
	if !ok {
		k.reason(u.rule, u.pos, "the growing alternative at %s contains %s, and its cycle can grow from a call of a rule that is not a leader",
			at(u.rule, u.pos), u.src)
		return
	}
	conts = append(conts, starts...)
	if ok, what := k.committing(self, conts, u.cycle); !ok {
		u.nc, u.witness = true, what
		return
	}
	if firstStep {
		if ok, what := k.committing(u.self, u.conts, 0); !ok {
			u.nc, u.witness = true, what
		}
	}
}

// growthStarts returns what follows the calls that start a growth of the cycle: the calls of its
// rules other than left calls, and the start of the parse when the start rule is on the cycle. It
// fails when such a call is of a rule that is not a leader.
func (k *classifier) growthStarts(cycle int) ([]contSpec, bool) {
	var out []contSpec
	if k.an.cyclic[k.start] && k.an.cycle[k.start] == cycle && !k.an.leaders[k.start] {
		return nil, false
	}
	for _, s := range k.sites {
		if !k.an.cyclic[s.callee] || k.an.cycle[s.callee] != cycle {
			continue
		}
		if k.sameCycle(s.caller, s.callee) && s.mustStart {
			continue
		}
		if !k.an.leaders[s.callee] {
			return nil, false
		}
		out = append(out, contSpec{paths: []*cont{s.cont}, growth: true,
			what: fmt.Sprintf("what follows the call %s at %s", s.callee, at(s.caller, exprPos(s.ref)))})
	}
	return out, true
}

// checkGrowth checks condition 3: no scanning source is evaluated at the position where a
// left-recursion cycle grows, other than inside a call of a memoized rule that is not on the
// cycle, because each step of the growth evaluates the bodies of the cycle again there.
func (k *classifier) checkGrowth() {
	seen := map[*source]bool{}
	for _, r := range k.rules {
		if !k.reach[r.Name] || !k.an.cyclic[r.Name] {
			continue
		}
		cycle := k.an.cycle[r.Name]
		var visit func(e grammar.Expr) bool // returns whether e can match without consuming input
		report := func(s *source) {
			if seen[s] {
				return
			}
			seen[s] = true
			k.cause(ClassPoly, 2, s.rule, s.pos, "%s: %s is evaluated in each step of the growth of the left-recursive rule %s, at the position where it grows",
				degreeText(ClassPoly, 2), s, r.Name)
		}
		visit = func(e grammar.Expr) bool {
			k.control.poll()
			switch e := e.(type) {
			case *grammar.Ref:
				if k.byName[e.Name] == nil {
					return true
				}
				if k.an.cyclic[e.Name] && k.an.cycle[e.Name] == cycle {
					return k.nullable(e)
				}
				if !k.memo[e.Name] {
					if s := k.ruleSrc[e.Name]; s != nil {
						report(s)
					}
				}
				return k.nullable(e)
			case *grammar.Seq:
				for _, it := range e.Items {
					if !visit(it) {
						return false
					}
				}
				return true
			case *grammar.Choice:
				null := false
				for _, alt := range e.Alts {
					null = visit(alt) || null
				}
				return null
			case *grammar.Repeat:
				if e.Max == 0 {
					return true
				}
				if e.Max < 0 {
					report(k.sourceAt(r.Name, e, "the repetition "+k.show(e)))
				}
				visit(e.Expr)
				return k.nullable(e)
			case *grammar.Optional:
				visit(e.Expr)
				return true
			case *grammar.And:
				visit(e.Expr)
				return true
			case *grammar.Not:
				visit(e.Expr)
				return true
			case *grammar.Group:
				return visit(e.Expr)
			case *grammar.Capture:
				return visit(e.Expr)
			case *grammar.Discard:
				return visit(e.Expr)
			case *grammar.Atomic:
				return visit(e.Expr)
			case *grammar.Stream:
				return visit(e.Expr)
			case *grammar.InlineAction:
				return visit(e.Expr)
			case *grammar.Recovery:
				report(k.sourceAt(r.Name, e, "the skip expression "+k.show(e.Skip)+" of #recover"))
				visit(e.Expr)
				return k.nullable(e)
			case *grammar.Attributed:
				if skip := recoverSkip(e); skip != nil {
					report(k.sourceAt(r.Name, e, "the skip expression "+k.show(skip)+" of #recover"))
				}
				visit(e.Expr)
				return k.nullable(e)
			case *grammar.OperatorsRule:
				report(k.sourceAt(r.Name, e, "the operators that the Operators Rule applies"))
				return k.nullable(e)
			case *grammar.Predicate:
				if s := k.predicateSource[e]; s != nil {
					report(s)
				}
				return true
			}
			return k.nullable(e)
		}
		visit(r.Expr)
	}
}

// sourceAt returns the source recorded at e, so that one cause is reported once.
func (k *classifier) sourceAt(rule string, e grammar.Expr, what string) *source {
	key := sourceKey{rule, exprPos(e), what}
	if s := k.sourceIndex[key]; s != nil {
		return s
	}
	s := &source{rule: rule, pos: exprPos(e), what: what}
	k.sourceIndex[key] = s
	return s
}

type sourceKey struct {
	rule string
	pos  grammar.Pos
	what string
}

// --- Nesting ---

// checkNesting turns the units that are not committing into causes. A unit restarts the scans
// inside it once for each evaluation of the construct that holds it, so it costs more than the
// input only when that construct is evaluated repeatedly: inside a repetition, an Operators Rule
// or a recursion. Units that are not committing and nested in one another multiply: the degree is
// one more than the length of the longest chain of them. A chain that passes through itself, by
// a recursion of rule calls, has no bound that the classifier knows.
func (k *classifier) checkNesting() {
	var nc []*unit
	for _, u := range k.units {
		if u.nc {
			nc = append(nc, u)
		}
	}
	if len(nc) == 0 {
		return
	}
	// ncIn[r] are the units that are not committing in the rules that r reaches.
	ncByRule := map[string][]*unit{}
	for _, u := range nc {
		ncByRule[u.rule] = append(ncByRule[u.rule], u)
	}
	inner := map[*unit][]*unit{}
	for _, u := range nc {
		seen := map[*unit]bool{}
		add := func(v *unit) {
			if v.nc && !seen[v] {
				seen[v] = true
				inner[u] = append(inner[u], v)
			}
		}
		for _, v := range u.inner {
			add(v)
		}
		for _, c := range slices.Sorted(maps.Keys(u.calls)) {
			for _, r := range slices.Sorted(maps.Keys(k.reachable(c))) {
				for _, v := range ncByRule[r] {
					add(v)
				}
			}
		}
	}
	// Units that can reach a cycle of nesting have no bound.
	state := map[*unit]int{} // 0 unvisited, 1 on the stack, 2 done
	unbounded := map[*unit]bool{}
	var dfs func(u *unit) bool
	dfs = func(u *unit) bool {
		k.control.poll()
		switch state[u] {
		case 1:
			return true
		case 2:
			return unbounded[u]
		}
		state[u] = 1
		bad := false
		for _, v := range inner[u] {
			if dfs(v) {
				bad = true
			}
		}
		state[u] = 2
		unbounded[u] = bad
		return bad
	}
	for _, u := range nc {
		dfs(u)
	}
	// depth[v] is the length of the longest chain of repeated units that ends at v.
	depth := map[*unit]int{}
	from := map[*unit]*unit{}
	var order []*unit
	done := map[*unit]bool{}
	var topo func(u *unit)
	topo = func(u *unit) {
		if done[u] || unbounded[u] {
			return
		}
		done[u] = true
		for _, v := range inner[u] {
			topo(v)
		}
		order = append(order, u)
	}
	for _, u := range nc {
		topo(u)
	}
	for _, u := range order {
		if u.repeated != "" || k.repeatedRule(u.rule) != "" {
			depth[u] = max(depth[u], 1)
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		u := order[i]
		if depth[u] == 0 {
			continue
		}
		for _, v := range inner[u] {
			if !unbounded[v] && depth[u]+1 > depth[v] {
				depth[v], from[v] = depth[u]+1, u
			}
		}
	}
	for _, u := range nc {
		switch {
		case unbounded[u]:
			k.reason(u.rule, u.pos, "%s at %s is not committing against %s, and units that are not committing are nested in it without bound through a recursion of rule calls",
				u.kind, at(u.rule, u.pos), u.witness)
		case depth[u] == 0:
			k.reason(u.rule, u.pos, "%s at %s contains %s and is not committing against %s",
				u.kind, at(u.rule, u.pos), u.src, u.witness)
		case k.leaf(u, inner, unbounded):
			degree := depth[u] + 1
			root := u
			var chain []string
			for v := from[u]; v != nil; v = from[v] {
				chain = append(chain, fmt.Sprintf("%s at %s", v.kind, at(v.rule, v.pos)))
				root = v
			}
			why := root.repeated
			if why == "" {
				why = k.repeatedRule(root.rule)
			}
			msg := fmt.Sprintf("%s: after %s at %s fails, %s can start again inside the input that %s examined",
				degreeText(ClassPoly, degree), u.kind, at(u.rule, u.pos), u.witness, u.src)
			for _, c := range chain {
				msg += "; it is evaluated again inside " + c
			}
			msg += "; this repeats in " + why
			k.cause(ClassPoly, degree, u.src.rule, u.src.pos, "%s", msg)
		}
	}
}

// leaf reports whether u has no bounded unit that is not committing inside it.
func (k *classifier) leaf(u *unit, inner map[*unit][]*unit, unbounded map[*unit]bool) bool {
	for _, v := range inner[u] {
		if !unbounded[v] {
			return false
		}
	}
	return true
}

// repeatedRule says why the rule can be evaluated many times in a parse: it is on a recursion of
// rule calls, or it is called repeatedly from a rule. It is "" for a rule that is evaluated a
// bounded number of times.
func (k *classifier) repeatedRule(rule string) string {
	if k.repeatedRules == nil {
		k.repeatedRules = map[string]string{}
		var queue []string
		for _, r := range k.rules {
			if k.reach[r.Name] && k.recursive[r.Name] {
				k.repeatedRules[r.Name] = "the recursion of rule " + r.Name
				queue = append(queue, r.Name)
			}
		}
		for _, s := range k.sites {
			if s.repeated != "" && k.repeatedRules[s.callee] == "" {
				k.repeatedRules[s.callee] = s.repeated
				queue = append(queue, s.callee)
			}
		}
		for len(queue) > 0 {
			r := queue[0]
			queue = queue[1:]
			for _, s := range k.sites {
				if s.caller == r && k.repeatedRules[s.callee] == "" {
					k.repeatedRules[s.callee] = k.repeatedRules[r]
					queue = append(queue, s.callee)
				}
			}
		}
	}
	return k.repeatedRules[rule]
}

// pathsContainSource reports whether what remains of a unit after its shared calls contains a
// scanning source.
func (k *classifier) pathsContainSource(paths []*cont) bool {
	for _, p := range paths {
		for c := p; c != nil; c = c.next {
			for _, it := range c.items {
				if it == nil {
					continue
				}
				if r, ok := it.(*grammar.Ref); ok && r.Level == rhsLevel {
					return true
				}
				if k.containsSource(it) {
					return true
				}
			}
			if c.next == nil && c.end != endNone {
				return true
			}
		}
	}
	return false
}
