package engine

import "github.com/ornew/pego/grammar"

// normalizeAttributes builds the executable grammar without changing the
// source AST. Attribute syntax remains generic; built-ins define their own
// binding and lowerings before type checking and backend generation.
func normalizeAttributes(g *grammar.Grammar) (*grammar.Grammar, error) {
	n := &attributeNormalizer{}
	copy := *g
	copy.Statements = make([]grammar.Statement, len(g.Statements))
	for i, statement := range g.Statements {
		if rd, ok := statement.(*grammar.RuleDef); ok {
			rule := *rd
			rule.Expr = n.expr(rd.Expr)
			copy.Statements[i] = &rule
		} else {
			copy.Statements[i] = statement
		}
	}
	if len(n.errors) != 0 {
		return nil, n.errors
	}
	return &copy, nil
}

type attributeNormalizer struct{ errors ErrorList }

func (n *attributeNormalizer) error(a *grammar.Attribute, message string) {
	n.errors = append(n.errors, &Error{Pos: a.Pos, Msg: annotationMarker(a) + a.Name + ": " + message})
}

func (n *attributeNormalizer) value(value any) any {
	if e, ok := value.(grammar.Expr); ok {
		return n.expr(e)
	}
	return value
}

func (n *attributeNormalizer) attribute(a *grammar.Attribute) *grammar.Attribute {
	copy := *a
	copy.Args = make([]any, len(a.Args))
	for i, value := range a.Args {
		copy.Args[i] = n.value(value)
	}
	if a.KwArgs != nil {
		copy.KwArgs = make(map[string]any, len(a.KwArgs))
		for name, value := range a.KwArgs {
			copy.KwArgs[name] = n.value(value)
		}
	}
	copy.KeywordOrder = append([]string(nil), a.KeywordOrder...)
	return &copy
}

func (n *attributeNormalizer) bind(a *grammar.Attribute, name string) {
	if len(a.Args) == 0 {
		return
	}
	if len(a.Args) != 1 || a.Args[0] == nil {
		n.error(a, "requires one nonempty positional argument")
		return
	}
	if _, exists := a.KwArgs[name]; exists {
		n.error(a, "argument "+name+" is specified twice")
		return
	}
	if a.KwArgs == nil {
		a.KwArgs = map[string]any{}
	}
	a.KwArgs[name] = a.Args[0]
	a.Args = nil
}

func (n *attributeNormalizer) repeat(e grammar.Expr, a *grammar.Attribute) grammar.Expr {
	if len(a.KwArgs) != 0 {
		n.error(a, "accepts only positional arguments")
		return e
	}
	if len(a.Args) != 1 && len(a.Args) != 2 {
		n.error(a, "requires one or two positional slots")
		return e
	}
	if a.Args[0] == nil && (len(a.Args) == 1 || a.Args[1] == nil) {
		n.error(a, "requires at least one bound")
		return e
	}
	min := 0
	if a.Args[0] != nil {
		value, ok := a.Args[0].(int)
		if !ok || value < 0 {
			n.error(a, "minimum must be a nonnegative integer")
			return e
		}
		min = value
	}
	max := min
	if len(a.Args) == 2 {
		max = -1
		if a.Args[1] != nil {
			value, ok := a.Args[1].(int)
			if !ok || value < 0 {
				n.error(a, "maximum must be a nonnegative integer")
				return e
			}
			max = value
		}
	}
	if max >= 0 && max < min {
		n.error(a, "maximum is less than minimum")
		return e
	}
	return &grammar.Repeat{Pos: a.Pos, Expr: e, Min: min, Max: max}
}

