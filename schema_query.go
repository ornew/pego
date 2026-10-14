package pego

import (
	"context"

	"github.com/ornew/pego/internal/engine"
)

// OutputSchema describes possible grammar outputs independently of a parse.
// Its JSON format and version are checked when decoding it with encoding/json.
type OutputSchema = engine.OutputSchema
type SchemaType = engine.SchemaType
type SchemaRule = engine.SchemaRule
type SchemaShape = engine.SchemaShape
type SchemaField = engine.SchemaField

// ErrSchemaUnavailable reports that checked grammar metadata is unavailable.
// In particular, a parser loaded from a compiled file cannot export a schema.
var ErrSchemaUnavailable = engine.ErrSchemaUnavailable

type schemaOptions struct{ allRules bool }

// SchemaOption configures one output schema export.
type SchemaOption func(*schemaOptions)

// WithAllSchemaRules includes unreachable rules and declared types in the schema.
// By default, export starts at the parser's configured start rule.
func WithAllSchemaRules() SchemaOption {
	return func(o *schemaOptions) { o.allRules = true }
}

// OutputSchema exports checked grammar metadata. It does not inspect parse
// results, change parsing, or describe exact CST child positions.
func (p *Parser) OutputSchema(opts ...SchemaOption) (*OutputSchema, error) {
	if p == nil || p.prog == nil {
		return nil, ErrSchemaUnavailable
	}
	var o schemaOptions
	for _, opt := range opts {
		opt(&o)
	}
	return p.prog.OutputSchemaPrototype(p.start, o.allRules)
}

// QueryPattern is a programmatic structural pattern. Empty constraints match
// any value of the applicable category. Children form an ordered prefix;
// required fields must be present, while extra children and fields are allowed.
type QueryPattern = engine.QueryPattern
type QueryFieldPattern = engine.QueryFieldPattern
type QueryEdge = engine.QueryEdge
type QueryCapture = engine.QueryCapture
type QueryMatch = engine.QueryMatch

// ErrQueryCycle reports a cycle on the active path through a Node or []any list.
var ErrQueryCycle = engine.ErrQueryCycle

// StructuralQuery is an immutable compiled programmatic pattern.
type StructuralQuery struct{ query *engine.StructuralQuery }

// CompileStructuralQuery validates and copies a pattern. A nil schema defers
// type and field checks until evaluation; a schema rejects known impossibilities.
func CompileStructuralQuery(pattern QueryPattern, schema *OutputSchema) (*StructuralQuery, error) {
	q, err := engine.CompileStructuralQuery(pattern, schema)
	if err != nil {
		return nil, err
	}
	return &StructuralQuery{query: q}, nil
}

// QueryOption configures one query evaluation.
type QueryOption func(*engine.QueryOptions)

// WithQueryContext selects cooperative cancellation. Nil disables it.
func WithQueryContext(ctx context.Context) QueryOption {
	return func(o *engine.QueryOptions) { o.Context = ctx }
}

// WithQueryWorkLimits bounds query work. MaxSteps is charged for traversal,
// matching, paths and results; MaxInputWork is unused by structural queries.
// A zero limit means unlimited work.
func WithQueryWorkLimits(limits WorkLimits) QueryOption {
	return func(o *engine.QueryOptions) { o.WorkLimits = limits }
}

// Matches evaluates every incoming graph edge in preorder: children by index,
// then fields by name, then nested list elements by index. Results borrow
// graph values; callers must keep the graph stable. Cancellation, work-limit
// exhaustion and cycles return an error without partial results.
func (q *StructuralQuery) Matches(root *Node, opts ...QueryOption) ([]QueryMatch, error) {
	var o engine.QueryOptions
	for _, opt := range opts {
		opt(&o)
	}
	if q == nil {
		return (*engine.StructuralQuery)(nil).Matches(root, o)
	}
	return q.query.Matches(root, o)
}
