package runtime

// FieldType is a declared struct field type that a constructor checks a dynamically typed value
// against. Generated code declares one for each checked field.
//
// FieldType is for generated parsers; not a stable API.
type FieldType struct {
	Kind byte // i int, s string, b bool, z nil, n node[_], S node[string], v node[elem], E error, N struct or named node type, l list, t tuple (alts), o optional, u union, _ top
	Name string
	Elem *FieldType
	Alts []*FieldType
	Text string
}

// CheckField returns v if it fits t, the type of field field of struct typ, and raises an
// evaluation error otherwise.
func (c *ActCtx) CheckField(v any, t *FieldType, field, typ string) any {
	if !c.P.fieldConforms(t, v) {
		c.P.evalErrorf("cannot use %s as %s in field %s of %s", typeName(v), t.Text, field, typ)
	}
	return v
}

// convertCheck returns v if it fits t, the underlying type of the named node type typ that it is
// converted to, and raises an evaluation error otherwise.
func (c *ActCtx) convertCheck(v any, t *FieldType, typ string) any {
	if !c.P.fieldConforms(t, v) {
		c.P.evalErrorf("cannot convert %s to %s", typeName(UnboxValue(v)), typ)
	}
	return v
}

// Cast returns v if it fits t, the type of a cast, and raises an evaluation error otherwise.
func (c *ActCtx) Cast(v any, t *FieldType) any {
	if !c.P.fieldConforms(t, v) {
		c.P.evalErrorf("cannot cast %s to %s", typeName(v), t.Text)
	}
	return v
}

// CheckResult fails the parse if v, a result of rule rule, does not fit t, the rule's declared
// type.
func (p *Parser) CheckResult(v *Node, t *FieldType, rule string) {
	if u := UnboxNode(v); !p.fieldConforms(t, u) {
		panic(fatal{resultError("cannot use " + typeName(u) + " as " + t.Text + " in result of rule " + rule)})
	}
}

// resultError is the error of a result that does not fit its declared type.
type resultError string

func (e resultError) Error() string { return string(e) }

