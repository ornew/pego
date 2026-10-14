package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

func at(p grammar.Pos) string {
	if !p.IsValid() {
		return ""
	}
	return fmt.Sprintf(" at %d:%d", p.Line, p.Col)
}

// --- unreachable-rule ---

func (l *linter) unreachable(start string) {
	control := l.control
	control.poll()

	a := l.an
	if a.rules[start] == nil {
		return
	}
	seen := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) > 0 {
		control.poll()
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range callsControlled(a.rules[n].Expr, control) {
			control.poll()
			if !seen[c] && a.rules[c] != nil {
				seen[c] = true
				stack = append(stack, c)
			}
		}
	}
	callers := map[string][]string{}
	for _, r := range a.order {
		control.poll()
		for _, c := range callsControlled(r.Expr, control) {
			control.poll()
			if c != r.Name && !slices.Contains(callers[c], r.Name) {
				callers[c] = append(callers[c], r.Name)
			}
		}
	}
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		if seen[r.Name] {
			return
		}
		fix := "remove the rule, or call it from a rule that " + start + " reaches"
		if cs := callers[r.Name]; len(cs) > 0 {
			l.report(CheckUnreachableRule, Warning, r.Diagnostic(), fix,
				"rule %s is never used: only %s, which %s does not reach either, call it", r.Name, strings.Join(cs, ", "), start)
			return
		}
		l.report(CheckUnreachableRule, Warning, r.Diagnostic(), fix, "rule %s is never used: %s does not call it, directly or indirectly", r.Name, start)
	})
}

// --- shadowed-alternative ---

func (l *linter) shadowed() {
	control := l.control
	control.poll()

	calledWithLevel := map[string]bool{}
	for _, r := range l.an.order {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			if ref, ok := e.(*grammar.Ref); ok && ref.Level != "" {
				calledWithLevel[ref.Name] = true
			}
		})
	}
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			switch e := analysisExpr(e).(type) {
			case *grammar.Choice:
				l.shadowedAlts(e.Alts, "alternative")
			case *grammar.OperatorsRule:
				var operands []grammar.Expr
				for _, o := range e.Operands {
					control.poll()
					operands = append(operands, o.Expr)
				}
				l.shadowedAlts(operands, "operand")
				l.shadowedOperators(e, !calledWithLevel[r.Name])
			}
		})
	})
}

// shadowedAlts reports the alternatives of an ordered choice that can never match because an
// earlier alternative matches wherever they could.
func (l *linter) shadowedAlts(alts []grammar.Expr, kind string) {
	control := l.control
	control.poll()

	a := l.an
	if len(alts) < 2 {
		return
	}
	for i, alt := range alts[:len(alts)-1] {
		control.poll()
		if a.succeeds(alt) {
			later := fmt.Sprintf("%s %d (%s)", kind, i+2, show(alts[i+1]))
			if i+2 < len(alts) {
				later = fmt.Sprintf("%ss %d to %d", kind, i+2, len(alts))
			}
			l.report(CheckShadowedAlt, Error, posOf(alts[i+1]),
				fmt.Sprintf("remove what follows %s %d, or make it fail where it matches nothing", kind, i+1)+
					a.shadowedSuffixCaveat(alt, alts[i+1:]...),
				"%s never tried: %s %d (%s%s) always succeeds", plural(later), kind, i+1, show(alt), at(posOf(alt)))
			alts = alts[:i+1]
			break
		}
	}
	for j := 1; j < len(alts); j++ {
		control.poll()
		for i := range j {
			control.poll()
			switch a.covers(alts[i], alts[j]) {
			case "duplicate":
				l.report(CheckShadowedAlt, Error, posOf(alts[j]), "remove it"+a.leftRecursionCaveat(alts[j]),
					"%s %d (%s) can never match: it is the same as %s %d%s", kind, j+1, show(alts[j]), kind, i+1, at(posOf(alts[i])))
			case "prefix":
				l.report(CheckShadowedAlt, Error, posOf(alts[j]), fmt.Sprintf("move it before %s %d", kind, i+1)+a.leftRecursionCaveat(alts[j]),
					"%s %d (%s) can never match: %s %d (%s%s) matches first wherever it could",
					kind, j+1, show(alts[j]), kind, i+1, show(alts[i]), at(posOf(alts[i])))
			default:
				continue
			}
			break
		}
	}
}

// The compiler's universal-success proof excludes this exact suffix from its
// left-call graph. Stronger, input-dependent lint proofs may still change the
// conservative graph when their suggested fix is applied.
func (a *analysis) shadowedSuffixCaveat(prior grammar.Expr, later ...grammar.Expr) string {
	control := a.control
	control.poll()

	if grammaranalysis.AlwaysSucceeds(prior, a.engineSucceeding) {
		return ""
	}
	return a.leftRecursionCaveat(later...)
}

