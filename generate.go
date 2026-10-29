package pego

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
)

// compileForGenerator is the gen.Compiler that GenerateGo and GenerateTypeScript pass to the
// generators: it compiles g with the engine and builds the recognizer of the program
// (engine.Program.Recognizer) on first use.
func compileForGenerator(g *grammar.Grammar, opts compile.Options) (*compile.Program, func() (*compile.Program, error), error) {
	prog, err := engine.Compile(g, opts)
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
