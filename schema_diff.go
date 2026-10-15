package pego

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// SchemaChangeKind identifies an entry added, removed, or changed by a diff.
type SchemaChangeKind string

const (
	SchemaAdded   SchemaChangeKind = "added"
	SchemaRemoved SchemaChangeKind = "removed"
	SchemaChanged SchemaChangeKind = "changed"
)

// SchemaTypeChange describes one type, keyed by its namespace-qualified ID.
// Before is nil for additions; After is nil for removals.
type SchemaTypeChange struct {
	ID     string
	Kind   SchemaChangeKind
	Before *SchemaType
	After  *SchemaType
}

// SchemaRuleChange describes one rule, using the rule name as its ID.
// Before is nil for additions; After is nil for removals.
type SchemaRuleChange struct {
	ID     string
	Kind   SchemaChangeKind
	Before *SchemaRule
	After  *SchemaRule
}

// OutputSchemaDiff contains type and rule changes sorted by ID. It does not
// classify compatibility or compare envelope fields such as Start.
type OutputSchemaDiff struct {
	Types []SchemaTypeChange
	Rules []SchemaRuleChange
}

// DiffOutputSchemas compares version-1 type and rule contents. Type and rule
// array order, field order, and union alternative order are insignificant;
// child order and named references are preserved. It rejects malformed entry
// identities and cyclic in-memory shapes, but does not validate external ref
// targets or the entire externally authored schema graph.
func DiffOutputSchemas(before, after *OutputSchema) (*OutputSchemaDiff, error) {
	left, err := indexOutputSchema(before)
	if err != nil {
		return nil, fmt.Errorf("before schema: %w", err)
	}
	right, err := indexOutputSchema(after)
	if err != nil {
		return nil, fmt.Errorf("after schema: %w", err)
	}
	diff := &OutputSchemaDiff{Types: []SchemaTypeChange{}, Rules: []SchemaRuleChange{}}
	for _, id := range sortedSchemaKeys(left.types, right.types) {
		old, hadOld := left.types[id]
		new, hasNew := right.types[id]
		switch {
		case !hadOld:
			diff.Types = append(diff.Types, SchemaTypeChange{ID: id, Kind: SchemaAdded, After: &new.value})
		case !hasNew:
			diff.Types = append(diff.Types, SchemaTypeChange{ID: id, Kind: SchemaRemoved, Before: &old.value})
		case !bytes.Equal(old.encoded, new.encoded):
			diff.Types = append(diff.Types, SchemaTypeChange{ID: id, Kind: SchemaChanged, Before: &old.value, After: &new.value})
		}
	}
	for _, id := range sortedSchemaKeys(left.rules, right.rules) {
		old, hadOld := left.rules[id]
		new, hasNew := right.rules[id]
		switch {
		case !hadOld:
			diff.Rules = append(diff.Rules, SchemaRuleChange{ID: id, Kind: SchemaAdded, After: &new.value})
		case !hasNew:
			diff.Rules = append(diff.Rules, SchemaRuleChange{ID: id, Kind: SchemaRemoved, Before: &old.value})
		case !bytes.Equal(old.encoded, new.encoded):
			diff.Rules = append(diff.Rules, SchemaRuleChange{ID: id, Kind: SchemaChanged, Before: &old.value, After: &new.value})
		}
	}
	return diff, nil
}

type indexedOutputSchema struct {
	types map[string]indexedSchemaEntry[SchemaType]
	rules map[string]indexedSchemaEntry[SchemaRule]
}

type indexedSchemaEntry[T any] struct {
	value   T
	encoded []byte
}

