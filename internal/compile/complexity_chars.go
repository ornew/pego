package compile

import (
	"math"
	"slices"
	"unicode/utf8"

	"github.com/ornew/zeroparse/grammar"
)

// charset is a set of characters as sorted, disjoint, non-adjacent ranges. The classifier uses it
// for the first characters of expressions, which it over-approximates: a character that an
// expression can examine first is always in its set.
type charset []grammar.CharRange

// maxChar is the largest character value. Input that is not valid UTF-8 decodes to characters
// below it, so every character that a parse can examine is in [0, maxChar].
const maxChar = math.MaxInt32

var allChars = charset{{Lo: 0, Hi: maxChar}}

// lineBreakChars are the characters that $ examines.
var lineBreakChars = charset{{Lo: '\n', Hi: '\n'}, {Lo: '\r', Hi: '\r'}}

func (s charset) union(t charset) charset {
	if len(t) == 0 {
		return s
	}
	if len(s) == 0 {
		return t
	}
	all := make([]grammar.CharRange, 0, len(s)+len(t))
	all = append(append(all, s...), t...)
	return normalizeRanges(all)
}

// normalizeRanges sorts rs and merges overlapping and adjacent ranges.
func normalizeRanges(rs []grammar.CharRange) charset {
	rs = slices.Clone(rs)
	slices.SortFunc(rs, func(a, b grammar.CharRange) int { return int(a.Lo) - int(b.Lo) })
	var out charset
	for _, r := range rs {
		if r.Lo > r.Hi {
			continue
		}
		if n := len(out); n > 0 && int64(r.Lo) <= int64(out[n-1].Hi)+1 {
			out[n-1].Hi = max(out[n-1].Hi, r.Hi)
			continue
		}
		out = append(out, r)
	}
	return out
}

func (s charset) intersects(t charset) bool {
	i, j := 0, 0
	for i < len(s) && j < len(t) {
		switch {
		case s[i].Hi < t[j].Lo:
			i++
		case t[j].Hi < s[i].Lo:
			j++
		default:
			return true
		}
	}
	return false
}

func (s charset) equal(t charset) bool { return slices.Equal(s, t) }

// complement returns the characters in [0, maxChar] that are not in s.
func (s charset) complement() charset {
	var out charset
	lo := rune(0)
	for _, r := range s {
		if r.Lo > lo {
			out = append(out, grammar.CharRange{Lo: lo, Hi: r.Lo - 1})
		}
		if r.Hi == maxChar {
			return out
		}
		lo = r.Hi + 1
	}
	return append(out, grammar.CharRange{Lo: lo, Hi: maxChar})
}

func charOf(r rune) charset { return charset{{Lo: r, Hi: r}} }

// asciiFold adds the other ASCII case of r, as case-insensitive literals match it.
func asciiFold(r rune) charset {
	switch {
	case 'a' <= r && r <= 'z':
		return normalizeRanges([]grammar.CharRange{{Lo: r, Hi: r}, {Lo: r - 'a' + 'A', Hi: r - 'a' + 'A'}})
	case 'A' <= r && r <= 'Z':
		return normalizeRanges([]grammar.CharRange{{Lo: r, Hi: r}, {Lo: r - 'A' + 'a', Hi: r - 'A' + 'a'}})
	}
	return charOf(r)
}

// literalFirst is the first character of a literal, or the empty set for the empty literal. A
// literal that holds a replacement character also matches input that is not valid UTF-8, which
// decodes to that character.
func literalFirst(l *grammar.Literal) charset {
	if l.Value == "" {
		return nil
	}
	r, _ := utf8.DecodeRuneInString(l.Value)
	if l.IgnoreCase {
		return asciiFold(r)
	}
	return charOf(r)
}

func classChars(c *grammar.CharClass) charset {
	s := normalizeRanges(c.Ranges)
	if c.Negated {
		return s.complement()
	}
	return s
}

