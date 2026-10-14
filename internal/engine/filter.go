package engine

// pushFiltered keeps original node values in input order. The predicate must
// return an actual bool; truthiness and static type narrowing are not implied.
func (c *evalCtx) pushFiltered(args []any) error {
	f, ok := args[1].(function)
	if !ok || f.arity() != 1 {
		return c.p.actionError("filter: second argument must be a function of one parameter")
	}
	items, err := c.p.valueListItems("filter", args[0])
	if err != nil {
		return err
	}
	for _, item := range items {
		c.p.stepWork()
		value, err := f.apply(nodeOrNil(item), nil)
		if err != nil {
			return err
		}
		keep, ok := value.(bool)
		if !ok {
			return c.p.actionError("filter: predicate must produce a bool, got ", typeName(value))
		}
		if keep {
			c.p.kidStack = append(c.p.kidStack, item)
		}
	}
	return nil
}
