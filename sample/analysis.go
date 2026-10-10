package sample

import (
	"cmp"
	"slices"
	"sort"
	"unicode"

	"github.com/ornew/pego/grammar"
)

// inf is the height or length of an expression that can never match.
const inf = 1 << 30

// Kinds of coverage targets.
const (
	targetRule = iota
	targetAlt
	targetOperand
	targetOperator
)

// target is something the generator tries to exercise: a rule, an alternative of an ordered choice, or
// an operand or operator of a Pratt expression.
type target struct {
	kind  int
	rule  string
	index int // alternative, operand or operator index
	expr  grammar.Expr
	// possible is false for targets that can never be part of a match: they can never match
	// themselves, or only occur inside an expression that can never match ("(" ("a" / "b") _|_).
	possible bool
}

// ruleInfo is what the generator knows about a rule.
type ruleInfo struct {
	def      *grammar.RuleDef
	index    int
	pratt    *grammar.Pratt
	levels   map[string]int // level name -> index in pratt.Levels
	terminal bool           // the rule is declared with a terminal type
	height   int            // minimal derivation height: nested rule calls needed to match it
	length   int            // minimal length in bytes of a match
	reach    bitset         // targets that generating the rule can exercise, its own included
	calls    []string
	// possibleCalls are the calls in contexts that can match, and possibleRefs the calls themselves.
	possibleCalls []string
	possibleRefs  []*grammar.Ref
	// always caches alwaysMatches for the body: 0 not computed yet, 1 being computed, 2 yes, 3 no.
	always int
	// stops caches, per minimum level, the expression that must not match where a chain of
	// operators of the Pratt expression ends (see gen.prattStopCheck).
	stops map[int]grammar.Expr
}

// info is the analysis of a grammar shared by the generator and the matcher.
type info struct {
	rules   map[string]*ruleInfo
	order   []*ruleInfo
	start   *ruleInfo
	targets []target
	// Target ids of the first alternative of each choice, of each Pratt operand and operator.
	altBase  map[*grammar.Choice]int
	operands map[*grammar.PrattOperand]int
	ops      map[*grammar.PrattOperator]int
	// reachable[i] is true if the rule with index i can be called from the start rule in a context
	// that can match; called[i] if it is called at all (outside negative lookaheads and #recover).
	reachable, called []bool
	reachMemo         map[grammar.Expr]bitset
	captures          map[grammar.Expr]bool                      // memo of hasCaptures
	classes           map[*grammar.CharClass][]grammar.CharRange // memo of classRanges
	// compared holds the captures of rule calls that a predicate in the same rule reads.
	compared map[*grammar.Capture]bool
	// alphabet holds the literal strings and sample characters of the grammar, for mutations.
	alphabet []string
}

