package genrt

// fieldType is a declared struct field type that a constructor checks a dynamically typed value
// against. Generated code declares one for each checked field.
type fieldType struct {
	kind byte // i int, s string, b bool, z nil, n node, t terminal, N named, l list, o optional, u union
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
	if !p.fieldConforms(t, v) {
		panic(fatal{resultError("cannot use " + typeName(nodeOrNil(v)) + " as " + t.text + " in result of rule " + rule)})
	}
}

// resultError is the error of a result that does not fit its declared type.
type resultError string

func (e resultError) Error() string { return string(e) }

// fieldConforms reports whether v is a value of type t, charging a step for each list element
// it examines. Error nodes stand for any node type, and for an empty list.
func (p *parser) fieldConforms(t *fieldType, v any) bool {
	n, isNode := v.(*Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	switch t.kind {
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
	typ := n.Type()
	if typ == "Error" {
		return true
	}
	switch t.kind {
	case 'n':
		return true
	case 't':
		return n.IsTerminal()
	case 'N':
		return typ == t.name
	case 'l':
		if typ != "List" {
			return false
		}
		for _, c := range n.Children {
			p.stepWork()
			if !p.fieldConforms(t.elem, c) {
				return false
			}
		}
		return true
	}
	return false
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
	typ := n.tname()
	if typ == "Error" {
		return true
	}
	switch t.kind {
	case 'n':
		return true
	case 't':
		_, ok := n.ttext()
		return ok
	case 'N':
		return typ == t.name
	case 'l':
		if typ != "List" {
			return false
		}
		for _, c := range n.tkids() {
			p.stepWork()
			if !tfieldConforms(p, t.elem, c) {
				return false
			}
		}
		return true
	}
	return false
}

// fieldcheck:typed:end
