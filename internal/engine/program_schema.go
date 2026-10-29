package engine

import (
	"github.com/ornew/pego/internal/compile"

	"github.com/ornew/pego/internal/schema"
)

// OutputSchemaPrototype exports the start rule, the rules it calls or whose values its values
// contain, and the struct types they refer to; or every rule and type with allRules. It has no
// effect on parsing or generation. A program loaded from a compiled grammar has no schema.
func (p *Program) OutputSchemaPrototype(start string, allRules bool) (*schema.Output, error) {
	if p == nil {
		return nil, schema.ErrUnavailable
	}
	if err := compile.EntryError(start); err != nil {
		return nil, err
	}
	if p.Grammar == nil {
		return nil, errSchemaNeedsSource
	}
	return schema.Export(p.staticProgram, start, allRules)
}
