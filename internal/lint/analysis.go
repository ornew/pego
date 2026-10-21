package lint

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// analysis holds what the checks need to know about a grammar. Every property is either an
// over-approximation or an under-approximation, chosen so that a check built on it reports only
// what is certain:
//
//   - nullable (may succeed without consuming input, at the end of the input or elsewhere) and
//     canMatch (may succeed at all) are over-approximations: "not nullable" and "cannot match"
//     are certain.
//   - succeeds (matchPrefix: certainly succeeds on every input that begins with a given string)
//     and fails (certainly fails on such inputs) are under-approximations.
//   - prefix (a string that every match begins with) is an under-approximation.
//
// None of them account for the nesting limit (WithMaxDepth), which can make any rule call fail.
type analysis struct {
	control *lintControl
	g       *grammar.Grammar
	order   []*grammar.RuleDef
	rules   map[string]*grammar.RuleDef

	// nullable[end] holds the rules that may succeed without consuming input at the end of the
	// input (end = true) or elsewhere (false).
	nullable [2]map[string]bool
	canMatch map[string]bool
	// baseCase holds the rules that can match if _|_ and ! of an expression that always
	// succeeds are taken to match: the rules not in it can only match by matching themselves
	// (or another rule not in it) first.
	baseCase map[string]bool
	// leftRecursive holds the rules on a left-recursion cycle. While such a rule grows its
	// match, a call of it inside the cycle returns the match found so far, which can be a
	// failure: a call of it is never certain to succeed.
	leftRecursive map[string]bool
	// engineNullable is nullability as the engine computes it to find left recursion (cruder
	// than nullable: every lookahead counts as nullable).
	engineNullable   map[string]bool
	engineSucceeding map[string]bool

	// Memos of the analyses that follow rule calls, and the rules being followed (a call of a
	// rule that is being followed is not followed again: the result is "not certain").
	matchMemo  map[matchKey]matchResult
	prefixMemo map[string]prefixResult
	busy       map[string]bool
	// cycles counts the calls not followed because of busy, so that results that depend on
	// them are not memoized.
	cycles int
}

func newAnalysis(g *grammar.Grammar) *analysis {
	return newAnalysisControlled(g, nil)
}

func newAnalysisControlled(g *grammar.Grammar, control *lintControl) *analysis {
	control.poll()

	a := &analysis{control: control, g: g, rules: map[string]*grammar.RuleDef{}, nullable: [2]map[string]bool{{}, {}}, canMatch: map[string]bool{},
		baseCase: map[string]bool{}, leftRecursive: map[string]bool{}, engineNullable: map[string]bool{},
		matchMemo: map[matchKey]matchResult{}, prefixMemo: map[string]prefixResult{}, busy: map[string]bool{}}
	for _, r := range control.rules(g) {
		control.poll()
		if a.rules[r.Name] == nil {
			a.rules[r.Name] = r
			a.order = append(a.order, r)
		}
	}
	// Left recursion first: the other analyses use it (through matchPrefix).
	for changed := true; changed; {
		control.poll()
		changed = false
		for _, r := range a.order {
			control.poll()
			if !a.engineNullable[r.Name] && a.isEngineNullable(r.Expr) {
				a.engineNullable[r.Name], changed = true, true
			}
		}
	}
	left := map[string][]string{}
	for _, r := range a.order {
		control.poll()
		left[r.Name] = a.leftCalls(r.Expr, nil)
	}
	// Match the compiler's graph/proof fixed point: breaking one cycle can
	// prove a rule successful and expose more unreachable call edges.
	for {
		control.poll()
		clear(a.leftRecursive)
		for _, r := range a.order {
			control.poll()
			if findCycleControlled(r.Name, left, control) != nil {
				a.leftRecursive[r.Name] = true
			}
		}
		if control == nil {
			a.engineSucceeding = grammaranalysis.SucceedingRules(a.order, a.leftRecursive)
		} else {
			a.engineSucceeding = grammaranalysis.SucceedingRulesWithPoll(a.order, a.leftRecursive, control.poll)
		}
		next := map[string][]string{}
		changed := false
		for _, r := range a.order {
			control.poll()
			next[r.Name] = a.leftCalls(r.Expr, nil)
			changed = changed || !slices.Equal(left[r.Name], next[r.Name])
		}
		if !changed {
			break
		}
		left = next
	}
	for _, end := range []bool{false, true} {
		control.poll()
		set := a.nullable[b2i(end)]
		for changed := true; changed; {
			control.poll()
			changed = false
			for _, r := range a.order {
				control.poll()
				if !set[r.Name] && a.nullableAt(r.Expr, end) {
					set[r.Name], changed = true, true
				}
			}
		}
	}
	for _, m := range []struct {
		set     map[string]bool
		lenient bool
	}{{a.canMatch, false}, {a.baseCase, true}} {
		for changed := true; changed; {
			control.poll()
			changed = false
			for _, r := range a.order {
				control.poll()
				if !m.set[r.Name] && a.mayMatchIn(r.Expr, m.set, m.lenient) {
					m.set[r.Name], changed = true, true
				}
			}
		}
	}
	return a
}

