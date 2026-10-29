package engine

// workActionFunc is a test action function shared by the default and the
// pego_work_controls_reference builds.
type workActionFunc struct {
	params int
	fn     func(any, any) (any, error)
}

func (f workActionFunc) Arity() int { return f.params }
func (f workActionFunc) Apply(x, y any) (any, error) {
	return f.fn(x, y)
}
