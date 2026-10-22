package lint

import (
	"context"
	"slices"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
)

// A Rewrite is a change to the source of a grammar that fixes a finding without changing what the
// grammar matches or the values it builds. Only three findings have one, each when the proof for
// its kind holds, and a tool that applies a Rewrite still checks the result:
//
//   - char-class, for ranges that overlap or repeat: the class is replaced by one with merged
//     ranges. The proof is that both classes contain the same code points, checked at the ends of
//     every range, and that negation is unchanged. A class that also matches punctuation between
//     letters, such as [A-z], has no Rewrite, because the writer must say which characters were meant.
//   - useless-lookahead, for a lookahead that always succeeds: it is removed. The proof is that the
//     lookahead matches only a combination of literals, character classes, any-character, groups,
//     sequences, choices, options and repetitions, so it holds no capture, cut, call, predicate,
//     recovery or attribute, and a lookahead consumes nothing and has no value.
//   - unreachable-rule: the rule is removed. The proof is that no start rule reaches it, directly
//     or through an inline action, so nothing calls it and no parse starts at it.
//
// Start and End delimit the source to replace by NewText, with End exclusive. For a rule the span is
// the whole definition including its attributes.
type Rewrite struct {
	Start, End grammar.Pos
	NewText    string
}

// IsValid reports whether the Rewrite holds a change.
func (r Rewrite) IsValid() bool { return r.Start.IsValid() && r.End.IsValid() }

// Rewrites returns, for each finding of the lint run of g from start, its Rewrite or the zero
// Rewrite. The findings must come from Run or RunContext on the same g and start, and g must be a
// valid grammar. Cancellation discards the result.
func Rewrites(ctx context.Context, g *grammar.Grammar, start string, findings []Finding) (out []Rewrite, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var control *lintControl
	if ctx.Done() != nil {
		control = &lintControl{ctx: ctx}
		defer func() {
			if failure := recover(); failure != nil {
				cancelled, ok := failure.(lintCancellation)
				if !ok {
					panic(failure)
				}
				out, err = nil, cancelled.err
			}
		}()
	}
	lowered, _, lowerErr := engine.LowerInlineActions(g)
	if lowerErr != nil {
		lowered = g
	}
	l := &linter{control: control, an: newAnalysisControlled(lowered, control)}
	out = make([]Rewrite, len(findings))
	for i, f := range findings {
		control.poll()
		if !f.Pos.IsValid() {
			continue
		}
		switch f.Check {
		case CheckCharClass:
			out[i] = l.classRewrite(f)
		case CheckUselessLookahead:
			out[i] = l.lookaheadRewrite(f)
		case CheckUnreachableRule:
			out[i] = l.ruleRewrite(f)
		}
	}
	return out, nil
}

// find returns the expression of the rules at pos for which match holds.
func (l *linter) find(pos grammar.Pos, match func(grammar.Expr) bool) grammar.Expr {
	control := l.control
	var found grammar.Expr
	for _, r := range l.an.order {
		control.poll()
		walkExprControlled(r.Expr, func(e grammar.Expr) {
			control.poll()
			if n, ok := e.(grammar.SourceNode); ok && found == nil && n.Source().Diagnostic() == pos && match(e) {
				found = e
			}
		}, control)
		if found != nil {
			break
		}
	}
	return found
}

func (l *linter) classRewrite(f Finding) Rewrite {
	control := l.control
	c, _ := l.find(f.Pos, func(e grammar.Expr) bool { _, ok := e.(*grammar.CharClass); return ok }).(*grammar.CharClass)
	if c == nil || !c.End.IsValid() {
		return Rewrite{}
	}
	overlaps := false
	for j, rj := range c.Ranges {
		control.poll()
		for _, ri := range c.Ranges[:j] {
			control.poll()
			overlaps = overlaps || ri.Lo <= rj.Hi && rj.Lo <= ri.Hi
		}
	}
	if !overlaps {
		return Rewrite{}
	}
	merged := &grammar.CharClass{Negated: c.Negated, Ranges: normalizeControlled(c.Ranges, control)}
	if !sameCodePoints(c.Ranges, merged.Ranges, control) {
		return Rewrite{}
	}
	return Rewrite{Start: c.Pos, End: c.End, NewText: control.formatExpr(merged)}
}

// sameCodePoints reports whether the ranges x and y contain the same code points. Two unions of
// intervals are equal when they agree on every end of every range and on the code point on each
// side of it, since they can differ only next to such a point.
func sameCodePoints(x, y []grammar.CharRange, control *lintControl) bool {
	in := func(rs []grammar.CharRange, r rune) bool {
		for _, rg := range rs {
			control.poll()
			if rg.Lo <= r && r <= rg.Hi {
				return true
			}
		}
		return false
	}
	for _, rs := range [][]grammar.CharRange{x, y} {
		for _, rg := range rs {
			for _, p := range []rune{rg.Lo - 1, rg.Lo, rg.Hi, rg.Hi + 1} {
				control.poll()
				if in(x, p) != in(y, p) {
					return false
				}
			}
		}
	}
	return true
}

func (l *linter) lookaheadRewrite(f Finding) Rewrite {
	control := l.control
	a := l.an
	// The lookahead itself, not a node that wraps it and starts at the same place.
	e := l.find(f.Pos, func(e grammar.Expr) bool {
		switch e.(type) {
		case *grammar.And, *grammar.Not:
			return true
		}
		return false
	})
	if e == nil {
		return Rewrite{}
	}
	var body grammar.Expr
	switch x := e.(type) {
	case *grammar.And:
		if !hasCapturesControlled(x.Expr, control) && a.succeeds(x.Expr) {
			body = x.Expr
		}
	case *grammar.Not:
		if !a.succeeds(x.Expr) && !a.mayMatch(x.Expr) {
			body = x.Expr
		}
	}
	node, ok := e.(grammar.SourceNode)
	if !ok || body == nil || !pureMatcher(body, control) {
		return Rewrite{}
	}
	src := node.Source()
	if !src.End.IsValid() {
		return Rewrite{}
	}
	return Rewrite{Start: src.Pos, End: src.End}
}

// pureMatcher reports whether e is made only of literals, character classes, any-character, groups,
// sequences, choices, options and repetitions.
func pureMatcher(e grammar.Expr, control *lintControl) bool {
	pure := true
	walkExprControlled(e, func(x grammar.Expr) {
		control.poll()
		switch x.(type) {
		case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Group, *grammar.Seq,
			*grammar.Choice, *grammar.Optional, *grammar.Repeat:
		default:
			pure = false
		}
	}, control)
	return pure
}

func (l *linter) ruleRewrite(f Finding) Rewrite {
	control := l.control
	i := slices.IndexFunc(l.an.order, func(r *grammar.RuleDef) bool { control.poll(); return r.Name == f.Rule })
	if i < 0 || len(l.an.order) < 2 {
		return Rewrite{}
	}
	r := l.an.order[i]
	if !r.End.IsValid() || r.Pos != f.Pos && r.Diagnostic() != f.Pos {
		return Rewrite{}
	}
	return Rewrite{Start: r.Pos, End: r.End}
}
