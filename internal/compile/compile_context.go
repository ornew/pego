package compile

import (
	"context"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// Cancellation is the panic value that unwinds a cancelled compilation with its error. The
// unwind avoids threading errors through recursive matcher/type construction. Only the engine's
// CompileContext catches it; unrelated panics retain their existing behavior. The control is
// local to this compilation.
type Cancellation struct{ Err error }

// PollStride is the number of compiler steps between context checks. A step is a visit
// of one expression, type or rule element; the heaviest, type-value inference, takes a few
// microseconds, so the stride bounds the work done after a cancellation to well under a
// millisecond (see design 042, "Compiler poll stride").
const PollStride = 16

type Control struct {
	Ctx   context.Context
	Steps uint32
}

func (c *Control) check() {
	if c != nil {
		if err := c.Ctx.Err(); err != nil {
			panic(Cancellation{err})
		}
	}
}

func (c *Control) poll() {
	if c == nil {
		return
	}
	c.Steps++
	if c.Steps%PollStride == 0 {
		c.check()
	}
}

func (c *Control) Rules(g *grammar.Grammar) []*grammar.RuleDef {
	if c == nil {
		return g.Rules()
	}
	result, err := g.RulesContext(c.Ctx)
	if err != nil {
		panic(Cancellation{err})
	}
	return result
}

func (c *Control) Types(g *grammar.Grammar) []grammar.TypeDecl {
	if c == nil {
		return g.Types()
	}
	result, err := g.TypesContext(c.Ctx)
	if err != nil {
		panic(Cancellation{err})
	}
	return result
}

func (c *Control) ExpressionArgs(a *grammar.Attribute) []grammar.Expr {
	if c == nil {
		return a.ExpressionArgs()
	}
	return grammaranalysis.ExpressionArgsWithPoll(a, c.poll)
}