// leftRecursionCaveat returns a caution to add to a fix that removes or moves the expressions es,
// if they call a left-recursive rule before consuming input: the engine decides how to grow left
// recursion from a conservative graph. Calls whose infeasibility is not established
// by the compiler still participate, so removing them can change the result.
func (a *analysis) leftRecursionCaveat(es ...grammar.Expr) string {
	control := a.control
	control.poll()

	for _, e := range es {
		control.poll()
		for _, c := range a.leftCalls(e, nil) {
			control.poll()
			if a.leftRecursive[c] {
				return " (it calls " + c + ", which is left-recursive, before consuming input: " +
					"this changes how the left recursion is grown, so compare the results)"
			}
		}
	}
	return ""
}

func plural(s string) string {
	if strings.Contains(s, " to ") {
		return s + " are"
	}
	return s + " is"
}

// covers tells whether x, tried before y in an ordered choice, matches wherever y could:
// "duplicate" if they are the same, "prefix" if x matches a prefix of every match of y, and ""
// if neither is certain.
//
// Both start in the same state at the same position. If y matches, its first k items, which
// are the same as those of x, match the same way in x; the rest of x then has to succeed on
// every input that begins with the text that the rest of y must begin with.
func (a *analysis) covers(x, y grammar.Expr) string {
	control := a.control
	control.poll()

	// Capture names only matter to predicates.
	caps := !hasPredicate(x) && !hasPredicate(y)
	xs, ys := items(x, caps), items(y, caps)
	k := 0
	for k < len(xs) && k < len(ys) && same(xs[k], ys[k], caps) {
		control.poll()
		k++
	}
	switch {
	case k == len(xs) && k == len(ys):
		return "duplicate"
	case k == len(xs):
		return "prefix"
	case k == len(ys):
		return ""
	}
	rest := &grammar.Seq{Items: xs[k:]}
	if a.matchPrefix(rest, a.prefix(&grammar.Seq{Items: ys[k:]}).s).ok {
		return "prefix"
	}
	return ""
}

// shadowedOperators reports operators of an Operators Rule expression that are never selected because an
// operator declared before them matches the same text: the longest match wins, and the first
// declared among equals. Operators of different levels are compared only if the rule is never
// called with a level (which can leave out the earlier one).
func (l *linter) shadowedOperators(pr *grammar.OperatorsRule, acrossLevels bool) {
	control := l.control
	control.poll()

	type op struct {
		*grammar.OperatorsRuleOperator
		level int
	}
	var ops []op
	for i, lv := range pr.Levels {
		control.poll()
		for _, o := range lv.Operators {
			control.poll()
			ops = append(ops, op{o, i})
		}
	}
	prefix := func(o op) bool { control.poll(); return o.Kind == grammar.Prefix }
	for j, oj := range ops {
		control.poll()
		for _, oi := range ops[:j] {
			control.poll()
			if prefix(oi) != prefix(oj) || (!acrossLevels && oi.level != oj.level) || !same(oi.Expr, oj.Expr, true) {
				continue
			}
			l.report(CheckShadowedAlt, Error, oj.Diagnostic(), "remove it, or merge the two operators",
				"%s operator %s is never selected: the %s operator%s matches the same text and is declared first",
				oj.Kind, show(oj.Expr), oi.Kind, at(oi.Diagnostic()))
			break
		}
	}
}

// --- never-matches ---

