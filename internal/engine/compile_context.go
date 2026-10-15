package engine

import (
	"context"

	"github.com/ornew/pego/grammar"
)

// CompileContext is the internal editor compilation entry point. Cancellation
// discards the entire program and accumulated diagnostics. Existing Compile and
// saved-program loading keep their ordinary path with no active control.
// Polls cover grammar AST validation, rule/dependency loops, expression
// construction and type checking; normalization and individual shared
// operations still have uncancellable stretches. This is not a wall-clock
// latency guarantee.
func CompileContext(ctx context.Context, g *grammar.Grammar, opts Options) (prog *Program, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return Compile(g, opts)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(compileCancellation); ok {
				prog, err = nil, cancelled.err
			} else {
				panic(failure)
			}
		}
	}()
	return buildControlled(g, opts, nil, &compileControl{ctx: ctx})
}

// The private unwind avoids threading errors through recursive matcher/type
// construction. Only CompileContext catches it; unrelated panics retain their
// existing behavior. The control is local to this compilation.
type compileCancellation struct{ err error }

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
	if c.steps%256 == 0 {
		c.check()
	}
}
