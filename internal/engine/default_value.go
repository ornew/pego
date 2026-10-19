package engine

import "github.com/ornew/pego/grammar"

// defaultValueGrammar returns g without the value actions and without the declared types of rules,
// so that every rule makes the default value of its expression (Options.DefaultValue). Predicates
// stay as written, because they decide the match, and so do the type declarations, which
// predicates can still use. A predicate can read the value of a capture, which an action may have
// made, so the rules that make the values predicates read (valueNeededRulesControlled, as in
// recognition) keep their actions and types. Unchanged declarations are shared with g; g itself
// does not change.
func defaultValueGrammar(g *grammar.Grammar, control *compileControl) *grammar.Grammar {
	needed := valueNeededRulesControlled(control.rules(g), control)
	decls, changed := normalizeItems(g.File.Decls, false, func(d grammar.Decl) grammar.Decl {
		control.poll()
		rd, ok := d.(*grammar.RuleDef)
		if !ok || needed[rd.Name] {
			return d
		}
		expr := defaultValueExpr(rd.Expr, control)
		if rd.Action == nil && rd.Type == nil && !rd.DefaultAction && expr == rd.Expr {
			return rd
		}
		rule := *rd
		rule.Expr, rule.Type, rule.Action, rule.DefaultAction = expr, nil, nil, false
		return &rule
	}, control)
	if !changed {
		return g
	}
	copy := *g
	copy.File.Decls = decls
	return &copy
}

// defaultValueExpr drops the actions of the Operators Rule at the root of a rule body, the only
// expression that has actions of its own (inline actions are not implemented).
func defaultValueExpr(e grammar.Expr, control *compileControl) grammar.Expr {
	switch e := e.(type) {
	case *grammar.Group:
		inner := defaultValueExpr(e.Expr, control)
		if inner == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = inner
		return &copy
	case *grammar.OperatorsRule:
		operands, operandsChanged := normalizeItems(e.Operands, false, func(o *grammar.OperatorsRuleOperand) *grammar.OperatorsRuleOperand {
			control.poll()
			if o.Action == nil && !o.DefaultAction {
				return o
			}
			copy := *o
			copy.Action, copy.DefaultAction = nil, false
			return &copy
		}, control)
		levels, levelsChanged := normalizeItems(e.Levels, false, func(l *grammar.OperatorsRuleLevel) *grammar.OperatorsRuleLevel {
			operators, changed := normalizeItems(l.Operators, false, func(o *grammar.OperatorsRuleOperator) *grammar.OperatorsRuleOperator {
				control.poll()
				if o.Action == nil && !o.DefaultAction {
					return o
				}
				copy := *o
				copy.Action, copy.DefaultAction = nil, false
				return &copy
			}, control)
			if !changed {
				return l
			}
			copy := *l
			copy.Operators = operators
			return &copy
		}, control)
		if !operandsChanged && !levelsChanged {
			return e
		}
		copy := *e
		copy.Operands, copy.Levels = operands, levels
		return &copy
	}
	return e
}