func (l *linter) neverMatches() {
	control := l.control
	control.poll()

	a := l.an
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			s, ok := e.(*grammar.Seq)
			if !ok {
				return
			}
			an := a.anchorConflict(s)
			if an == nil {
				return
			}
			i := slices.Index(s.Items, an)
			before, after := &grammar.Seq{Items: s.Items[:i]}, &grammar.Seq{Items: s.Items[i+1:]}
			switch an.(type) {
			case *grammar.EndInput:
				l.report(CheckNeverMatches, Error, posOf(an), "move $$ to the end of the sequence",
					"`$$` is followed by %s, which cannot match at the end of the input, so the sequence can never match", show(after))
			case *grammar.BeginInput:
				l.report(CheckNeverMatches, Error, posOf(an), "move ^^ to the beginning of the sequence",
					"`^^` follows %s, which must consume input, so the sequence can never match", show(before))
			case *grammar.EndLine:
				l.report(CheckNeverMatches, Error, posOf(an), "match the line break after $, or remove $",
					"`$` (end of line) is followed by %s, which begins with %q rather than a line break, so the sequence can never match",
					show(after), firstChar(a.prefix(after).s))
			case *grammar.BeginLine:
				l.report(CheckNeverMatches, Error, posOf(an), "remove ^, or match a line break before it",
					"`^` (beginning of line) follows %s, which never ends with a line feed, so the sequence can never match", show(before))
			}
		})
	})

	// Recursion without a base case. The rules that cannot match even if _|_ and ! of an
	// expression that always succeeds are taken to match fail because of recursion. Group them by
	// mutual recursion, and report a group only if it still cannot match when every rule outside
	// it is assumed to: otherwise the cause is a rule it calls, which is reported in its own group.
	var stuck []string
	edges := map[string][]string{}
	for _, r := range a.order {
		control.poll()
		if a.baseCase[r.Name] {
			continue
		}
		stuck = append(stuck, r.Name)
		for _, c := range callsControlled(r.Expr, control) {
			control.poll()
			if a.rules[c] != nil && !a.baseCase[c] && !slices.Contains(edges[r.Name], c) {
				edges[r.Name] = append(edges[r.Name], c)
			}
		}
	}
	report := map[string][]string{} // rule -> its group
	for _, group := range sccsControlled(stuck, edges, control) {
		control.poll()
		if len(group) == 1 && !slices.Contains(edges[group[0]], group[0]) {
			continue // not recursive
		}
		assumed := map[string]bool{}
		for _, n := range stuck {
			control.poll()
			if !slices.Contains(group, n) {
				assumed[n] = true
			}
		}
		for n := range a.baseCase {
			control.poll()
			assumed[n] = true
		}
		for changed := true; changed; {
			control.poll()
			changed = false
			for _, n := range group {
				control.poll()
				if !assumed[n] && a.mayMatchIn(a.rules[n].Expr, assumed, true) {
					assumed[n], changed = true, true
				}
			}
		}
		for _, n := range group {
			control.poll()
			if !assumed[n] {
				report[n] = group
			}
		}
	}
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		group, ok := report[r.Name]
		if !ok {
			return
		}
		what := r.Name
		if len(group) > 1 {
			what = "one of " + strings.Join(group, ", ")
		}
		l.report(CheckNeverMatches, Error, r.Diagnostic(), "add an alternative that does not recurse (a base case)",
			"rule %s can never match: every match of it would contain a match of %s, so the recursion has no base case",
			r.Name, what)
	})
}

// sccs returns the strongly connected components of the graph, each in the order of nodes, and
// the components in the order of their first nodes.
func sccs(nodes []string, edges map[string][]string) [][]string {
	return sccsControlled(nodes, edges, nil)
}

func sccsControlled(nodes []string, edges map[string][]string, control *lintControl) [][]string {
	control.poll()

	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	var visit func(v string)
	visit = func(v string) {
		control.poll()
		index[v], low[v] = len(index), len(index)
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			control.poll()
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var c []string
			for {
				control.poll()
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				c = append(c, w)
				if w == v {
					break
				}
			}
			out = append(out, c)
		}
	}
	for _, n := range nodes {
		control.poll()
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}
	pos := map[string]int{}
	for i, n := range nodes {
		control.poll()
		pos[n] = i
	}
	for _, c := range out {
		control.poll()
		slices.SortFunc(c, func(x, y string) int { control.poll(); return pos[x] - pos[y] })
	}
	slices.SortFunc(out, func(x, y []string) int { control.poll(); return pos[x[0]] - pos[y[0]] })
	return out
}

func firstChar(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// findCycle returns a path of edges from start back to start (without the final start), or nil.
func findCycle(start string, edges map[string][]string) []string {
	return findCycleControlled(start, edges, nil)
}

func findCycleControlled(start string, edges map[string][]string, control *lintControl) []string {
	control.poll()

	seen := map[string]bool{}
	var path []string
	var visit func(n string) bool
	visit = func(n string) bool {
		control.poll()
		path = append(path, n)
		for _, m := range edges[n] {
			control.poll()
			if m == start {
				return true
			}
			if !seen[m] {
				seen[m] = true
				if visit(m) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if visit(start) {
		return path
	}
	return nil
}

// --- useless-lookahead ---

func (l *linter) lookaheads() {
	control := l.control
	control.poll()

	a := l.an
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			switch e := analysisExpr(e).(type) {
			case *grammar.And:
				// Captures in a positive lookahead stay in effect, so it may be there for them.
				if !hasCaptures(e.Expr) && a.succeeds(e.Expr) {
					l.report(CheckUselessLookahead, Warning, e.Diagnostic(), "remove the lookahead",
						"%s always succeeds, because %s always does: the lookahead has no effect", show(e), show(e.Expr))
				}
			case *grammar.Not:
				switch {
				case a.succeeds(e.Expr):
					l.report(CheckUselessLookahead, Error, e.Diagnostic(), "negate an expression that must consume input",
						"%s can never succeed, because %s always does (it can match without consuming input)", show(e), show(e.Expr))
				case !a.mayMatch(e.Expr):
					l.report(CheckUselessLookahead, Warning, e.Diagnostic(), "remove the lookahead",
						"%s always succeeds, because %s can never match", show(e), show(e.Expr))
				}
			}
		})
	})
}

