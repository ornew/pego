package engine

import (
	"fmt"
	"strings"
)

// A query pattern compiled with a schema must be able to match some value the schema admits where
// it is applied: the pattern of a field must match some value of the field's shape, the pattern
// of an element some value of the element's shape, and the pattern of a node's value some value
// of the node's value shape. A union or optional shape needs only one alternative that matches;
// where nothing is known about a value, every pattern can match it. Rule constraints are not
// checked against shapes, since the schema does not record which rule produces a node. The check
// never rejects a pattern that can match a value the schema describes.

// queryAtom is one alternative of a shape that is not a union, optional value or inferred rule
// reference.
type queryAtom struct {
	// kind is "any" (nothing is known), "nil", "scalar", "error", "node", "tuple", "list" or
	// "struct".
	kind   string
	scalar string
	// name and fields describe a struct.
	name   string
	fields []SchemaField
	// value is the value shape of a node, and element the element shape of a list.
	value, element *SchemaShape
	// elements are the element shapes of a tuple.
	elements []SchemaShape
}

// category returns the query category of the values of a, or "" for any.
func (a queryAtom) category() string {
	switch a.kind {
	case "any":
		return ""
	case "scalar":
		return a.scalar
	}
	return a.kind
}

// describe names the values of a in reasons.
func (a queryAtom) describe() string {
	switch a.kind {
	case "struct":
		return "a struct " + a.name
	case "scalar":
		return a.scalar
	case "nil", "error":
		return a.kind
	}
	return "a " + a.kind
}

type queryShapeKey struct {
	pattern *QueryPattern
	shape   *SchemaShape
}

type queryShapeChecker struct {
	s *QuerySchema
	// done holds the results of checks already made, so shared shapes are checked once per
	// pattern.
	done map[queryShapeKey]error
}

// errorFields are the fields of error values.
var errorFields = []SchemaField{
	{Name: "expected", Shape: SchemaShape{Kind: "scalar", Scalar: "string"}},
	{Name: "message", Shape: SchemaShape{Kind: "scalar", Scalar: "string"}},
}

// checkQueryShapes reports a pattern of p, a compiled copy, that matches no value the schema s
// admits where it is applied.
func checkQueryShapes(p *QueryPattern, s *QuerySchema) error {
	k := &queryShapeChecker{s: s, done: map[queryShapeKey]error{}}
	if err := k.check(p, nil); err != nil {
		return fmt.Errorf("query pattern matches no value of the schema: %w", err)
	}
	return nil
}

// check reports whether p can match a value of shape (nil if unknown).
func (k *queryShapeChecker) check(p *QueryPattern, shape *SchemaShape) error {
	key := queryShapeKey{p, shape}
	if err, ok := k.done[key]; ok {
		return err
	}
	err := k.anyAtom(k.shapeAtoms(shape), func(a queryAtom) error { return k.match(p, a) })
	k.done[key] = err
	return err
}

// shapeAtoms returns the alternatives of shape, or any if shape is nil.
func (k *queryShapeChecker) shapeAtoms(shape *SchemaShape) []queryAtom {
	if shape == nil {
		return []queryAtom{{kind: "any"}}
	}
	return k.atoms(shape, nil, map[string]bool{}, map[*SchemaShape]bool{})
}

// anyAtom reports nil if f accepts one of atoms, and the first reason otherwise.
func (k *queryShapeChecker) anyAtom(atoms []queryAtom, f func(queryAtom) error) error {
	if len(atoms) == 0 {
		return fmt.Errorf("the shape has no values")
	}
	var first, err error
	for _, a := range atoms {
		if err = f(a); err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
	}
	if len(atoms) > 1 {
		// Only the first reason is kept: each one may hold the reasons of nested patterns.
		return fmt.Errorf("none of %d alternatives matches; the first: %w", len(atoms), first)
	}
	return err
}

// atoms appends the alternatives of shape to out. active holds the rules being expanded: a rule
// met again adds no values of its own, as its values are the least solution of its equations.
// seen holds the shapes already expanded, whose alternatives are in out, so that a shared inline
// DAG is expanded once.
func (k *queryShapeChecker) atoms(shape *SchemaShape, out []queryAtom, active map[string]bool, seen map[*SchemaShape]bool) []queryAtom {
	if seen[shape] {
		return out
	}
	seen[shape] = true
	switch shape.Kind {
	case "nil", "error":
		return append(out, queryAtom{kind: shape.Kind})
	case "scalar":
		return append(out, queryAtom{kind: "scalar", scalar: shape.Scalar})
	case "node":
		return append(out, queryAtom{kind: "node", value: shape.Value})
	case "tuple":
		return append(out, queryAtom{kind: "tuple", elements: shape.Elements})
	case "list":
		return append(out, queryAtom{kind: "list", element: shape.Element})
	case "struct":
		return append(out, queryAtom{kind: "struct", fields: shape.Fields})
	case "optional":
		// nil comes last, so the reasons a pattern does not match name the other values first.
		if shape.Element != nil {
			out = k.atoms(shape.Element, out, active, seen)
		}
		return append(out, queryAtom{kind: "nil"})
	case "union":
		for i := range shape.Alternatives {
			out = k.atoms(&shape.Alternatives[i], out, active, seen)
		}
		return out
	case "ref":
		t := k.s.byID[shape.Ref]
		if t == nil {
			break
		}
		if t.Origin == "declared" {
			return append(out, k.typeAtom(t))
		}
		// A rule's inferred value.
		if active[t.ID] {
			return out
		}
		active[t.ID] = true
		out = k.atoms(&t.Shape, out, active, seen)
		delete(active, t.ID)
		return out
	}
	return append(out, queryAtom{kind: "any"})
}

