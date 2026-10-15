package grammar

import "context"

// RulesContext is Rules with cooperative cancellation. It returns no partial
// slice on cancellation. The caller must not mutate g during collection.
func (g *Grammar) RulesContext(ctx context.Context) (result []*RuleDef, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return g.rules(nil), nil
	}
	defer recoverMetadata(&result, &err)
	control := metadataControl{ctx: ctx}
	result = g.rules(&control)
	control.check()
	return result, nil
}

// TypesContext is Types with the same cancellation contract as RulesContext.
func (g *Grammar) TypesContext(ctx context.Context) (result []*TypeDef, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return g.types(nil), nil
	}
	defer recoverMetadata(&result, &err)
	control := metadataControl{ctx: ctx}
	result = g.types(&control)
	control.check()
	return result, nil
}

// AllCommentsContext is AllComments with cooperative cancellation through the
// grammar, comment copies and attribute arguments. It returns no partial slice
// on cancellation. Individual allocations and string comparisons may not stop
// immediately. The caller must not mutate g during collection.
func (g *Grammar) AllCommentsContext(ctx context.Context) (result []*Comment, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return g.allComments(nil), nil
	}
	defer recoverMetadata(&result, &err)
	control := metadataControl{ctx: ctx}
	result = g.allComments(&control)
	control.check()
	return result, nil
}

// ExpressionArgsContext is ExpressionArgs with cooperative cancellation during
// argument collection and keyword sorting. It returns no partial slice on
// cancellation. The caller must not mutate a during collection.
func (a *Attribute) ExpressionArgsContext(ctx context.Context) (result []Expr, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return a.expressionArgs(nil), nil
	}
	defer recoverMetadata(&result, &err)
	control := metadataControl{ctx: ctx}
	result = a.expressionArgs(&control)
	control.check()
	return result, nil
}

// One control is shared by nested collections, so many small argument lists
// cannot hide an unbounded comment traversal between cancellation checks.
type metadataControl struct {
	ctx   context.Context
	steps uint32
}

type metadataCancellation struct{ err error }

func (c *metadataControl) poll() {
	if c != nil {
		c.steps++
		if c.steps%256 == 0 {
			c.check()
		}
	}
}

func (c *metadataControl) check() {
	if err := c.ctx.Err(); err != nil {
		panic(metadataCancellation{err})
	}
}

// Concrete collector calls keep the control on the stack. This deferred helper
// translates only this package's cancellation unwind and discards partial data.
func recoverMetadata[T any](result *T, err *error) {
	if failure := recover(); failure != nil {
		if stopped, ok := failure.(metadataCancellation); ok {
			var zero T
			*result, *err = zero, stopped.err
		} else {
			panic(failure)
		}
	}
}

func appendMetadata[T any](dst, src []T, control *metadataControl) []T {
	if control == nil {
		return append(dst, src...)
	}
	for _, item := range src {
		control.poll()
		dst = append(dst, item)
	}
	return dst
}