// visible reports whether the expression has a value (as the engine decides it).
func visible(e grammar.Expr) bool {
	switch e := analysisExpr(e).(type) {
	case *grammar.And, *grammar.Not, *grammar.Discard, *grammar.Cut, *grammar.Bottom,
		*grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine, *grammar.EndLine,
		*grammar.Predicate:
		return false
	case *grammar.Capture:
		return visible(e.Expr)
	case *grammar.Attributed:
		return visible(e.Expr)
	}
	return true
}

func hasCaptures(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(x grammar.Expr) {
		if _, ok := x.(*grammar.Capture); ok {
			found = true
		}
	})
	return found
}

// --- nullable-repetition, redundant-optional ---

func (l *linter) nullableRepetitions() {
	control := l.control
	control.poll()

	a := l.an
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			rp, ok := e.(*grammar.Repeat)
			if !ok || rp.Max == 0 || !a.isNullable(rp.Expr) {
				return
			}
			fix := "make the element consume input"
			switch inner := neutral(rp.Expr, false).(type) {
			case *grammar.Optional:
				fix = fmt.Sprintf("repeat %s instead of %s", show(inner.Expr), show(inner))
			case *grammar.Repeat:
				if inner.Min <= 1 && inner.Max < 0 {
					fix = fmt.Sprintf("repeat %s once, without the inner repetition", show(inner.Expr))
				}
			}
			l.report(CheckNullableRepetition, Warning, rp.Diagnostic(), fix,
				"the element of %s can match without consuming input: the repetition then stops, with an empty element in its list",
				show(rp))
		})
	})
}

func (l *linter) redundantOptionals() {
	control := l.control
	control.poll()

	a := l.an
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			// x? has a value (nil) even where x has none, so only an x with a value is the same.
			if o, ok := e.(*grammar.Optional); ok && visible(o.Expr) && a.succeeds(o.Expr) {
				l.report(CheckRedundantOptional, Warning, o.Diagnostic(), "remove the ?",
					"%s is the same as %s, which always succeeds", show(o), show(o.Expr))
			}
		})
	})
}

// --- unused-capture ---

func (l *linter) unusedCaptures() {
	control := l.control
	control.poll()

	terminal := map[string]bool{}
	for _, t := range l.an.g.Types() {
		control.poll()
		if _, ok := t.Spec.(*grammar.TerminalSpec); ok {
			terminal[t.Name] = true
		}
	}
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		if pr, ok := r.Expr.(*grammar.OperatorsRule); ok {
			for _, o := range pr.Operands {
				control.poll()
				if o.Action != nil {
					l.scopeCaptures(o.Expr, o.Action, usesIndex(o.Action), "the operand's action does not refer to it")
				}
			}
			for _, lv := range pr.Levels {
				control.poll()
				for _, op := range lv.Operators {
					control.poll()
					if op.Action != nil {
						l.scopeCaptures(op.Expr, op.Action, false, "the operator's action does not refer to it")
					}
				}
			}
			return
		}
		switch t, _ := ungroupType(r.Type).(*grammar.TypeRef); {
		case r.Action != nil:
			l.scopeCaptures(r.Expr, r.Action, usesIndex(r.Action), "the action does not refer to it")
		case t != nil && terminal[t.Name]:
			l.scopeCaptures(r.Expr, nil, false, fmt.Sprintf("the rule produces a terminal of type %s, which keeps no captures", t.Name))
		}
	})
}

// scopeCaptures reports the captures of a rule body (or an Operators Rule line) that are never read: the
// body's own captures that neither action nor predicates refer to, and the captures inside
// repetitions whose values are discarded. kept reports whether the value of the body itself can
// be read ($n in the action).
func (l *linter) scopeCaptures(body grammar.Expr, action grammar.Term, kept bool, why string) {
	control := l.control
	control.poll()

	refs := map[string]bool{}
	captureRefsControlled(action, nil, refs, control)
	scopePredicateRefsControlled(body, refs, control)
	var visit func(e grammar.Expr, inCapture bool)
	visit = func(e grammar.Expr, inCapture bool) {
		control.poll()
		switch e := analysisExpr(e).(type) {
		case *grammar.Capture:
			if !refs[e.Name] {
				l.report(CheckUnusedCapture, Warning, e.Diagnostic(), fmt.Sprintf("remove %s:, or use $%s", e.Name, e.Name),
					"capture %s is never used: %s", e.Name, why)
			}
			visit(e.Expr, true)
		case *grammar.Repeat:
			l.elementCaptures(e.Expr, kept || inCapture)
		default:
			forChildren(e, func(c grammar.Expr) { control.poll(); visit(c, inCapture) })
		}
	}
	visit(body, false)
}

