package pego

import (
	"cmp"
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
// child order and named references are preserved. Both inputs must satisfy the
// complete version-1 schema contract checked by OutputSchema.Validate.
func DiffOutputSchemas(before, after *OutputSchema) (*OutputSchemaDiff, error) {
	canonical := &schemaCanonicalizer{copies: make(map[*SchemaShape]canonicalSchemaCopy), comparisons: make(map[schemaShapePair]int)}
	left, err := indexOutputSchema(before, canonical)
	if err != nil {
		return nil, fmt.Errorf("before schema: %w", err)
	}
	right, err := indexOutputSchema(after, canonical)
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
		case old.value.Origin != new.value.Origin || canonical.compare(old.shape, new.shape) != 0:
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
		case old.value.Origin != new.value.Origin || canonical.compare(old.shape, new.shape) != 0:
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
	value T
	shape *SchemaShape
}

func indexOutputSchema(schema *OutputSchema, canonical *schemaCanonicalizer) (indexedOutputSchema, error) {
	var index indexedOutputSchema
	if err := schema.Validate(); err != nil {
		return index, err
	}
	index.types = make(map[string]indexedSchemaEntry[SchemaType], len(schema.Types))
	for i := range schema.Types {
		entry := schema.Types[i]
		shape, _, err := canonical.copy(&schema.Types[i].Shape, 2)
		if err != nil {
			return index, fmt.Errorf("type %q: %w", entry.ID, err)
		}
		entry.Shape = *shape
		index.types[entry.ID] = indexedSchemaEntry[SchemaType]{value: entry, shape: shape}
	}
	index.rules = make(map[string]indexedSchemaEntry[SchemaRule], len(schema.Rules))
	for i := range schema.Rules {
		entry := schema.Rules[i]
		shape, _, err := canonical.copy(&schema.Rules[i].Result, 2)
		if err != nil {
			return index, fmt.Errorf("rule %q: %w", entry.Name, err)
		}
		entry.Result = *shape
		index.rules[entry.Name] = indexedSchemaEntry[SchemaRule]{value: entry, shape: shape}
	}
	return index, nil
}

type canonicalSchemaCopy struct {
	shape  *SchemaShape
	height int
}

type schemaShapePair struct{ left, right *SchemaShape }

type schemaCanonicalizer struct {
	copies      map[*SchemaShape]canonicalSchemaCopy
	comparisons map[schemaShapePair]int
}

// Validation has already rejected cycles. Copy each inline node once and keep
// shared pointers; serializing a DAG to JSON here would expand it exponentially.
func (c *schemaCanonicalizer) copy(shape *SchemaShape, depth int) (*SchemaShape, int, error) {
	if shape == nil {
		return nil, 0, nil
	}
	if depth > 10000 {
		return nil, 0, fmt.Errorf("encode canonical entry: maximum JSON nesting depth exceeded")
	}
	if cached, ok := c.copies[shape]; ok {
		if depth+cached.height-1 > 10000 {
			return nil, 0, fmt.Errorf("encode canonical entry: maximum JSON nesting depth exceeded")
		}
		return cached.shape, cached.height, nil
	}
	out := &SchemaShape{Kind: shape.Kind, Ref: shape.Ref, Scalar: shape.Scalar}
	height := 1
	copyChild := func(child *SchemaShape, offset int) (*SchemaShape, error) {
		value, childHeight, err := c.copy(child, depth+offset)
		if child != nil {
			height = max(height, offset+childHeight)
		}
		return value, err
	}
	var err error
	if out.Element, err = copyChild(shape.Element, 1); err != nil {
		return nil, 0, err
	}
	if out.Children, err = copyChild(shape.Children, 1); err != nil {
		return nil, 0, err
	}
	alternatives := make([]*SchemaShape, len(shape.Alternatives))
	for i := range shape.Alternatives {
		if alternatives[i], err = copyChild(&shape.Alternatives[i], 2); err != nil {
			return nil, 0, err
		}
	}
	sort.Slice(alternatives, func(i, j int) bool { return c.compare(alternatives[i], alternatives[j]) < 0 })
	for _, alternative := range alternatives {
		out.Alternatives = append(out.Alternatives, *alternative)
	}
	for i := range shape.Fields {
		field := &shape.Fields[i]
		value, err := copyChild(&field.Shape, 3)
		if err != nil {
			return nil, 0, err
		}
		out.Fields = append(out.Fields, SchemaField{Name: field.Name, Origin: field.Origin, Shape: *value})
	}
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Name < out.Fields[j].Name })
	c.copies[shape] = canonicalSchemaCopy{shape: out, height: height}
	return out, height, nil
}

// Canonical children are immutable before comparison. Remember shape pairs so
// comparing separately allocated but equal shared DAGs also avoids path expansion.
func (c *schemaCanonicalizer) compare(left, right *SchemaShape) int {
	if left == right {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	pair := schemaShapePair{left, right}
	if result, ok := c.comparisons[pair]; ok {
		return result
	}
	result := c.compareContents(left, right)
	c.comparisons[pair] = result
	c.comparisons[schemaShapePair{right, left}] = -result
	return result
}

func (c *schemaCanonicalizer) compareContents(left, right *SchemaShape) int {
	for _, pair := range [][2]string{{left.Kind, right.Kind}, {left.Ref, right.Ref}, {left.Scalar, right.Scalar}} {
		if order := cmp.Compare(pair[0], pair[1]); order != 0 {
			return order
		}
	}
	if order := c.compare(left.Element, right.Element); order != 0 {
		return order
	}
	if order := c.compare(left.Children, right.Children); order != 0 {
		return order
	}
	for i := range min(len(left.Fields), len(right.Fields)) {
		a, b := &left.Fields[i], &right.Fields[i]
		if order := cmp.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Origin, b.Origin); order != 0 {
			return order
		}
		if order := c.compare(&a.Shape, &b.Shape); order != 0 {
			return order
		}
	}
	if order := cmp.Compare(len(left.Fields), len(right.Fields)); order != 0 {
		return order
	}
	for i := range min(len(left.Alternatives), len(right.Alternatives)) {
		if order := c.compare(&left.Alternatives[i], &right.Alternatives[i]); order != 0 {
			return order
		}
	}
	return cmp.Compare(len(left.Alternatives), len(right.Alternatives))
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
