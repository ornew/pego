package engine

import (
	"context"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// Complexity is the classification of the cost of a grammar from one start rule (see
// compile.ClassifyComplexity).
type Complexity = compile.Complexity

// ComplexityCause is a witness of a polynomial or exponential class, or a reason for the class
// unknown.
type ComplexityCause = compile.ComplexityCause

// GrowingBinding is a binding that the growing-variable lint check reports.
type GrowingBinding = compile.GrowingBinding

// ComplexityClass is a class of the cost of a parse.
type ComplexityClass = compile.ComplexityClass

// The classes, from the strongest claim to the weakest.
const (
	ClassLinear    = compile.ClassLinear
	ClassLinearEnv = compile.ClassLinearEnv
	ClassUnknown   = compile.ClassUnknown
	ClassPoly      = compile.ClassPoly
	ClassExp       = compile.ClassExp
)

// ClassifyComplexity classifies the cost of g from the start rule start ("" selects the default
// start rule). g must compile and start must be one of its start rules. It returns ctx's error,
// and no classification, if ctx ends.
func ClassifyComplexity(ctx context.Context, g *grammar.Grammar, start string) (c *Complexity, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return compile.ClassifyComplexity(g, start, nil)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(compile.Cancellation); ok {
				c, err = nil, cancelled.Err
			} else {
				panic(failure)
			}
		}
	}()
	return compile.ClassifyComplexity(g, start, &compile.Control{Ctx: ctx})
}

// GrowingBindings returns the Growing member of ClassifyComplexity(ctx, g, start) at a lower
// cost.
func GrowingBindings(ctx context.Context, g *grammar.Grammar, start string) (b []GrowingBinding, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return compile.GrowingBindings(g, start, nil)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(compile.Cancellation); ok {
				b, err = nil, cancelled.Err
			} else {
				panic(failure)
			}
		}
	}()
	return compile.GrowingBindings(g, start, &compile.Control{Ctx: ctx})
}
