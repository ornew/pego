package core

import (
	"strconv"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// ConvertError reports a value that does not convert to the named node type typ.
func (p *Parser) ConvertError(v any, typ string) error {
	return p.ActionError("cannot convert ", TypeName(v), " to ", typ)
}

// FieldConforms reports whether v is a value of type t in the value model. It charges a step for
// each list or tuple element it examines. node[T] is a node whose value is of type T (node[_] any
// node with a value), and error an error value (an error or a missing token); a list or tuple type
// takes a list or tuple value, not the node of a repetition or sequence.
func (p *Parser) FieldConforms(t *compile.FieldType, v any) bool {
	n, isNode := v.(*value.Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	if isNode && value.ShapeOf(n) == value.ShapeBox {
		return p.FieldConforms(t, value.D(n).Box)
	}
	switch t.Kind {
	case compile.FtTop:
		return true
	case compile.FtInt:
		_, ok := v.(int)
		return ok
	case compile.FtString:
		_, ok := v.(string)
		return ok
	case compile.FtBool:
		_, ok := v.(bool)
		return ok
	case compile.FtNil:
		return v == nil
	case compile.FtOptional:
		return v == nil || p.FieldConforms(t.Elem, v)
	case compile.FtJoin:
		for _, a := range t.Alts {
			if p.FieldConforms(a, v) {
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
	switch t.Kind {
	case compile.FtNode:
		return valueNode
	case compile.FtStringNode:
		return shape == value.ShapeString
	case compile.FtError:
		return shape == value.ShapeError
	case compile.FtMissing:
		return shape == value.ShapeError && n.Type() == value.TypeMissing
	case compile.FtNamed:
		return n.Type() == t.Name
	case compile.FtList:
		return shape == value.ShapeListValue && p.elemsConform(t.Elem, value.D(n).Children)
	case compile.FtTuple:
		return shape == value.ShapeTupleValue && p.tupleConforms(t.Alts, value.D(n).Children)
	case compile.FtNodeOf:
		return p.nodeValueConforms(t.Elem, n, shape)
	}
	return false
}

// nodeValueConforms reports whether the value of node n, of shape shape, is of type t: a string
// node's value is a string, and a tuple or list node's value its elements.
func (p *Parser) nodeValueConforms(t *compile.FieldType, n *value.Node, shape value.Shape) bool {
	switch t.Kind {
	case compile.FtTop:
		return shape == value.ShapeString || shape == value.ShapeTuple || shape == value.ShapeList
	case compile.FtString:
		return shape == value.ShapeString
	case compile.FtList:
		return shape == value.ShapeList && p.elemsConform(t.Elem, value.D(n).Children)
	case compile.FtTuple:
		return shape == value.ShapeTuple && p.tupleConforms(t.Alts, value.D(n).Children)
	case compile.FtOptional:
		return p.nodeValueConforms(t.Elem, n, shape) // a node always has a value
	case compile.FtJoin:
		for _, a := range t.Alts {
			if p.nodeValueConforms(a, n, shape) {
				return true
			}
		}
	}
	// No node has a value of another type yet.
	return false
}

// elemsConform reports whether every element of a list is of type t, charging a step for each.
func (p *Parser) elemsConform(t *compile.FieldType, elems []*value.Node) bool {
	if t.Kind == compile.FtTop {
		return true
	}
	for _, c := range elems {
		p.StepWork()
		if !p.FieldConforms(t, c) {
			return false
		}
	}
	return true
}

// tupleConforms reports whether the elements of a tuple are of the types ts, charging a step for
// each.
func (p *Parser) tupleConforms(ts []*compile.FieldType, elems []*value.Node) bool {
	if len(elems) != len(ts) {
		return false
	}
	for i, c := range elems {
		p.StepWork()
		if !p.FieldConforms(ts[i], c) {
			return false
		}
	}
	return true
}

// CastError reports a cast value that is not of the type it is cast to. pos is the input position
// where the evaluation started, and gline:gcol the grammar position of the cast (0 if unknown).
func (p *Parser) CastError(v any, t *compile.FieldType, pos, gline, gcol int) error {
	line, col := p.Input.lineCol(pos)
	parts := []string{"cannot cast ", TypeName(v), " to ", t.Text, " at input position ", strconv.Itoa(pos),
		" (line ", strconv.Itoa(line), ", column ", strconv.Itoa(col), ")"}
	if gline > 0 {
		parts = append(parts, "; cast at grammar position ", strconv.Itoa(gline), ":", strconv.Itoa(gcol))
	}
	return p.ActionError(parts...)
}
