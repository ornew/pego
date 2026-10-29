package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gen/tsgen"
	"github.com/ornew/pego/internal/schema"
)

// The engine does not import the generators or the output schema; packages gen and pego join them
// (gen.Go, gen.TypeScript and Parser.OutputSchema). These helpers join them the
// same way for the tests of this package.

// Generate generates a Go parser for g, as gen.Go.Generate does.
func Generate(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	return gogen.Generate(g, compileForGenerator, opts)
}

// GenerateTS generates a TypeScript parser for g, as gen.TypeScript.Generate does.
func GenerateTS(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	return tsgen.Generate(g, compileForGenerator, opts)
}

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
	return schema.Export(sp, start, allRules)
}