func (n *attributeNormalizer) expr(expr grammar.Expr) grammar.Expr {
	switch e := expr.(type) {
	case *grammar.Group:
		return n.expr(e.Expr)
	case *grammar.Predicate:
		if block, ok := e.Term.(*grammar.Block); ok && len(block.Statements) == 1 {
			copy := *e
			copy.Term = block.Statements[0]
			return &copy
		}
		return e
	case *grammar.Seq:
		copy := *e
		copy.Items = make([]grammar.Expr, len(e.Items))
		for i, item := range e.Items {
			copy.Items[i] = n.expr(item)
		}
		return &copy
	case *grammar.Choice:
		copy := *e
		copy.Alts = make([]grammar.Expr, len(e.Alts))
		for i, item := range e.Alts {
			copy.Alts[i] = n.expr(item)
		}
		return &copy
	case *grammar.Repeat:
		copy := *e
		copy.Expr = n.expr(e.Expr)
		return &copy
	case *grammar.Optional:
		copy := *e
		copy.Expr = n.expr(e.Expr)
		return &copy
	case *grammar.And:
		copy := *e
		copy.Expr = n.expr(e.Expr)
		return &copy
	case *grammar.Not:
		copy := *e
		copy.Expr = n.expr(e.Expr)
		return &copy
	case *grammar.Stream:
		return n.wrap(n.expr(e.Expr), &grammar.Attribute{Pos: e.Pos, Name: "stream", Pragma: true})
	case *grammar.Recovery:
		return n.wrap(n.expr(e.Expr), &grammar.Attribute{Pos: e.Pos, Name: "recover", Pragma: true, KwArgs: map[string]any{"skip": n.expr(e.Skip)}})
	case *grammar.Atomic:
		copy := *e
		copy.Expr = n.expr(e.Expr)
		return &copy
	case *grammar.Discard:
		copy := *e
		copy.Expr = n.expr(e.Expr)
		return &copy
	case *grammar.Capture:
		copy := *e
		copy.Expr = n.expr(e.Expr)
		return &copy
	case *grammar.Attributed:
		inner := n.expr(e.Expr)
		for _, source := range e.Attrs {
			a := n.attribute(source)
			pragma := a.Name == "repeat" || a.Name == "atomic" || a.Name == "recover" || a.Name == "stream"
			if pragma && !a.Pragma {
				n.error(a, "is a pragma; use #"+a.Name)
				continue
			}
			if a.Name == "error" && a.Pragma {
				n.error(a, "is an attribute; use @error")
				continue
			}
			switch a.Name {
			case "repeat":
				inner = n.repeat(inner, a)
				continue
			case "atomic":
				if len(a.Args)+len(a.KwArgs) != 0 {
					n.error(a, "takes no arguments")
				}
				inner = &grammar.Atomic{Pos: a.Pos, Expr: inner}
				continue
			case "error":
				n.bind(a, "message")
			case "recover":
				n.bind(a, "skip")
			}
			if old, ok := inner.(*grammar.Attributed); ok {
				copy := *old
				copy.Attrs = append(append([]*grammar.Attribute(nil), old.Attrs...), a)
				inner = &copy
			} else {
				inner = &grammar.Attributed{Expr: inner, Attrs: []*grammar.Attribute{a}}
			}
		}
		return inner
	case *grammar.OperatorsRule:
		copy := *e
		copy.Skip = n.expr(e.Skip)
		copy.Operands = make([]*grammar.OperatorsRuleOperand, len(e.Operands))
		for i, o := range e.Operands {
			operand := *o
			operand.Expr = n.expr(o.Expr)
			copy.Operands[i] = &operand
		}
		copy.Levels = make([]*grammar.OperatorsRuleLevel, len(e.Levels))
		for i, l := range e.Levels {
			level := *l
			level.Operators = make([]*grammar.OperatorsRuleOperator, len(l.Operators))
			for j, o := range l.Operators {
				operator := *o
				operator.Expr = n.expr(o.Expr)
				level.Operators[j] = &operator
			}
			copy.Levels[i] = &level
		}
		return &copy
	}
	return expr
}

func attributeMessage(a *grammar.Attribute) string {
	value, _ := a.Arg("message")
	return value.(*grammar.Literal).Value
}

func annotationMarker(a *grammar.Attribute) string {
	if a.Pragma {
		return "#"
	}
	return "@"
}

func (n *attributeNormalizer) wrap(e grammar.Expr, a *grammar.Attribute) grammar.Expr {
	if old, ok := e.(*grammar.Attributed); ok {
		copy := *old
		copy.Attrs = append(append([]*grammar.Attribute(nil), old.Attrs...), a)
		return &copy
	}
	return &grammar.Attributed{Expr: e, Attrs: []*grammar.Attribute{a}}
}
