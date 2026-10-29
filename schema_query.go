package pego

import (
	"context"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/schema"
)

// OutputSchema describes possible grammar outputs independently of a parse.
// Validate checks its version 2 envelope and shape graph, including schemas built in
// memory. Decoding it with encoding/json performs the same check.
type OutputSchema = schema.Output
type SchemaType = schema.Type
type SchemaRule = schema.Rule
type SchemaShape = schema.Shape
type SchemaField = schema.Field
type SchemaCapture = schema.Capture

// ErrSchemaUnavailable reports that checked grammar metadata is unavailable.
// In particular, a parser loaded from a compiled file cannot export a schema.
var ErrSchemaUnavailable = schema.ErrUnavailable

type schemaOptions struct{ allRules bool }

// SchemaOption configures one output schema export.
type SchemaOption func(*schemaOptions)

// WithAllSchemaRules includes unreachable rules and declared types in the schema.
// By default, export starts at the parser's configured start rule.
func WithAllSchemaRules() SchemaOption {
	return func(o *schemaOptions) { o.allRules = true }
}

// OutputSchema exports checked grammar metadata. It does not inspect parse
// results or change parsing.
func (p *Parser) OutputSchema(opts ...SchemaOption) (*OutputSchema, error) {
	if p == nil || p.prog == nil {
		return nil, ErrSchemaUnavailable
	}
	var o schemaOptions
	for _, opt := range opts {
		opt(&o)
	}
	return outputSchemaPrototype(p.prog, p.start, o.allRules)
}

// outputSchemaPrototype exports the start rule of prog, the rules it calls or whose values its
// values contain, and the struct types they refer to; or every rule and type with allRules. It
// has no effect on parsing or generation. A program loaded from a compiled grammar has no schema
// (engine.Program.SchemaProgram).
func outputSchemaPrototype(prog *engine.Program, start string, allRules bool) (*OutputSchema, error) {
	sp, err := prog.SchemaProgram(start)
	if err != nil {
		return nil, err
	}
	return schema.Export(sp, start, allRules)
}

// QueryPattern is a programmatic structural pattern. Empty constraints match
// any value of the applicable category. Elements form an ordered prefix of a
// tuple, a list or a node's value; required fields must be present, while extra
// elements and fields are allowed.
type QueryPattern = schema.QueryPattern
type QueryFieldPattern = schema.QueryFieldPattern
type QueryEdge = schema.QueryEdge
type QueryCapture = schema.QueryCapture
type QueryMatch = schema.QueryMatch

// ErrQueryCycle reports a cycle on the active path through the values.
var ErrQueryCycle = schema.ErrQueryCycle

// ErrQueryNoSourceInfo reports a query with a rule constraint on values that
// record no rules, because the grammar was compiled without source information.
var ErrQueryNoSourceInfo = schema.ErrQueryNoSourceInfo

// StructuralQuery is an immutable compiled programmatic pattern.
type StructuralQuery struct{ query *schema.StructuralQuery }

// CompileStructuralQuery validates and copies a pattern. A nil schema defers
// type and field checks until evaluation; a supplied schema is validated first
// and rejects known impossibilities.
func CompileStructuralQuery(pattern QueryPattern, out *OutputSchema) (*StructuralQuery, error) {
	q, err := schema.CompileStructuralQuery(pattern, out)
	if err != nil {
		return nil, err
	}
	return &StructuralQuery{query: q}, nil
}

// QuerySchema is an output schema validated once for compiling many queries.
// It owns a copy of the schema it was prepared from, so later changes to that
// schema do not affect it. It is immutable and safe for concurrent use. Obtain
// one from PrepareQuerySchema: a nil or zero QuerySchema holds no schema and
// defers type and field checks until evaluation.
type QuerySchema struct{ schema *schema.QuerySchema }

// PrepareQuerySchema validates and copies the schema. Compiling a query with the
// result performs the same checks as CompileStructuralQuery with the schema, but
// does not validate the schema again.
func PrepareQuerySchema(out *OutputSchema) (*QuerySchema, error) {
	s, err := schema.PrepareQuerySchema(out)
	if err != nil {
		return nil, err
	}
	return &QuerySchema{schema: s}, nil
}

// CompileStructuralQuery validates and copies a pattern, rejecting known
// impossibilities in the prepared schema. A nil or zero QuerySchema defers type
// and field checks until evaluation, as a nil OutputSchema does.
func (s *QuerySchema) CompileStructuralQuery(pattern QueryPattern) (*StructuralQuery, error) {
	var es *schema.QuerySchema
	if s != nil {
		es = s.schema
	}
	q, err := es.CompileStructuralQuery(pattern)
	if err != nil {
		return nil, err
	}
	return &StructuralQuery{query: q}, nil
}

// QueryOption configures one query evaluation.
type QueryOption func(*schema.QueryOptions)

// WithQueryContext selects cooperative cancellation. Nil disables it.
func WithQueryContext(ctx context.Context) QueryOption {
	return func(o *schema.QueryOptions) { o.Context = ctx }
}

// WithQueryWorkLimits bounds query work. MaxSteps is charged for traversal,
// matching, paths and results; MaxInputWork is unused by structural queries.
// A zero limit means unlimited work.
func WithQueryWorkLimits(limits WorkLimits) QueryOption {
	return func(o *schema.QueryOptions) { o.WorkLimits = limits }
}

// Matches evaluates every incoming graph edge in preorder: children by index,
// then fields by name, then nested list elements by index. Results borrow
// graph values; callers must keep the graph stable. Cancellation, work-limit
// exhaustion and cycles return an error without partial results.
func (q *StructuralQuery) Matches(root *Node, opts ...QueryOption) ([]QueryMatch, error) {
	var o schema.QueryOptions
	for _, opt := range opts {
		opt(&o)
	}
	if q == nil {
		return (*schema.StructuralQuery)(nil).Matches(root, o)
	}
	return q.query.Matches(root, o)
}
