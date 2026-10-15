package lint

import (
	"context"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// RunContext is the internal editor entry point. Cancellation discards all
// findings, including those accumulated before the stopping point. Run retains
// its ordinary behavior without an active control. Polling covers lint-owned
// analysis, check and suppression loops, recursive analyses and result sorting.
// Shared success proofs and selected metadata walks also poll; character-range
// work, grammar formatting and other individual operations retain unpolled spans. No wall-clock bound is promised.
func RunContext(ctx context.Context, g *grammar.Grammar, start string, opts Options) (findings []Finding, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return Run(g, start, opts), nil
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(lintCancellation); ok {
				findings, err = nil, cancelled.err
			} else {
				panic(failure)
			}
		}
	}()
	return runControlled(g, start, opts, &lintControl{ctx: ctx}), nil
}

// Recursive proof routines unwind only to RunContext. A control belongs to one
// lint invocation; no global state, background workers or abandoned jobs exist.
type lintCancellation struct{ err error }

type lintControl struct {
	ctx   context.Context
	steps uint32
}

func (c *lintControl) check() {
	if c != nil {
		if err := c.ctx.Err(); err != nil {
			panic(lintCancellation{err})
		}
	}
}

func (c *lintControl) poll() {
	if c == nil {
		return
	}
	c.steps++
	if c.steps%256 == 0 {
		c.check()
	}
}

func (c *lintControl) rules(g *grammar.Grammar) []*grammar.RuleDef {
	if c == nil {
		return g.Rules()
	}
	result, err := g.RulesContext(c.ctx)
	if err != nil {
		panic(lintCancellation{err})
	}
	return result
}

func (c *lintControl) types(g *grammar.Grammar) []*grammar.TypeDef {
	if c == nil {
		return g.Types()
	}
	result, err := g.TypesContext(c.ctx)
	if err != nil {
		panic(lintCancellation{err})
	}
	return result
}

func (c *lintControl) comments(g *grammar.Grammar) []*grammar.Comment {
	if c == nil {
		return g.AllComments()
	}
	result, err := g.AllCommentsContext(c.ctx)
	if err != nil {
		panic(lintCancellation{err})
	}
	return result
}

func (c *lintControl) expressionArgs(a *grammar.Attribute) []grammar.Expr {
	if c == nil {
		return a.ExpressionArgs()
	}
	return grammaranalysis.ExpressionArgsWithPoll(a, c.poll)
}
