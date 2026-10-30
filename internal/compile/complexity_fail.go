package compile

import "github.com/ornew/pego/grammar"

// A unit restarts the input that a scanning source inside it examined only when it fails after
// the source has run. A unit whose sources are followed by nothing that can fail, such as the
// iteration of ([^"]+ / "\"\"")*, fails only before its sources examine any input; what the parser
// evaluates next then examines no more than the unit examined outside its sources, which the other
// conditions bound.

// containsSource reports whether e contains a scanning source, in itself or in a rule it calls.
func (k *classifier) containsSource(e grammar.Expr) bool {
	if v, ok := k.srcMemo[e]; ok {
		return v
	}
	found := false
	WalkExpr(e, func(x grammar.Expr) {
		if found {
			return
		}
		if k.srcExprs[x] {
			found = true
			return
		}
		if r, ok := x.(*grammar.Ref); ok && k.ruleSrc[r.Name] != nil {
			found = true
		}
	})
	k.srcMemo[e] = found
	return found
}

// computeFailRules computes, for each reachable rule, whether its body can fail after a scanning
// source in it examined input. The least fixpoint is the answer: a rule fails after a source only
// through an evaluation that ends.
func (k *classifier) computeFailRules() {
	k.failRule = map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			k.control.poll()
			if !k.reach[r.Name] || k.failRule[r.Name] {
				continue
			}
			if k.failsAfterSource(r.Expr) {
				k.failRule[r.Name] = true
				changed = true
			}
		}
	}
}

// failsAfterSource reports whether e can fail after a scanning source in it examined input.
func (k *classifier) failsAfterSource(e grammar.Expr) bool {
	switch x := e.(type) {
	case nil:
		return false
	case *grammar.Seq:
		for i, it := range x.Items {
			if k.failsAfterSource(it) {
				return true
			}
			if k.containsSource(it) {
				for _, later := range x.Items[i+1:] {
					if !k.an.AlwaysSucceeds(later) {
						return true
					}
				}
			}
		}
		return false
	case *grammar.Choice:
		for _, alt := range x.Alts {
			if k.failsAfterSource(alt) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		if x.Min == 0 || x.Max == 0 {
			return false
		}
		return k.failsAfterSource(x.Expr) || x.Min > 1 && k.containsSource(x.Expr) && !k.an.AlwaysSucceeds(x.Expr)
	case *grammar.Optional:
		return false
	case *grammar.And:
		return k.failsAfterSource(x.Expr)
	case *grammar.Not:
		return k.containsSource(x.Expr)
	case *grammar.Group:
		return k.failsAfterSource(x.Expr)
	case *grammar.Capture:
		return k.failsAfterSource(x.Expr)
	case *grammar.Discard:
		return k.failsAfterSource(x.Expr)
	case *grammar.Atomic:
		return k.failsAfterSource(x.Expr)
	case *grammar.Stream:
		return k.failsAfterSource(x.Expr)
	case *grammar.InlineAction:
		return k.failsAfterSource(x.Expr)
	case *grammar.Ref:
		if k.srcExprs[x] {
			return true
		}
		return k.failRule[x.Name]
	case *grammar.Predicate:
		return k.srcExprs[x]
	case *grammar.Attributed:
		if recoverSkip(x) != nil || hasPragma(x, "missing") {
			return k.containsSource(x)
		}
		return k.failsAfterSource(x.Expr)
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Cut, *grammar.Top, *grammar.Bottom,
		*grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine, *grammar.EndLine:
		return false
	}
	return k.containsSource(e)
}

// unitFailsAfterSource reports whether the unit can fail after a scanning source inside it
// examined input. Lookaheads are abandoned whether they match or not, and the units of an Operators
// Rule and growing units are not refined.
func (k *classifier) unitFailsAfterSource(u *unit) bool {
	switch u.kind {
	case unitAlt:
		if u.growing {
			return true
		}
		return k.failsAfterSource(u.expr)
	case unitIteration:
		return k.failsAfterSource(u.expr.(*grammar.Repeat).Expr)
	case unitOption:
		return k.failsAfterSource(u.expr.(*grammar.Optional).Expr)
	}
	return true
}