// fieldConforms reports whether v is a value of type t in the value model, charging a step for
// each list or tuple element it examines. node[T] is a node whose value is of type T (node[_] any
// node with a value), and error an error value; a list or tuple type takes a list or tuple value,
// not the node of a repetition or sequence.
func (p *Parser) fieldConforms(t *FieldType, v any) bool {
	n, isNode := v.(*Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	if isNode && n.shape() == shapeBox {
		return p.fieldConforms(t, n.box)
	}
	switch t.Kind {
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
		return v == nil || p.fieldConforms(t.Elem, v)
	case 'u':
		for _, a := range t.Alts {
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
	switch t.Kind {
	case 'n':
		return valueNode
	case 'S':
		return shape == shapeString
	case 'E':
		return shape == shapeError
	case 'N':
		return n.Type() == t.Name
	case 'l':
		return shape == shapeListValue && p.elemsConform(t.Elem, n.RawChildren)
	case 't':
		return shape == shapeTupleValue && p.tupleConforms(t.Alts, n.RawChildren)
	case 'v':
		return p.nodeValueConforms(t.Elem, n, shape)
	}
	return false
}

// nodeValueConforms reports whether the value of node n, of shape shape, is of type t: a string
// node's value is a string, and a tuple or list node's value its elements.
func (p *Parser) nodeValueConforms(t *FieldType, n *Node, shape nodeShape) bool {
	switch t.Kind {
	case '_':
		return shape == shapeString || shape == shapeTuple || shape == shapeList
	case 's':
		return shape == shapeString
	case 'l':
		return shape == shapeList && p.elemsConform(t.Elem, n.RawChildren)
	case 't':
		return shape == shapeTuple && p.tupleConforms(t.Alts, n.RawChildren)
	case 'o':
		return p.nodeValueConforms(t.Elem, n, shape) // a node always has a value
	case 'u':
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
func (p *Parser) elemsConform(t *FieldType, elems []*Node) bool {
	if t.Kind == '_' {
		return true
	}
	for _, c := range elems {
		p.StepWork()
		if !p.fieldConforms(t, c) {
			return false
		}
	}
	return true
}

// tupleConforms reports whether the elements of a tuple are of the types ts, charging a step for
// each.
func (p *Parser) tupleConforms(ts []*FieldType, elems []*Node) bool {
	if len(elems) != len(ts) {
		return false
	}
	for i, c := range elems {
		p.StepWork()
		if !p.fieldConforms(ts[i], c) {
			return false
		}
	}
	return true
}

// CheckField returns v if it fits t, the type of field field of struct typ, and raises an
// evaluation error otherwise.
func (c *TActCtx) CheckField(v any, t *FieldType, field, typ string) any {
	if !tfieldConforms(c.P.Parser, t, v) {
		c.P.evalErrorf("cannot use %s as %s in field %s of %s", ttypeName(v), t.Text, field, typ)
	}
	return v
}

// convertCheck returns v if it fits t, the underlying type of the named node type typ that it is
// converted to, and raises an evaluation error otherwise.
func (c *TActCtx) convertCheck(v any, t *FieldType, typ string) any {
	if !tfieldConforms(c.P.Parser, t, v) {
		c.P.evalErrorf("cannot convert %s to %s", ttypeName(v), typ)
	}
	return v
}

// Cast returns v if it fits t, the type of a cast, and raises an evaluation error otherwise.
func (c *TActCtx) Cast(v any, t *FieldType) any {
	if !tfieldConforms(c.P.Parser, t, v) {
		c.P.evalErrorf("cannot cast %s to %s", ttypeName(v), t.Text)
	}
	return v
}

// TCheckResult is CheckResult for the values of the typed runtime.
func (p *TParser) TCheckResult(v any, t *FieldType, rule string) {
	if !tfieldConforms(p.Parser, t, v) {
		panic(fatal{resultError("cannot use " + ttypeName(v) + " as " + t.Text + " in result of rule " + rule)})
	}
}

// tfieldConforms is fieldConforms for the values of the typed runtime.
func tfieldConforms(p *Parser, t *FieldType, v any) bool {
	switch t.Kind {
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
		return v == nil || tfieldConforms(p, t.Elem, v)
	case 'u':
		for _, a := range t.Alts {
			if tfieldConforms(p, a, v) {
				return true
			}
		}
		return false
	}
	n := AsTval(v)
	if n == nil {
		return false
	}
	shape := tshape(n)
	valueNode := shape == shapeString || shape == shapeTuple || shape == shapeList
	switch t.Kind {
	case 'n':
		return valueNode
	case 'S':
		return shape == shapeString
	case 'E':
		return shape == shapeError
	case 'N':
		return n.TName() == t.Name
	case 'l':
		return shape == shapeListValue && telemsConform(p, t.Elem, n.TKids())
	case 't':
		return shape == shapeTupleValue && ttupleConforms(p, t.Alts, n.TKids())
	case 'v':
		return tnodeValueConforms(p, t.Elem, n, shape)
	}
	return false
}

// tnodeValueConforms is nodeValueConforms for the values of the typed runtime.
func tnodeValueConforms(p *Parser, t *FieldType, n TVal, shape nodeShape) bool {
	switch t.Kind {
	case '_':
		return shape == shapeString || shape == shapeTuple || shape == shapeList
	case 's':
		return shape == shapeString
	case 'l':
		return shape == shapeList && telemsConform(p, t.Elem, n.TKids())
	case 't':
		return shape == shapeTuple && ttupleConforms(p, t.Alts, n.TKids())
	case 'o':
		return tnodeValueConforms(p, t.Elem, n, shape)
	case 'u':
		for _, a := range t.Alts {
			if tnodeValueConforms(p, a, n, shape) {
				return true
			}
		}
	}
	return false
}

// telemsConform is elemsConform for the values of the typed runtime.
func telemsConform(p *Parser, t *FieldType, elems []any) bool {
	if t.Kind == '_' {
		return true
	}
	for _, c := range elems {
		p.StepWork()
		if !tfieldConforms(p, t, c) {
			return false
		}
	}
	return true
}

// ttupleConforms is tupleConforms for the values of the typed runtime.
func ttupleConforms(p *Parser, ts []*FieldType, elems []any) bool {
	if len(elems) != len(ts) {
		return false
	}
	for i, c := range elems {
		p.StepWork()
		if !tfieldConforms(p, ts[i], c) {
			return false
		}
	}
	return true
}
