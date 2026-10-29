package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	ischema "github.com/ornew/pego/internal/schema"
	"github.com/ornew/pego/schema"
)

// The engine does not import the generators or the output schema; packages gen and pego join them
// (gen.Go, gen.TypeScript and Parser.OutputSchema). These helpers join them the
// same way for the tests of this package.

// compileForGenerator compiles g for the generators, as the root package's compileForGenerator
// does.
func compileForGenerator(g *grammar.Grammar, opts compile.Options) (*compile.Program, func() (*compile.Program, error), error) {
	prog, err := Compile(g, opts)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*compile.Program, error) {
		rec, err := prog.Recognizer()
		if err != nil {
			return nil, err
		}
		return rec.Static(), nil
	}
	return prog.Static(), recognizer, nil
}

// outputSchemaPrototype exports the output schema of p for start, as Parser.OutputSchema does.
func outputSchemaPrototype(p *Program, start string, allRules bool) (*schema.Output, error) {
	sp, err := p.SchemaProgram(start)
	if err != nil {
		return nil, err
	}
	return ischema.Export(sp, start, allRules)
}