// isEngineNullable reports whether e may succeed without consuming input, as the engine
// computes it.
func (a *analysis) isEngineNullable(e grammar.Expr) bool {
	control := a.control
	control.poll()

	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Ref:
		return a.engineNullable[e.Name]
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.CharClass, *grammar.Any, *grammar.Bottom:
		return false
	case *grammar.Seq:
		for _, it := range e.Items {
			control.poll()
			if !a.isEngineNullable(it) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			control.poll()
			if a.isEngineNullable(alt) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		return e.Min == 0 || e.Min == 1 && a.isEngineNullable(e.Expr)
	case *grammar.Atomic:
		return a.isEngineNullable(e.Expr)
	case *grammar.Discard:
		return a.isEngineNullable(e.Expr)
	case *grammar.Capture:
		return a.isEngineNullable(e.Expr)
	case *grammar.Attributed:
		return a.isEngineNullable(e.Expr)
	case *grammar.OperatorsRule:
		for _, o := range e.Operands {
			control.poll()
			if a.isEngineNullable(o.Expr) {
				return true
			}
		}
		return false
	}
	return true
}

// leftCalls returns the rules that e may call before consuming input, as the engine finds them.
func (a *analysis) leftCalls(e grammar.Expr, acc []string) []string {
	control := a.control
	control.poll()

	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Ref:
		return append(acc, e.Name)
	case *grammar.Seq:
		for _, it := range e.Items {
			control.poll()
			acc = a.leftCalls(it, acc)
			if !a.isEngineNullable(it) {
				break
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			control.poll()
			acc = a.leftCalls(alt, acc)
			if a.alwaysSucceeds(alt) {
				break
			}
		}
	case *grammar.Repeat:
		if e.Max != 0 {
			acc = a.leftCalls(e.Expr, acc)
		}
	case *grammar.Optional:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.And:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Not:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Atomic:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Discard:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Capture:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Attributed:
		acc = a.leftCalls(e.Expr, acc)
		if !a.alwaysSucceeds(e.Expr) {
			for _, at := range e.Attrs {
				control.poll()
				if at.Name == "recover" {
					if skip, ok := at.Arg("skip"); ok {
						acc = a.leftCalls(skip, acc)
					}
				}
			}
		}
	case *grammar.OperatorsRule:
		if e.Skip != nil {
			acc = a.leftCalls(e.Skip, acc)
		}
		for _, o := range e.Operands {
			control.poll()
			acc = a.leftCalls(o.Expr, acc)
			if a.alwaysSucceeds(o.Expr) {
				break
			}
		}
		for _, l := range e.Levels {
			control.poll()
			for _, op := range l.Operators {
				control.poll()
				if op.Kind == grammar.Prefix {
					acc = a.leftCalls(op.Expr, acc)
				}
			}
		}
	}
	return acc
}

