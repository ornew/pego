package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

// Generate generates the files of a Go parser for a grammar: parser.go with the code generated
// from the grammar, api.go, which re-exports the names the runtime defines for the users of the
// parser, with opts.Types types/types.go, the package of the Go types of the typed values, and, with
// opts.Runtime RuntimeEmbed, a copy of the runtime in internal/runtime. Paths are relative to the
// directory of the generated package.
//
// Eligible rule bodies inline their expressions; other expressions become Go functions. Actions
// and predicates become Go expressions, and the runtime (package runtime of this module) behaves
// exactly like the engine in this package. Without opts.Stream, #stream is a plain repetition.
func Generate(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	return generateGo(g, compileForGenerator, opts)
}

// GenerateTS generates the files of a TypeScript parser for a grammar that behaves exactly like the
// engine (and the generated Go parsers): runtime.ts, with the runtime (RuntimeEmbed), parser.ts, with
// the code generated from the grammar, and with opts.Types types.ts, with the declarations of the
// grammar's types. RuntimeImport imports the runtime from opts.RuntimeModule instead of writing it,
// and RuntimeOnly writes only runtime.ts, for which g may be nil.
//
// Each parsing expression becomes a function and actions and predicates become TypeScript
// expressions, as in Generate; the embedded runtime (tsrt) mirrors runtime. opts.Package is not used.
// opts.Types adds declarations and Node-to-value conversion entry points.
func GenerateTS(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	return generateTS(g, compileForGenerator, opts)
}

// compileForGenerator is the gen.Compiler of the engine: it compiles g with Compile and builds
// the recognizer of the program (Program.recognizer) on first use.
func compileForGenerator(g *grammar.Grammar, opts compile.Options) (*compile.Program, func() (*compile.Program, error), error) {
	prog, err := Compile(g, opts)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*compile.Program, error) {
		rec, err := prog.recognizer()
		if err != nil {
			return nil, err
		}
		return rec.staticProgram, nil
	}
	return prog.staticProgram, recognizer, nil
}
