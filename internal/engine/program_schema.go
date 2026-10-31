package engine

import (
	"github.com/ornew/zeroparse/internal/compile"

	"github.com/ornew/zeroparse/internal/engine/core"
)

// Static returns the static program that p runs: the checked grammar, its rules and their facts,
// which the generators and the output schema read.
func (p *Program) Static() *compile.Program { return core.StaticProgramOf(&p.sharedProgram) }

// SchemaProgram returns the static program that an output schema of the start rule is exported
// from, after checking that the export can run: p must not be nil, start must be able to start a
// parse, and p must hold its grammar, which a program loaded from a compiled grammar does not.
// The errors of a missing program or grammar wrap compile.ErrSchemaUnavailable.
func (p *Program) SchemaProgram(start string) (*compile.Program, error) {
	if p == nil {
		return nil, compile.ErrSchemaUnavailable
	}
	if err := compile.EntryError(start); err != nil {
		return nil, err
	}
	if p.Grammar == nil {
		return nil, errSchemaNeedsSource
	}
	return p.Static(), nil
}
