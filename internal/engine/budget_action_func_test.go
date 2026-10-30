package engine

// budgetActionFunc is a test action function shared by the default and the
// pego_budget_controls_reference builds.
type budgetActionFunc struct {
	params int
	fn     func(any, any) (any, error)
}

func (f budgetActionFunc) Arity() int { return f.params }
func (f budgetActionFunc) Apply(x, y any) (any, error) {
	return f.fn(x, y)
}
