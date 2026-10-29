// Package query matches structural patterns against parse results. A Pattern
// describes values by category, type, rule, fields and elements and names the
// values to capture; Compile checks it against an output schema (package
// schema) and Query.Matches finds every place it matches in a result.
package query

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
	"github.com/ornew/pego/schema"
)

// Pattern is a programmatic structural pattern. Empty Type, Rule and Category are
// unconstrained. Categories are node, struct, tuple, list, int, string, bool, nil and error.
// Type names a struct type, or error for the values of error recovery. Rule requires a node
// produced by the rule, which only grammars compiled with source information record. Fields
// require a struct or error value; extra fields are allowed. Elements match the first elements of
// a tuple or list, and see through a node to its value as the language's x[i] does; extra
// elements are allowed. Value matches the value of a node.
type Pattern struct {
	Type     string
	Rule     string
	Category string
	Capture  string
	Fields   []FieldPattern
	Value    *Pattern
	Elements []Pattern
}

// FieldPattern requires the field Name, whose value matches Pattern.
type FieldPattern struct {
	Name    string
	Pattern Pattern
}

// Query is a compiled pattern. It is immutable and safe for concurrent use.
type Query struct{ pattern Pattern }

// Edge is one edge of the path from the root to a captured value (Capture).
type Edge struct {
	Kind  string
	Name  string
	Index int
}

// Capture borrows Value and Parent from the graph. Path identifies the edges from the root:
// "element" (Index) into a tuple, a list or a node's value, "field" (Name) into a struct or
// error, "capture" (Name) to a node's capture that is not an element, and "value" from a node to
// its value. Shared occurrences remain distinct.
type Capture struct {
	Name, Category string
	Value          any
	Parent         *pego.Node
	Path           []Edge
}

// Match is one match of a pattern: the values its captures name.
type Match struct{ Captures []Capture }

type options struct {
	ctx    context.Context
	limits limits.WorkLimits
}

// Option configures one query evaluation.
type Option func(*options)

// WithContext selects cooperative cancellation. Nil disables it.
func WithContext(ctx context.Context) Option {
	return func(o *options) { o.ctx = ctx }
}

// WithWorkLimits bounds query work. MaxSteps is charged for traversal, matching, paths and
// results; MaxInputWork is unused by structural queries. A zero limit means unlimited work.
func WithWorkLimits(limits pego.WorkLimits) Option {
	return func(o *options) { o.limits = limits }
}

// ErrCycle reports a cycle on the active path through the values.
var ErrCycle = errors.New("structural query graph cycle")

// ErrNoSourceInfo reports a query with a rule constraint on values that record no rules, because
// the grammar was compiled without source information.
var ErrNoSourceInfo = errors.New("structural query: rule constraints need values with source information; compile the grammar with source information")

// categories lists the categories of query values.
var categories = [...]string{"node", "struct", "tuple", "list", "int", "string", "bool", "nil", "error"}

// Compile validates and copies a pattern. A nil schema defers type and field checks until
// evaluation; a supplied schema is validated first and rejects known impossibilities. To compile
// many patterns with one schema, Prepare it once.
func Compile(pattern Pattern, out *schema.Output) (*Query, error) {
	var s *Schema
	if out != nil {
		if err := out.Validate(); err != nil {
			return nil, fmt.Errorf("invalid query schema: %w", err)
		}
		s = indexSchema(out)
	}
	return s.Compile(pattern)
}

