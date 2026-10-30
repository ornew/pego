package gentest

import (
	"sync"
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

// expectationsOff holds the names of the tests that generate parsers without expectations.
var expectationsOff sync.Map

// OmitExpectations makes the Go parsers that t and its subtests generate record no expectations,
// as a parser generated without gen.Go.Expectations. Generate and GenerateTS record them by
// default, so that the tests that compare error messages keep their assertions.
func OmitExpectations(t testing.TB) {
	name := t.Name()
	expectationsOff.Store(name, true)
	t.Cleanup(func() { expectationsOff.Delete(name) })
}

// Generate generates a Go parser for g, as gen.Go.Generate does, with the hooks of the generator
// that t set (SetGenHooks). The parser records expectations unless t called OmitExpectations.
func Generate(t testing.TB, g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if _, off := byTest(&expectationsOff, t); !off {
		opts.Expectations = true
	}
	return GenerateRaw(t, g, opts)
}

// GenerateRaw is Generate with the options as given.
func GenerateRaw(t testing.TB, g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if Compile == nil {
		t.Fatal("gentest.Compile is not set: the test package must set it before generating")
	}
	if opts.Runtime == gen.RuntimeOnly {
		return gogen.Generate(g, Compile, opts)
	}
	return gogen.GenerateParser(g, Compile, opts, testGenHooks(t))
}

// GenerateTS generates a TypeScript parser for g that records expectations, as
// gen.TypeScript.Generate does with Expectations.
func GenerateTS(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	opts.Expectations = true
	return GenerateTSRaw(g, opts)
}

// GenerateTSRaw is GenerateTS with the options as given.
func GenerateTSRaw(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if Compile == nil {
		panic("gentest.Compile is not set: the test package must set it before generating")
	}
	return tsgen.Generate(g, Compile, opts)
}
