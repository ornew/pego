package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// QueryPattern is the experimental programmatic structural pattern model. Empty Type, Rule and
// Category are unconstrained. Categories are node, struct, tuple, list, int, string, bool, nil
// and error. Type names a struct type, or error for the values of error recovery. Rule requires
// a node produced by the rule, which only grammars compiled with source information record.
// Fields require a struct or error value. Elements match the first elements of a tuple or list,
// and see through a node to its value as the language's x[i] does; Value matches the value of a
// node.
type QueryPattern struct {
	Type     string
	Rule     string
	Category string
	Capture  string
	Fields   []QueryFieldPattern
	Value    *QueryPattern
	Elements []QueryPattern
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

// QueryCapture borrows Value and Parent from the graph. Path identifies the edges from the root:
// "element" (Index) into a tuple, a list or a node's value, "field" (Name) into a struct or
// error, "capture" (Name) to a node's capture that is not an element, and "value" from a node to
// its value. Shared occurrences remain distinct.
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

// ErrQueryNoSourceInfo reports a query with a rule constraint on values without rule names.
var ErrQueryNoSourceInfo = errors.New("structural query: rule constraints need values with source information; compile the grammar with source information")

// queryCategories lists the categories of query values.
var queryCategories = [...]string{"node", "struct", "tuple", "list", "int", "string", "bool", "nil", "error"}

// CompileStructuralQuery copies a programmatic pattern and checks known schema
// constraints. A nil schema defers all type/field checks to runtime. Text syntax,
// predicates, highlighting and generated-runtime adapters are not implemented.
func CompileStructuralQuery(pattern QueryPattern, schema *OutputSchema) (*StructuralQuery, error) {
	var s *QuerySchema
	if schema != nil {
		if err := schema.Validate(); err != nil {
			return nil, fmt.Errorf("invalid query schema: %w", err)
		}
		s = indexQuerySchema(schema)
	}
	return s.CompileStructuralQuery(pattern)
}
func copyQueryPattern(p QueryPattern, s *QuerySchema, depth int) (QueryPattern, error) {
	if depth >= 256 {
		return p, fmt.Errorf("query pattern nesting exceeds prototype limit 256")
	}
	if p.Category != "" && !slices.Contains(queryCategories[:], p.Category) {
		return p, fmt.Errorf("unknown query category %q", p.Category)
	}
	if err := checkQueryConstraints(&p); err != nil {
		return p, err
	}
	if s != nil {
		if p.Rule != "" {
			if !s.s.SourceInfo {
				return p, fmt.Errorf("query rule %q: the schema has no source information, so values record no rules", p.Rule)
			}
			if !s.rules[p.Rule] {
				return p, fmt.Errorf("unknown query rule %q", p.Rule)
			}
		}
		if p.Type != "" && p.Type != "error" {
			t := s.byName[p.Type]
			if t == nil {
				return p, fmt.Errorf("unknown query type %q", p.Type)
			}
			for _, f := range p.Fields {
				if !slices.ContainsFunc(t.Shape.Fields, func(known SchemaField) bool { return known.Name == f.Name }) {
					return p, fmt.Errorf("type %s has no field %s", p.Type, f.Name)
				}
			}
		}
	}
	if p.Type == "error" {
		for _, f := range p.Fields {
			if f.Name != "message" && f.Name != "expected" {
				return p, fmt.Errorf("type error has no field %s", f.Name)
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
	if p.Value != nil {
		c, err := copyQueryPattern(*p.Value, s, depth+1)
		if err != nil {
			return p, err
		}
		p.Value = &c
	}
	p.Elements = append([]QueryPattern(nil), p.Elements...)
	for i := range p.Elements {
		c, err := copyQueryPattern(p.Elements[i], s, depth+1)
		if err != nil {
			return p, err
		}
		p.Elements[i] = c
	}
	return p, nil
}

// checkQueryConstraints reports constraints of p that no value can satisfy together.
func checkQueryConstraints(p *QueryPattern) error {
	type constraint struct {
		name       string
		on         bool
		categories []string
	}
	typeCategory := "struct"
	if p.Type == "error" {
		typeCategory = "error"
	}
	constraints := []constraint{
		{"category " + p.Category, p.Category != "", []string{p.Category}},
		{"type", p.Type != "", []string{typeCategory}},
		{"rule", p.Rule != "", []string{"node"}},
		{"value", p.Value != nil, []string{"node"}},
		{"fields", len(p.Fields) > 0, []string{"struct", "error"}},
		{"elements", len(p.Elements) > 0, []string{"node", "tuple", "list"}},
	}
	allowed := queryCategories[:]
	var names []string
	for _, c := range constraints {
		if !c.on {
			continue
		}
		names = append(names, c.name)
		var next []string
		for _, a := range allowed {
			if slices.Contains(c.categories, a) {
				next = append(next, a)
			}
		}
		if len(next) == 0 {
			return fmt.Errorf("query pattern constraints %s match no value", strings.Join(names, ", "))
		}
		allowed = next
	}
	return nil
}

// usesRule reports whether p or a pattern in it has a rule constraint.
func (p *QueryPattern) usesRule() bool {
	if p.Rule != "" || (p.Value != nil && p.Value.usesRule()) {
		return true
	}
	for i := range p.Fields {
		if p.Fields[i].Pattern.usesRule() {
			return true
		}
	}
	for i := range p.Elements {
		if p.Elements[i].usesRule() {
			return true
		}
	}
	return false
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

// Matches returns all results or an error, never partial results. Structural matching inspects
// no source text and therefore consumes no input-work units. Every node, struct, tuple, list and
// error value is a candidate. A query with a rule constraint reports ErrQueryNoSourceInfo when
// the graph has nodes but none records a rule.
func (q *StructuralQuery) Matches(root *Node, opts QueryOptions) ([]QueryMatch, error) {
	if q == nil {
		return nil, fmt.Errorf("nil structural query")
	}
	b := &queryBudget{ctx: opts.Context, limits: opts.WorkLimits}
	if err := b.charge(0); err != nil {
		return nil, err
	}
	results := []QueryMatch{}
	sawNode, sawRule := false, false
	err := visitQueryGraph(root, b, func(v queryVisit) error {
		n, ok := v.value.(*Node)
		if !ok || n == nil {
			return nil
		}
		if category, _ := queryCategory(n); category == "node" {
			sawNode = true
			sawRule = sawRule || n.Rule() != ""
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
	if sawNode && !sawRule && q.pattern.usesRule() {
		return nil, ErrQueryNoSourceInfo
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
		v.value = unboxValue(v.value)
		category, err := queryCategory(v.value)
		if err != nil {
			return nil, false, err
		}
		if p.Category != "" && category != p.Category {
			return nil, false, nil
		}
		n, _ := v.value.(*Node)
		if p.Type != "" {
			if p.Type == "error" {
				if category != "error" {
					return nil, false, nil
				}
			} else if category != "struct" || n.Type() != p.Type {
				return nil, false, nil
			}
		}
		if p.Rule != "" && (category != "node" || n.Rule() != p.Rule) {
			return nil, false, nil
		}
		if len(p.Fields) > 0 {
			if category != "struct" && category != "error" {
				return nil, false, nil
			}
			if err := b.charge(uint64(len(p.Fields) + n.fieldCount())); err != nil {
				return nil, false, err
			}
			for i := len(p.Fields) - 1; i >= 0; i-- {
				f := &p.Fields[i]
				value, ok := n.field(f.Name)
				if !ok {
					return nil, false, nil
				}
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return nil, false, err
				}
				tasks = append(tasks, queryMatchTask{&f.Pattern, queryVisit{value, n, appendQueryPath(v.path, QueryEdge{Kind: "field", Name: f.Name})}})
			}
		}
		if p.Value != nil {
			if category != "node" {
				return nil, false, nil
			}
			if err := b.charge(uint64(len(v.path) + 1)); err != nil {
				return nil, false, err
			}
			tasks = append(tasks, queryMatchTask{p.Value, queryVisit{queryNodeValue(n), n, appendQueryPath(v.path, QueryEdge{Kind: "value"})}})
		}
		if len(p.Elements) > 0 {
			if !queryHasElements(n) || len(n.children) < len(p.Elements) {
				return nil, false, nil
			}
			if err := b.charge(uint64(len(p.Elements))); err != nil {
				return nil, false, err
			}
			for i := len(p.Elements) - 1; i >= 0; i-- {
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return nil, false, err
				}
				tasks = append(tasks, queryMatchTask{&p.Elements[i], queryVisit{n.children[i], n, appendQueryPath(v.path, QueryEdge{Kind: "element", Index: i})}})
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

// queryHasElements reports whether n is a tuple or list, or a node whose value is one.
func queryHasElements(n *Node) bool {
	if n == nil {
		return false
	}
	switch n.shape() {
	case shapeTuple, shapeList, shapeTupleValue, shapeListValue:
		return true
	}
	return false
}

// queryNodeValue returns the value of the node n: its text, or a tuple or list value sharing its
// elements.
func queryNodeValue(n *Node) any {
	switch n.shape() {
	case shapeTuple:
		return &Node{kind: kindTupleValue, start: n.start, end: n.end, children: n.children}
	case shapeList:
		return &Node{kind: kindListValue, start: n.start, end: n.end, children: n.children}
	}
	return n.text
}

func appendQueryPath(path []QueryEdge, e QueryEdge) []QueryEdge {
	p := make([]QueryEdge, len(path)+1)
	copy(p, path)
	p[len(path)] = e
	return p
}

// queryCategory returns the category of an unboxed value.
func queryCategory(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "nil", nil
	case *Node:
		if v == nil {
			return "nil", nil
		}
		switch v.shape() {
		case shapeString, shapeTuple, shapeList:
			return "node", nil
		case shapeError:
			return "error", nil
		case shapeTupleValue:
			return "tuple", nil
		case shapeListValue:
			return "list", nil
		case shapeStruct:
			return "struct", nil
		}
	case int:
		return "int", nil
	case string:
		return "string", nil
	case bool:
		return "bool", nil
	}
	return "", fmt.Errorf("unsupported query value %T", v)
}