// elementCaptures reports the captures in the element of a repetition whose value is discarded
// (kept is false), unless a predicate of the element reads them.
func (l *linter) elementCaptures(elem grammar.Expr, kept bool) {
	control := l.control
	control.poll()

	refs := map[string]bool{}
	scopePredicateRefsControlled(elem, refs, control)
	var visit func(e grammar.Expr, inCapture bool)
	visit = func(e grammar.Expr, inCapture bool) {
		control.poll()
		switch e := analysisExpr(e).(type) {
		case *grammar.Capture:
			if !kept && !refs[e.Name] {
				l.report(CheckUnusedCapture, Warning, e.Diagnostic(), fmt.Sprintf("remove %s:, or capture the repetition and use it", e.Name),
					"capture %s is never used: the value of the repetition it is in is discarded", e.Name)
			}
			visit(e.Expr, true)
		case *grammar.Repeat:
			l.elementCaptures(e.Expr, kept || inCapture)
		default:
			forChildren(e, func(c grammar.Expr) { control.poll(); visit(c, inCapture) })
		}
	}
	visit(elem, false)
}

// forChildren calls f for the parsing expressions directly inside e (not attribute arguments,
// which cannot hold captures).
func forChildren(e grammar.Expr, f func(grammar.Expr)) {
	switch e := analysisExpr(e).(type) {
	case *grammar.Seq:
		for _, it := range e.Items {
			f(it)
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			f(alt)
		}
	case *grammar.Repeat:
		f(e.Expr)
	case *grammar.Optional:
		f(e.Expr)
	case *grammar.And:
		f(e.Expr)
	case *grammar.Not:
		f(e.Expr)
	case *grammar.Atomic:
		f(e.Expr)
	case *grammar.Discard:
		f(e.Expr)
	case *grammar.Capture:
		f(e.Expr)
	case *grammar.Attributed:
		f(e.Expr)
	}
}

// scopePredicateRefs adds the captures that the predicates of a scope read (not those in the
// elements of repetitions, which are scopes of their own) to refs.
func scopePredicateRefs(e grammar.Expr, refs map[string]bool) {
	scopePredicateRefsControlled(e, refs, nil)
}

func scopePredicateRefsControlled(e grammar.Expr, refs map[string]bool, control *lintControl) {
	control.poll()

	switch e := analysisExpr(e).(type) {
	case *grammar.Predicate:
		captureRefsControlled(e.Term, nil, refs, control)
	case *grammar.Repeat:
	default:
		forChildren(e, func(c grammar.Expr) { control.poll(); scopePredicateRefsControlled(c, refs, control) })
	}
}

// captureRefs adds the names of the captures that t refers to ($name, not counting function
// parameters) to refs.
func captureRefs(t grammar.Term, params []string, refs map[string]bool) {
	captureRefsControlled(t, params, refs, nil)
}

func captureRefsControlled(t grammar.Term, params []string, refs map[string]bool, control *lintControl) {
	control.poll()

	switch t := t.(type) {
	case *grammar.TermGroup:
		captureRefsControlled(t.Term, params, refs, control)
	case *grammar.CaptureRef:
		if !slices.Contains(params, t.Name) {
			refs[t.Name] = true
		}
	case *grammar.Lambda:
		captureRefsControlled(t.Body, append(slices.Clip(params), t.Params...), refs, control)
	case *grammar.Member:
		captureRefsControlled(t.X, params, refs, control)
	case *grammar.New:
		for _, fi := range t.Fields {
			control.poll()
			captureRefsControlled(fi.Value, params, refs, control)
		}
	case *grammar.ListConstructor:
		for _, item := range t.Items {
			control.poll()
			captureRefsControlled(item, params, refs, control)
		}
	case *grammar.TerminalConstructor:
		for _, arg := range t.Args {
			control.poll()
			captureRefsControlled(arg, params, refs, control)
		}
	case *grammar.Call:
		for _, a := range t.Args {
			control.poll()
			captureRefsControlled(a, params, refs, control)
		}
	case *grammar.Binary:
		captureRefsControlled(t.L, params, refs, control)
		captureRefsControlled(t.R, params, refs, control)
	case *grammar.Unary:
		captureRefsControlled(t.X, params, refs, control)
	case *grammar.Assign:
		captureRefsControlled(t.Value, params, refs, control)
	}
}

