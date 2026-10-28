package engine

import (
	"context"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// The private unwind avoids threading errors through recursive matcher/type
// construction. Only CompileContext catches it; unrelated panics retain their
// existing behavior. The control is local to this compilation.
type compileCancellation struct{ err error }

// compilePollStride is the number of compiler steps between context checks. A step is a visit
// of one expression, type or rule element; the heaviest, type-value inference, takes a few
// microseconds, so the stride bounds the work done after a cancellation to well under a
// millisecond (see design 042, "Compiler poll stride").
const compilePollStride = 16

type compileControl struct {
	ctx   context.Context
	steps uint32
}

func (c *compileControl) check() {
	if c != nil {
		if err := c.ctx.Err(); err != nil {
			panic(compileCancellation{err})
		}
	}
}

func (c *compileControl) poll() {
	if c == nil {
		return
	}
	c.steps++
	if c.steps%compilePollStride == 0 {
		c.check()
	}
}

func (c *compileControl) rules(g *grammar.Grammar) []*grammar.RuleDef {
	if c == nil {
		return g.Rules()
	}
	result, err := g.RulesContext(c.ctx)
	if err != nil {
		panic(compileCancellation{err})
	}
	return result
}

func (c *compileControl) types(g *grammar.Grammar) []grammar.TypeDecl {
	if c == nil {
		return g.Types()
	}
	result, err := g.TypesContext(c.ctx)
	if err != nil {
		panic(compileCancellation{err})
	}
	return result
}

func (c *compileControl) expressionArgs(a *grammar.Attribute) []grammar.Expr {
	if c == nil {
		return a.ExpressionArgs()
	}
	return grammaranalysis.ExpressionArgsWithPoll(a, c.poll)
}
