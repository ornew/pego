package compile

import (
	"github.com/ornew/zeroparse/grammar"

	"github.com/ornew/zeroparse/internal/value"
)

// The static shapes of values in the value model. Every backend builds the values of an
// expression from these, so that they agree with the inferred types (value_types.go).

// shapeElems returns the elements e contributes to the tuple of its scope, without types of rule
// calls (they do not change the shape).
func shapeElems(p *Program, e grammar.Expr) []valueElem {
	var control *Control
	if p != nil {
		control = p.shapeControl
	}
	return (&ValueInference{Prog: p, control: control}).elems(e)
}

// SpliceBody returns the body of the rule name if it is a splice rule: a rule that makes no node of
// its own, so that a call of it contributes what its body does, as a group (Rule.Splice).
func (p *Program) SpliceBody(name string) (grammar.Expr, bool) {
	if p == nil {
		return nil, false
	}
	if r := p.ByName[name]; r != nil && r.Splice {
		return r.Def.Expr, true
	}
	return nil, false
}

// isSeq reports whether e is a sequence, as grouped, with attributes other than recovery, or as the body of a splice rule it calls.
func isSeq(p *Program, e grammar.Expr) bool {
	var seen map[string]bool
	for {
		switch x := ungroupExpr(e).(type) {
		case *grammar.Seq:
			return true
		case *grammar.Attributed:
			// An attribute other than recovery only labels e; recovery makes it one element.
			if recovers(x) {
				return false
			}
			e = x.Expr
		case *grammar.Ref:
			body, ok := p.SpliceBody(x.Name)
			if !ok || seen[x.Name] {
				return false
			}
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[x.Name] = true
			e = body
		default:
			return false
		}
	}
}

// ValuedCount returns the number of tuple elements e contributes: its elements with a value.
// Elements without one only hold captures kept beside the tuple.
func ValuedCount(p *Program, e grammar.Expr) int {
	n := 0
	for _, el := range shapeElems(p, e) {
		if el.t != nil {
			n++
		}
	}
	return n
}

// IsElems reports whether e builds the sequence of its elements, a tuple node that a sequence
// containing e flattens: a sequence, possibly grouped or with attributes other than recovery.
func IsElems(p *Program, e grammar.Expr) bool {
	return isElemsSeen(p, e, nil)
}

func isElemsSeen(p *Program, e grammar.Expr, seen map[string]bool) bool {
	switch e := ungroupExpr(e).(type) {
	case *grammar.Seq:
		return true
	case *grammar.Attributed:
		return !recovers(e) && isElemsSeen(p, e.Expr, seen)
	case *grammar.Ref:
		if body, ok := p.SpliceBody(e.Name); ok && !seen[e.Name] {
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[e.Name] = true
			return isElemsSeen(p, body, seen)
		}
	}
	return false
}

// recovers reports whether e has #recover or #missing, which make it one element.
func recovers(e *grammar.Attributed) bool {
	for _, a := range e.Attrs {
		if a.Name == "recover" || a.Name == "missing" {
			return true
		}
	}
	return false
}

// SingleMode is how single (value_types.go) takes the value of an expression where it takes one
// element: an alternative, the operand of ?, a capture or a recovered expression.
type SingleMode uint8

const (
	singleNone  SingleMode = iota // no value
	SinglePass                    // the one element
	SingleTuple                   // the node of the tuple of the elements
)

// SingleModeOf returns how single takes the value of e.
func SingleModeOf(p *Program, e grammar.Expr) SingleMode {
	k := ValuedCount(p, e)
	seq := isSeq(p, e)
	switch {
	case k == 0:
		return singleNone
	case k == 1 && !seq:
		return SinglePass
	}
	return SingleTuple
}

// ScopeTuple reports whether the value of a scope with the body e (a rule body, a repetition
// element or an Operators Rule line) is the node of its tuple, rather than its one element.
func ScopeTuple(p *Program, e grammar.Expr) bool {
	elems := shapeElems(p, e)
	seq := isSeq(p, e)
	return seq || len(elems) != 1 || elems[0].t == nil || len(elems[0].caps) != 0
}

// Shaper converts the result of an expression's matcher to the value where it takes one element
// or is a scope.
type Shaper struct {
	Elems  bool // the matcher returns the tuple node of its elements
	Shared bool // the tuple node is the result of a splice rule: a memo entry shares it
	K      int  // the number of valued elements
}

// ShaperOf returns the Shaper of e.
func ShaperOf(p *Program, e grammar.Expr) Shaper {
	return Shaper{Elems: IsElems(p, e), Shared: SharesTuple(p, e), K: ValuedCount(p, e)}
}

// SharesTuple reports whether the tuple node e builds (isElems) is the result of a splice rule,
// shared through its memo entry: a node that takes it as its own takes a copy.
func SharesTuple(p *Program, e grammar.Expr) bool {
	for {
		switch x := ungroupExpr(e).(type) {
		case *grammar.Attributed:
			if recovers(x) {
				return false
			}
			e = x.Expr
		case *grammar.Ref:
			_, ok := p.SpliceBody(x.Name)
			return ok
		default:
			return false
		}
	}
}

// One returns the one element of the result v.
func (s Shaper) One(v *value.Node) *value.Node {
	if s.Elems {
		if v == nil || len(value.D(v).Children) == 0 {
			return nil
		}
		return value.D(v).Children[0]
	}
	return v
}