// mayMatch reports whether e may succeed.
func (a *analysis) mayMatch(e grammar.Expr) bool {
	control := a.control
	control.poll()
	return a.mayMatchIn(e, a.canMatch, false)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isNullable reports whether e may succeed without consuming input.
func (a *analysis) isNullable(e grammar.Expr) bool {
	control := a.control
	control.poll()

	return a.nullableAt(e, false) || a.nullableAt(e, true)
}

// nullableAt reports whether e may succeed without consuming input at the end of the input (end
// set) or at another position. Telling the two apart makes "!$$ x" not nullable when x can only
// match nothing at the end of the input, as in a line that ends with ("\n" / $$).
func (a *analysis) nullableAt(e grammar.Expr, end bool) bool {
	control := a.control
	control.poll()

	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Ref:
		return a.nullable[b2i(end)][e.Name]
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.CharClass, *grammar.Any, *grammar.Bottom:
		return false
	case *grammar.EndInput:
		return end
	case *grammar.Seq:
		for _, it := range e.Items {
			control.poll()
			if !a.nullableAt(it, end) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			control.poll()
			if a.nullableAt(alt, end) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		return e.Min == 0 || e.Min == 1 && a.nullableAt(e.Expr, end)
	case *grammar.And:
		// At the end of the input, the operand can only match without consuming input.
		return !end || a.nullableAt(e.Expr, true)
	case *grammar.Not:
		return !a.succeedsAt(e.Expr, end)
	case *grammar.Atomic:
		return a.nullableAt(e.Expr, end)
	case *grammar.Discard:
		return a.nullableAt(e.Expr, end)
	case *grammar.Capture:
		return a.nullableAt(e.Expr, end)
	case *grammar.Attributed:
		// A recovery must consume input.
		return a.nullableAt(e.Expr, end)
	case *grammar.OperatorsRule:
		for _, o := range e.Operands {
			control.poll()
			if a.nullableAt(o.Expr, end) {
				return true
			}
		}
		return false
	}
	// Optional, predicate, other anchors, cut, top
	return true
}

// succeedsAt reports whether e certainly succeeds at the end of the input (end set) or at any
// other position.
func (a *analysis) succeedsAt(e grammar.Expr, end bool) bool {
	control := a.control
	control.poll()

	switch e := neutralControlled(e, true, control).(type) {
	case *grammar.EndInput:
		return end
	case *grammar.Any:
		return !end
	case *grammar.Not:
		return a.failsAt(e.Expr, end)
	}
	return a.succeeds(e)
}

// failsAt reports whether e certainly fails at the end of the input (end set) or at any other
// position.
func (a *analysis) failsAt(e grammar.Expr, end bool) bool {
	control := a.control
	control.poll()

	switch e := neutralControlled(e, true, control).(type) {
	case *grammar.EndInput:
		return !end
	case *grammar.Any, *grammar.CharClass:
		return end
	case *grammar.Literal:
		return end && e.Value != ""
	case *grammar.Bottom:
		return true
	case *grammar.Not:
		return a.succeedsAt(e.Expr, end)
	}
	return false
}

// mayMatchIn reports whether e may succeed, given the rules in set that may (a least fixed
// point: a rule that can only match by matching itself first never matches). With lenient set,
// _|_ and ! of an expression that always succeeds are taken to match.
//
// Anchors that contradict their neighbors (anchorConflict) are not taken into account, so that
// canMatch is known before prefix, which uses it, runs.
func (a *analysis) mayMatchIn(e grammar.Expr, set map[string]bool, lenient bool) bool {
	control := a.control
	control.poll()

	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Ref:
		return set[e.Name]
	case *grammar.Bottom:
		return lenient
	case *grammar.Seq:
		for _, it := range e.Items {
			control.poll()
			if !a.mayMatchIn(it, set, lenient) {
				return false
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			control.poll()
			if a.mayMatchIn(alt, set, lenient) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		return e.Min == 0 || a.mayMatchIn(e.Expr, set, lenient)
	case *grammar.And:
		return a.mayMatchIn(e.Expr, set, lenient)
	case *grammar.Not:
		return lenient || !a.succeeds(e.Expr)
	case *grammar.Atomic:
		return a.mayMatchIn(e.Expr, set, lenient)
	case *grammar.Discard:
		return a.mayMatchIn(e.Expr, set, lenient)
	case *grammar.Capture:
		return a.mayMatchIn(e.Expr, set, lenient)
	case *grammar.Attributed:
		return hasAttrControlled(e, "recover", control) || hasAttrControlled(e, "missing", control) || a.mayMatchIn(e.Expr, set, lenient)
	case *grammar.OperatorsRule:
		for _, o := range e.Operands {
			control.poll()
			if a.mayMatchIn(o.Expr, set, lenient) {
				return true
			}
		}
		return false
	}
	return true
}

func hasAttrControlled(e *grammar.Attributed, name string, control *lintControl) bool {
	for _, at := range e.Attrs {
		control.poll()
		if at.Name == name {
			return true
		}
	}
	return false
}

// anchorConflict returns the anchor of the sequence that can never match where it is, if any:
//   - $$ followed by items that cannot match without consuming input at the end of the input;
//   - ^^ preceded by items that must consume input;
//   - $ followed by items whose matches all begin with a character other than a line break;
//   - ^ preceded by items that always consume the same text, which does not end with "\n".
func (a *analysis) anchorConflict(s *grammar.Seq) grammar.Expr {
	control := a.control
	control.poll()

	for i, it := range s.Items {
		control.poll()
		after := &grammar.Seq{Items: s.Items[i+1:]}
		before := &grammar.Seq{Items: s.Items[:i]}
		switch it.(type) {
		case *grammar.EndInput:
			if !a.nullableAt(after, true) {
				return it
			}
		case *grammar.BeginInput:
			if !a.isNullable(before) {
				return it
			}
		case *grammar.EndLine:
			if p := a.prefix(after); p.s != "" && p.s[0] != '\n' && p.s[0] != '\r' {
				return it
			}
		case *grammar.BeginLine:
			if p := a.prefix(before); p.exact && p.s != "" && !strings.HasSuffix(p.s, "\n") {
				return it
			}
		}
	}
	return nil
}

// succeeds reports whether e certainly succeeds, whatever the input.
func (a *analysis) succeeds(e grammar.Expr) bool {
	control := a.control
	control.poll()
	return a.matchPrefix(e, "").ok
}

// matchResult is the result of matchPrefix.
type matchResult struct {
	ok bool
	// exact reports that e consumes exactly the part of t before rest.
	exact bool
	rest  string
}

type matchKey struct {
	rule string
	t    string
}

// matchPrefix reports whether e certainly succeeds on every input that begins with t.
func (a *analysis) matchPrefix(e grammar.Expr, t string) matchResult {
	control := a.control
	control.poll()

	ok := func(rest string) matchResult { return matchResult{ok: true, exact: true, rest: rest} }
	inexact := matchResult{ok: true}
	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Literal:
		if strings.HasPrefix(t, e.Value) {
			return ok(t[len(e.Value):])
		}
	case *grammar.CharClass:
		if r, size := utf8.DecodeRuneInString(t); t != "" && classAcceptsControlled(e, r, control) {
			return ok(t[size:])
		}
	case *grammar.Any:
		if _, size := utf8.DecodeRuneInString(t); t != "" {
			return ok(t[size:])
		}
	case *grammar.Top, *grammar.Cut:
		return ok(t)
	case *grammar.Seq:
		cur := ok(t)
		for _, it := range e.Items {
			control.poll()
			if cur.exact {
				if cur = a.matchPrefix(it, cur.rest); !cur.ok {
					return cur
				}
			} else if !a.succeeds(it) {
				return matchResult{}
			}
		}
		return cur
	case *grammar.Choice:
		for k, alt := range e.Alts {
			control.poll()
			if r := a.matchPrefix(alt, t); r.ok {
				if k == 0 {
					return r
				}
				return inexact
			}
			// A cut that commits the choice to an earlier alternative can make it fail before
			// it gets here.
			if a.hasLooseCut(alt) {
				break
			}
		}
	case *grammar.Repeat:
		cur := ok(t)
		for i := range e.Min {
			control.poll()
			if cur.exact {
				before := cur.rest
				if cur = a.matchPrefix(e.Expr, cur.rest); !cur.ok {
					return cur
				}
				if cur.exact && cur.rest == before {
					if i+1 < e.Min {
						return matchResult{}
					}
					return cur // the first empty iteration ends the repetition
				}
				// An inexact match may consume nothing and stop before the
				// minimum. Do not prove another iteration from its success.
				if !cur.exact && i+1 < e.Min {
					return matchResult{}
				}
			} else if !a.succeeds(e.Expr) {
				return matchResult{}
			}
		}
		if e.Max == e.Min {
			return cur
		}
		// Further iterations are optional, unless one fails after a cut.
		if a.hasLooseCut(e.Expr) && !a.succeeds(e.Expr) {
			return matchResult{}
		}
		return inexact
	case *grammar.Optional:
		if r := a.matchPrefix(e.Expr, t); r.ok {
			return r
		}
		if !a.hasLooseCut(e.Expr) {
			return inexact
		}
	case *grammar.Capture:
		return a.matchPrefix(e.Expr, t)
	case *grammar.Atomic:
		return a.matchPrefix(e.Expr, t)
	case *grammar.Discard:
		return a.matchPrefix(e.Expr, t)
	case *grammar.Attributed:
		// #recover only adds ways to succeed; @error and #stream do not change matching.
		return a.matchPrefix(e.Expr, t)
	case *grammar.And:
		if a.matchPrefix(e.Expr, t).ok {
			return ok(t)
		}
	case *grammar.Not:
		if a.fails(e.Expr, t) {
			return ok(t)
		}
	case *grammar.Ref:
		r := a.rules[e.Name]
		if r == nil || e.Level != "" || a.leftRecursive[e.Name] {
			break
		}
		if _, operatorsRule := r.Expr.(*grammar.OperatorsRule); operatorsRule {
			break
		}
		key := matchKey{e.Name, t}
		if m, ok := a.matchMemo[key]; ok {
			return m
		}
		if a.busy[e.Name] {
			a.cycles++
			break
		}
		a.busy[e.Name] = true
		before := a.cycles
		m := a.matchPrefix(r.Expr, t)
		delete(a.busy, e.Name)
		if a.cycles == before {
			a.matchMemo[key] = m
		}
		return m
	}
	// Predicates, anchors, bottom and Operators Rule expressions are not certain to succeed.
	return matchResult{}
}

// fails reports whether e certainly fails on every input that begins with t.
func (a *analysis) fails(e grammar.Expr, t string) bool {
	control := a.control
	control.poll()

	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Literal:
		return !strings.HasPrefix(t, e.Value) && !strings.HasPrefix(e.Value, t)
	case *grammar.CharClass:
		r, _ := utf8.DecodeRuneInString(t)
		return t != "" && !classAcceptsControlled(e, r, control)
	case *grammar.Bottom:
		return true
	case *grammar.Seq:
		cur := t
		for _, it := range e.Items {
			control.poll()
			if a.fails(it, cur) {
				return true
			}
			r := a.matchPrefix(it, cur)
			if !r.ok || !r.exact {
				return false
			}
			cur = r.rest
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			control.poll()
			if !a.fails(alt, t) {
				return false
			}
		}
		return true
	case *grammar.Repeat:
		return e.Min > 0 && a.fails(e.Expr, t)
	case *grammar.Capture:
		return a.fails(e.Expr, t)
	case *grammar.Atomic:
		return a.fails(e.Expr, t)
	case *grammar.Discard:
		return a.fails(e.Expr, t)
	case *grammar.Attributed:
		return !hasAttrControlled(e, "recover", control) && !hasAttrControlled(e, "missing", control) && a.fails(e.Expr, t)
	case *grammar.Ref:
		r := a.rules[e.Name]
		if r == nil || e.Level != "" || a.busy[e.Name] {
			return false
		}
		if _, operatorsRule := r.Expr.(*grammar.OperatorsRule); operatorsRule {
			return false
		}
		a.busy[e.Name] = true
		defer delete(a.busy, e.Name)
		return a.fails(r.Expr, t)
	}
	return false
}

