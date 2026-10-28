package engine

import "github.com/ornew/pego/grammar"

// resumable reports whether a repetition with the element e can be resumed. An element whose
// captures are written to the enclosing scope (those inside &, when the element has no captures of
// its own) cannot be: reusing it would not write them.
func (c *compiler) resumable(e grammar.Expr) bool {
	control := c.control
	ok := elementScopedControlled(e, control) || !anyCapture(e, control)
	walkExprControlled(e, func(x grammar.Expr) {
		switch x := x.(type) {
		case *grammar.Predicate:
			ok = false
		case *grammar.Ref:
			if r := c.prog.byName[x.Name]; r == nil || len(r.vars) > 0 {
				ok = false
			}
		}
	}, control)
	return ok
}

// anyCapture reports whether e contains a capture anywhere, lookaheads included.
func anyCapture(e grammar.Expr, control *compileControl) bool {
	control.poll()
	found := false
	walkExprControlled(e, func(x grammar.Expr) {
		if _, ok := x.(*grammar.Capture); ok {
			found = true
		}
	}, control)
	return found
}

// positional reports whether the value of e may contain position values.
func (c *compiler) positional(e grammar.Expr) bool {
	control := c.control
	found := false
	walkExprControlled(e, func(x grammar.Expr) {
		if x, ok := x.(*grammar.Ref); ok {
			if r := c.prog.byName[x.Name]; r == nil || r.positional {
				found = true
			}
		}
	}, control)
	return found
}
