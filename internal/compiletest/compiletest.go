// Package compiletest compiles grammars to their static programs for the tests of the packages
// that work on a compiled program without running it (the Core IR build, the output schema). It
// runs the compiler with a builder that builds no matchers, so a test needs neither the engine
// nor its closures.
package compiletest

import (
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/starttest"
	"github.com/ornew/zeroparse/internal/syntax"
)

// Parse parses ZeroParse source for a test whose subject is not start rules: unless the source has
// declaration attributes, every rule becomes a start rule, the first one the default.
func Parse(src string) (*grammar.Grammar, error) {
	g, err := syntax.Parse(src)
	return starttest.MarkAll(g), err
}

// Compile compiles g as the engine does, without building matchers. The errors name the hidden
// rules of inline actions as the engine's do.
func Compile(g *grammar.Grammar, opts compile.Options) (*compile.Program, error) {
	names := compile.InlineNames{}
	p, err := compile.CompileProgram(g, opts, nil, names, nil, func(*compile.Compiler) compile.ExprBuilder[struct{}] { return NoExprs{} })
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return p, nil
}

// Program parses and compiles src with opts, failing the test on an error.
func Program(tb testing.TB, src string, opts compile.Options) *compile.Program {
	tb.Helper()
	g, err := Parse(src)
	if err != nil {
		tb.Fatalf("syntax error: %v\n%s", err, src)
	}
	p, err := Compile(g, opts)
	if err != nil {
		tb.Fatalf("compile error: %v\n%s", err, src)
	}
	return p
}

// NoExprs is the expression builder that builds nothing (compile.NoExprs).
type NoExprs = compile.NoExprs