// hasLooseCut reports whether e contains a cut that commits a choice, repetition or optional
// expression around e: one that is not inside a choice, repetition, optional expression or
// lookahead of its own (which stop it), or in another rule (cuts do not cross rules).
func hasLooseCut(e grammar.Expr) bool {
	return grammaranalysis.HasLooseCut(e)
}

// prefixResult is the result of prefix.
type prefixResult struct {
	s string
	// exact reports that every match consumes exactly s.
	exact bool
}

// prefix returns a string that every match of e begins with.
func (a *analysis) prefix(e grammar.Expr) prefixResult {
	control := a.control
	control.poll()

	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Literal:
		return prefixResult{e.Value, true}
	case *grammar.CharClass:
		if len(e.Ranges) == 1 && e.Ranges[0].Lo == e.Ranges[0].Hi && !e.Negated {
			return prefixResult{string(e.Ranges[0].Lo), true}
		}
	case *grammar.And, *grammar.Not, *grammar.Predicate, *grammar.Cut, *grammar.Top,
		*grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine, *grammar.EndLine:
		return prefixResult{"", true}
	case *grammar.Seq:
		var b strings.Builder
		for _, it := range e.Items {
			control.poll()
			p := a.prefix(it)
			b.WriteString(p.s)
			if !p.exact {
				return prefixResult{b.String(), false}
			}
		}
		return prefixResult{b.String(), true}
	case *grammar.Choice:
		var out prefixResult
		first := true
		for _, alt := range e.Alts {
			control.poll()
			if !a.mayMatch(alt) {
				continue // contributes no matches
			}
			p := a.prefix(alt)
			if first {
				out, first = p, false
				continue
			}
			n := 0
			for n < len(out.s) && n < len(p.s) && out.s[n] == p.s[n] {
				control.poll()
				n++
			}
			for n > 0 && n < len(out.s) && !utf8.RuneStart(out.s[n]) {
				control.poll()
				n-- // keep whole characters
			}
			out = prefixResult{out.s[:n], out.exact && p.exact && out.s == p.s}
		}
		return out
	case *grammar.Repeat:
		if e.Min == 0 {
			return prefixResult{"", e.Max == 0}
		}
		p := a.prefix(e.Expr)
		if !p.exact {
			return p
		}
		return prefixResult{strings.Repeat(p.s, e.Min), e.Max == e.Min}
	case *grammar.Capture:
		return a.prefix(e.Expr)
	case *grammar.Atomic:
		return a.prefix(e.Expr)
	case *grammar.Discard:
		return a.prefix(e.Expr)
	case *grammar.Attributed:
		if !hasAttrControlled(e, "recover", control) && !hasAttrControlled(e, "missing", control) { // recovery invalidates a guaranteed prefix
			return a.prefix(e.Expr)
		}
	case *grammar.Ref:
		r := a.rules[e.Name]
		if r == nil || e.Level != "" {
			break
		}
		if p, ok := a.prefixMemo[e.Name]; ok {
			return p
		}
		if a.busy[e.Name] {
			a.cycles++
			break
		}
		a.busy[e.Name] = true
		before := a.cycles
		p := a.prefix(r.Expr)
		delete(a.busy, e.Name)
		if a.cycles == before {
			a.prefixMemo[e.Name] = p
		}
		return p
	}
	return prefixResult{}
}

