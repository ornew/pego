package engine

import "github.com/ornew/pego/grammar"

// Projected repetitions.
//
// first:x rest:(-"," r:x)* -> concat(list($first), map($rest, (e) => $e.r)) is how lists are
// written: the repetition builds a record (a Seq node with the field r) per element, only for the
// action to take r out of each. When the action reads the capture only that way, every backend
// compiles the repetition to gather the values of r directly, and the map call to take the list
// as it is (both produce a list of the same values; the list the repetition makes is visible to the
// action alone).

// ruleProjections returns the captures of repetitions that the rule r's action reads only as
// map($x, (e) => $e.f), mapped to the field f. The conditions:
//   - the action uses no $n, which could reach the same list (it is an element of the body);
//   - no predicate reads $x, and the names x and f are captured only once in the rule;
//   - f is captured in every element (an item of the element's sequence), with a value that is
//     never nil (nonNil): otherwise an element can have no record, and $e.f would be an error.
func ruleProjections(prog *Program, r *rule) map[string]string {
	if prog.noProjections || r.action == nil || usesItems(r.action) {
		return nil
	}
	if _, pratt := r.def.Expr.(*grammar.Pratt); pratt {
		return nil
	}
	cands := map[string]*grammar.Repeat{}
	var walk func(e grammar.Expr)
	walk = func(e grammar.Expr) {
		switch e := e.(type) {
		case *grammar.Capture:
			if rp, ok := e.Expr.(*grammar.Repeat); ok && hasCaptures(rp.Expr) {
				cands[e.Name] = rp
				return
			}
			walk(e.Expr)
		case *grammar.Seq:
			for _, it := range e.Items {
				walk(it)
			}
		case *grammar.Choice:
			for _, a := range e.Alts {
				walk(a)
			}
		case *grammar.Optional:
			walk(e.Expr)
		case *grammar.Attributed:
			if !isStream(e) {
				walk(e.Expr)
			}
		case *grammar.Repeat:
			if !hasCaptures(e.Expr) {
				walk(e.Expr)
			}
		case *grammar.And:
			walk(e.Expr)
		}
	}
	walk(r.def.Expr)
	if len(cands) == 0 {
		return nil
	}
	count := map[string]int{}
	walkExpr(r.def.Expr, func(x grammar.Expr) {
		switch x := x.(type) {
		case *grammar.Capture:
			count[x.Name]++
		case *grammar.Predicate:
			walkTerm(x.Term, func(t grammar.Term) {
				if c, ok := t.(*grammar.CaptureRef); ok {
					delete(cands, c.Name)
				}
			})
		}
	})
	fields := map[string]string{}
	bad := map[string]bool{}
	var use func(t grammar.Term, shadow map[string]bool)
	use = func(t grammar.Term, shadow map[string]bool) {
		switch t := t.(type) {
		case *grammar.CaptureRef:
			if !shadow[t.Name] {
				bad[t.Name] = true
			}
		case *grammar.Call:
			if t.Func == "map" && len(t.Args) == 2 {
				if x, ok := t.Args[0].(*grammar.CaptureRef); ok && !shadow[x.Name] {
					if f, ok := projField(t.Args[1]); ok && (fields[x.Name] == "" || fields[x.Name] == f) {
						fields[x.Name] = f
						return
					}
				}
			}
			for _, a := range t.Args {
				use(a, shadow)
			}
		case *grammar.Lambda:
			inner := map[string]bool{}
			for k := range shadow {
				inner[k] = true
			}
			for _, p := range t.Params {
				inner[p] = true
			}
			use(t.Body, inner)
		case *grammar.Member:
			use(t.X, shadow)
		case *grammar.New:
			for _, f := range t.Fields {
				use(f.Value, shadow)
			}
		case *grammar.Binary:
			use(t.L, shadow)
			use(t.R, shadow)
		case *grammar.Unary:
			use(t.X, shadow)
		}
	}
	use(r.action, map[string]bool{})
	proj := map[string]string{}
	for name, rp := range cands {
		f := fields[name]
		if f == "" || bad[name] || count[name] > 1 || count[f] > 1 {
			continue
		}
		if c := directCapture(rp.Expr, f); c != nil && nonNil(prog, c.Expr, map[string]bool{}) {
			proj[name] = f
		}
	}
	return proj
}

// projectedMap reports whether the call t is map($x, (e) => $e.f) for a projected capture x (proj
// as from ruleProjections; locals are names bound by enclosing lambdas).
func projectedMap(t *grammar.Call, proj map[string]string, local func(string) bool) (*grammar.CaptureRef, bool) {
	if proj == nil || t.Func != "map" || len(t.Args) != 2 {
		return nil, false
	}
	x, ok := t.Args[0].(*grammar.CaptureRef)
	if !ok || proj[x.Name] == "" || local(x.Name) {
		return nil, false
	}
	if f, ok := projField(t.Args[1]); !ok || f != proj[x.Name] {
		return nil, false
	}
	return x, true
}

