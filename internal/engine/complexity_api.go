package engine

import (
	"context"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
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

// complexitySinkKey is the context key of the function that WithComplexitySink installs.
type complexitySinkKey struct{}

// WithComplexitySink returns a context under which ClassifyComplexity passes each classification
// it completes to sink. A caller that runs the lint checks with the classification requested
// gets in this way the classification behind the super-linear findings, without classifying the
// grammar again.
func WithComplexitySink(ctx context.Context, sink func(*Complexity)) context.Context {
	return context.WithValue(ctx, complexitySinkKey{}, sink)
}

// ClassifyComplexity classifies the cost of g from the start rule start ("" selects the default
// start rule). g must compile and start must be one of its start rules. It returns ctx's error,
// and no classification, if ctx ends. It passes the classification to the sink of ctx, if any
// (see WithComplexitySink).
func ClassifyComplexity(ctx context.Context, g *grammar.Grammar, start string) (*Complexity, error) {
	c, err := classifyComplexity(ctx, g, start)
	if sink, ok := ctx.Value(complexitySinkKey{}).(func(*Complexity)); ok && err == nil {
		sink(c)
	}
	return c, err
}

func classifyComplexity(ctx context.Context, g *grammar.Grammar, start string) (c *Complexity, err error) {
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