// classAccepts reports whether the character class matches r.
func classAccepts(c *grammar.CharClass, r rune) bool {
	for _, rg := range c.Ranges {
		if rg.Lo <= r && r <= rg.Hi {
			return !c.Negated
		}
	}
	return c.Negated
}

// --- Structure ---

// neutralControlled strips the wrappers that do not change whether an expression matches or how much
// input it consumes: #atomic, -, @error and #stream, and captures if caps is set.
func neutralControlled(e grammar.Expr, caps bool, control *lintControl) grammar.Expr {
	for {
		control.poll()
		e = analysisExprControlled(e, control)
		switch x := e.(type) {
		case *grammar.Atomic:
			e = x.Expr
		case *grammar.Discard:
			e = x.Expr
		case *grammar.Capture:
			if !caps {
				return e
			}
			e = x.Expr
		case *grammar.Attributed:
			if hasAttrControlled(x, "recover", control) {
				return e
			}
			e = x.Expr
		default:
			return e
		}
	}
}

// itemsControlled returns the items of e as a sequence.
func itemsControlled(e grammar.Expr, caps bool, control *lintControl) []grammar.Expr {
	if s, ok := neutralControlled(e, caps, control).(*grammar.Seq); ok {
		return s.Items
	}
	return []grammar.Expr{e}
}

