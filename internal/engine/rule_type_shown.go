package engine

import "github.com/ornew/pego/grammar"

// shownRuleTypes returns the type of each rule as Program.RuleType shows it: the declared type of
// a rule with one, and otherwise the rule's value type, in which a reference to a rule with a
// declared type is that type. A reference to a rule without one keeps the rule's name, which
// keeps recursive and large values short.
func shownRuleTypes(prog *Program, rules []*grammar.RuleDef, values map[string]ty, control *compileControl) map[string]ty {
	k := &typeResolver{prog: prog}
	declared := map[string]ty{}
	for _, rd := range rules {
		control.poll()
		if rd.Type != nil {
			declared[rd.Name] = fromLegacy(k.resolve(rd.Type))
		}
	}
	var show func(t ty) ty
	show = func(t ty) ty {
		control.poll()
		switch t := t.(type) {
		case ruleTy:
			if d, ok := declared[t.name]; ok {
				return d
			}
		case listTy:
			return listTy{show(t.elem)}
		case optTy:
			// A declared type may be optional itself; an optional is not nested.
			return vopt(show(t.elem))
		case unionTy:
			alts := make([]ty, len(t.alts))
			for i, a := range t.alts {
				alts[i] = show(a)
			}
			return vunion(alts...)
		case tupleTy:
			elems := make([]ty, len(t.elems))
			for i, e := range t.elems {
				elems[i] = show(e)
			}
			return tupleTy{elems}
		case nodeTy:
			return nodeTy{value: show(t.value), caps: t.caps}
		}
		return t
	}
	shown := make(map[string]ty, len(rules))
	for _, rd := range rules {
		if d, ok := declared[rd.Name]; ok {
			shown[rd.Name] = d
		} else if t, ok := values[rd.Name]; ok {
			shown[rd.Name] = show(t)
		}
	}
	return shown
}