func copyPattern(p Pattern, s *Schema, depth int) (Pattern, error) {
	if depth >= 256 {
		return p, fmt.Errorf("query pattern nesting exceeds prototype limit 256")
	}
	if p.Category != "" && !slices.Contains(categories[:], p.Category) {
		return p, fmt.Errorf("unknown query category %q", p.Category)
	}
	if err := checkConstraints(&p); err != nil {
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
				if !slices.ContainsFunc(t.Shape.Fields, func(known schema.Field) bool { return known.Name == f.Name }) {
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
	p.Fields = append([]FieldPattern(nil), p.Fields...)
	sort.Slice(p.Fields, func(i, j int) bool { return p.Fields[i].Name < p.Fields[j].Name })
	for i := range p.Fields {
		if i > 0 && p.Fields[i-1].Name == p.Fields[i].Name {
			return p, fmt.Errorf("duplicate query field %q", p.Fields[i].Name)
		}
		c, err := copyPattern(p.Fields[i].Pattern, s, depth+1)
		if err != nil {
			return p, err
		}
		p.Fields[i].Pattern = c
	}
	if p.Value != nil {
		c, err := copyPattern(*p.Value, s, depth+1)
		if err != nil {
			return p, err
		}
		p.Value = &c
	}
	p.Elements = append([]Pattern(nil), p.Elements...)
	for i := range p.Elements {
		c, err := copyPattern(p.Elements[i], s, depth+1)
		if err != nil {
			return p, err
		}
		p.Elements[i] = c
	}
	return p, nil
}

// checkConstraints reports constraints of p that no value can satisfy together.
func checkConstraints(p *Pattern) error {
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
	allowed := categories[:]
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
func (p *Pattern) usesRule() bool {
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

// budget charges the steps of one evaluation against its work limits, checking its
// context for cancellation.
type budget struct {
	ctx    context.Context
	limits limits.WorkLimits
	steps  uint64
}

func (b *budget) charge(n uint64) error {
	if b.ctx != nil {
		if err := b.ctx.Err(); err != nil {
			return err
		}
	}
	if limits.ExceedsLimit(b.steps, n, b.limits.MaxSteps) {
		return &limits.WorkLimitError{Kind: limits.StepWorkLimit, Limit: b.limits.MaxSteps, Used: b.steps, Requested: n}
	}
	b.steps = limits.SaturatingAdd(b.steps, n)
	return nil
}

// Matches evaluates the pattern at every value reachable from root, per incoming edge, visiting
// each value before its outgoing edges: the elements of a tuple, a list or a node's value by
// index, then a node's captures that are not elements, by name, and the fields of a struct by
// name. Every node, struct, tuple, list and error value is a candidate. Results borrow graph
// values; callers must keep the graph stable. Structural matching inspects no source text and
// therefore consumes no input work. Cancellation, work-limit exhaustion and cycles (ErrCycle)
// return an error without partial results. A query with a rule constraint reports
// ErrNoSourceInfo when the graph has nodes but none records a rule.
func (q *Query) Matches(root *pego.Node, opts ...Option) ([]Match, error) {
	if q == nil {
		return nil, fmt.Errorf("nil structural query")
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	b := &budget{ctx: o.ctx, limits: o.limits}
	if err := b.charge(0); err != nil {
		return nil, err
	}
	results := []Match{}
	sawNode, sawRule := false, false
	err := visitGraph(root, b, func(v graphVisit) error {
		n, ok := v.Value.(*value.Node)
		if !ok || n == nil {
			return nil
		}
		if category, _ := valueCategory(n); category == "node" {
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
		results = append(results, Match{Captures: captures})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if sawNode && !sawRule && q.pattern.usesRule() {
		return nil, ErrNoSourceInfo
	}
	if err := b.charge(0); err != nil {
		return nil, err
	}
	return results, nil
}

type matchTask struct {
	pattern *Pattern
	visit   graphVisit
}

func (q *Query) match(v graphVisit, b *budget) ([]Capture, bool, error) {
	tasks := []matchTask{{&q.pattern, v}}
	captures := []Capture{}
	for len(tasks) > 0 {
		if err := b.charge(1); err != nil {
			return nil, false, err
		}
		task := tasks[len(tasks)-1]
		tasks = tasks[:len(tasks)-1]
		p, v := task.pattern, task.visit
		v.Value = value.UnboxValue(v.Value)
		category, err := valueCategory(v.Value)
		if err != nil {
			return nil, false, err
		}
		if p.Category != "" && category != p.Category {
			return nil, false, nil
		}
		n, _ := v.Value.(*value.Node)
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
			if err := b.charge(uint64(len(p.Fields) + len(value.D(n).Fields))); err != nil {
				return nil, false, err
			}
			for i := len(p.Fields) - 1; i >= 0; i-- {
				f := &p.Fields[i]
				val, ok := value.D(n).Fields.Get(f.Name)
				if !ok {
					return nil, false, nil
				}
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return nil, false, err
				}
				tasks = append(tasks, matchTask{&f.Pattern, graphVisit{val, n, appendPath(v.path, Edge{Kind: "field", Name: f.Name})}})
			}
		}
		if p.Value != nil {
			if category != "node" {
				return nil, false, nil
			}
			if err := b.charge(uint64(len(v.path) + 1)); err != nil {
				return nil, false, err
			}
			tasks = append(tasks, matchTask{p.Value, graphVisit{nodeValue(n), n, appendPath(v.path, Edge{Kind: "value"})}})
		}
		if len(p.Elements) > 0 {
			if !hasElements(n) || len(value.D(n).Children) < len(p.Elements) {
				return nil, false, nil
			}
			if err := b.charge(uint64(len(p.Elements))); err != nil {
				return nil, false, err
			}
			for i := len(p.Elements) - 1; i >= 0; i-- {
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return nil, false, err
				}
				tasks = append(tasks, matchTask{&p.Elements[i], graphVisit{value.D(n).Children[i], n, appendPath(v.path, Edge{Kind: "element", Index: i})}})
			}
		}
		if p.Capture != "" {
			if err := b.charge(uint64(1 + len(v.path))); err != nil {
				return nil, false, err
			}
			captures = append(captures, Capture{p.Capture, category, v.Value, v.parent, append([]Edge(nil), v.path...)})
		}
	}
	return captures, true, nil
}

// hasElements reports whether n is a tuple or list, or a node whose value is one.
func hasElements(n *value.Node) bool {
	if n == nil {
		return false
	}
	switch value.ShapeOf(n) {
	case value.ShapeTuple, value.ShapeList, value.ShapeTupleValue, value.ShapeListValue:
		return true
	}
	return false
}

// nodeValue returns the value of the node n: its text, or a tuple or list value sharing its
// elements.
func nodeValue(n *value.Node) any {
	switch value.ShapeOf(n) {
	case value.ShapeTuple:
		return value.Ptr(value.Data{Kind: value.TupleValueKind, Start: value.D(n).Start, End: value.D(n).End, Children: value.D(n).Children})
	case value.ShapeList:
		return value.Ptr(value.Data{Kind: value.ListValueKind, Start: value.D(n).Start, End: value.D(n).End, Children: value.D(n).Children})
	}
	return value.D(n).Text
}

func appendPath(path []Edge, e Edge) []Edge {
	p := make([]Edge, len(path)+1)
	copy(p, path)
	p[len(path)] = e
	return p
}

// valueCategory returns the category of an unboxed value.
func valueCategory(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "nil", nil
	case *value.Node:
		if v == nil {
			return "nil", nil
		}
		switch value.ShapeOf(v) {
		case value.ShapeString, value.ShapeTuple, value.ShapeList:
			return "node", nil
		case value.ShapeError:
			return "error", nil
		case value.ShapeTupleValue:
			return "tuple", nil
		case value.ShapeListValue:
			return "list", nil
		case value.ShapeStruct:
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