// sameControlled reports whether x and y match the same inputs in the same way, by comparing their
// structure. If caps is set, the names of captures are ignored.
func sameControlled(x, y grammar.Expr, caps bool, control *lintControl) bool {
	control.poll()
	x, y = neutralControlled(x, caps, control), neutralControlled(y, caps, control)
	switch x := x.(type) {
	case *grammar.Ref:
		y, ok := y.(*grammar.Ref)
		return ok && x.Name == y.Name && x.Level == y.Level
	case *grammar.Literal:
		y, ok := y.(*grammar.Literal)
		return ok && x.Value == y.Value
	case *grammar.CharClass:
		y, ok := y.(*grammar.CharClass)
		return ok && x.Negated == y.Negated && sameRangesControlled(normalizeControlled(x.Ranges, control), normalizeControlled(y.Ranges, control), control)
	case *grammar.Seq:
		y, ok := y.(*grammar.Seq)
		return ok && sameListControlled(x.Items, y.Items, caps, control)
	case *grammar.Choice:
		y, ok := y.(*grammar.Choice)
		return ok && sameListControlled(x.Alts, y.Alts, caps, control)
	case *grammar.Repeat:
		y, ok := y.(*grammar.Repeat)
		return ok && x.Min == y.Min && x.Max == y.Max && sameControlled(x.Expr, y.Expr, caps, control)
	case *grammar.Optional:
		y, ok := y.(*grammar.Optional)
		return ok && sameControlled(x.Expr, y.Expr, caps, control)
	case *grammar.And:
		y, ok := y.(*grammar.And)
		return ok && sameControlled(x.Expr, y.Expr, caps, control)
	case *grammar.Not:
		y, ok := y.(*grammar.Not)
		return ok && sameControlled(x.Expr, y.Expr, caps, control)
	case *grammar.Capture:
		y, ok := y.(*grammar.Capture)
		return ok && x.Name == y.Name && sameControlled(x.Expr, y.Expr, caps, control)
	case *grammar.Predicate:
		y, ok := y.(*grammar.Predicate)
		return ok && control.formatTerm(x.Term) == control.formatTerm(y.Term)
	case *grammar.Attributed:
		y, ok := y.(*grammar.Attributed)
		return ok && control.formatExpr(x) == control.formatExpr(y) && sameControlled(x.Expr, y.Expr, caps, control)
	case *grammar.Any, *grammar.Cut, *grammar.Top, *grammar.Bottom, *grammar.BeginInput, *grammar.EndInput,
		*grammar.BeginLine, *grammar.EndLine:
		return sameType(x, y)
	}
	return false
}

func sameType(x, y grammar.Expr) bool {
	switch x.(type) {
	case *grammar.Any:
		_, ok := y.(*grammar.Any)
		return ok
	case *grammar.Cut:
		_, ok := y.(*grammar.Cut)
		return ok
	case *grammar.Top:
		_, ok := y.(*grammar.Top)
		return ok
	case *grammar.Bottom:
		_, ok := y.(*grammar.Bottom)
		return ok
	case *grammar.BeginInput:
		_, ok := y.(*grammar.BeginInput)
		return ok
	case *grammar.EndInput:
		_, ok := y.(*grammar.EndInput)
		return ok
	case *grammar.BeginLine:
		_, ok := y.(*grammar.BeginLine)
		return ok
	case *grammar.EndLine:
		_, ok := y.(*grammar.EndLine)
		return ok
	}
	return false
}