func usesIndex(t grammar.Term) bool {
	found := false
	walkTerm(t, func(x grammar.Term) {
		if _, ok := x.(*grammar.IndexRef); ok {
			found = true
		}
	})
	return found
}

// --- duplicate-capture ---

func (l *linter) duplicateCaptures() {
	control := l.control
	control.poll()

	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		if pr, ok := r.Expr.(*grammar.OperatorsRule); ok {
			for _, o := range pr.Operands {
				control.poll()
				l.scopeDuplicates(o.Expr)
			}
			for _, lv := range pr.Levels {
				control.poll()
				for _, op := range lv.Operators {
					control.poll()
					l.scopeDuplicates(op.Expr)
				}
			}
			return
		}
		l.scopeDuplicates(r.Expr)
	})
}

// scopeDuplicates reports the names captured twice in one match of a scope.
func (l *linter) scopeDuplicates(scope grammar.Expr) {
	control := l.control
	control.poll()

	// captured returns the captures that a match of e can make, in evaluation order, with one
	// capture per name (the first).
	var captured func(e grammar.Expr) []*grammar.Capture
	merge := func(acc []*grammar.Capture, cs []*grammar.Capture, report bool) []*grammar.Capture {
		for _, c := range cs {
			control.poll()
			i := slices.IndexFunc(acc, func(p *grammar.Capture) bool { control.poll(); return p.Name == c.Name })
			switch {
			case i < 0:
				acc = append(acc, c)
			case report:
				l.report(CheckDuplicateCapture, Warning, c.Diagnostic(), "rename one of the captures",
					"%s is captured again in the same match, overwriting the capture%s", c.Name, at(acc[i].Diagnostic()))
			}
		}
		return acc
	}
	captured = func(e grammar.Expr) []*grammar.Capture {
		switch e := analysisExpr(e).(type) {
		case *grammar.Capture:
			// A capture stores its span after its expression matches, so nested captures
			// are evaluated first and the outer capture overwrites them.
			return merge(captured(e.Expr), []*grammar.Capture{e}, true)
		case *grammar.Seq:
			var acc []*grammar.Capture
			for _, it := range e.Items {
				control.poll()
				acc = merge(acc, captured(it), true)
			}
			return acc
		case *grammar.Choice:
			// The alternatives exclude each other.
			var acc []*grammar.Capture
			for _, alt := range e.Alts {
				control.poll()
				acc = merge(acc, captured(alt), false)
			}
			return acc
		case *grammar.Repeat:
			l.scopeDuplicates(e.Expr) // a scope of its own
			return nil
		}
		var acc []*grammar.Capture
		forChildren(e, func(c grammar.Expr) { control.poll(); acc = merge(acc, captured(c), true) })
		return acc
	}
	captured(scope)
}

// --- char-class ---

func (l *linter) charClasses() {
	control := l.control
	control.poll()

	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			if c, ok := e.(*grammar.CharClass); ok {
				l.charClass(c)
			}
		})
	})
}

func (l *linter) charClass(c *grammar.CharClass) {
	control := l.control
	control.poll()

	show := func(rg grammar.CharRange) string {
		return strings.TrimSuffix(strings.TrimPrefix(grammar.FormatExpr(&grammar.CharClass{Ranges: []grammar.CharRange{rg}}), "["), "]")
	}
	fixed := &grammar.CharClass{Negated: c.Negated, Ranges: normalize(c.Ranges)}
	for j, rj := range c.Ranges {
		control.poll()
		for _, ri := range c.Ranges[:j] {
			control.poll()
			if ri.Lo <= rj.Hi && rj.Lo <= ri.Hi {
				what := fmt.Sprintf("%s overlaps %s", show(rj), show(ri))
				if ri == rj {
					what = fmt.Sprintf("%s is listed twice", show(rj))
				}
				l.report(CheckCharClass, Warning, c.Diagnostic(), "write "+grammar.FormatExpr(fixed),
					"in %s, %s", grammar.FormatExpr(c), what)
				return
			}
		}
	}
	kind := func(r rune) int {
		control.poll()
		switch {
		case '0' <= r && r <= '9':
			return 1
		case 'A' <= r && r <= 'Z':
			return 2
		case 'a' <= r && r <= 'z':
			return 3
		}
		return 0
	}
	alnum := []grammar.CharRange{{Lo: '0', Hi: '9'}, {Lo: 'A', Hi: 'Z'}, {Lo: 'a', Hi: 'z'}}
	var spans []string
	var ranges []grammar.CharRange
	for _, rg := range c.Ranges {
		control.poll()
		kl, kh := kind(rg.Lo), kind(rg.Hi)
		if kl == 0 || kh == 0 || kl == kh {
			ranges = append(ranges, rg)
			continue
		}
		var extra strings.Builder
		for r := rg.Lo; r <= rg.Hi; r++ {
			control.poll()
			if kind(r) == 0 {
				extra.WriteRune(r)
			}
		}
		spans = append(spans, fmt.Sprintf("%s also matches %q", show(rg), extra.String()))
		for _, an := range alnum {
			control.poll()
			if lo, hi := max(rg.Lo, an.Lo), min(rg.Hi, an.Hi); lo <= hi {
				ranges = append(ranges, grammar.CharRange{Lo: lo, Hi: hi})
			}
		}
	}
	if len(spans) > 0 {
		fix := &grammar.CharClass{Negated: c.Negated, Ranges: ranges}
		l.report(CheckCharClass, Warning, c.Diagnostic(), "write "+grammar.FormatExpr(fix)+" if only letters and digits are meant",
			"in %s, %s", grammar.FormatExpr(c), strings.Join(spans, ", "))
	}
}

