package gentest

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gen/gogen"
	"github.com/ornew/zeroparse/internal/gen/tsgen"
)

// Compile compiles grammars for the generators, as the root package's compiler for them does. The
// package of the tests sets it: this package cannot import the engine, whose tests import it.
var Compile gen.Compiler

// MutateCode, if set, changes the code that GenerateCode returns, for negative controls.
var MutateCode func([]byte) []byte

// expectationsOff holds the names of the tests that generate parsers without expectations.
var expectationsOff sync.Map

// OmitExpectations makes the Go parsers that t and its subtests generate record no expectations,
// as a parser generated without gen.Go.Expectations. Generate and GenerateTS generate parsers that
// record them on every parse (see askExpectations), so that the tests that compare error messages
// keep their assertions.
func OmitExpectations(t testing.TB) {
	name := t.Name()
	expectationsOff.Store(name, true)
	t.Cleanup(func() { expectationsOff.Delete(name) })
}

// Generate generates a Go parser for g, as gen.Go.Generate does, with the hooks of the generator
// that t set (SetGenHooks). The parser records expectations unless t called OmitExpectations.
func Generate(t testing.TB, g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if _, off := byTest(&expectationsOff, t); off {
		return GenerateRaw(t, g, opts)
	}
	opts.Expectations = true
	files, err := GenerateRaw(t, g, opts)
	if err != nil || opts.Runtime == gen.RuntimeOnly {
		return files, err
	}
	return askExpectations(files), nil
}

// goEntryPoint matches the declaration of a generated Go function that takes parse options.
var goEntryPoint = regexp.MustCompile(`(?m)^func [^\n]*opts \.\.\.((?:rt\.)?)ParseOption[^\n]*\{$`)

// goOneLineEntryPoint matches a generated Go function with parse options whose body is on one line.
var goOneLineEntryPoint = regexp.MustCompile(`(?m)^(func [^\n]*opts \.\.\.((?:rt\.)?)ParseOption[^\n]*?\{) ([^\n]*\})$`)

// askExpectations makes every parse of the generated Go files pass WithExpectations first, as a
// caller that wants the expectations does, so that tests of a parser generated with expectations
// see the messages without each call passing the option. The tests of the default behavior
// generate with GenerateRaw.
func askExpectations(files []gen.GeneratedFile) []gen.GeneratedFile {
	out := make([]gen.GeneratedFile, len(files))
	for i, f := range files {
		if strings.HasSuffix(f.Path, ".go") {
			f.Content = goEntryPoint.ReplaceAll(f.Content, []byte("$0\n\topts = append([]${1}ParseOption{${1}WithExpectations()}, opts...)"))
			f.Content = goOneLineEntryPoint.ReplaceAll(f.Content, []byte("$1 opts = append([]${2}ParseOption{${2}WithExpectations()}, opts...); $3"))
		}
		out[i] = f
	}
	return out
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
	files, err := GenerateTSRaw(g, opts)
	if err != nil {
		return nil, err
	}
	return askTSExpectations(files), nil
}

// askTSExpectations makes every parse of the generated TypeScript files record expectations unless
// its options say otherwise, as askExpectations does for Go.
func askTSExpectations(files []gen.GeneratedFile) []gen.GeneratedFile {
	out := make([]gen.GeneratedFile, len(files))
	for i, f := range files {
		text := strings.Replace(string(f.Content), "p.expectations = traceOptions?.expectations === true;", "p.expectations = g.expectations && traceOptions?.expectations !== false;", 1)
		f.Content = []byte(strings.Replace(text, "expectations: options?.expectations === true, signal,", "expectations: options?.expectations !== false, signal,", 1))
		out[i] = f
	}
	return out
}

// GenerateTSRaw is GenerateTS with the options as given.
func GenerateTSRaw(g *grammar.Grammar, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if Compile == nil {
		panic("gentest.Compile is not set: the test package must set it before generating")
	}
	return tsgen.Generate(g, Compile, opts)
}