func analyze(g *grammar.Grammar, start string) *info {
	in := &info{
		rules:     map[string]*ruleInfo{},
		altBase:   map[*grammar.Choice]int{},
		operands:  map[*grammar.PrattOperand]int{},
		ops:       map[*grammar.PrattOperator]int{},
		reachMemo: map[grammar.Expr]bitset{},
		compared:  map[*grammar.Capture]bool{},
		captures:  map[grammar.Expr]bool{},
		classes:   map[*grammar.CharClass][]grammar.CharRange{},
	}
	terminals := map[string]bool{}
	for _, t := range g.Types() {
		if _, ok := t.Spec.(*grammar.TerminalSpec); ok {
			terminals[t.Name] = true
		}
	}
	for i, r := range g.Rules() {
		ri := &ruleInfo{def: r, index: i, height: inf, length: inf, stops: map[int]grammar.Expr{}}
		if pr, ok := r.Expr.(*grammar.Pratt); ok {
			ri.pratt = pr
			ri.levels = map[string]int{}
			for j, l := range pr.Levels {
				if l.Name != "" {
					ri.levels[l.Name] = j
				}
			}
		}
		if t, ok := r.Type.(*grammar.TypeRef); ok && terminals[t.Name] {
			ri.terminal = true
		}
		in.rules[r.Name] = ri
		in.order = append(in.order, ri)
		in.targets = append(in.targets, target{kind: targetRule, rule: r.Name})
	}
	in.start = in.rules[start]
	// Targets inside rule bodies.
	for _, ri := range in.order {
		walk(ri.def.Expr, func(e grammar.Expr) {
			switch e := e.(type) {
			case *grammar.Ref:
				ri.calls = append(ri.calls, e.Name)
			case *grammar.Choice:
				in.altBase[e] = len(in.targets)
				for j, a := range e.Alts {
					in.targets = append(in.targets, target{kind: targetAlt, rule: ri.def.Name, index: j, expr: a})
				}
			case *grammar.Pratt:
				for j, o := range e.Operands {
					in.operands[o] = len(in.targets)
					in.targets = append(in.targets, target{kind: targetOperand, rule: ri.def.Name, index: j, expr: o.Expr})
				}
				j := 0
				for _, l := range e.Levels {
					for _, op := range l.Operators {
						in.ops[op] = len(in.targets)
						in.targets = append(in.targets, target{kind: targetOperator, rule: ri.def.Name, index: j, expr: op.Expr})
						j++
					}
				}
			case *grammar.Literal:
				if e.Value != "" {
					in.alphabet = append(in.alphabet, e.Value)
				}
			case *grammar.CharClass:
				if !e.Negated {
					for _, r := range e.Ranges {
						in.alphabet = append(in.alphabet, string(r.Lo), string(r.Hi))
					}
				}
			}
		})
	}
	in.alphabet = dedupe(in.alphabet)
	for _, ri := range in.order {
		read := map[string]bool{}
		walk(ri.def.Expr, func(e grammar.Expr) {
			if p, ok := e.(*grammar.Predicate); ok {
				walkTerm(p.Term, func(t grammar.Term) {
					if c, ok := t.(*grammar.CaptureRef); ok {
						read[c.Name] = true
					}
				})
			}
		})
		walk(ri.def.Expr, func(e grammar.Expr) {
			if c, ok := e.(*grammar.Capture); ok && read[c.Name] {
				in.compared[c] = true
			}
		})
	}

	// Resolve callees first and revisit only callers of changed estimates.
	deps := in.dependencies()
	deps.fixedPoint(in, func(ri *ruleInfo) bool {
		h, l := in.height(ri.def.Expr), in.length(ri.def.Expr)
		if h >= ri.height && l >= ri.length {
			return false
		}
		ri.height, ri.length = min(h, ri.height), min(l, ri.length)
		return true
	})
	// Targets and calls in contexts that can match.
	for _, ri := range in.order {
		if ri.height >= inf {
			continue
		}
		in.targets[ri.index].possible = true
		in.walkPossible(ri.def.Expr, func(e grammar.Expr) {
			switch e := e.(type) {
			case *grammar.Ref:
				ri.possibleCalls = append(ri.possibleCalls, e.Name)
				ri.possibleRefs = append(ri.possibleRefs, e)
			case *grammar.Choice:
				for j, a := range e.Alts {
					in.targets[in.altBase[e]+j].possible = in.height(a) < inf
				}
			case *grammar.Pratt:
				for _, o := range e.Operands {
					in.targets[in.operands[o]].possible = in.height(o.Expr) < inf
				}
				for _, l := range e.Levels {
					for _, op := range l.Operators {
						in.targets[in.ops[op]].possible = in.height(op.Expr) < inf
					}
				}
			}
		})
	}

	// Reach sets of rules: their own targets and those of the rules they call, transitively.
	for _, ri := range in.order {
		ri.reach = newBitset(len(in.targets))
		ri.reach.set(ri.index) // the target of a rule is its index
		in.localTargets(ri.def.Expr, ri.reach)
	}
	deps.fixedPoint(in, func(ri *ruleInfo) bool {
		changed := false
		for _, c := range ri.calls {
			if callee := in.rules[c]; callee != nil && ri.reach.union(callee.reach) {
				changed = true
			}
		}
		return changed
	})
	in.reachable = in.closure(func(ri *ruleInfo) []string { return ri.possibleCalls })
	in.called = in.closure(func(ri *ruleInfo) []string { return ri.calls })
	in.restrictLevels()
	return in
}

// localTargets adds the targets inside e, not counting the rules it calls, to s.
func (in *info) localTargets(e grammar.Expr, s bitset) {
	walk(e, func(x grammar.Expr) {
		switch x := x.(type) {
		case *grammar.Choice:
			for j := range x.Alts {
				s.set(in.altBase[x] + j)
			}
		case *grammar.Pratt:
			for _, o := range x.Operands {
				s.set(in.operands[o])
			}
			for _, l := range x.Levels {
				for _, op := range l.Operators {
					s.set(in.ops[op])
				}
			}
		}
	})
}

