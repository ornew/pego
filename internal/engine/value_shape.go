package engine

import "github.com/ornew/pego/grammar"

// The static shapes of values in the value model. Every backend builds the values of an
// expression from these, so that they agree with the inferred types (value_types.go).

// shapeElems returns the elements e contributes to the tuple of its scope, without types of rule
// calls (they do not change the shape).
func shapeElems(e grammar.Expr) []valueElem {
	return (&valueInference{}).elems(e)
}

// valuedCount returns the number of tuple elements e contributes: its elements with a value.
// Elements without one only hold captures kept beside the tuple.
func valuedCount(e grammar.Expr) int {
	n := 0
	for _, el := range shapeElems(e) {
		if el.t != nil {
			n++
		}
	}
	return n
}

// isElems reports whether e builds the sequence of its elements, a tuple node that a sequence
// containing e flattens: a sequence, possibly grouped or with attributes other than recovery.
func isElems(e grammar.Expr) bool {
	switch e := ungroupExpr(e).(type) {
	case *grammar.Seq:
		return true
	case *grammar.Attributed:
		return !recovers(e) && isElems(e.Expr)
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

// singleMode is how single (value_types.go) takes the value of an expression where it takes one
// element: an alternative, the operand of ?, a capture or a recovered expression.
type singleMode uint8

const (
	singleNone  singleMode = iota // no value
	singlePass                    // the one element
	singleTuple                   // the node of the tuple of the elements
)

func singleModeOf(e grammar.Expr) singleMode {
	k := valuedCount(e)
	_, seq := ungroupExpr(e).(*grammar.Seq)
	switch {
	case k == 0:
		return singleNone
	case k == 1 && !seq:
		return singlePass
	}
	return singleTuple
}

// scopeTuple reports whether the value of a scope with the body e (a rule body, a repetition
// element or an Operators Rule line) is the node of its tuple, rather than its one element.
func scopeTuple(e grammar.Expr) bool {
	elems := shapeElems(e)
	_, seq := ungroupExpr(e).(*grammar.Seq)
	return seq || len(elems) != 1 || elems[0].t == nil || len(elems[0].caps) != 0
}

// shaper converts the result of an expression's matcher to the value where it takes one element
// or is a scope.
type shaper struct {
	elems bool // the matcher returns the tuple node of its elements
	k     int  // the number of valued elements
}

func shaperOf(e grammar.Expr) shaper { return shaper{elems: isElems(e), k: valuedCount(e)} }

// tuple returns the tuple node of the result v of a match of [start, p.pos).
func (s shaper) tuple(p *parser, v *Node, start int) *Node {
	if s.elems {
		return v
	}
	var kids []*Node
	if s.k == 1 {
		kids = p.nodes(1)
		kids[0] = v
	}
	return p.newNode(Node{kind: kindSeq, Start: int32(start), End: int32(p.pos), Children: kids, fresh: true})
}

// one returns the one element of the result v.
func (s shaper) one(v *Node) *Node {
	if s.elems {
		if v == nil || len(v.Children) == 0 {
			return nil
		}
		return v.Children[0]
	}
	return v
}

// single returns the value of the result v where the expression takes one element; ok is false
// when it has no value.
func (s shaper) single(p *parser, mode singleMode, v *Node, start int) (*Node, bool) {
	switch mode {
	case singlePass:
		return s.one(v), true
	case singleTuple:
		return s.tuple(p, v, start), true
	}
	return nil, false
}

// singleMatcher returns a matcher of the value of e where it takes one element (an alternative or
// the operand of ?), from the building matcher m of e. Without a value the result is nil.
func singleMatcher(e grammar.Expr, m matcher) matcher {
	mode, sh := singleModeOf(e), shaperOf(e)
	if mode == singlePass && !sh.elems {
		return m
	}
	return func(p *parser) (*Node, bool) {
		start := p.pos
		v, ok := m(p)
		if !ok {
			return nil, false
		}
		v, _ = sh.single(p, mode, v, start)
		return v, true
	}
}

// captureMatcher is singleMatcher for a capture or a recovered expression, whose value is the
// unit node of the range matched when e has no value.
func captureMatcher(e grammar.Expr, m matcher) matcher {
	mode, sh := singleModeOf(e), shaperOf(e)
	if mode == singlePass && !sh.elems {
		return m
	}
	return func(p *parser) (*Node, bool) {
		start := p.pos
		v, ok := m(p)
		if !ok {
			return nil, false
		}
		if v, ok = sh.single(p, mode, v, start); !ok {
			v = p.newNode(Node{kind: kindSeq, Start: int32(start), End: int32(p.pos), fresh: true})
		}
		return v, true
	}
}

// scopeValue returns the value of a scope whose body e matched [start, p.pos) with the result v.
func scopeValueOf(e grammar.Expr) func(p *parser, v *Node, start int) *Node {
	sh := shaperOf(e)
	if scopeTuple(e) {
		return sh.tuple
	}
	return func(_ *parser, v *Node, _ int) *Node { return sh.one(v) }
}
