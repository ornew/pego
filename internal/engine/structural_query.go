package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// QueryPattern is the experimental programmatic structural pattern model.
// Empty Type, Rule and Category are unconstrained. Categories are node, int,
// string, bool, list and nil; Fields and Children require node values.
type QueryPattern struct {
	Type     string
	Rule     string
	Category string
	Capture  string
	Fields   []QueryFieldPattern
	Children []QueryPattern
}
type QueryFieldPattern struct {
	Name    string
	Pattern QueryPattern
}
type StructuralQuery struct{ pattern QueryPattern }
type QueryEdge struct {
	Kind  string
	Name  string
	Index int
}

// QueryCapture borrows Value and Parent from the graph. Path identifies the
// incoming edge from the root, so shared node occurrences remain distinct.
type QueryCapture struct {
	Name, Category string
	Value          any
	Parent         *Node
	Path           []QueryEdge
}
type QueryMatch struct{ Captures []QueryCapture }
type QueryOptions struct {
	Context    context.Context
	WorkLimits WorkLimits
}

var ErrQueryCycle = errors.New("structural query graph cycle")

// CompileStructuralQuery copies a programmatic pattern and checks known schema
// constraints. A nil schema defers all type/field checks to runtime. Text syntax,
// predicates, highlighting and generated-runtime adapters are not implemented.
func CompileStructuralQuery(pattern QueryPattern, schema *OutputSchema) (*StructuralQuery, error) {
	if schema != nil && (schema.Format != "pego-output-schema" || schema.Version != 1) {
		return nil, fmt.Errorf("unsupported query schema")
	}
	copy, err := copyQueryPattern(pattern, schema, 0)
	if err != nil {
		return nil, err
	}
	return &StructuralQuery{pattern: copy}, nil
}
func copyQueryPattern(p QueryPattern, s *OutputSchema, depth int) (QueryPattern, error) {
	if depth >= 256 {
		return p, fmt.Errorf("query pattern nesting exceeds prototype limit 256")
	}
	switch p.Category {
	case "", "node", "int", "string", "bool", "list", "nil":
	default:
		return p, fmt.Errorf("unknown query category %q", p.Category)
	}
	if (p.Type != "" || p.Rule != "" || len(p.Fields) > 0 || len(p.Children) > 0) && p.Category != "" && p.Category != "node" {
		return p, fmt.Errorf("node constraint on %s pattern", p.Category)
	}
	if s != nil {
		if p.Rule != "" {
			found := false
			for _, r := range s.Rules {
				found = found || r.Name == p.Rule
			}
			if !found {
				return p, fmt.Errorf("unknown query rule %q", p.Rule)
			}
		}
		if p.Type != "" {
			found := false
			for _, t := range s.Types {
				if t.Name == p.Type && t.Origin != "inferred" {
					found = true
					for _, f := range p.Fields {
						if !schemaMayHaveField(t.Shape, f.Name, s, map[string]bool{}) {
							return p, fmt.Errorf("type %s has no field %s", p.Type, f.Name)
						}
						for _, known := range t.Shape.Fields {
							if known.Name == f.Name {
								shape := schemaDeclaredQueryAlias(known.Shape, s)
								nilAllowed := false
								if shape.Kind == "optional" && shape.Element != nil && shape.Element.Kind == "scalar" {
									shape = *shape.Element
									nilAllowed = true
								}
								if shape.Kind != "scalar" {
									continue
								}
								category := f.Pattern.Category
								if f.Pattern.Type != "" || f.Pattern.Rule != "" || len(f.Pattern.Fields) > 0 || len(f.Pattern.Children) > 0 {
									category = "node"
								}
								if category != "" && category != shape.Scalar && !(nilAllowed && category == "nil") {
									allowed := shape.Scalar
									if nilAllowed {
										allowed += " or nil"
									}
									return p, fmt.Errorf("field %s.%s is %s, incompatible with %s pattern", p.Type, f.Name, allowed, category)
								}
							}
						}
					}
				}
			}
			if !found {
				return p, fmt.Errorf("unknown query type %q", p.Type)
			}
		}
	}
	p.Fields = append([]QueryFieldPattern(nil), p.Fields...)
	sort.Slice(p.Fields, func(i, j int) bool { return p.Fields[i].Name < p.Fields[j].Name })
	for i := range p.Fields {
		if i > 0 && p.Fields[i-1].Name == p.Fields[i].Name {
			return p, fmt.Errorf("duplicate query field %q", p.Fields[i].Name)
		}
		c, err := copyQueryPattern(p.Fields[i].Pattern, s, depth+1)
		if err != nil {
			return p, err
		}
		p.Fields[i].Pattern = c
	}
	p.Children = append([]QueryPattern(nil), p.Children...)
	for i := range p.Children {
		c, err := copyQueryPattern(p.Children[i], s, depth+1)
		if err != nil {
			return p, err
		}
		p.Children[i] = c
	}
	return p, nil
}
func schemaMayHaveField(shape SchemaShape, name string, s *OutputSchema, active map[string]bool) bool {
	for _, f := range shape.Fields {
		if f.Name == name {
			return true
		}
	}
	switch shape.Kind {
	case "any", "node", "cst":
		return true
	case "optional":
		return shape.Element != nil && schemaMayHaveField(*shape.Element, name, s, active)
	case "union":
		for _, a := range shape.Alternatives {
			if schemaMayHaveField(a, name, s, active) {
				return true
			}
		}
	}
	if shape.Ref != "" && !active[shape.Ref] {
		active[shape.Ref] = true
		defer delete(active, shape.Ref)
		for _, t := range s.Types {
			if t.ID == shape.Ref {
				return schemaMayHaveField(t.Shape, name, s, active)
			}
		}
	}
	return false
}

