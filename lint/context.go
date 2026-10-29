package lint

import (
	"context"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// runContext is the cancellable form of runUnchecked, behind Config.RunUnchecked.
// Cancellation discards all findings, including those accumulated before the
// stopping point. Without an active control it is runUnchecked. Polling covers lint-owned
// analysis, check and suppression loops, recursive analyses and result sorting.
// Shared success proofs, metadata walks, character-range work and diagnostic
// formatting also poll. Other individual operations retain unpolled spans; no
// wall-clock bound is promised.
func runContext(ctx context.Context, g *grammar.Grammar, start string, opts options) (findings []Finding, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return runUnchecked(g, start, opts), nil
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
	return run(g, start, opts, &lintControl{ctx: ctx}), nil
}

// Recursive proof routines unwind only to runContext and Rewrites. A control belongs to one
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

func (c *lintControl) types(g *grammar.Grammar) []grammar.TypeDecl {
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

func (c *lintControl) formatExpr(e grammar.Expr) string {
	if c == nil {
		return grammar.FormatExpr(e)
	}
	result, err := grammar.FormatExprContext(c.ctx, e)
	if err != nil {
		panic(lintCancellation{err})
	}
	return result
}

func (c *lintControl) formatTerm(t grammar.Term) string {
	if c == nil {
		return grammar.FormatTerm(grammaranalysis.UngroupTerm(t))
	}
	t = grammaranalysis.UngroupTermWithPoll(t, c.poll)
	result, err := grammar.FormatTermContext(c.ctx, t)
	if err != nil {
		panic(lintCancellation{err})
	}
	return result
}