// markProjectedMaps adds to set the map calls in the action t that read a projected capture.
func markProjectedMaps(t grammar.Term, proj map[string]string, set map[*grammar.Call]bool) {
	var walk func(t grammar.Term, shadow map[string]bool)
	walk = func(t grammar.Term, shadow map[string]bool) {
		switch t := t.(type) {
		case *grammar.Call:
			if _, ok := projectedMap(t, proj, func(name string) bool { return shadow[name] }); ok {
				set[t] = true
				return
			}
			for _, a := range t.Args {
				walk(a, shadow)
			}
		case *grammar.Lambda:
			inner := map[string]bool{}
			for k := range shadow {
				inner[k] = true
			}
			for _, p := range t.Params {
				inner[p] = true
			}
			walk(t.Body, inner)
		case *grammar.Member:
			walk(t.X, shadow)
		case *grammar.New:
			for _, f := range t.Fields {
				walk(f.Value, shadow)
			}
		case *grammar.Binary:
			walk(t.L, shadow)
			walk(t.R, shadow)
		case *grammar.Unary:
			walk(t.X, shadow)
		}
	}
	walk(t, map[string]bool{})
}

// projField reports whether the lambda is (e) => $e.f and returns f.
func projField(t grammar.Term) (string, bool) {
	l, ok := t.(*grammar.Lambda)
	if !ok || len(l.Params) != 1 {
		return "", false
	}
	m, ok := l.Body.(*grammar.Member)
	if !ok {
		return "", false
	}
	x, ok := m.X.(*grammar.CaptureRef)
	if !ok || x.Name != l.Params[0] || m.Name == "startPos" || m.Name == "endPos" || m.Name == "children" {
		return "", false
	}
	return m.Name, true
}

// directCapture returns the capture of name that e makes unconditionally: e itself, or an item of
// the sequence e (also behind #error, which changes no value).
func directCapture(e grammar.Expr, name string) *grammar.Capture {
	if a, ok := e.(*grammar.Attributed); ok {
		for _, at := range a.Attrs {
			if at.Name != "error" {
				return nil
			}
		}
		return directCapture(a.Expr, name)
	}
	switch e := e.(type) {
	case *grammar.Capture:
		if e.Name == name {
			return e
		}
	case *grammar.Seq:
		for _, it := range e.Items {
			if c, ok := it.(*grammar.Capture); ok && c.Name == name {
				return c
			}
		}
	}
	return nil
}

// nonNil reports whether the value of e, built as a capture builds it, is never nil. A rule's
// value is never nil when it has a terminal type, when its action makes a struct or returns a
// capture whose value is never nil, or when it has no action and its body's value is never nil
// (without the type checker's results, which the engine computes after compiling).
func nonNil(prog *Program, e grammar.Expr, seen map[string]bool) bool {
	switch e := e.(type) {
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Atomic, *grammar.Top, *grammar.Seq, *grammar.Repeat:
		return true
	case *grammar.Capture:
		return nonNil(prog, e.Expr, seen)
	case *grammar.Choice:
		for _, a := range e.Alts {
			if !nonNil(prog, a, seen) {
				return false
			}
		}
		return true
	case *grammar.Attributed:
		return nonNil(prog, e.Expr, seen) // #recover makes an Error node
	case *grammar.Ref:
		r := prog.byName[e.Name]
		if r == nil || r.def == nil {
			return false
		}
		if seen[e.Name] {
			// A value is built by a finite derivation: a rule that reaches itself yields nil only
			// if one of its other cases does, which the check of those cases finds.
			return true
		}
		seen[e.Name] = true
		defer delete(seen, e.Name)
		if r.terminalType != "" {
			return true
		}
		if pr, ok := r.def.Expr.(*grammar.Pratt); ok {
			// Every line's value is never nil ($lhs and $rhs are values of the rule itself).
			for _, o := range pr.Operands {
				if !lineNonNil(prog, o.Expr, o.Action, false, seen) {
					return false
				}
			}
			for _, l := range pr.Levels {
				for _, op := range l.Operators {
					if op.Action != nil && !lineNonNil(prog, op.Expr, op.Action, true, seen) {
						return false // (an operator without an action makes an Operator node)
					}
				}
			}
			return true
		}
		return lineNonNil(prog, r.def.Expr, r.action, false, seen)
	}
	return false
}

// lineNonNil reports whether a rule body or Pratt line e with the action t never has a nil value.
func lineNonNil(prog *Program, e grammar.Expr, t grammar.Term, operator bool, seen map[string]bool) bool {
	switch t := t.(type) {
	case nil:
		return nonNil(prog, e, seen)
	case *grammar.New:
		return true
	case *grammar.CaptureRef:
		if operator && (t.Name == "lhs" || t.Name == "rhs" || t.Name == "op") {
			return true
		}
		c := directCapture(e, t.Name)
		return c != nil && nonNil(prog, c.Expr, seen)
	}
	return false
}

// usesItems reports whether the action refers to the body's elements ($n).
func usesItems(t grammar.Term) bool {
	found := false
	walkTerm(t, func(x grammar.Term) {
		if _, ok := x.(*grammar.IndexRef); ok {
			found = true
		}
	})
	return found
}
