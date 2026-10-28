package engine

import (
	"slices"

	"github.com/ornew/pego/grammar"
)

// checkLeftRecursionBindings rejects a binding of a variable that a left-recursion cycle reads
// when the binding may run before a left call into the cycle. Such a binding would give the
// re-entered call another variable environment than the call being grown; the language defines
// left recursion only for re-entries in the same environment (see spec/language/parser-expressions.md).
//
// The bindings before a call are found with the traversal of leftCalls: the calls that may run
// before any input is consumed, and with each the bindings that may run before it in the same
// rule body. Bindings in a called rule end with the call, so they never reach a caller's calls.
func (c *compiler) checkLeftRecursionBindings(rules []*grammar.RuleDef, vars map[string][]string) {
	a := c.an
	if len(a.cycle) == 0 {
		return
	}
	for _, r := range rules {
		a.control.poll()
		cycle, ok := a.cycle[r.Name]
		if !ok || len(vars[r.Name]) == 0 {
			continue
		}
		read := vars[r.Name]
		reported := map[*grammar.Bind]bool{}
		a.leftBindings(r.Expr, nil, func(callee string, bound []*grammar.Bind) {
			if k, ok := a.cycle[callee]; !ok || k != cycle {
				return
			}
			for _, b := range bound {
				if !reported[b] && slices.Contains(read, b.Name) {
					reported[b] = true
					c.errorf(b.Diagnostic(), "rule %s binds variable %s before its left-recursive call of %s; the left recursion reads %s", r.Name, b.Name, callee, b.Name)
				}
			}
		})
	}
}

// leftBindings visits the calls that leftCalls finds in e, each with the bindings that may run
// before it, given the bindings in that may run before e. It returns the bindings that may run
// before an expression following e. The result over-approximates: a binding in a choice
// alternative or an optional counts even when input is consumed or the binding is undone before
// the call runs.
func (a *analysis) leftBindings(e grammar.Expr, in []*grammar.Bind, visit func(string, []*grammar.Bind)) []*grammar.Bind {
	a.control.poll()
	switch e := e.(type) {
	case *grammar.Ref:
		visit(e.Name, in)
	case *grammar.Predicate:
		walkTerm(e.Term, func(t grammar.Term) {
			if b, ok := t.(*grammar.Bind); ok {
				in = append(slices.Clip(in), b)
			}
		})
	case *grammar.Seq:
		for _, it := range e.Items {
			in = a.leftBindings(it, in, visit)
			if !a.isNullable(it) {
				break
			}
		}
	case *grammar.Choice:
		out := in
		for _, alt := range e.Alts {
			out = unionBinds(out, a.leftBindings(alt, in, visit))
			if a.alwaysSucceeds(alt) {
				break
			}
		}
		in = out
	case *grammar.Repeat:
		if e.Max != 0 {
			out := a.leftBindings(e.Expr, in, visit)
			if a.isNullable(e.Expr) && len(out) > len(in) {
				// A later iteration may start without consuming input after an earlier one bound.
				out = a.leftBindings(e.Expr, out, visit)
			}
			in = out
		}
	case *grammar.Optional:
		in = a.leftBindings(e.Expr, in, visit)
	case *grammar.And:
		// A positive lookahead returns to its start and keeps its bindings, so every binding
		// in its body may run before what follows, even one after consumed input.
		a.leftBindings(e.Expr, in, visit)
		walkExprControlled(e.Expr, func(x grammar.Expr) {
			if p, ok := x.(*grammar.Predicate); ok {
				in = a.leftBindings(p, in, visit)
			}
		}, a.control)
	case *grammar.Not:
		// A negative lookahead succeeds only when its body fails, which undoes its bindings.
		a.leftBindings(e.Expr, in, visit)
	case *grammar.Atomic:
		in = a.leftBindings(e.Expr, in, visit)
	case *grammar.Discard:
		in = a.leftBindings(e.Expr, in, visit)
	case *grammar.Capture:
		in = a.leftBindings(e.Expr, in, visit)
	case *grammar.Attributed:
		in = a.leftBindings(e.Expr, in, visit)
		if !a.alwaysSucceeds(e.Expr) {
			for _, at := range e.Attrs {
				if at.Name == "recover" {
					if skip, ok := at.Arg("skip"); ok {
						in = a.leftBindings(skip, in, visit)
					}
				}
			}
		}
	case *grammar.OperatorsRule:
		if e.Skip != nil {
			in = a.leftBindings(e.Skip, in, visit)
		}
		out := in
		for _, o := range e.Operands {
			out = unionBinds(out, a.leftBindings(o.Expr, in, visit))
			if a.alwaysSucceeds(o.Expr) {
				break
			}
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				if op.Kind == grammar.Prefix {
					out = unionBinds(out, a.leftBindings(op.Expr, in, visit))
				}
			}
		}
		in = out
	}
	return in
}

// unionBinds returns the bindings of a followed by those of b that a does not hold. Every list
// extends a common prefix, so the result is small and keeps the order of discovery.
func unionBinds(a, b []*grammar.Bind) []*grammar.Bind {
	for _, x := range b {
		if !slices.Contains(a, x) {
			a = append(slices.Clip(a), x)
		}
	}
	return a
}
