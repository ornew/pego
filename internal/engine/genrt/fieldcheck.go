package genrt

// fieldType is a declared struct field type that a constructor checks a dynamically typed value
// against. Generated code declares one for each checked field.
type fieldType struct {
	kind byte // i int, s string, b bool, z nil, n node[_], S node[string], v node[elem], E error, N struct or named node type, l list, t tuple (alts), o optional, u union, _ top
	name string
	elem *fieldType
	alts []*fieldType
	text string
}

// checkField returns v if it fits t, the type of field field of struct typ, and raises an
// evaluation error otherwise.
func (c *actx) checkField(v any, t *fieldType, field, typ string) any {
	if !c.p.fieldConforms(t, v) {
		c.p.evalErrorf("cannot use %s as %s in field %s of %s", typeName(v), t.text, field, typ)
	}
	return v
}

// convertCheck returns v if it fits t, the underlying type of the named node type typ that it is
// converted to, and raises an evaluation error otherwise.
func (c *actx) convertCheck(v any, t *fieldType, typ string) any {
	if !c.p.fieldConforms(t, v) {
		c.p.evalErrorf("cannot convert %s to %s", typeName(unboxValue(v)), typ)
	}
	return v
}

// cast returns v if it fits t, the type of a cast, and raises an evaluation error otherwise.
func (c *actx) cast(v any, t *fieldType) any {
	if !c.p.fieldConforms(t, v) {
		c.p.evalErrorf("cannot cast %s to %s", typeName(v), t.text)
	}
	return v
}

// checkResult fails the parse if v, a result of rule rule, does not fit t, the rule's declared
// type.
func (p *parser) checkResult(v *Node, t *fieldType, rule string) {
	if u := unboxNode(v); !p.fieldConforms(t, u) {
		panic(fatal{resultError("cannot use " + typeName(u) + " as " + t.text + " in result of rule " + rule)})
	}
}

// resultError is the error of a result that does not fit its declared type.
type resultError string

func (e resultError) Error() string { return string(e) }

