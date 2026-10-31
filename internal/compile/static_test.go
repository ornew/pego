package compile

import (
	"context"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/starttest"
	"github.com/ornew/zeroparse/internal/syntax"
)

// The tests of this package compile grammars to their static programs with noExprs, which builds
// no matchers: they check what the compiler decides, which the engine's tests check by running it.

// parseTest parses ZeroParse source for a test whose subject is not start rules: unless the source has
// declaration attributes, every rule becomes a start rule, the first one the default.
func parseTest(src string) (*grammar.Grammar, error) {
	g, err := syntax.Parse(src)
	return starttest.MarkAll(g), err
}

// parseLegacyTest is parseTest for the legacy syntax.
func parseLegacyTest(src string) (*grammar.Grammar, error) {
	g, err := syntax.ParseLegacy(src)
	return starttest.MarkAll(g), err
}

// compileStatic compiles g as the engine does, without building matchers; flags, if not nil,
// replace static analysis and type checking.
func compileStatic(g *grammar.Grammar, opts Options, flags []RuleFlags) (*Program, error) {
	names := InlineNames{}
	p, err := CompileProgram(g, opts, flags, names, nil, func(*Compiler) ExprBuilder[struct{}] { return noExprs{} })
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return p, nil
}

// compilePollContext cancels at a deterministic poll, without timing or global compiler hooks.
type compilePollContext struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *compilePollContext) Err() error {
	c.left--
	if c.left == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

// compileTestContext returns a context that is cancelled at its polls-th check.
func compileTestContext(t *testing.T, polls int) *compilePollContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &compilePollContext{Context: ctx, cancel: cancel, left: polls}
}

// noExprs is an expression builder that builds nothing.
type noExprs struct{}

var _ ExprBuilder[struct{}] = noExprs{}

func (noExprs) Literal(e *grammar.Literal, desc ExpID, build bool) struct{} { return struct{}{} }
func (noExprs) Class(e *grammar.CharClass, desc ExpID, build bool) struct{} { return struct{}{} }
func (noExprs) Any(build bool) struct{}                                     { return struct{}{} }
func (noExprs) Call(r *Rule, min int) struct{}                              { return struct{}{} }
func (noExprs) Seq(items []struct{}, flat, valued []bool, n int, build bool) struct{} {
	return struct{}{}
}
func (noExprs) Choice(e *grammar.Choice, alts []struct{}, firsts []First, build bool) struct{} {
	return struct{}{}
}
func (noExprs) Optional(e *grammar.Optional, m struct{}, build bool) struct{} { return struct{}{} }
func (noExprs) And(m struct{}) struct{}                                       { return struct{}{} }
func (noExprs) Not(m struct{}) struct{}                                       { return struct{}{} }
func (noExprs) Atomic(m struct{}, build bool) struct{}                        { return struct{}{} }
func (noExprs) Discard(m struct{}) struct{}                                   { return struct{}{} }
func (noExprs) Capture(e *grammar.Capture, slot int, m struct{}, projected, build bool) struct{} {
	return struct{}{}
}
func (noExprs) Cut() struct{}                                     { return struct{}{} }
func (noExprs) Empty() struct{}                                   { return struct{}{} }
func (noExprs) Fail() struct{}                                    { return struct{}{} }
func (noExprs) Anchor(e grammar.Expr, desc ExpID) struct{}        { return struct{}{} }
func (noExprs) Predicate(e *grammar.Predicate, s *Scope) struct{} { return struct{}{} }
func (noExprs) ScanRepeat(e *grammar.Repeat, desc ExpID) struct{} { return struct{}{} }
func (noExprs) Repeat(e *grammar.Repeat, elem struct{}, elemScope *Scope, ownScope, build, stream bool, site int, shiftable bool, _ *Rule) struct{} {
	return struct{}{}
}
func (noExprs) ProjectRepeat(e *grammar.Repeat, elem struct{}, slot, n int) struct{} {
	return struct{}{}
}
func (noExprs) Missing(m struct{}, site int, expected string, build bool) struct{} { return struct{}{} }
func (noExprs) ErrorMessage(m struct{}, msg ExpID, missingScope bool) struct{}     { return struct{}{} }
func (noExprs) Diagnostic(m struct{}, name ExpID) struct{}                         { return struct{}{} }
func (noExprs) Recover(e grammar.Expr, m, skip struct{}, build bool) struct{}      { return struct{}{} }
func (noExprs) RuleBody(r *Rule, body struct{})                                    {}
func (noExprs) OperatorsRule(r *Rule, skip struct{}, hasSkip bool, operands, operators []OpLine[struct{}]) {
}
