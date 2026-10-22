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
	needed := valueNeededRulesControlled(control.rules(g), control, false)
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
		if hiddenRule(rd.Name) && (rd.Action != nil || rd.DefaultAction) {
			// A hidden rule without an action splices into its caller; one with an action makes
			// its value. Its default value keeps that node, as its action did.
			rule.DefaultAction = true
		}
		return &rule
	}, control)
	if !changed {
		return g
	}
	copy := *g
	copy.File.Decls = decls
	return &copy
}

// sourceDefaultValueGrammar returns the grammar as written, source, with what defaultValueGrammar
// dropped from its lowered form: dv is defaultValueGrammar(lowered), and names the inline actions
// that lowered was made with. Validating it reports a defect where the grammar as written has it,
// with the paths and positions of that grammar, not of the lowered one, whose declarations are
// numbered differently and whose generated rules the author never wrote.
func sourceDefaultValueGrammar(source, lowered, dv *grammar.Grammar, names inlineNames, control *compileControl) *grammar.Grammar {
	dvRules := map[string]*grammar.RuleDef{}
	for _, rd := range control.rules(dv) {
		dvRules[rd.Name] = rd
	}
	// An inline action keeps its action when the rule it was lowered to does.
	keep := map[*grammar.InlineAction]bool{}
	for name, site := range names {
		if rd := dvRules[name]; rd != nil && rd.Action != nil {
			keep[site.node] = true
		}
	}
	walk := &inlineLowerer{control: control, keepInline: func(e *grammar.InlineAction, inner grammar.Expr) grammar.Expr {
		if keep[e] && inner == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = inner
		if !keep[e] {
			copy.Action, copy.DefaultAction = nil, true
		}
		return &copy
	}}
	// Each declaration of the source is followed in the lowered grammar by the rules generated
	// from its inline actions.
	decls := make([]grammar.Decl, len(source.File.Decls))
	j := 0
	for i, d := range source.File.Decls {
		control.poll()
		decls[i] = d
		lowRule, _ := lowered.File.Decls[j].(*grammar.RuleDef)
		dvRule, _ := dv.File.Decls[j].(*grammar.RuleDef)
		for j++; j < len(lowered.File.Decls); j++ {
			generated, ok := lowered.File.Decls[j].(*grammar.RuleDef)
			if !ok || names[generated.Name].node == nil {
				break
			}
		}
		rd, isRule := d.(*grammar.RuleDef)
		if !isRule || lowRule == nil || dvRule == nil {
			continue
		}
		expr := walk.expr(rd.Expr)
		if dvRule != lowRule {
			expr = defaultValueExpr(expr, control)
		}
		if expr == rd.Expr && dvRule.Action == rd.Action && dvRule.Type == rd.Type && dvRule.DefaultAction == rd.DefaultAction {
			continue
		}
		rule := *rd
		rule.Expr, rule.Type, rule.Action, rule.DefaultAction = expr, dvRule.Type, dvRule.Action, dvRule.DefaultAction
		decls[i] = &rule
	}
	copy := *source
	copy.File.Decls = decls
	return &copy
}

// defaultValueExpr drops the actions of the Operators Rule at the root of a rule body, the only
// expression that has actions of its own (inline actions are lowered to rules before this runs).
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
