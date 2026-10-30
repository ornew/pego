// Package compiletest compiles grammars to their static programs for the tests of the packages
// that work on a compiled program without running it (the Core IR build, the output schema). It
// runs the compiler with a builder that builds no matchers, so a test needs neither the engine
// nor its closures.
package compiletest

import (
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/syntax"
)

// Parse parses PEGO source for a test whose subject is not start rules: unless the source has
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

// NoExprs is an expression builder that builds nothing: the compiler still walks every rule body
// and decides its static facts. Its methods implement compile.ExprBuilder, which documents them,
// and return the zero form.
type NoExprs struct{}

var _ compile.ExprBuilder[struct{}] = NoExprs{}

func (NoExprs) Literal(e *grammar.Literal, desc compile.ExpID, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Class(e *grammar.CharClass, desc compile.ExpID, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Any(build bool) struct{}                { return struct{}{} }
func (NoExprs) Call(r *compile.Rule, min int) struct{} { return struct{}{} }
func (NoExprs) Seq(items []struct{}, flat, valued []bool, n int, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Choice(e *grammar.Choice, alts []struct{}, firsts []compile.First, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Optional(e *grammar.Optional, m struct{}, build bool) struct{} { return struct{}{} }
func (NoExprs) And(m struct{}) struct{}                                       { return struct{}{} }
func (NoExprs) Not(m struct{}) struct{}                                       { return struct{}{} }
func (NoExprs) Atomic(m struct{}, build bool) struct{}                        { return struct{}{} }
func (NoExprs) Discard(m struct{}) struct{}                                   { return struct{}{} }
func (NoExprs) Capture(e *grammar.Capture, slot int, m struct{}, projected, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Cut() struct{}                                             { return struct{}{} }
func (NoExprs) Empty() struct{}                                           { return struct{}{} }
func (NoExprs) Fail() struct{}                                            { return struct{}{} }
func (NoExprs) Anchor(e grammar.Expr, desc compile.ExpID) struct{}        { return struct{}{} }
func (NoExprs) Predicate(e *grammar.Predicate, s *compile.Scope) struct{} { return struct{}{} }
func (NoExprs) ScanRepeat(e *grammar.Repeat, desc compile.ExpID) struct{} { return struct{}{} }
func (NoExprs) Repeat(e *grammar.Repeat, elem struct{}, elemScope *compile.Scope, ownScope, build, stream bool, site int, shiftable bool) struct{} {
	return struct{}{}
}
func (NoExprs) ProjectRepeat(e *grammar.Repeat, elem struct{}, slot, n int) struct{} {
	return struct{}{}
}
func (NoExprs) Missing(m struct{}, site int, expected string, build bool) struct{} { return struct{}{} }
func (NoExprs) ErrorMessage(m struct{}, msg compile.ExpID, missingScope bool) struct{} {
	return struct{}{}
}
func (NoExprs) Diagnostic(m struct{}, name compile.ExpID) struct{}            { return struct{}{} }
func (NoExprs) Recover(e grammar.Expr, m, skip struct{}, build bool) struct{} { return struct{}{} }
func (NoExprs) RuleBody(r *compile.Rule, body struct{})                       {}
func (NoExprs) OperatorsRule(r *compile.Rule, skip struct{}, hasSkip bool, operands, operators []compile.OpLine[struct{}]) {
}
