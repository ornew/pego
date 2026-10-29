package core

import "github.com/ornew/pego/internal/value"

// pushFiltered keeps original node values in input order. The predicate must
// return an actual bool; truthiness and static type narrowing are not implied.
func (c *EvalCtx) pushFiltered(args []any) error {
	f, ok := args[1].(Function)
	if !ok || f.Arity() != 1 {
		return c.P.ActionError("filter: second argument must be a function of one parameter")
	}
	items, err := c.P.ValueListItems("filter", args[0])
	if err != nil {
		return err
	}
	for _, item := range items {
		c.P.StepWork()
		val, err := f.Apply(value.UnboxNode(item), nil)
		if err != nil {
			return err
		}
		keep, ok := val.(bool)
		if !ok {
			return c.P.ActionError("filter: predicate must produce a bool, got ", TypeName(val))
		}
		if keep {
			c.P.KidStack = append(c.P.KidStack, item)
		}
	}
	return nil
}
