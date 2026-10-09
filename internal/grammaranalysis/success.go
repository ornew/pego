// Package grammaranalysis provides conservative matching facts shared by the
// compiler and linter. Unknown facts never justify excluding a rule call.
package grammaranalysis

import "github.com/ornew/pego/grammar"

// SucceedingRules finds ordinary, non-cyclic rules that cannot return a matching
// failure. A recursive call can read a failing growth seed even when its rule's
// body always succeeds, so cyclic rules are excluded. Pratt calls are unknown.
func SucceedingRules(rules []*grammar.RuleDef, cyclic map[string]bool) map[string]bool {
	sure := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, r := range rules {
			if !cyclic[r.Name] && !sure[r.Name] && AlwaysSucceeds(r.Expr, sure) {
				sure[r.Name], changed = true, true
			}
		}
	}
	return sure
}

// AlwaysSucceeds reports a sufficient proof that e cannot return a matching
// failure, at any input position. Rule-action errors and execution limits abort
// the parse instead of trying another alternative; they are not matching failure.
func AlwaysSucceeds(e grammar.Expr, sure map[string]bool) bool {
	switch e := e.(type) {
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.Top, *grammar.Cut:
		return true
	case *grammar.Ref:
		return e.Level == "" && sure[e.Name]
	case *grammar.Seq:
		for _, it := range e.Items {
			if !AlwaysSucceeds(it, sure) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if AlwaysSucceeds(alt, sure) {
				return true
			}
			if HasLooseCut(alt) {
				return false // It may fail after committing this choice.
			}
		}
	case *grammar.Optional:
		return !HasLooseCut(e.Expr) || AlwaysSucceeds(e.Expr, sure)
	case *grammar.Repeat:
		if e.Max == 0 {
			return e.Min == 0
		}
		if e.Min == 0 {
			return !HasLooseCut(e.Expr) || AlwaysSucceeds(e.Expr, sure)
		}
		// A stream ends after its first empty iteration, so an empty body
		// cannot prove larger minima even if a whole-input parse repeats it.
		return e.Min == 1 && AlwaysSucceeds(e.Expr, sure)
	case *grammar.And:
		return AlwaysSucceeds(e.Expr, sure)
	case *grammar.Not:
		return alwaysFails(e.Expr, sure)
	case *grammar.Capture:
		return AlwaysSucceeds(e.Expr, sure)
	case *grammar.Atomic:
		return AlwaysSucceeds(e.Expr, sure)
	case *grammar.Discard:
		return AlwaysSucceeds(e.Expr, sure)
	case *grammar.Attributed:
		return AlwaysSucceeds(e.Expr, sure)
	}
	return false
}

func alwaysFails(e grammar.Expr, sure map[string]bool) bool {
	switch e := e.(type) {
	case *grammar.Bottom:
		return true
	case *grammar.Not:
		return AlwaysSucceeds(e.Expr, sure)
	case *grammar.And:
		return alwaysFails(e.Expr, sure)
	case *grammar.Seq:
		for _, it := range e.Items {
			if alwaysFails(it, sure) {
				return true
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if !alwaysFails(alt, sure) {
				return false
			}
		}
		return true
	case *grammar.Capture:
		return alwaysFails(e.Expr, sure)
	case *grammar.Atomic:
		return alwaysFails(e.Expr, sure)
	case *grammar.Discard:
		return alwaysFails(e.Expr, sure)
	case *grammar.Attributed:
		for _, at := range e.Attrs {
			if at.Name == "recover" {
				return false
			}
		}
		return alwaysFails(e.Expr, sure)
	}
	return false
}

// HasLooseCut reports a cut that can commit the enclosing choice, optional or
// repetition. Nested choices, optional expressions, repetitions, lookaheads and
// rule calls delimit cuts.
func HasLooseCut(e grammar.Expr) bool {
	switch e := e.(type) {
	case *grammar.Cut:
		return true
	case *grammar.Seq:
		for _, it := range e.Items {
			if HasLooseCut(it) {
				return true
			}
		}
	case *grammar.Capture:
		return HasLooseCut(e.Expr)
	case *grammar.Atomic:
		return HasLooseCut(e.Expr)
	case *grammar.Discard:
		return HasLooseCut(e.Expr)
	case *grammar.Attributed:
		if HasLooseCut(e.Expr) {
			return true
		}
		for _, at := range e.Attrs {
			if at.Name == "recover" {
				if skip, ok := at.Arg("skip"); ok && HasLooseCut(skip) {
					return true // Recovery executes skip in the same cut scope.
				}
			}
		}
	}
	return false
}
