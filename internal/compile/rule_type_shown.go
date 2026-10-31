package compile

import "github.com/ornew/zeroparse/grammar"

// shownRuleTypes returns the type of each rule as Program.RuleType shows it: the declared type of
// a rule with one, and otherwise the rule's value type, in which a reference to a rule with a
// declared type is that type. A reference to a rule without one keeps the rule's name, which
// keeps recursive and large values short.
func shownRuleTypes(prog *Program, rules []*grammar.RuleDef, values map[string]Ty, control *Control) map[string]Ty {
	k := &TypeResolver{Prog: prog}
	declared := map[string]Ty{}
	for _, rd := range rules {
		control.poll()
		if rd.Type != nil {
			declared[rd.Name] = k.Resolve(rd.Type)
		}
	}
	var show func(t Ty) Ty
	show = func(t Ty) Ty {
		control.poll()
		switch t := t.(type) {
		case RuleTy:
			if d, ok := declared[t.Name]; ok {
				return d
			}
		case ListTy:
			return ListTy{show(t.Elem)}
		case OptTy:
			// A declared type may be optional itself; an optional is not nested.
			return vopt(show(t.Elem))
		case JoinTy:
			alts := make([]Ty, len(t.Alts))
			for i, a := range t.Alts {
				alts[i] = show(a)
			}
			return Vjoin(alts...)
		case TupleTy:
			elems := make([]Ty, len(t.Elems))
			for i, e := range t.Elems {
				elems[i] = show(e)
			}
			return TupleTy{elems}
		case NodeTy:
			if t.Name != "" {
				return t
			}
			return NodeTy{Value: show(t.Value), Caps: t.Caps}
		}
		return t
	}
	shown := make(map[string]Ty, len(rules))
	for _, rd := range rules {
		if d, ok := declared[rd.Name]; ok {
			shown[rd.Name] = d
		} else if t, ok := values[rd.Name]; ok {
			shown[rd.Name] = show(t)
		}
	}
	return shown
}
