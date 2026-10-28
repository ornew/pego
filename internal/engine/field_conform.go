package engine

import (
	"strconv"

	"github.com/ornew/pego/internal/value"
)

// convertError reports a value that does not convert to the named node type typ.
func (p *parser) convertError(v any, typ string) error {
	return p.actionError("cannot convert ", typeName(v), " to ", typ)
}

// fieldConforms reports whether v is a value of type t in the value model. It charges a step for
// each list or tuple element it examines. node[T] is a node whose value is of type T (node[_] any
// node with a value), and error an error value (an error or a missing token); a list or tuple type
// takes a list or tuple value, not the node of a repetition or sequence.
func (p *parser) fieldConforms(t *fieldType, v any) bool {
	n, isNode := v.(*value.Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	if isNode && value.ShapeOf(n) == value.ShapeBox {
		return p.fieldConforms(t, value.D(n).Box)
	}
	switch t.kind {
	case ftTop:
		return true
	case ftInt:
		_, ok := v.(int)
		return ok
	case ftString:
		_, ok := v.(string)
		return ok
	case ftBool:
		_, ok := v.(bool)
		return ok
	case ftNil:
		return v == nil
	case ftOptional:
		return v == nil || p.fieldConforms(t.elem, v)
	case ftJoin:
		for _, a := range t.alts {
			if p.fieldConforms(a, v) {
				return true
			}
		}
		return false
	}
	if !isNode {
		return false
	}
	shape := value.ShapeOf(n)
	valueNode := shape == value.ShapeString || shape == value.ShapeTuple || shape == value.ShapeList
	switch t.kind {
	case ftNode:
		return valueNode
	case ftStringNode:
		return shape == value.ShapeString
	case ftError:
		return shape == value.ShapeError
	case ftMissing:
		return shape == value.ShapeError && n.Type() == value.TypeMissing
	case ftNamed:
		return n.Type() == t.name
	case ftList:
		return shape == value.ShapeListValue && p.elemsConform(t.elem, value.D(n).Children)
	case ftTuple:
		return shape == value.ShapeTupleValue && p.tupleConforms(t.alts, value.D(n).Children)
	case ftNodeOf:
		return p.nodeValueConforms(t.elem, n, shape)
	}
	return false
}

// nodeValueConforms reports whether the value of node n, of shape shape, is of type t: a string
// node's value is a string, and a tuple or list node's value its elements.
func (p *parser) nodeValueConforms(t *fieldType, n *value.Node, shape value.Shape) bool {
	switch t.kind {
	case ftTop:
		return shape == value.ShapeString || shape == value.ShapeTuple || shape == value.ShapeList
	case ftString:
		return shape == value.ShapeString
	case ftList:
		return shape == value.ShapeList && p.elemsConform(t.elem, value.D(n).Children)
	case ftTuple:
		return shape == value.ShapeTuple && p.tupleConforms(t.alts, value.D(n).Children)
	case ftOptional:
		return p.nodeValueConforms(t.elem, n, shape) // a node always has a value
	case ftJoin:
		for _, a := range t.alts {
			if p.nodeValueConforms(a, n, shape) {
				return true
			}
		}
	}
	// No node has a value of another type yet.
	return false
}

// elemsConform reports whether every element of a list is of type t, charging a step for each.
func (p *parser) elemsConform(t *fieldType, elems []*value.Node) bool {
	if t.kind == ftTop {
		return true
	}
	for _, c := range elems {
		p.stepWork()
		if !p.fieldConforms(t, c) {
			return false
		}
	}
	return true
}

// tupleConforms reports whether the elements of a tuple are of the types ts, charging a step for
// each.
func (p *parser) tupleConforms(ts []*fieldType, elems []*value.Node) bool {
	if len(elems) != len(ts) {
		return false
	}
	for i, c := range elems {
		p.stepWork()
		if !p.fieldConforms(ts[i], c) {
			return false
		}
	}
	return true
}

// castError reports a cast value that is not of the type it is cast to. pos is the input position
// where the evaluation started, and gline:gcol the grammar position of the cast (0 if unknown).
func (p *parser) castError(v any, t *fieldType, pos, gline, gcol int) error {
	line, col := p.input.lineCol(pos)
	parts := []string{"cannot cast ", typeName(v), " to ", t.text, " at input position ", strconv.Itoa(pos),
		" (line ", strconv.Itoa(line), ", column ", strconv.Itoa(col), ")"}
	if gline > 0 {
		parts = append(parts, "; cast at grammar position ", strconv.Itoa(gline), ":", strconv.Itoa(gcol))
	}
	return p.actionError(parts...)
}