// alwaysMatches reports whether e matches at any position of any input, if only the empty string: an
// optional expression, a repetition with minimum 0, and sequences and choices of such. Cuts (which can
// make an optional expression fail), lookaheads and predicates make it false.
func (in *info) alwaysMatches(e grammar.Expr) bool {
	switch e := e.(type) {
	case *grammar.Top:
		return true
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.Optional:
		return !in.hasCut(e.Expr)
	case *grammar.Repeat:
		return e.Min == 0 && !in.hasCut(e.Expr) || e.Min == 1 && in.alwaysMatches(e.Expr)
	case *grammar.Seq:
		for _, it := range e.Items {
			if !in.alwaysMatches(it) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, a := range e.Alts {
			if in.alwaysMatches(a) {
				return !in.hasCut(e)
			}
		}
		return false
	case *grammar.Atomic:
		return in.alwaysMatches(e.Expr)
	case *grammar.Discard:
		return in.alwaysMatches(e.Expr)
	case *grammar.Capture:
		return in.alwaysMatches(e.Expr)
	case *grammar.Attributed:
		return in.alwaysMatches(e.Expr)
	case *grammar.Ref:
		ri := in.rules[e.Name]
		if ri == nil || ri.pratt != nil {
			return false
		}
		switch ri.always {
		case 0:
			ri.always = 1 // a recursive call does not count
			v := in.alwaysMatches(ri.def.Expr)
			ri.always = 3
			if v {
				ri.always = 2
			}
			return v
		case 2:
			return true
		}
	}
	return false
}

// binds reports whether e makes captures or defines variables, not counting called rules (whose
// captures and variables are not visible to the caller).
func (in *info) binds(e grammar.Expr) bool {
	b := false
	walk(e, func(x grammar.Expr) {
		switch x := x.(type) {
		case *grammar.Capture:
			b = true
		case *grammar.Predicate:
			if _, ok := x.Term.(*grammar.Assign); ok {
				b = true
			}
		}
	})
	return b
}

// classRanges returns the code points that the class matches as sorted, disjoint ranges of valid code
// points: the complement for a negated class, and without the surrogates, which no input can contain.
func (in *info) classRanges(c *grammar.CharClass) []grammar.CharRange {
	if rs, ok := in.classes[c]; ok {
		return rs
	}
	rs := append([]grammar.CharRange(nil), c.Ranges...)
	slices.SortFunc(rs, func(a, b grammar.CharRange) int { return cmp.Compare(a.Lo, b.Lo) })
	var merged []grammar.CharRange
	for _, r := range rs {
		if n := len(merged); n > 0 && r.Lo <= merged[n-1].Hi+1 {
			merged[n-1].Hi = max(merged[n-1].Hi, r.Hi)
		} else {
			merged = append(merged, r)
		}
	}
	if c.Negated {
		var comp []grammar.CharRange
		next := rune(0)
		for _, r := range merged {
			if r.Lo > next {
				comp = append(comp, grammar.CharRange{Lo: next, Hi: r.Lo - 1})
			}
			next = max(next, r.Hi+1)
		}
		if next <= unicode.MaxRune {
			comp = append(comp, grammar.CharRange{Lo: next, Hi: unicode.MaxRune})
		}
		merged = comp
	}
	var valid []grammar.CharRange
	for _, r := range merged {
		r.Hi = min(r.Hi, unicode.MaxRune)
		for _, part := range []grammar.CharRange{{Lo: r.Lo, Hi: min(r.Hi, 0xD7FF)}, {Lo: max(r.Lo, 0xE000), Hi: r.Hi}} {
			if part.Lo <= part.Hi {
				valid = append(valid, part)
			}
		}
	}
	in.classes[c] = valid
	return valid
}

// hasCut reports whether e contains a cut, not counting called rules (a cut does not reach beyond its
// rule).
func (in *info) hasCut(e grammar.Expr) bool {
	cut := false
	walk(e, func(x grammar.Expr) {
		if _, ok := x.(*grammar.Cut); ok {
			cut = true
		}
	})
	return cut
}

// hasCaptures reports whether e contains a capture, not counting called rules.
func (in *info) hasCaptures(e grammar.Expr) bool {
	if v, ok := in.captures[e]; ok {
		return v
	}
	v := false
	walk(e, func(x grammar.Expr) {
		if _, ok := x.(*grammar.Capture); ok {
			v = true
		}
	})
	in.captures[e] = v
	return v
}

// reach returns the targets that generating e can exercise.
func (in *info) reach(e grammar.Expr) bitset {
	if s, ok := in.reachMemo[e]; ok {
		return s
	}
	s := newBitset(len(in.targets))
	in.localTargets(e, s)
	walk(e, func(x grammar.Expr) {
		if r, ok := x.(*grammar.Ref); ok {
			if ri := in.rules[r.Name]; ri != nil {
				s.union(ri.reach)
			}
		}
	})
	in.reachMemo[e] = s
	return s
}

// height returns the minimal number of nested rule calls needed to match e, or inf if e can never
// match, using the current estimates of the rules' heights.
func (in *info) height(e grammar.Expr) int {
	switch e := e.(type) {
	case *grammar.Ref:
		if ri := in.rules[e.Name]; ri != nil && ri.height < inf {
			return ri.height + 1
		}
		return inf
	case *grammar.Bottom:
		return inf
	case *grammar.Seq:
		h := 0
		for _, it := range e.Items {
			h = max(h, in.height(it))
		}
		return h
	case *grammar.Choice:
		h := inf
		for _, a := range e.Alts {
			h = min(h, in.height(a))
		}
		return h
	case *grammar.Repeat:
		if e.Min == 0 {
			return 0
		}
		return in.height(e.Expr)
	case *grammar.Optional, *grammar.Not:
		return 0
	case *grammar.And:
		return in.height(e.Expr)
	case *grammar.Atomic:
		return in.height(e.Expr)
	case *grammar.Discard:
		return in.height(e.Expr)
	case *grammar.Capture:
		return in.height(e.Expr)
	case *grammar.Attributed:
		return in.height(e.Expr)
	case *grammar.Pratt:
		h := inf
		for _, o := range e.Operands {
			h = min(h, in.height(o.Expr))
		}
		return h
	}
	return 0
}

// length returns the minimal length in bytes of a match of e, or inf if e can never match.
func (in *info) length(e grammar.Expr) int {
	switch e := e.(type) {
	case *grammar.Ref:
		if ri := in.rules[e.Name]; ri != nil {
			return ri.length
		}
		return inf
	case *grammar.Literal:
		return len(e.Value)
	case *grammar.CharClass, *grammar.Any:
		return 1
	case *grammar.Bottom:
		return inf
	case *grammar.Seq:
		l := 0
		for _, it := range e.Items {
			l = min(inf, l+in.length(it))
		}
		return l
	case *grammar.Choice:
		l := inf
		for _, a := range e.Alts {
			l = min(l, in.length(a))
		}
		return l
	case *grammar.Repeat:
		if e.Min == 0 {
			return 0
		}
		return min(inf, e.Min*in.length(e.Expr))
	case *grammar.Optional, *grammar.Not, *grammar.And:
		return 0
	case *grammar.Atomic:
		return in.length(e.Expr)
	case *grammar.Discard:
		return in.length(e.Expr)
	case *grammar.Capture:
		return in.length(e.Expr)
	case *grammar.Attributed:
		return in.length(e.Expr)
	case *grammar.Pratt:
		l := inf
		for _, o := range e.Operands {
			l = min(l, in.length(o.Expr))
		}
		return l
	}
	return 0
}

// restrictLevels leaves out of the coverage the operators of Pratt levels that no call reaches: when a
// Pratt rule is only called with a level (e(mul)), the operators of looser levels are never generated.
func (in *info) restrictLevels() {
	lowest := map[*ruleInfo]int{}
	if in.start != nil && in.start.pratt != nil {
		lowest[in.start] = 0
	}
	for _, ri := range in.order {
		if !in.reachable[ri.index] {
			continue
		}
		for _, r := range ri.possibleRefs {
			callee := in.rules[r.Name]
			if callee == nil || callee.pratt == nil {
				continue
			}
			l := callee.levels[r.Level] // 0 without a level
			if old, ok := lowest[callee]; !ok || l < old {
				lowest[callee] = l
			}
		}
	}
	for ri, l := range lowest {
		for i := 0; i < l; i++ {
			for _, op := range ri.pratt.Levels[i].Operators {
				in.targets[in.ops[op]].possible = false
			}
		}
	}
}

// closure returns, for each rule, whether the start rule reaches it through the calls that calls
// returns.
func (in *info) closure(calls func(*ruleInfo) []string) []bool {
	seen := make([]bool, len(in.order))
	var visit func(ri *ruleInfo)
	visit = func(ri *ruleInfo) {
		if seen[ri.index] {
			return
		}
		seen[ri.index] = true
		for _, c := range calls(ri) {
			if callee := in.rules[c]; callee != nil {
				visit(callee)
			}
		}
	}
	if in.start != nil {
		visit(in.start)
	}
	return seen
}

// walkPossible is walk restricted to expressions that can match: it skips an expression that can never
// match and everything inside it.
func (in *info) walkPossible(e grammar.Expr, f func(grammar.Expr)) {
	if e == nil || in.height(e) >= inf {
		return
	}
	f(e)
	switch e := e.(type) {
	case *grammar.Seq:
		for _, it := range e.Items {
			in.walkPossible(it, f)
		}
	case *grammar.Choice:
		for _, a := range e.Alts {
			in.walkPossible(a, f)
		}
	case *grammar.Repeat:
		in.walkPossible(e.Expr, f)
	case *grammar.Optional:
		in.walkPossible(e.Expr, f)
	case *grammar.And:
		in.walkPossible(e.Expr, f)
	case *grammar.Atomic:
		in.walkPossible(e.Expr, f)
	case *grammar.Discard:
		in.walkPossible(e.Expr, f)
	case *grammar.Capture:
		in.walkPossible(e.Expr, f)
	case *grammar.Attributed:
		in.walkPossible(e.Expr, f)
	case *grammar.Pratt:
		in.walkPossible(e.Skip, f)
		for _, o := range e.Operands {
			in.walkPossible(o.Expr, f)
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				in.walkPossible(op.Expr, f)
			}
		}
	}
}

