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
	g     *grammar.Grammar
	order []*grammar.RuleDef
	rules map[string]*grammar.RuleDef

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
	a := &analysis{g: g, rules: map[string]*grammar.RuleDef{}, nullable: [2]map[string]bool{{}, {}}, canMatch: map[string]bool{},
		baseCase: map[string]bool{}, leftRecursive: map[string]bool{}, engineNullable: map[string]bool{},
		matchMemo: map[matchKey]matchResult{}, prefixMemo: map[string]prefixResult{}, busy: map[string]bool{}}
	for _, r := range g.Rules() {
		if a.rules[r.Name] == nil {
			a.rules[r.Name] = r
			a.order = append(a.order, r)
		}
	}
	// Left recursion first: the other analyses use it (through matchPrefix).
	for changed := true; changed; {
		changed = false
		for _, r := range a.order {
			if !a.engineNullable[r.Name] && a.isEngineNullable(r.Expr) {
				a.engineNullable[r.Name], changed = true, true
			}
		}
	}
	left := map[string][]string{}
	for _, r := range a.order {
		left[r.Name] = a.leftCalls(r.Expr, nil)
	}
	// Match the compiler's graph/proof fixed point: breaking one cycle can
	// prove a rule successful and expose more unreachable call edges.
	for {
		clear(a.leftRecursive)
		for _, r := range a.order {
			if findCycle(r.Name, left) != nil {
				a.leftRecursive[r.Name] = true
			}
		}
		a.engineSucceeding = grammaranalysis.SucceedingRules(a.order, a.leftRecursive)
		next := map[string][]string{}
		changed := false
		for _, r := range a.order {
			next[r.Name] = a.leftCalls(r.Expr, nil)
			changed = changed || !slices.Equal(left[r.Name], next[r.Name])
		}
		if !changed {
			break
		}
		left = next
	}
	for _, end := range []bool{false, true} {
		set := a.nullable[b2i(end)]
		for changed := true; changed; {
			changed = false
			for _, r := range a.order {
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
			changed = false
			for _, r := range a.order {
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
	switch e := e.(type) {
	case *grammar.Ref:
		return a.engineNullable[e.Name]
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.CharClass, *grammar.Any, *grammar.Bottom:
		return false
	case *grammar.Seq:
		for _, it := range e.Items {
			if !a.isEngineNullable(it) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if a.isEngineNullable(alt) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		return e.Min == 0 || a.isEngineNullable(e.Expr)
	case *grammar.Atomic:
		return a.isEngineNullable(e.Expr)
	case *grammar.Discard:
		return a.isEngineNullable(e.Expr)
	case *grammar.Capture:
		return a.isEngineNullable(e.Expr)
	case *grammar.Attributed:
		return a.isEngineNullable(e.Expr)
	case *grammar.Pratt:
		for _, o := range e.Operands {
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
	switch e := e.(type) {
	case *grammar.Ref:
		return append(acc, e.Name)
	case *grammar.Seq:
		for _, it := range e.Items {
			acc = a.leftCalls(it, acc)
			if !a.isEngineNullable(it) {
				break
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			acc = a.leftCalls(alt, acc)
			if grammaranalysis.AlwaysSucceeds(alt, a.engineSucceeding) {
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
		if !grammaranalysis.AlwaysSucceeds(e.Expr, a.engineSucceeding) {
			for _, at := range e.Attrs {
				if at.Name == "recover" {
					if skip, ok := at.Arg("skip"); ok {
						acc = a.leftCalls(skip, acc)
					}
				}
			}
		}
	case *grammar.Pratt:
		if e.Skip != nil {
			acc = a.leftCalls(e.Skip, acc)
		}
		for _, o := range e.Operands {
			acc = a.leftCalls(o.Expr, acc)
			if grammaranalysis.AlwaysSucceeds(o.Expr, a.engineSucceeding) {
				break
			}
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				if op.Kind == grammar.Prefix {
					acc = a.leftCalls(op.Expr, acc)
				}
			}
		}
	}
	return acc
}

// mayMatch reports whether e may succeed.
func (a *analysis) mayMatch(e grammar.Expr) bool { return a.mayMatchIn(e, a.canMatch, false) }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isNullable reports whether e may succeed without consuming input.
func (a *analysis) isNullable(e grammar.Expr) bool {
	return a.nullableAt(e, false) || a.nullableAt(e, true)
}

// nullableAt reports whether e may succeed without consuming input at the end of the input (end
// set) or at another position. Telling the two apart makes "!$$ x" not nullable when x can only
// match nothing at the end of the input, as in a line that ends with ("\n" / $$).
func (a *analysis) nullableAt(e grammar.Expr, end bool) bool {
	switch e := e.(type) {
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
			if !a.nullableAt(it, end) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if a.nullableAt(alt, end) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		return e.Min == 0 || a.nullableAt(e.Expr, end)
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
	case *grammar.Pratt:
		for _, o := range e.Operands {
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
	switch e := neutral(e, true).(type) {
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
	switch e := neutral(e, true).(type) {
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
	switch e := e.(type) {
	case *grammar.Ref:
		return set[e.Name]
	case *grammar.Bottom:
		return lenient
	case *grammar.Seq:
		for _, it := range e.Items {
			if !a.mayMatchIn(it, set, lenient) {
				return false
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
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
		return hasAttr(e, "recover") || a.mayMatchIn(e.Expr, set, lenient)
	case *grammar.Pratt:
		for _, o := range e.Operands {
			if a.mayMatchIn(o.Expr, set, lenient) {
				return true
			}
		}
		return false
	}
	return true
}

func hasAttr(e *grammar.Attributed, name string) bool {
	for _, at := range e.Attrs {
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
	for i, it := range s.Items {
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
func (a *analysis) succeeds(e grammar.Expr) bool { return a.matchPrefix(e, "").ok }

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
	ok := func(rest string) matchResult { return matchResult{ok: true, exact: true, rest: rest} }
	inexact := matchResult{ok: true}
	switch e := e.(type) {
	case *grammar.Literal:
		if strings.HasPrefix(t, e.Value) {
			return ok(t[len(e.Value):])
		}
	case *grammar.CharClass:
		if r, size := utf8.DecodeRuneInString(t); t != "" && classAccepts(e, r) {
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
			if r := a.matchPrefix(alt, t); r.ok {
				if k == 0 {
					return r
				}
				return inexact
			}
			// A cut that commits the choice to an earlier alternative can make it fail before
			// it gets here.
			if hasLooseCut(alt) {
				break
			}
		}
	case *grammar.Repeat:
		cur := ok(t)
		for range e.Min {
			if cur.exact {
				if cur = a.matchPrefix(e.Expr, cur.rest); !cur.ok {
					return cur
				}
			} else if !a.succeeds(e.Expr) {
				return matchResult{}
			}
		}
		if e.Max == e.Min {
			return cur
		}
		// Further iterations are optional, unless one fails after a cut.
		if hasLooseCut(e.Expr) && !a.succeeds(e.Expr) {
			return matchResult{}
		}
		return inexact
	case *grammar.Optional:
		if r := a.matchPrefix(e.Expr, t); r.ok {
			return r
		}
		if !hasLooseCut(e.Expr) {
			return inexact
		}
	case *grammar.Capture:
		return a.matchPrefix(e.Expr, t)
	case *grammar.Atomic:
		return a.matchPrefix(e.Expr, t)
	case *grammar.Discard:
		return a.matchPrefix(e.Expr, t)
	case *grammar.Attributed:
		// #recover only adds ways to succeed; #error and #stream do not change matching.
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
		if _, pratt := r.Expr.(*grammar.Pratt); pratt {
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
	// Predicates, anchors, bottom and Pratt expressions are not certain to succeed.
	return matchResult{}
}

// fails reports whether e certainly fails on every input that begins with t.
func (a *analysis) fails(e grammar.Expr, t string) bool {
	switch e := e.(type) {
	case *grammar.Literal:
		return !strings.HasPrefix(t, e.Value) && !strings.HasPrefix(e.Value, t)
	case *grammar.CharClass:
		r, _ := utf8.DecodeRuneInString(t)
		return t != "" && !classAccepts(e, r)
	case *grammar.Bottom:
		return true
	case *grammar.Seq:
		cur := t
		for _, it := range e.Items {
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
		return !hasAttr(e, "recover") && a.fails(e.Expr, t)
	case *grammar.Ref:
		r := a.rules[e.Name]
		if r == nil || e.Level != "" || a.busy[e.Name] {
			return false
		}
		if _, pratt := r.Expr.(*grammar.Pratt); pratt {
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
	switch e := e.(type) {
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
				n++
			}
			for n > 0 && n < len(out.s) && !utf8.RuneStart(out.s[n]) {
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
		if !hasAttr(e, "recover") { // a recovered error covers whatever was skipped
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

// neutral strips the wrappers that do not change whether an expression matches or how much
// input it consumes: @, -, #error and #stream, and captures if caps is set.
func neutral(e grammar.Expr, caps bool) grammar.Expr {
	for {
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
			if hasAttr(x, "recover") {
				return e
			}
			e = x.Expr
		default:
			return e
		}
	}
}

// items returns the items of e as a sequence.
func items(e grammar.Expr, caps bool) []grammar.Expr {
	if s, ok := neutral(e, caps).(*grammar.Seq); ok {
		return s.Items
	}
	return []grammar.Expr{e}
}

// same reports whether x and y match the same inputs in the same way, by comparing their
// structure. If caps is set, the names of captures are ignored.
func same(x, y grammar.Expr, caps bool) bool {
	x, y = neutral(x, caps), neutral(y, caps)
	switch x := x.(type) {
	case *grammar.Ref:
		y, ok := y.(*grammar.Ref)
		return ok && x.Name == y.Name && x.Level == y.Level
	case *grammar.Literal:
		y, ok := y.(*grammar.Literal)
		return ok && x.Value == y.Value
	case *grammar.CharClass:
		y, ok := y.(*grammar.CharClass)
		return ok && x.Negated == y.Negated && sameRanges(normalize(x.Ranges), normalize(y.Ranges))
	case *grammar.Seq:
		y, ok := y.(*grammar.Seq)
		return ok && sameList(x.Items, y.Items, caps)
	case *grammar.Choice:
		y, ok := y.(*grammar.Choice)
		return ok && sameList(x.Alts, y.Alts, caps)
	case *grammar.Repeat:
		y, ok := y.(*grammar.Repeat)
		return ok && x.Min == y.Min && x.Max == y.Max && same(x.Expr, y.Expr, caps)
	case *grammar.Optional:
		y, ok := y.(*grammar.Optional)
		return ok && same(x.Expr, y.Expr, caps)
	case *grammar.And:
		y, ok := y.(*grammar.And)
		return ok && same(x.Expr, y.Expr, caps)
	case *grammar.Not:
		y, ok := y.(*grammar.Not)
		return ok && same(x.Expr, y.Expr, caps)
	case *grammar.Capture:
		y, ok := y.(*grammar.Capture)
		return ok && x.Name == y.Name && same(x.Expr, y.Expr, caps)
	case *grammar.Predicate:
		y, ok := y.(*grammar.Predicate)
		return ok && grammar.FormatTerm(x.Term) == grammar.FormatTerm(y.Term)
	case *grammar.Attributed:
		y, ok := y.(*grammar.Attributed)
		return ok && grammar.FormatExpr(x) == grammar.FormatExpr(y) && same(x.Expr, y.Expr, caps)
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

func sameList(xs, ys []grammar.Expr, caps bool) bool {
	if len(xs) != len(ys) {
		return false
	}
	for i := range xs {
		if !same(xs[i], ys[i], caps) {
			return false
		}
	}
	return true
}

// hasPredicate reports whether e contains a predicate (in the rule, not in the rules it calls).
func hasPredicate(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(x grammar.Expr) {
		if _, ok := x.(*grammar.Predicate); ok {
			found = true
		}
	})
	return found
}

// --- Walking ---

// walkExpr visits the expression and its subexpressions in preorder, including attribute
// arguments and the parts of Pratt expressions.
func walkExpr(e grammar.Expr, f func(grammar.Expr)) {
	if e == nil {
		return
	}
	f(e)
	switch e := e.(type) {
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
	case *grammar.Discard:
		walkExpr(e.Expr, f)
	case *grammar.Capture:
		walkExpr(e.Expr, f)
	case *grammar.Attributed:
		walkExpr(e.Expr, f)
		for _, at := range e.Attrs {
			for _, arg := range at.Args {
				walkExpr(arg.Value, f)
			}
		}
	case *grammar.Pratt:
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

// walkTerm visits the value expression and its subexpressions in preorder.
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

// calls returns the rules that e calls, in order of appearance.
func calls(e grammar.Expr) []string {
	var out []string
	walkExpr(e, func(x grammar.Expr) {
		if r, ok := x.(*grammar.Ref); ok {
			out = append(out, r.Name)
		}
	})
	return out
}

// posOf returns the position where e begins, or the zero Pos if it is unknown.
func posOf(e grammar.Expr) grammar.Pos {
	switch e := e.(type) {
	case *grammar.Ref:
		return e.Pos
	case *grammar.Literal:
		return e.Pos
	case *grammar.CharClass:
		return e.Pos
	case *grammar.Any:
		return e.Pos
	case *grammar.Seq:
		if len(e.Items) > 0 {
			return posOf(e.Items[0])
		}
	case *grammar.Choice:
		if len(e.Alts) > 0 {
			return posOf(e.Alts[0])
		}
	case *grammar.Repeat:
		return e.Pos
	case *grammar.Optional:
		return e.Pos
	case *grammar.And:
		return e.Pos
	case *grammar.Not:
		return e.Pos
	case *grammar.Atomic:
		return e.Pos
	case *grammar.Discard:
		return e.Pos
	case *grammar.Capture:
		return e.Pos
	case *grammar.Cut:
		return e.Pos
	case *grammar.Top:
		return e.Pos
	case *grammar.Bottom:
		return e.Pos
	case *grammar.BeginInput:
		return e.Pos
	case *grammar.EndInput:
		return e.Pos
	case *grammar.BeginLine:
		return e.Pos
	case *grammar.EndLine:
		return e.Pos
	case *grammar.Predicate:
		return e.Pos
	case *grammar.Attributed:
		return posOf(e.Expr)
	case *grammar.Pratt:
		return e.Pos
	}
	return grammar.Pos{}
}

// show formats e for a message, shortened if it is long.
func show(e grammar.Expr) string {
	s := grammar.FormatExpr(e)
	if r := []rune(s); len(r) > 40 {
		s = string(r[:37]) + "..."
	}
	return "`" + s + "`"
}