func sameListControlled(xs, ys []grammar.Expr, caps bool, control *lintControl) bool {
	if len(xs) != len(ys) {
		return false
	}
	for i := range xs {
		control.poll()
		if !sameControlled(xs[i], ys[i], caps, control) {
			return false
		}
	}
	return true
}

// hasPredicateControlled reports whether e contains a predicate (in the rule, not in the rules it calls).
func hasPredicateControlled(e grammar.Expr, control *lintControl) bool {
	found := false
	walkExprControlled(e, func(x grammar.Expr) {
		control.poll()
		if _, ok := x.(*grammar.Predicate); ok {
			found = true
		}
	}, control)
	return found
}

// --- Walking ---

// walkExpr visits the expression and its subexpressions in preorder, including attribute
// arguments and the parts of Operators Rule expressions.
func walkExprControlled(e grammar.Expr, f func(grammar.Expr), control *lintControl) {
	if control == nil {
		walkExpr(e, f)
		return
	}
	control.poll()
	if e == nil {
		return
	}
	f(e)
	switch e := e.(type) {
	case *grammar.Group:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Seq:
		for _, it := range e.Items {
			walkExprControlled(it, f, control)
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			walkExprControlled(alt, f, control)
		}
	case *grammar.Repeat:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Optional:
		walkExprControlled(e.Expr, f, control)
	case *grammar.And:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Not:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Atomic:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Stream:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Recovery:
		walkExprControlled(e.Expr, f, control)
		walkExprControlled(e.Skip, f, control)
	case *grammar.Discard:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Capture:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Attributed:
		walkExprControlled(e.Expr, f, control)
		for _, at := range e.Attrs {
			control.poll()
			for _, arg := range control.expressionArgs(at) {
				walkExprControlled(arg, f, control)
			}
		}
	case *grammar.OperatorsRule:
		walkExprControlled(e.Skip, f, control)
		for _, o := range e.Operands {
			walkExprControlled(o.Expr, f, control)
		}
		for _, l := range e.Levels {
			control.poll()
			for _, op := range l.Operators {
				walkExprControlled(op.Expr, f, control)
			}
		}
	}
}

