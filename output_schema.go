package zp

import (
	"github.com/ornew/zeroparse/internal/engine"
	ischema "github.com/ornew/zeroparse/internal/schema"
	"github.com/ornew/zeroparse/schema"
)

// ErrSchemaUnavailable reports that checked grammar metadata is unavailable.
// In particular, a parser loaded from a compiled file cannot export a schema.
var ErrSchemaUnavailable = ischema.ErrUnavailable

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
func (p *Parser) OutputSchema(opts ...SchemaOption) (*schema.Output, error) {
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
func outputSchemaPrototype(prog *engine.Program, start string, allRules bool) (*schema.Output, error) {
	sp, err := prog.SchemaProgram(start)
	if err != nil {
		return nil, err
	}
	return ischema.Export(sp, start, allRules)
}