// typeAtom returns the atom of the values of the declared struct type t.
func (k *queryShapeChecker) typeAtom(t *SchemaType) queryAtom {
	return queryAtom{kind: "struct", name: t.Name, fields: t.Shape.Fields}
}

// patternAtom returns the atom of the values of the type a pattern names, where nothing else is
// known about the value.
func (k *queryShapeChecker) patternAtom(name string) queryAtom {
	if name == "error" {
		return queryAtom{kind: "error"}
	}
	if t := k.s.byName[name]; t != nil {
		return k.typeAtom(t)
	}
	return queryAtom{kind: "any"}
}

// match reports whether p can match a value of atom a.
func (k *queryShapeChecker) match(p *QueryPattern, a queryAtom) error {
	if a.kind == "any" {
		if p.Type != "" {
			if t := k.patternAtom(p.Type); t.kind != "any" {
				return k.match(p, t)
			}
		}
		return k.unknownParts(p)
	}
	category := a.category()
	if p.Category != "" && p.Category != category {
		return fmt.Errorf("value is %s, not %s", a.describe(), p.Category)
	}
	switch {
	case p.Type == "error" && a.kind != "error", p.Type != "" && p.Type != "error" && (a.kind != "struct" || a.name != p.Type):
		return fmt.Errorf("value is %s, not %s", a.describe(), p.Type)
	case p.Rule != "" && a.kind != "node":
		return fmt.Errorf("value is %s, not a node of rule %s", a.describe(), p.Rule)
	case p.Value != nil && a.kind != "node":
		return fmt.Errorf("value is %s, not a node", a.describe())
	case len(p.Fields) > 0 && a.kind != "struct" && a.kind != "error":
		return fmt.Errorf("value is %s, which has no fields", a.describe())
	}
	fields := a.fields
	if a.kind == "error" {
		fields = errorFields
	}
	for i := range p.Fields {
		f := &p.Fields[i]
		var shape *SchemaShape
		for j := range fields {
			if fields[j].Name == f.Name {
				shape = &fields[j].Shape
				break
			}
		}
		if shape == nil {
			return fmt.Errorf("%s has no field %s", a.describe(), f.Name)
		}
		if err := k.check(&f.Pattern, shape); err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
	}
	if p.Value != nil {
		if err := k.check(p.Value, a.value); err != nil {
			return fmt.Errorf("node value: %w", err)
		}
	}
	if len(p.Elements) > 0 {
		atoms := []queryAtom{a}
		if a.kind == "node" {
			atoms = k.shapeAtoms(a.value)
		}
		if err := k.anyAtom(atoms, func(a queryAtom) error { return k.elements(p.Elements, a) }); err != nil {
			return err
		}
	}
	return nil
}

// elements reports whether patterns can match the first elements of a value of atom a.
func (k *queryShapeChecker) elements(patterns []QueryPattern, a queryAtom) error {
	switch a.kind {
	case "any":
		for i := range patterns {
			if err := k.check(&patterns[i], nil); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		return nil
	case "tuple":
		if len(a.elements) < len(patterns) {
			return fmt.Errorf("value is a tuple of %d elements, not at least %d", len(a.elements), len(patterns))
		}
		for i := range patterns {
			if err := k.check(&patterns[i], &a.elements[i]); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		return nil
	case "list":
		for i := range patterns {
			if err := k.check(&patterns[i], a.element); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		return nil
	}
	return fmt.Errorf("value is %s, which has no elements", strings.TrimPrefix(a.describe(), "a "))
}

// unknownParts checks the field, value and element patterns of p against values of unknown
// shape.
func (k *queryShapeChecker) unknownParts(p *QueryPattern) error {
	for i := range p.Fields {
		if err := k.check(&p.Fields[i].Pattern, nil); err != nil {
			return fmt.Errorf("field %s: %w", p.Fields[i].Name, err)
		}
	}
	if p.Value != nil {
		if err := k.check(p.Value, nil); err != nil {
			return fmt.Errorf("node value: %w", err)
		}
	}
	return k.elements(p.Elements, queryAtom{kind: "any"})
}
