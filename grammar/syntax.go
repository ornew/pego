package grammar

// FormatSyntax renders the parsing syntax of g without modifying it. It omits
// type declarations, result annotations, Value Actions (including inline and
// Operators Rule actions), metadata attributes and recovery wrappers. Predicate
// Actions, captures, matching operators and other pragmas are retained.
//
// This is a documentation view, not an equivalent replacement parser: a
// predicate may depend on values constructed by an omitted action or type.
// Recovery diagnostics and returned values are intentionally not preserved.
// Comments on retained source forms follow Format's layout rules; comments
// attached only to removed forms may be omitted.
func FormatSyntax(g *Grammar) string {
	view := *g
	view.Statements = nil
	p := syntaxProjection{seen: make(map[Expr]Expr)}
	for _, statement := range g.Statements {
		if rule, ok := statement.(*RuleDef); ok {
			r := *rule
			r.Type, r.Action, r.DefaultAction, r.ActionBreak = nil, nil, false, nil
			r.Expr = p.expr(rule.Expr)
			view.Statements = append(view.Statements, &r)
		}
	}
	return Format(&view)
}

type syntaxProjection struct{ seen map[Expr]Expr }

func (p *syntaxProjection) expr(expr Expr) Expr {
	if expr == nil {
		return nil
	}
	if v, ok := p.seen[expr]; ok {
		return v
	}
	var result Expr = expr
	switch e := expr.(type) {
	case *Seq:
		v := *e
		v.Items = make([]Expr, len(e.Items))
		for i, item := range e.Items {
			v.Items[i] = p.expr(item)
		}
		result = &v
	case *Choice:
		v := *e
		v.Alts = make([]Expr, len(e.Alts))
		for i, alt := range e.Alts {
			v.Alts[i] = p.expr(alt)
		}
		result = &v
	case *Group:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *InlineAction:
		result = p.expr(e.Expr)
	case *Attributed:
		v := *e
		v.Expr = p.expr(e.Expr)
		v.Attrs, v.Breaks = nil, nil
		for i, attr := range e.Attrs {
			if attr.Pragma && attr.Name != "recover" {
				v.Attrs = append(v.Attrs, attr)
				if len(e.Breaks) != 0 {
					v.Breaks = append(v.Breaks, breakAt(e.Breaks, i))
				}
			}
		}
		if len(v.Attrs) == 0 {
			result = v.Expr
		} else {
			result = &v
		}
	case *Recovery:
		result = p.expr(e.Expr)
	case *Capture:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *And:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *Not:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *Discard:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *Repeat:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *Optional:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *Atomic:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *Stream:
		v := *e
		v.Expr = p.expr(e.Expr)
		result = &v
	case *OperatorsRule:
		v := *e
		v.Skip = p.expr(e.Skip)
		v.Operands = make([]*OperatorsRuleOperand, len(e.Operands))
		for i, operand := range e.Operands {
			o := *operand
			o.Expr = p.expr(operand.Expr)
			o.Action, o.DefaultAction = nil, false
			v.Operands[i] = &o
		}
		v.Levels = make([]*OperatorsRuleLevel, len(e.Levels))
		for i, level := range e.Levels {
			l := *level
			l.Operators = make([]*OperatorsRuleOperator, len(level.Operators))
			for j, operator := range level.Operators {
				o := *operator
				o.Expr = p.expr(operator.Expr)
				o.Action, o.DefaultAction = nil, false
				l.Operators[j] = &o
			}
			v.Levels[i] = &l
		}
		result = &v
	}
	p.seen[expr] = result
	return result
}