// hasPragma reports whether e carries the pragma or attribute name.
func hasPragma(e *grammar.Attributed, name string) bool {
	for _, a := range e.Attrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// recoverSkip returns the skip expression of a #recover on e, if it has one.
func recoverSkip(e *grammar.Attributed) grammar.Expr {
	for _, a := range e.Attrs {
		if a.Name == "recover" {
			if skip, ok := a.Arg("skip"); ok {
				return skip
			}
		}
	}
	return nil
}

// nullable reports whether e can succeed without consuming input. It over-approximates: a literal
// with #missing can be inserted, and a repetition of a nullable element is nullable whatever its
// minimum.
func (k *classifier) nullable(e grammar.Expr) bool {
	k.control.poll()
	switch e := e.(type) {
	case nil:
		return true
	case *grammar.Ref:
		if e.Level == rhsLevel {
			return k.operandsNullable(e.Name)
		}
		return k.nullRule[e.Name]
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.CharClass, *grammar.Any, *grammar.Bottom:
		return false
	case *grammar.Seq:
		for _, it := range e.Items {
			if !k.nullable(it) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if k.nullable(alt) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		return e.Min == 0 || e.Max == 0 || k.nullable(e.Expr)
	case *grammar.Group:
		return k.nullable(e.Expr)
	case *grammar.Capture:
		return k.nullable(e.Expr)
	case *grammar.Discard:
		return k.nullable(e.Expr)
	case *grammar.Atomic:
		return k.nullable(e.Expr)
	case *grammar.Stream:
		return k.nullable(e.Expr)
	case *grammar.InlineAction:
		return k.nullable(e.Expr)
	case *grammar.Recovery:
		return k.nullable(e.Expr) || k.nullable(e.Skip)
	case *grammar.Attributed:
		if hasPragma(e, "missing") {
			return true
		}
		if skip := recoverSkip(e); skip != nil && k.nullable(skip) {
			return true
		}
		return k.nullable(e.Expr)
	case *grammar.OperatorsRule:
		for _, o := range e.Operands {
			if k.nullable(o.Expr) {
				return true
			}
		}
		return false
	}
	// Optional, lookaheads, predicates, anchors, cuts and the top expression.
	return true
}

// first returns the first characters of e: the characters that an evaluation of e can examine
// beyond its start, as it consumes them or as a lookahead that it evaluates before it consumes
// input examines them. Characters that follow e when it matches without consuming input are not
// included; firstCont adds them.
func (k *classifier) first(e grammar.Expr) charset {
	k.control.poll()
	switch e := e.(type) {
	case nil:
		return nil
	case *grammar.Ref:
		if e.Level == rhsLevel {
			return k.operandFirst(e.Name)
		}
		return k.firstRule[e.Name]
	case *grammar.Literal:
		return literalFirst(e)
	case *grammar.CharClass:
		return classChars(e)
	case *grammar.Any:
		return allChars
	case *grammar.EndLine:
		return lineBreakChars
	case *grammar.Seq:
		var s charset
		for _, it := range e.Items {
			s = s.union(k.first(it))
			if !k.nullable(it) {
				break
			}
		}
		return s
	case *grammar.Choice:
		var s charset
		for _, alt := range e.Alts {
			s = s.union(k.first(alt))
		}
		return s
	case *grammar.Repeat:
		if e.Max == 0 {
			return nil
		}
		return k.first(e.Expr)
	case *grammar.Optional:
		return k.first(e.Expr)
	case *grammar.And:
		return k.first(e.Expr)
	case *grammar.Not:
		return k.first(e.Expr)
	case *grammar.Group:
		return k.first(e.Expr)
	case *grammar.Capture:
		return k.first(e.Expr)
	case *grammar.Discard:
		return k.first(e.Expr)
	case *grammar.Atomic:
		return k.first(e.Expr)
	case *grammar.Stream:
		return k.first(e.Expr)
	case *grammar.InlineAction:
		return k.first(e.Expr)
	case *grammar.Recovery:
		return k.first(e.Expr).union(k.first(e.Skip))
	case *grammar.Attributed:
		s := k.first(e.Expr)
		if skip := recoverSkip(e); skip != nil {
			s = s.union(k.first(skip))
		}
		return s
	case *grammar.OperatorsRule:
		return k.operatorsStart(e)
	}
	// Predicates, anchors other than $, cuts, the top and bottom expressions.
	return nil
}

// operatorsStart is the first characters of an Operators Rule expression and of each right
// operand: its skip, its operands and its prefix operator parts.
func (k *classifier) operatorsStart(pr *grammar.OperatorsRule) charset {
	s := k.first(pr.Skip)
	for _, o := range pr.Operands {
		s = s.union(k.first(o.Expr))
	}
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			if op.Kind == grammar.Prefix {
				s = s.union(k.first(op.Expr))
			}
		}
	}
	return s
}