// fieldConforms reports whether v is a value of type t in the value model, charging a step for
// each list or tuple element it examines. node[T] is a node whose value is of type T (node[_] any
// node with a value), and error an error value; a list or tuple type takes a list or tuple value,
// not the node of a repetition or sequence.
func (p *parser) fieldConforms(t *fieldType, v any) bool {
	n, isNode := v.(*Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	if isNode && n.shape() == shapeBox {
		return p.fieldConforms(t, n.box)
	}
	switch t.kind {
	case '_':
		return true
	case 'i':
		_, ok := v.(int)
		return ok
	case 's':
		_, ok := v.(string)
		return ok
	case 'b':
		_, ok := v.(bool)
		return ok
	case 'z':
		return v == nil
	case 'o':
		return v == nil || p.fieldConforms(t.elem, v)
	case 'u':
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
	shape := n.shape()
	valueNode := shape == shapeString || shape == shapeTuple || shape == shapeList
	switch t.kind {
	case 'n':
		return valueNode
	case 'S':
		return shape == shapeString
	case 'E':
		return shape == shapeError
	case 'N':
		return n.Type() == t.name
	case 'l':
		return shape == shapeListValue && p.elemsConform(t.elem, n.children)
	case 't':
		return shape == shapeTupleValue && p.tupleConforms(t.alts, n.children)
	case 'v':
		return p.nodeValueConforms(t.elem, n, shape)
	}
	return false
}

// nodeValueConforms reports whether the value of node n, of shape shape, is of type t: a string
// node's value is a string, and a tuple or list node's value its elements.
func (p *parser) nodeValueConforms(t *fieldType, n *Node, shape nodeShape) bool {
	switch t.kind {
	case '_':
		return shape == shapeString || shape == shapeTuple || shape == shapeList
	case 's':
		return shape == shapeString
	case 'l':
		return shape == shapeList && p.elemsConform(t.elem, n.children)
	case 't':
		return shape == shapeTuple && p.tupleConforms(t.alts, n.children)
	case 'o':
		return p.nodeValueConforms(t.elem, n, shape) // a node always has a value
	case 'u':
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
func (p *parser) elemsConform(t *fieldType, elems []*Node) bool {
	if t.kind == '_' {
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
func (p *parser) tupleConforms(ts []*fieldType, elems []*Node) bool {
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

// fieldcheck:typed:begin

// checkField returns v if it fits t, the type of field field of struct typ, and raises an
// evaluation error otherwise.
func (c *tctx) checkField(v any, t *fieldType, field, typ string) any {
	if !tfieldConforms(c.p.parser, t, v) {
		c.p.evalErrorf("cannot use %s as %s in field %s of %s", ttypeName(v), t.text, field, typ)
	}
	return v
}

// convertCheck returns v if it fits t, the underlying type of the named node type typ that it is
// converted to, and raises an evaluation error otherwise.
func (c *tctx) convertCheck(v any, t *fieldType, typ string) any {
	if !tfieldConforms(c.p.parser, t, v) {
		c.p.evalErrorf("cannot convert %s to %s", ttypeName(v), typ)
	}
	return v
}

// cast returns v if it fits t, the type of a cast, and raises an evaluation error otherwise.
func (c *tctx) cast(v any, t *fieldType) any {
	if !tfieldConforms(c.p.parser, t, v) {
		c.p.evalErrorf("cannot cast %s to %s", ttypeName(v), t.text)
	}
	return v
}

// tcheckResult is checkResult for the values of the typed runtime.
func (p *tparser) tcheckResult(v any, t *fieldType, rule string) {
	if !tfieldConforms(p.parser, t, v) {
		panic(fatal{resultError("cannot use " + ttypeName(v) + " as " + t.text + " in result of rule " + rule)})
	}
}

// tfieldConforms is fieldConforms for the values of the typed runtime.
func tfieldConforms(p *parser, t *fieldType, v any) bool {
	switch t.kind {
	case '_':
		return true
	case 'i':
		_, ok := v.(int)
		return ok
	case 's':
		_, ok := v.(string)
		return ok
	case 'b':
		_, ok := v.(bool)
		return ok
	case 'z':
		return v == nil
	case 'o':
		return v == nil || tfieldConforms(p, t.elem, v)
	case 'u':
		for _, a := range t.alts {
			if tfieldConforms(p, a, v) {
				return true
			}
		}
		return false
	}
	n := asTval(v)
	if n == nil {
		return false
	}
	shape := tshape(n)
	valueNode := shape == shapeString || shape == shapeTuple || shape == shapeList
	switch t.kind {
	case 'n':
		return valueNode
	case 'S':
		return shape == shapeString
	case 'E':
		return shape == shapeError
	case 'N':
		return n.tname() == t.name
	case 'l':
		return shape == shapeListValue && telemsConform(p, t.elem, n.tkids())
	case 't':
		return shape == shapeTupleValue && ttupleConforms(p, t.alts, n.tkids())
	case 'v':
		return tnodeValueConforms(p, t.elem, n, shape)
	}
	return false
}

// tnodeValueConforms is nodeValueConforms for the values of the typed runtime.
func tnodeValueConforms(p *parser, t *fieldType, n tval, shape nodeShape) bool {
	switch t.kind {
	case '_':
		return shape == shapeString || shape == shapeTuple || shape == shapeList
	case 's':
		return shape == shapeString
	case 'l':
		return shape == shapeList && telemsConform(p, t.elem, n.tkids())
	case 't':
		return shape == shapeTuple && ttupleConforms(p, t.alts, n.tkids())
	case 'o':
		return tnodeValueConforms(p, t.elem, n, shape)
	case 'u':
		for _, a := range t.alts {
			if tnodeValueConforms(p, a, n, shape) {
				return true
			}
		}
	}
	return false
}

// telemsConform is elemsConform for the values of the typed runtime.
func telemsConform(p *parser, t *fieldType, elems []any) bool {
	if t.kind == '_' {
		return true
	}
	for _, c := range elems {
		p.stepWork()
		if !tfieldConforms(p, t, c) {
			return false
		}
	}
	return true
}

// ttupleConforms is tupleConforms for the values of the typed runtime.
func ttupleConforms(p *parser, ts []*fieldType, elems []any) bool {
	if len(elems) != len(ts) {
		return false
	}
	for i, c := range elems {
		p.stepWork()
		if !tfieldConforms(p, ts[i], c) {
			return false
		}
	}
	return true
}

// fieldcheck:typed:end