func indexOutputSchema(schema *OutputSchema) (indexedOutputSchema, error) {
	var index indexedOutputSchema
	if schema == nil {
		return index, fmt.Errorf("nil output schema")
	}
	if schema.Format != "pego-output-schema" || schema.Version != 1 {
		return index, fmt.Errorf("unsupported output schema format %q version %d", schema.Format, schema.Version)
	}
	index.types = make(map[string]indexedSchemaEntry[SchemaType], len(schema.Types))
	for _, entry := range schema.Types {
		if entry.Name == "" || (entry.Origin != "declared" && entry.Origin != "inferred" && entry.Origin != "cst") || entry.ID != entry.Origin+"/"+entry.Name {
			return index, fmt.Errorf("invalid type identity %q", entry.ID)
		}
		if _, exists := index.types[entry.ID]; exists {
			return index, fmt.Errorf("duplicate type ID %q", entry.ID)
		}
		shape, err := canonicalSchemaShape(entry.Shape, make(map[*SchemaShape]bool))
		if err != nil {
			return index, fmt.Errorf("type %q: %w", entry.ID, err)
		}
		entry.Shape = shape
		encoded, err := json.Marshal(entry)
		if err != nil {
			return index, fmt.Errorf("type %q: encode canonical entry: %w", entry.ID, err)
		}
		index.types[entry.ID] = indexedSchemaEntry[SchemaType]{value: entry, encoded: encoded}
	}
	index.rules = make(map[string]indexedSchemaEntry[SchemaRule], len(schema.Rules))
	for _, entry := range schema.Rules {
		if entry.Name == "" || (entry.Origin != "declared" && entry.Origin != "inferred") {
			return index, fmt.Errorf("invalid rule identity %q", entry.Name)
		}
		if _, exists := index.rules[entry.Name]; exists {
			return index, fmt.Errorf("duplicate rule ID %q", entry.Name)
		}
		shape, err := canonicalSchemaShape(entry.Result, make(map[*SchemaShape]bool))
		if err != nil {
			return index, fmt.Errorf("rule %q: %w", entry.Name, err)
		}
		entry.Result = shape
		encoded, err := json.Marshal(entry)
		if err != nil {
			return index, fmt.Errorf("rule %q: encode canonical entry: %w", entry.Name, err)
		}
		index.rules[entry.Name] = indexedSchemaEntry[SchemaRule]{value: entry, encoded: encoded}
	}
	return index, nil
}

func canonicalSchemaShape(shape SchemaShape, active map[*SchemaShape]bool) (SchemaShape, error) {
	if shape.Kind == "" {
		return SchemaShape{}, fmt.Errorf("shape has no kind")
	}
	if shape.Kind == "ref" && shape.Ref == "" {
		return SchemaShape{}, fmt.Errorf("reference has no ID")
	}
	var out SchemaShape
	out.Kind, out.Ref, out.Scalar = shape.Kind, shape.Ref, shape.Scalar
	var err error
	if out.Element, err = canonicalSchemaPointer(shape.Element, active); err != nil {
		return SchemaShape{}, err
	}
	if out.Children, err = canonicalSchemaPointer(shape.Children, active); err != nil {
		return SchemaShape{}, err
	}
	type alternative struct {
		shape   SchemaShape
		encoded []byte
	}
	var alternatives []alternative
	for i := range shape.Alternatives {
		v, err := canonicalSchemaPointer(&shape.Alternatives[i], active)
		if err != nil {
			return SchemaShape{}, err
		}
		encoded, err := json.Marshal(v)
		if err != nil {
			return SchemaShape{}, fmt.Errorf("encode union alternative: %w", err)
		}
		alternatives = append(alternatives, alternative{shape: *v, encoded: encoded})
	}
	sort.Slice(alternatives, func(i, j int) bool {
		return bytes.Compare(alternatives[i].encoded, alternatives[j].encoded) < 0
	})
	for _, alt := range alternatives {
		out.Alternatives = append(out.Alternatives, alt.shape)
	}
	seen := make(map[string]bool, len(shape.Fields))
	for i := range shape.Fields {
		field := &shape.Fields[i]
		if field.Name == "" || seen[field.Name] {
			return SchemaShape{}, fmt.Errorf("empty or duplicate field name %q", field.Name)
		}
		seen[field.Name] = true
		v, err := canonicalSchemaPointer(&field.Shape, active)
		if err != nil {
			return SchemaShape{}, fmt.Errorf("field %q: %w", field.Name, err)
		}
		out.Fields = append(out.Fields, SchemaField{Name: field.Name, Origin: field.Origin, Shape: *v})
	}
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Name < out.Fields[j].Name })
	return out, nil
}

func canonicalSchemaPointer(shape *SchemaShape, active map[*SchemaShape]bool) (*SchemaShape, error) {
	if shape == nil {
		return nil, nil
	}
	if active[shape] {
		return nil, fmt.Errorf("cyclic inline shape")
	}
	active[shape] = true
	v, err := canonicalSchemaShape(*shape, active)
	delete(active, shape)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func sortedSchemaKeys[T any](left, right map[string]T) []string {
	keys := make(map[string]bool, len(left)+len(right))
	for key := range left {
		keys[key] = true
	}
	for key := range right {
		keys[key] = true
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