// walkTerm visits the value expression and its subexpressions in preorder.
func walkTerm(t grammar.Term, f func(grammar.Term)) {
	if t == nil {
		return
	}
	f(t)
	switch t := t.(type) {
	case *grammar.TermGroup:
		walkTerm(t.Term, f)
	case *grammar.Block:
		for _, stmt := range t.Statements {
			walkTerm(stmt, f)
		}
	case *grammar.Member:
		walkTerm(t.X, f)
	case *grammar.New:
		for _, fi := range t.Fields {
			walkTerm(fi.Value, f)
		}
		for _, x := range t.Args {
			walkTerm(x, f)
		}
	case *grammar.ListConstructor:
		for _, item := range t.Items {
			walkTerm(item, f)
		}
	case *grammar.TupleLit:
		for _, x := range t.Elems {
			walkTerm(x, f)
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
	case *grammar.Cast:
		walkTerm(t.X, f)
	case *grammar.Bind:
		walkTerm(t.Value, f)
	}
}

// calls returns the rules that e calls, in order of appearance.
func calls(e grammar.Expr) []string {
	return callsControlled(e, nil)
}

func callsControlled(e grammar.Expr, control *lintControl) []string {
	control.poll()

	var out []string
	walkExprControlled(e, func(x grammar.Expr) {
		control.poll()
		if r, ok := x.(*grammar.Ref); ok {
			out = append(out, r.Name)
		}
	}, control)
	return out
}

// posOf returns the position where e begins, or the zero Pos if it is unknown.
func posOf(e grammar.Expr) grammar.Pos { return posOfControlled(e, nil) }

func posOfControlled(e grammar.Expr, control *lintControl) grammar.Pos {
	control.poll()
	switch e := analysisExprControlled(e, control).(type) {
	case *grammar.Ref:
		return e.Diagnostic()
	case *grammar.Literal:
		return e.Diagnostic()
	case *grammar.CharClass:
		return e.Diagnostic()
	case *grammar.Any:
		return e.Diagnostic()
	case *grammar.Seq:
		if len(e.Items) > 0 {
			return posOfControlled(e.Items[0], control)
		}
	case *grammar.Choice:
		if len(e.Alts) > 0 {
			return posOfControlled(e.Alts[0], control)
		}
	case *grammar.Repeat:
		return e.Diagnostic()
	case *grammar.Optional:
		return e.Diagnostic()
	case *grammar.And:
		return e.Diagnostic()
	case *grammar.Not:
		return e.Diagnostic()
	case *grammar.Atomic:
		return e.Diagnostic()
	case *grammar.Discard:
		return e.Diagnostic()
	case *grammar.Capture:
		return e.Diagnostic()
	case *grammar.Cut:
		return e.Diagnostic()
	case *grammar.Top:
		return e.Diagnostic()
	case *grammar.Bottom:
		return e.Diagnostic()
	case *grammar.BeginInput:
		return e.Diagnostic()
	case *grammar.EndInput:
		return e.Diagnostic()
	case *grammar.BeginLine:
		return e.Diagnostic()
	case *grammar.EndLine:
		return e.Diagnostic()
	case *grammar.Predicate:
		return e.Diagnostic()
	case *grammar.Attributed:
		return posOfControlled(e.Expr, control)
	case *grammar.OperatorsRule:
		return e.Diagnostic()
	}
	return grammar.Pos{}
}

// showControlled formats e for a message, shortened if it is long.
func showControlled(e grammar.Expr, control *lintControl) string {
	s := control.formatExpr(e)
	// Keep only the displayed prefix and a bounded lookahead. The rune
	// conversion preserves the old normalization of invalid UTF-8 even in
	// caller-built ASTs that have not passed validation.
	var prefix [37]rune
	count := 0
	for _, r := range s {
		if count == 40 {
			return "`" + string(prefix[:]) + "...`"
		}
		if count < len(prefix) {
			prefix[count] = r
		}
		count++
	}
	return "`" + s + "`"
}

// analysisExprControlled presents behavior pragmas to the existing matching proofs without
// mutating the source tree or losing the wrapped expression's positions.
func analysisExprControlled(e grammar.Expr, control *lintControl) grammar.Expr {
	switch x := e.(type) {
	case *grammar.Group:
		control.poll()
		return analysisExprControlled(x.Expr, control)
	case *grammar.Stream:
		return &grammar.Attributed{Expr: x.Expr, Attrs: []*grammar.Attribute{{Pos: x.Diagnostic(), Name: "stream", Pragma: true}}}
	case *grammar.Recovery:
		return &grammar.Attributed{Expr: x.Expr, Attrs: []*grammar.Attribute{{Pos: x.Diagnostic(), Name: "recover", Pragma: true, KwArgs: map[string]any{"skip": x.Skip}}}}
	}
	return e
}

// alwaysSucceeds preserves the ordinary proof and shares the invocation control.
func (a *analysis) alwaysSucceeds(e grammar.Expr) bool {
	if a.control == nil {
		return grammaranalysis.AlwaysSucceeds(e, a.engineSucceeding)
	}
	return grammaranalysis.AlwaysSucceedsWithPoll(e, a.engineSucceeding, a.control.poll)
}

func (a *analysis) hasLooseCut(e grammar.Expr) bool {
	if a.control == nil {
		return hasLooseCut(e)
	}
	return grammaranalysis.HasLooseCutWithPoll(e, a.control.poll)
}

func walkExpr(e grammar.Expr, f func(grammar.Expr)) {
	if e == nil {
		return
	}
	f(e)
	switch e := e.(type) {
	case *grammar.Group:
		walkExpr(e.Expr, f)
	case *grammar.Seq:
		for _, it := range e.Items {
			walkExpr(it, f)
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			walkExpr(alt, f)
		}
	case *grammar.Repeat:
		walkExpr(e.Expr, f)
	case *grammar.Optional:
		walkExpr(e.Expr, f)
	case *grammar.And:
		walkExpr(e.Expr, f)
	case *grammar.Not:
		walkExpr(e.Expr, f)
	case *grammar.Atomic:
		walkExpr(e.Expr, f)
	case *grammar.Stream:
		walkExpr(e.Expr, f)
	case *grammar.Recovery:
		walkExpr(e.Expr, f)
		walkExpr(e.Skip, f)
	case *grammar.Discard:
		walkExpr(e.Expr, f)
	case *grammar.Capture:
		walkExpr(e.Expr, f)
	case *grammar.Attributed:
		walkExpr(e.Expr, f)
		for _, at := range e.Attrs {
			for _, arg := range at.ExpressionArgs() {
				walkExpr(arg, f)
			}
		}
	case *grammar.OperatorsRule:
		walkExpr(e.Skip, f)
		for _, o := range e.Operands {
			walkExpr(o.Expr, f)
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				walkExpr(op.Expr, f)
			}
		}
	}
}
