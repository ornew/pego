package engine

import (
	"fmt"
	"strings"
)

// A query pattern compiled with a schema must be able to match some value the schema admits where
// it is applied: the pattern of a field must match some value of the field's shape, and the
// pattern of a child some value of the children's element shape. A union or optional shape needs
// only one alternative that matches; where nothing is known about a value, every pattern can
// match it. Where the schema includes the recovery types Error or Missing, their nodes can stand
// for any node or list. The check never rejects a pattern that can match a value the schema
// describes.

// queryAtom is one alternative of a shape that is not a union, optional value or alias.
type queryAtom struct {
	// kind is "any" (nothing is known), "node" (a node of unknown type), "nil", "scalar", or
	// "named" (a node of type name).
	kind   string
	scalar string
	name   string
	// For named atoms: fields are the known fields; open allows others, of unknown shape.
	// children is the shape of every child, or nil if unknown; leaf nodes have none.
	fields   []SchemaField
	open     bool
	children *SchemaShape
	leaf     bool
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
	// recovery holds the atoms of the recovery nodes the schema includes.
	recovery []queryAtom
}

// recoveryFields returns the fields of the nodes of the reserved recovery type name, which the
// schema's terminal shape of the type does not list, or nil if name is not a recovery type.
func recoveryFields(name string) []SchemaField {
	text := SchemaShape{Kind: "scalar", Scalar: "string"}
	switch name {
	case TypeError:
		return []SchemaField{{Name: "message", Origin: "capture", Shape: text}}
	case TypeMissing:
		return []SchemaField{{Name: "expected", Origin: "capture", Shape: text}, {Name: "message", Origin: "capture", Shape: text}}
	}
	return nil
}

// checkQueryShapes reports a pattern of p, a compiled copy, that matches no value the schema s
// admits where it is applied.
func checkQueryShapes(p *QueryPattern, s *QuerySchema) error {
	k := &queryShapeChecker{s: s, done: map[queryShapeKey]error{}}
	for i := range s.s.Types {
		if t := &s.s.Types[i]; t.Origin == "cst" && recoveryFields(t.Name) != nil {
			k.recovery = append(k.recovery, k.typeAtom(t))
		}
	}
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
	var atoms []queryAtom
	if shape == nil {
		atoms = []queryAtom{{kind: "any"}}
	} else {
		atoms = k.atoms(shape, nil, map[string]bool{})
		for _, a := range atoms {
			if a.kind == "named" {
				atoms = append(atoms, k.recovery...)
				break
			}
		}
	}
	var first, err error
	for _, a := range atoms {
		if err = k.match(p, a); err == nil {
			break
		}
		if first == nil {
			first = err
		}
	}
	if err != nil && len(atoms) > 1 {
		// Only the first reason is kept: each one may hold the reasons of nested patterns.
		err = fmt.Errorf("none of %d alternatives matches; the first: %w", len(atoms), first)
	}
	k.done[key] = err
	return err
}

// atoms appends the alternatives of shape to out. active holds the aliases being expanded.
func (k *queryShapeChecker) atoms(shape *SchemaShape, out []queryAtom, active map[string]bool) []queryAtom {
	switch shape.Kind {
	case "nil":
		return append(out, queryAtom{kind: "nil"})
	case "scalar":
		return append(out, queryAtom{kind: "scalar", scalar: shape.Scalar})
	case "node", "terminal":
		return append(out, queryAtom{kind: "node"})
	case "optional":
		// nil comes last, so the reasons a pattern does not match name the other values first.
		if shape.Element != nil {
			out = k.atoms(shape.Element, out, active)
		}
		return append(out, queryAtom{kind: "nil"})
	case "union":
		for i := range shape.Alternatives {
			out = k.atoms(&shape.Alternatives[i], out, active)
		}
		return out
	case "list":
		// Lists are List nodes, whose children are the elements.
		return append(out, queryAtom{kind: "named", name: TypeList, open: true, children: shape.Element})
	case "cst":
		// An inferred capture record, or a reserved type.
		return append(out, k.cstAtom(strings.TrimPrefix(shape.Ref, "cst/"), shape))
	case "ref":
		t := k.typeByID(shape.Ref)
		if t == nil {
			break
		}
		switch t.Origin {
		case "cst":
			return append(out, k.typeAtom(t))
		case "declared":
			if t.Shape.Kind == "struct" || t.Shape.Kind == "terminal" {
				return append(out, k.typeAtom(t))
			}
		}
		// An alias, or a rule's inferred result.
		if active[t.ID] {
			break
		}
		active[t.ID] = true
		out = k.atoms(&t.Shape, out, active)
		delete(active, t.ID)
		return out
	}
	return append(out, queryAtom{kind: "any"})
}