// walk calls f for e and every parsing expression inside it that generation can produce: it does not
// enter called rules, negative lookaheads or attribute arguments (the skip of #recover).
func walk(e grammar.Expr, f func(grammar.Expr)) {
	if e == nil {
		return
	}
	f(e)
	switch e := e.(type) {
	case *grammar.Seq:
		for _, it := range e.Items {
			walk(it, f)
		}
	case *grammar.Choice:
		for _, a := range e.Alts {
			walk(a, f)
		}
	case *grammar.Repeat:
		walk(e.Expr, f)
	case *grammar.Optional:
		walk(e.Expr, f)
	case *grammar.And:
		walk(e.Expr, f)
	case *grammar.Not:
		// Not walked: generation never produces what a negative lookahead matches.
	case *grammar.Atomic:
		walk(e.Expr, f)
	case *grammar.Discard:
		walk(e.Expr, f)
	case *grammar.Capture:
		walk(e.Expr, f)
	case *grammar.Attributed:
		// Attribute arguments (the skip of #recover) are not walked: valid inputs never
		// exercise them.
		walk(e.Expr, f)
	case *grammar.Pratt:
		walk(e.Skip, f)
		for _, o := range e.Operands {
			walk(o.Expr, f)
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				walk(op.Expr, f)
			}
		}
	}
}