// operandFirst is the first characters of a right operand of the Operators Rule of rule.
func (k *classifier) operandFirst(rule string) charset {
	if pr := k.operators(rule); pr != nil {
		return k.operatorsStart(pr)
	}
	return allChars
}

func (k *classifier) operandsNullable(rule string) bool {
	pr := k.operators(rule)
	return pr == nil || k.nullable(pr)
}

// operators returns the Operators Rule that is the body of rule, or nil.
func (k *classifier) operators(rule string) *grammar.OperatorsRule {
	if r := k.byName[rule]; r != nil {
		pr, _ := grammar.RuleOperators(ungroup(r.Expr))
		return pr
	}
	return nil
}

func ungroup(e grammar.Expr) grammar.Expr {
	for {
		g, ok := e.(*grammar.Group)
		if !ok {
			return e
		}
		e = g.Expr
	}
}

// computeFirst computes the nullable rules and the first characters of every rule as fixed
// points.
func (k *classifier) computeFirst() {
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if !k.nullRule[r.Name] && k.nullable(r.Expr) {
				k.nullRule[r.Name] = true
				changed = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if s := k.first(r.Expr).union(k.firstRule[r.Name]); !s.equal(k.firstRule[r.Name]) {
				k.firstRule[r.Name] = s
				changed = true
			}
		}
	}
}

// bounded reports whether every match of e is shorter than a constant of the grammar.
func (k *classifier) bounded(e grammar.Expr) bool {
	k.control.poll()
	switch e := e.(type) {
	case nil, *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Predicate, *grammar.Cut,
		*grammar.Top, *grammar.Bottom, *grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine,
		*grammar.EndLine, *grammar.And, *grammar.Not:
		return true
	case *grammar.Ref:
		return e.Level != rhsLevel && k.boundedRule[e.Name]
	case *grammar.Seq:
		for _, it := range e.Items {
			if !k.bounded(it) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if !k.bounded(alt) {
				return false
			}
		}
		return true
	case *grammar.Repeat:
		return e.Max >= 0 && k.bounded(e.Expr)
	case *grammar.Optional:
		return k.bounded(e.Expr)
	case *grammar.Group:
		return k.bounded(e.Expr)
	case *grammar.Capture:
		return k.bounded(e.Expr)
	case *grammar.Discard:
		return k.bounded(e.Expr)
	case *grammar.Atomic:
		return k.bounded(e.Expr)
	case *grammar.Stream:
		return k.bounded(e.Expr)
	case *grammar.InlineAction:
		return k.bounded(e.Expr)
	case *grammar.Recovery:
		return k.bounded(e.Expr) && k.bounded(e.Skip)
	case *grammar.Attributed:
		if skip := recoverSkip(e); skip != nil && !k.bounded(skip) {
			return false
		}
		return k.bounded(e.Expr)
	}
	return false
}

// computeBounded marks the rules whose matches are bounded in length: the greatest fixed point
// would admit recursive rules, so a rule is bounded only if it is not recursive.
func (k *classifier) computeBounded() {
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if !k.boundedRule[r.Name] && !k.recursive[r.Name] && k.bounded(r.Expr) {
				k.boundedRule[r.Name] = true
				changed = true
			}
		}
	}
}