func (k *queryShapeChecker) typeByID(id string) *SchemaType {
	return k.s.byID[id]
}

// typeAtom returns the atom of the nodes of the struct, terminal or reserved type t.
func (k *queryShapeChecker) typeAtom(t *SchemaType) queryAtom {
	switch t.Shape.Kind {
	case "struct":
		return queryAtom{kind: "named", name: t.Name, fields: t.Shape.Fields}
	case "terminal":
		return queryAtom{kind: "named", name: t.Name, fields: recoveryFields(t.Name), leaf: true}
	case "cst":
		return k.cstAtom(t.Name, &t.Shape)
	}
	return queryAtom{kind: "node"}
}

func (k *queryShapeChecker) cstAtom(name string, shape *SchemaShape) queryAtom {
	a := queryAtom{kind: "named", name: name, fields: shape.Fields, open: true}
	if c := shape.Children; c != nil && c.Kind == "list" {
		a.children = c.Element
	}
	return a
}

// patternAtom returns the atom of the nodes of the type a pattern names, where nothing else is
// known about the value.
func (k *queryShapeChecker) patternAtom(name string) queryAtom {
	for t := range k.s.named(name) {
		if t.Origin == "cst" || t.Shape.Kind == "struct" || t.Shape.Kind == "terminal" {
			return k.typeAtom(t)
		}
	}
	return queryAtom{kind: "node"}
}

// match reports whether p can match a value of atom a.
func (k *queryShapeChecker) match(p *QueryPattern, a queryAtom) error {
	nodeConstraint := p.Type != "" || p.Rule != "" || len(p.Fields) > 0 || len(p.Children) > 0
	switch a.kind {
	case "nil", "scalar":
		what := "nil"
		if a.kind == "scalar" {
			what = a.scalar
		}
		if nodeConstraint {
			return fmt.Errorf("value is %s, not a node", what)
		}
		if p.Category != "" && p.Category != what {
			return fmt.Errorf("value is %s, not %s", what, p.Category)
		}
		return nil
	case "any", "node":
		if a.kind == "node" && p.Category != "" && p.Category != "node" {
			return fmt.Errorf("value is a node, not %s", p.Category)
		}
		if p.Type != "" {
			if t := k.patternAtom(p.Type); t.kind == "named" {
				return k.match(p, t)
			}
		}
		return k.unknownParts(p)
	}
	if p.Category != "" && p.Category != "node" {
		return fmt.Errorf("value is a node of type %s, not %s", a.name, p.Category)
	}
	if p.Type != "" && p.Type != a.name {
		return fmt.Errorf("value is %s, not %s", a.name, p.Type)
	}
	for i := range p.Fields {
		f := &p.Fields[i]
		var shape *SchemaShape
		known := false
		for j := range a.fields {
			if a.fields[j].Name == f.Name {
				shape, known = &a.fields[j].Shape, true
				break
			}
		}
		if !known && !a.open {
			return fmt.Errorf("%s has no field %s", a.name, f.Name)
		}
		if err := k.check(&f.Pattern, shape); err != nil {
			return fmt.Errorf("field %s.%s: %w", a.name, f.Name, err)
		}
	}
	if a.leaf && len(p.Children) > 0 {
		return fmt.Errorf("%s has no children", a.name)
	}
	for i := range p.Children {
		if err := k.check(&p.Children[i], a.children); err != nil {
			return fmt.Errorf("child %d of %s: %w", i, a.name, err)
		}
	}
	return nil
}

// unknownParts checks the field and child patterns of p against values of unknown shape.
func (k *queryShapeChecker) unknownParts(p *QueryPattern) error {
	for i := range p.Fields {
		if err := k.check(&p.Fields[i].Pattern, nil); err != nil {
			return fmt.Errorf("field %s: %w", p.Fields[i].Name, err)
		}
	}
	for i := range p.Children {
		if err := k.check(&p.Children[i], nil); err != nil {
			return fmt.Errorf("child %d: %w", i, err)
		}
	}
	return nil
}
