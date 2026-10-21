package engine

import "fmt"

// validateOutputSchemaGraph checks version 2 identities and inline shapes. Named-reference
// cycles are valid recursion; the traversal deliberately does not expand them.
func validateOutputSchemaGraph(s OutputSchema) error {
	ids := make(map[string]bool, len(s.Types))
	for i := range s.Types {
		t := &s.Types[i]
		if t.ID == "" || ids[t.ID] {
			return fmt.Errorf("duplicate or empty output schema type ID %q", t.ID)
		}
		ids[t.ID] = true
		if t.Name == "" || (t.Origin != "declared" && t.Origin != "inferred") || t.ID != t.Origin+"/"+t.Name {
			return fmt.Errorf("invalid output schema type identity %q", t.ID)
		}
		if t.Origin == "declared" && t.Shape.Kind != "struct" && t.Shape.Kind != "node" {
			return fmt.Errorf("declared output schema type %q must be a struct or a node", t.ID)
		}
	}
	type shapeFrame struct {
		shape *SchemaShape
		exit  bool
	}
	stack := make([]shapeFrame, 0, 32)
	active := make(map[*SchemaShape]bool)
	// Shared inline DAGs are valid. Rechecking them by path can take exponential work.
	completed := make(map[*SchemaShape]bool)
	seenNames := make(map[string]bool)
	check := func(root *SchemaShape, ownerKind, ownerName string) error {
		const (
			memberRef = 1 << iota
			memberScalar
			memberElement
			memberElements
			memberAlternatives
			memberFields
			memberValue
			memberCaptures
		)
		stack = append(stack[:0], shapeFrame{shape: root})
		for len(stack) > 0 {
			frame := stack[len(stack)-1]
			stack[len(stack)-1] = shapeFrame{}
			stack = stack[:len(stack)-1]
			shape := frame.shape
			if frame.exit {
				delete(active, shape)
				completed[shape] = true
				continue
			}
			if active[shape] {
				return fmt.Errorf("cyclic inline output schema shape in %s %s", ownerKind, ownerName)
			}
			if completed[shape] {
				continue
			}
			active[shape] = true
			stack = append(stack, shapeFrame{shape: shape, exit: true})
			members := 0
			if shape.Ref != "" {
				members |= memberRef
			}
			if shape.Scalar != "" {
				members |= memberScalar
			}
			if shape.Element != nil {
				members |= memberElement
			}
			if len(shape.Elements) != 0 {
				members |= memberElements
			}
			if len(shape.Alternatives) != 0 {
				members |= memberAlternatives
			}
			if len(shape.Fields) != 0 {
				members |= memberFields
			}
			if shape.Value != nil {
				members |= memberValue
			}
			if len(shape.Captures) != 0 {
				members |= memberCaptures
			}
			allowed := 0
			switch shape.Kind {
			case "ref":
				if shape.Ref == "" {
					return fmt.Errorf("empty output schema reference in %s %s", ownerKind, ownerName)
				}
				allowed = memberRef
			case "scalar":
				if shape.Scalar != "int" && shape.Scalar != "string" && shape.Scalar != "bool" {
					return fmt.Errorf("unknown output schema scalar %q in %s %s", shape.Scalar, ownerKind, ownerName)
				}
				allowed = memberScalar
			case "list", "optional":
				if shape.Element == nil {
					return fmt.Errorf("missing output schema %s element in %s %s", shape.Kind, ownerKind, ownerName)
				}
				allowed = memberElement
			case "tuple":
				allowed = memberElements
			case "join":
				allowed = memberAlternatives
			case "struct":
				allowed = memberFields
			case "node":
				if shape.Value == nil {
					return fmt.Errorf("missing output schema node value in %s %s", ownerKind, ownerName)
				}
				allowed = memberValue | memberCaptures
			case "top", "nil", "error", "missing":
			default:
				return fmt.Errorf("unknown output schema shape kind %q in %s %s", shape.Kind, ownerKind, ownerName)
			}
			if members&^allowed != 0 {
				return fmt.Errorf("unexpected output schema members on %s shape in %s %s", shape.Kind, ownerKind, ownerName)
			}
			if shape.Ref != "" && !ids[shape.Ref] {
				return fmt.Errorf("unresolved output schema reference %q in %s %s", shape.Ref, ownerKind, ownerName)
			}
			for _, sub := range []*SchemaShape{shape.Element, shape.Value} {
				if sub != nil {
					stack = append(stack, shapeFrame{shape: sub})
				}
			}
			for i := range shape.Elements {
				stack = append(stack, shapeFrame{shape: &shape.Elements[i]})
			}
			for i := range shape.Alternatives {
				stack = append(stack, shapeFrame{shape: &shape.Alternatives[i]})
			}
			clear(seenNames)
			for i := range shape.Fields {
				field := &shape.Fields[i]
				if field.Name == "" || seenNames[field.Name] {
					return fmt.Errorf("duplicate or empty output schema field %q in %s %s", field.Name, ownerKind, ownerName)
				}
				seenNames[field.Name] = true
				stack = append(stack, shapeFrame{shape: &field.Shape})
			}
			clear(seenNames)
			for i := range shape.Captures {
				c := &shape.Captures[i]
				if c.Name == "" || seenNames[c.Name] {
					return fmt.Errorf("duplicate or empty output schema capture %q in %s %s", c.Name, ownerKind, ownerName)
				}
				seenNames[c.Name] = true
				if len(c.Paths) == 0 && !c.Side {
					return fmt.Errorf("output schema capture %q in %s %s has no location", c.Name, ownerKind, ownerName)
				}
				for _, path := range c.Paths {
					for _, index := range path {
						if index < 0 {
							return fmt.Errorf("negative output schema capture index for %q in %s %s", c.Name, ownerKind, ownerName)
						}
					}
				}
				stack = append(stack, shapeFrame{shape: &c.Shape})
			}
		}
		return nil
	}
	for i := range s.Types {
		t := &s.Types[i]
		if err := check(&t.Shape, "type", t.ID); err != nil {
			return err
		}
	}
	seenRules := make(map[string]bool, len(s.Rules))
	for i := range s.Rules {
		r := &s.Rules[i]
		if r.Name == "" || seenRules[r.Name] {
			return fmt.Errorf("duplicate or empty output schema rule %q", r.Name)
		}
		seenRules[r.Name] = true
		if r.Origin != "declared" && r.Origin != "inferred" {
			return fmt.Errorf("unknown output schema rule origin %q", r.Origin)
		}
		if err := check(&r.Result, "rule", r.Name); err != nil {
			return err
		}
		if r.Result.Kind != "ref" || r.Result.Ref != "inferred/"+r.Name {
			return fmt.Errorf("output schema rule %q must reference inferred/%s", r.Name, r.Name)
		}
	}
	if !seenRules[s.Start] {
		return fmt.Errorf("output schema start rule %q is not in rules", s.Start)
	}
	return nil
}