// schemaDeclaredQueryAlias follows declared type references only. Unknown or
// cyclic references remain unresolved, so query compilation stays conservative.
func schemaDeclaredQueryAlias(shape SchemaShape, s *OutputSchema) SchemaShape {
	if shape.Kind != "ref" {
		return shape
	}
	seen := map[string]bool{}
	for shape.Kind == "ref" && !seen[shape.Ref] {
		seen[shape.Ref] = true
		found := false
		for _, typ := range s.Types {
			if typ.ID == shape.Ref && typ.Origin == "declared" {
				shape = typ.Shape
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return shape
}

type queryBudget struct {
	ctx    context.Context
	limits WorkLimits
	steps  uint64
}

func (b *queryBudget) charge(n uint64) error {
	if b.ctx != nil {
		if err := b.ctx.Err(); err != nil {
			return err
		}
	}
	if exceedsLimit(b.steps, n, b.limits.MaxSteps) {
		return &WorkLimitError{Kind: StepWorkLimit, Limit: b.limits.MaxSteps, Used: b.steps, Requested: n}
	}
	b.steps = saturatingAdd(b.steps, n)
	return nil
}

// Matches returns all results or an error, never partial results. Structural
// matching inspects no source text and therefore consumes no input-work units.
func (q *StructuralQuery) Matches(root *Node, opts QueryOptions) ([]QueryMatch, error) {
	if q == nil {
		return nil, fmt.Errorf("nil structural query")
	}
	b := &queryBudget{ctx: opts.Context, limits: opts.WorkLimits}
	if err := b.charge(0); err != nil {
		return nil, err
	}
	results := []QueryMatch{}
	err := visitQueryGraph(root, b, func(v queryVisit) error {
		if _, ok := v.value.(*Node); !ok || v.value == (*Node)(nil) {
			return nil
		}
		captures, ok, err := q.match(v, b)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := b.charge(1); err != nil {
			return err
		}
		results = append(results, QueryMatch{Captures: captures})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := b.charge(0); err != nil {
		return nil, err
	}
	return results, nil
}

type queryMatchTask struct {
	pattern *QueryPattern
	visit   queryVisit
}

func (q *StructuralQuery) match(v queryVisit, b *queryBudget) ([]QueryCapture, bool, error) {
	tasks := []queryMatchTask{{&q.pattern, v}}
	captures := []QueryCapture{}
	for len(tasks) > 0 {
		if err := b.charge(1); err != nil {
			return nil, false, err
		}
		task := tasks[len(tasks)-1]
		tasks = tasks[:len(tasks)-1]
		p, v := task.pattern, task.visit
		category, err := queryCategory(v.value)
		if err != nil {
			return nil, false, err
		}
		if p.Category != "" && category != p.Category {
			return nil, false, nil
		}
		n, _ := v.value.(*Node)
		if p.Type != "" && (n == nil || n.Type() != p.Type) {
			return nil, false, nil
		}
		if p.Rule != "" && (n == nil || n.Rule() != p.Rule) {
			return nil, false, nil
		}
		if len(p.Fields) > 0 || len(p.Children) > 0 {
			if n == nil || len(n.Children) < len(p.Children) {
				return nil, false, nil
			}
			if err := b.charge(uint64(len(p.Fields) + len(p.Children) + len(n.Fields))); err != nil {
				return nil, false, err
			}
			fields := make(map[string]any, len(n.Fields))
			for _, f := range n.Fields {
				fields[f.Name] = f.Value
			}
			for i := len(p.Fields) - 1; i >= 0; i-- {
				f := &p.Fields[i]
				value, ok := fields[f.Name]
				if !ok {
					return nil, false, nil
				}
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return nil, false, err
				}
				tasks = append(tasks, queryMatchTask{&f.Pattern, queryVisit{value, n, appendQueryPath(v.path, QueryEdge{Kind: "field", Name: f.Name})}})
			}
			for i := len(p.Children) - 1; i >= 0; i-- {
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return nil, false, err
				}
				tasks = append(tasks, queryMatchTask{&p.Children[i], queryVisit{n.Children[i], n, appendQueryPath(v.path, QueryEdge{Kind: "child", Index: i})}})
			}
		}
		if p.Capture != "" {
			if err := b.charge(uint64(1 + len(v.path))); err != nil {
				return nil, false, err
			}
			captures = append(captures, QueryCapture{p.Capture, category, v.value, v.parent, append([]QueryEdge(nil), v.path...)})
		}
	}
	return captures, true, nil
}
func appendQueryPath(path []QueryEdge, e QueryEdge) []QueryEdge {
	p := make([]QueryEdge, len(path)+1)
	copy(p, path)
	p[len(path)] = e
	return p
}
func queryCategory(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "nil", nil
	case *Node:
		if v == nil {
			return "nil", nil
		}
		return "node", nil
	case int:
		return "int", nil
	case string:
		return "string", nil
	case bool:
		return "bool", nil
	case []any:
		return "list", nil
	}
	return "", fmt.Errorf("unsupported query value %T", v)
}