// normalize sorts ranges and merges those that overlap or touch.
func normalize(rs []grammar.CharRange) []grammar.CharRange {
	out := slices.Clone(rs)
	slices.SortFunc(out, func(a, b grammar.CharRange) int { return int(a.Lo - b.Lo) })
	var merged []grammar.CharRange
	for _, r := range out {
		if n := len(merged); n > 0 && r.Lo <= merged[n-1].Hi+1 {
			merged[n-1].Hi = max(merged[n-1].Hi, r.Hi)
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func sameRanges(x, y []grammar.CharRange) bool { return slices.Equal(x, y) }

// --- right-recursion (hint) ---

func (l *linter) rightRecursion() {
	control := l.control
	control.poll()

	a := l.an
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		if _, ok := r.Expr.(*grammar.OperatorsRule); ok {
			return
		}
		var found *grammar.Ref
		var tails func(e grammar.Expr, before []grammar.Expr)
		tails = func(e grammar.Expr, before []grammar.Expr) {
			control.poll()
			switch e := neutral(e, true).(type) {
			case *grammar.Ref:
				if found == nil && e.Name == r.Name && e.Level == "" && !a.isNullable(&grammar.Seq{Items: before}) &&
					slices.ContainsFunc(before, func(x grammar.Expr) bool {
						control.poll()
						return slices.ContainsFunc(callsControlled(x, control), func(c string) bool { control.poll(); return c != r.Name })
					}) {
					found = e
				}
			case *grammar.Seq:
				n := len(e.Items)
				tails(e.Items[n-1], append(slices.Clip(before), e.Items[:n-1]...))
			case *grammar.Choice:
				for _, alt := range e.Alts {
					control.poll()
					tails(alt, before)
				}
			case *grammar.Optional:
				tails(e.Expr, before)
			}
		}
		tails(r.Expr, nil)
		if found != nil {
			l.report(CheckRightRecursion, Hint, found.Diagnostic(), "use a repetition, such as item+ or item (sep item)*",
				"rule %s continues a list by calling itself at its end: each call spans the rest of the list, so after an edit "+
					"a Document evaluates again every call that begins before it, and long lists nest deeply", r.Name)
		}
	})
}

// --- positions (hint) ---

func (l *linter) positions(start string) {
	control := l.control
	control.poll()

	a := l.an
	// Rules called repeatedly: those called in the element of a repetition, and those on a
	// recursion cycle, with the rules they call.
	repeated := map[string]bool{}
	var mark func(name string)
	mark = func(name string) {
		control.poll()
		if repeated[name] || a.rules[name] == nil {
			return
		}
		repeated[name] = true
		for _, c := range callsControlled(a.rules[name].Expr, control) {
			control.poll()
			mark(c)
		}
	}
	edges := map[string][]string{}
	for _, r := range a.order {
		control.poll()
		edges[r.Name] = callsControlled(r.Expr, control)
	}
	for _, r := range a.order {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			if rp, ok := e.(*grammar.Repeat); ok && rp.Max != 0 && rp.Max != 1 {
				for _, c := range callsControlled(rp.Expr, control) {
					control.poll()
					mark(c)
				}
			}
		})
		if findCycleControlled(r.Name, edges, control) != nil {
			mark(r.Name)
		}
	}
	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		p, ok := positionRead(r)
		if !ok || !repeated[r.Name] || start != "" && r.Name != start && !reachesControlled(start, r.Name, edges, control) {
			return
		}
		// The rules that call it, directly or indirectly, may hold its positions too.
		n := 0
		for _, c := range a.order {
			control.poll()
			if c.Name != r.Name && reachesControlled(c.Name, r.Name, edges, control) {
				n++
			}
		}
		callers := ""
		switch n {
		case 0:
		case 1:
			callers = " (nor those of the 1 rule that calls it)"
		default:
			callers = fmt.Sprintf(" (nor those of the %d rules that call it)", n)
		}
		l.report(CheckPositions, Hint, p, "take positions from the tree instead (Node.Start and Node.End are shifted for you)",
			"rule %s reads startPos or endPos and is used repeatedly: a Document cannot move its results past an edit%s, "+
				"so it evaluates them again after every edit before them", r.Name, callers)
	})
}

