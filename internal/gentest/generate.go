package gentest

import (
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gen/tsgen"
)

// Compile compiles grammars for the generators, as the root package's compiler for them does. The
// package of the tests sets it: this package cannot import the engine, whose tests import it.
var Compile gen.Compiler

// MutateCode, if set, changes the code that GenerateCode returns, for negative controls.
var MutateCode func([]byte) []byte

// Generate generates a Go parser for g, as gen.Go.Generate does, with the hooks of the generator
// that t set (SetGenHooks).
func Generate(t testing.TB, g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if Compile == nil {
		t.Fatal("gentest.Compile is not set: the test package must set it before generating")
	}
	if opts.Runtime == gen.RuntimeOnly {
		return gogen.Generate(g, Compile, opts)
	}
	return gogen.GenerateParser(g, Compile, opts, testGenHooks(t))
}

// GenerateTS generates a TypeScript parser for g, as gen.TypeScript.Generate does.
func GenerateTS(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if Compile == nil {
		panic("gentest.Compile is not set: the test package must set it before generating")
	}
	return tsgen.Generate(g, Compile, opts)
}