func dedupe(xs []string) []string {
	sort.Strings(xs)
	out := xs[:0]
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// bitset is a set of small non-negative integers.
type bitset []uint64

func newBitset(n int) bitset { return make(bitset, (n+63)/64) }

func (s bitset) set(i int)      { s[i/64] |= 1 << (i % 64) }
func (s bitset) clear(i int)    { s[i/64] &^= 1 << (i % 64) }
func (s bitset) has(i int) bool { return s[i/64]&(1<<(i%64)) != 0 }

// union adds t to s and reports whether s changed.
func (s bitset) union(t bitset) bool {
	changed := false
	for i, w := range t {
		if s[i]|w != s[i] {
			s[i] |= w
			changed = true
		}
	}
	return changed
}

// anyNotIn reports whether s has an element that is in neither a nor b.
func (s bitset) anyNotIn(a, b bitset) bool {
	for i, w := range s {
		if w&^a[i]&^b[i] != 0 {
			return true
		}
	}
	return false
}

// walkTerm calls f for t and every term inside it.
func walkTerm(t grammar.Term, f func(grammar.Term)) {
	if t == nil {
		return
	}
	f(t)
	switch t := t.(type) {
	case *grammar.Member:
		walkTerm(t.X, f)
	case *grammar.New:
		for _, fi := range t.Fields {
			walkTerm(fi.Value, f)
		}
	case *grammar.Call:
		for _, a := range t.Args {
			walkTerm(a, f)
		}
	case *grammar.Lambda:
		walkTerm(t.Body, f)
	case *grammar.Binary:
		walkTerm(t.L, f)
		walkTerm(t.R, f)
	case *grammar.Unary:
		walkTerm(t.X, f)
	case *grammar.Assign:
		walkTerm(t.Value, f)
	}
}