// reaches reports whether from calls to, directly or indirectly.
func reaches(from, to string, edges map[string][]string) bool {
	return reachesControlled(from, to, edges, nil)
}

func reachesControlled(from, to string, edges map[string][]string, control *lintControl) bool {
	control.poll()

	seen := map[string]bool{from: true}
	stack := []string{from}
	for len(stack) > 0 {
		control.poll()
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range edges[n] {
			control.poll()
			if c == to {
				return true
			}
			if !seen[c] {
				seen[c] = true
				stack = append(stack, c)
			}
		}
	}
	return false
}

// positionRead returns the position of the first startPos or endPos that the rule's actions or
// predicates read (the zero Pos if it is unknown), and whether there is one.
func positionRead(r *grammar.RuleDef) (grammar.Pos, bool) {
	var terms []grammar.Term
	if r.Action != nil {
		terms = append(terms, r.Action)
	}
	walkExpr(r.Expr, func(e grammar.Expr) {
		switch e := analysisExpr(e).(type) {
		case *grammar.Predicate:
			terms = append(terms, e.Term)
		case *grammar.OperatorsRule:
			for _, o := range e.Operands {
				if o.Action != nil {
					terms = append(terms, o.Action)
				}
			}
			for _, lv := range e.Levels {
				for _, op := range lv.Operators {
					if op.Action != nil {
						terms = append(terms, op.Action)
					}
				}
			}
		}
	})
	for _, t := range terms {
		var found *grammar.Member
		walkTerm(t, func(x grammar.Term) {
			if m, ok := x.(*grammar.Member); ok && (m.Name == "startPos" || m.Name == "endPos") && found == nil {
				found = m
			}
		})
		if found != nil {
			return found.Diagnostic(), true
		}
	}
	return grammar.Pos{}, false
}

// --- long-lookahead (hint) ---

func (l *linter) longLookaheads() {
	control := l.control
	control.poll()

	l.eachRule(func(r *grammar.RuleDef) {
		control.poll()
		walkExpr(r.Expr, func(e grammar.Expr) {
			control.poll()
			var inner grammar.Expr
			var pos grammar.Pos
			switch e := analysisExpr(e).(type) {
			case *grammar.And:
				inner, pos = e.Expr, e.Diagnostic()
			case *grammar.Not:
				inner, pos = e.Expr, e.Diagnostic()
			default:
				return
			}
			if rp := anyRepeat(inner); rp != nil {
				l.report(CheckLongLookahead, Hint, pos, "limit the lookahead to the current line or token",
					"%s can examine any amount of input (%s repeats over any character, line breaks included): in a Document, "+
						"an edit anywhere in what it examined makes this rule be evaluated again", show(e), show(rp))
			}
		})
	})
}

// anyRepeat returns an unbounded repetition written in e whose element consumes any character,
// line breaks included: . or a negated character class that does not exclude the line feed.
// Repetitions in the rules that e calls are not considered: those are usually tokens and
// whitespace, which end soon.
func anyRepeat(e grammar.Expr) grammar.Expr {
	var found grammar.Expr
	walkExpr(e, func(x grammar.Expr) {
		if rp, ok := x.(*grammar.Repeat); ok && found == nil && rp.Max < 0 && consumesAny(rp.Expr) {
			found = rp
		}
	})
	return found
}

func consumesAny(e grammar.Expr) bool {
	switch e := analysisExpr(e).(type) {
	case *grammar.Any:
		return true
	case *grammar.CharClass:
		return e.Negated && classAccepts(e, '\n')
	case *grammar.Not, *grammar.And, *grammar.Ref:
		return false
	}
	found := false
	forChildren(e, func(c grammar.Expr) { found = found || consumesAny(c) })
	return found
}

func ungroupType(t grammar.TypeExpr) grammar.TypeExpr {
	for {
		g, ok := t.(*grammar.TypeGroup)
		if !ok {
			return t
		}
		t = g.Type
	}
}
