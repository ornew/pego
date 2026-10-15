package engine

import "fmt"

// validateOutputSchemaGraph checks v1 identities and inline shapes. Named-reference
// cycles are valid recursion; the traversal deliberately does not expand them.
func validateOutputSchemaGraph(s OutputSchema) error {
	ids := make(map[string]bool, len(s.Types))
	for i := range s.Types {
		t := &s.Types[i]
		if t.ID == "" || ids[t.ID] {
			return fmt.Errorf("duplicate or empty output schema type ID %q", t.ID)
		}
		ids[t.ID] = true
		if t.Name == "" || (t.Origin != "declared" && t.Origin != "inferred" && t.Origin != "cst") || t.ID != t.Origin+"/"+t.Name {
			return fmt.Errorf("invalid output schema type identity %q", t.ID)
		}
		if t.Origin == "cst" {
			expected, ok := reservedOutputSchemaShape(t.Name)
			if !ok || !matchesReservedSchemaShape(&t.Shape, &expected) {
				return fmt.Errorf("invalid reserved output schema CST type %q", t.ID)
			}
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
	seenFields := make(map[string]bool)
	check := func(root *SchemaShape, ownerKind, ownerName string) error {
		const (
			memberRef = 1 << iota
			memberScalar
			memberElement
			memberAlternatives
			memberFields
			memberChildren
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
			if len(shape.Alternatives) != 0 {
				members |= memberAlternatives
			}
			if len(shape.Fields) != 0 {
				members |= memberFields
			}
			if shape.Children != nil {
				members |= memberChildren
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
			case "union":
				allowed = memberAlternatives
			case "struct":
				allowed = memberFields
			case "cst":
				// Inferred capture records include a Seq reference, ordered
				// children and capture fields on the same shape.
				allowed = memberRef | memberFields | memberChildren
			case "node", "terminal", "any", "nil":
			default:
				return fmt.Errorf("unknown output schema shape kind %q in %s %s", shape.Kind, ownerKind, ownerName)
			}
			if members&^allowed != 0 {
				return fmt.Errorf("unexpected output schema members on %s shape in %s %s", shape.Kind, ownerKind, ownerName)
			}
			if shape.Ref != "" && !ids[shape.Ref] {
				return fmt.Errorf("unresolved output schema reference %q in %s %s", shape.Ref, ownerKind, ownerName)
			}
			if shape.Element != nil {
				stack = append(stack, shapeFrame{shape: shape.Element})
			}
			if shape.Children != nil {
				stack = append(stack, shapeFrame{shape: shape.Children})
			}
			for i := range shape.Alternatives {
				stack = append(stack, shapeFrame{shape: &shape.Alternatives[i]})
			}
			if len(shape.Fields) > 2 {
				clear(seenFields)
			}
			for i := range shape.Fields {
				field := &shape.Fields[i]
				duplicate := i == 1 && field.Name == shape.Fields[0].Name
				if len(shape.Fields) > 2 {
					duplicate = seenFields[field.Name]
				}
				if field.Name == "" || duplicate {
					return fmt.Errorf("duplicate or empty output schema field %q in %s %s", field.Name, ownerKind, ownerName)
				}
				if len(shape.Fields) > 2 {
					seenFields[field.Name] = true
				}
				if (shape.Kind == "struct" && field.Origin != "declared") || (shape.Kind == "cst" && field.Origin != "capture") {
					return fmt.Errorf("invalid output schema field origin %q for %s %s.%s", field.Origin, ownerKind, ownerName, field.Name)
				}
				stack = append(stack, shapeFrame{shape: &field.Shape})
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

func reservedOutputSchemaShape(name string) (SchemaShape, bool) {
	known := false
	for _, reserved := range reservedTypes {
		known = known || name == reserved
	}
	if !known {
		return SchemaShape{}, false
	}
	if name == TypeSeq || name == TypeList || name == TypeOperator {
		element := SchemaShape{Kind: "optional", Element: &SchemaShape{Kind: "node"}}
		return SchemaShape{Kind: "cst", Children: &SchemaShape{Kind: "list", Element: &element}}, true
	}
	return SchemaShape{Kind: "terminal"}, true
}

// The expected reserved shape has bounded depth and no fields/alternatives.
// Empty and omitted slices have the same schema meaning.
func matchesReservedSchemaShape(shape, expected *SchemaShape) bool {
	if shape == nil || expected == nil {
		return shape == expected
	}
	return shape.Kind == expected.Kind && shape.Ref == expected.Ref &&
		shape.Scalar == expected.Scalar && len(shape.Fields) == 0 &&
		len(shape.Alternatives) == 0 &&
		matchesReservedSchemaShape(shape.Element, expected.Element) &&
		matchesReservedSchemaShape(shape.Children, expected.Children)
}
