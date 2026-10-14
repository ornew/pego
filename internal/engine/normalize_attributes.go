package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
	"strings"
)

// normalizeAttributes builds the executable grammar without changing the
// source AST. Attribute syntax remains generic; built-ins define their own
// binding and lowerings before type checking and backend generation.
func normalizeAttributes(g *grammar.Grammar) (*grammar.Grammar, error) {
	return normalizeAttributesWithCopies(g, false)
}

// eager preserves the allocation-heavy normalization path for counterfactual
// measurements. It affects compilation only, never generated parser execution.
func normalizeAttributesWithCopies(g *grammar.Grammar, eager bool) (*grammar.Grammar, error) {
	n := &attributeNormalizer{eager: eager}
	statements, changed := normalizeItems(g.Statements, eager, n.statement)
	if len(n.errors) != 0 {
		return nil, n.errors
	}
	if !changed {
		return g, nil
	}
	copy := *g
	copy.Statements = statements
	return &copy, nil
}

type attributeNormalizer struct {
	errors ErrorList
	eager  bool
}

func (n *attributeNormalizer) statement(statement grammar.Statement) grammar.Statement {
	switch rd := statement.(type) {
	case *grammar.RuleDef:
		expr := n.expr(rd.Expr)
		typ := grammaranalysis.UngroupType(rd.Type)
		action := grammaranalysis.UngroupTerm(rd.Action)
		if !n.eager && expr == rd.Expr && typ == rd.Type && action == rd.Action {
			return rd
		}
		rule := *rd
		rule.Expr, rule.Type, rule.Action = expr, typ, action
		return &rule
	case *grammar.TypeDef:
		return n.typeDef(rd)
	default:
		return statement
	}
}

// Changed containers are copied before their first write. Nil input containers
// still become nonnil empty slices, matching the executable AST representation.
func normalizeItems[T comparable](items []T, eager bool, normalize func(T) T) ([]T, bool) {
	var normalized []T
	changed := eager || items == nil
	if changed {
		normalized = make([]T, len(items))
		copy(normalized, items)
	}
	for i, item := range items {
		child := normalize(item)
		if child != item && !changed {
			normalized = make([]T, len(items))
			copy(normalized, items)
			changed = true
		}
		if changed {
			normalized[i] = child
		}
	}
	if changed {
		return normalized, true
	}
	return items, false
}

func (n *attributeNormalizer) error(a *grammar.Attribute, message string) {
	n.errors = append(n.errors, &Error{Pos: a.Diagnostic(), Msg: annotationMarker(a) + a.Name + ": " + message})
}

func (n *attributeNormalizer) value(value any) (any, bool) {
	if e, ok := value.(grammar.Expr); ok {
		child := n.expr(e)
		return child, child != e
	}
	// Extension values can be maps or slices; comparing arbitrary interface
	// values would panic. Only expression arguments undergo normalization.
	return value, false
}

func (n *attributeNormalizer) attribute(a *grammar.Attribute) *grammar.Attribute {
	copy := *a
	changed := n.eager || a.Args == nil || (len(a.KeywordOrder) == 0 && a.KeywordOrder != nil)
	argsCopied := n.eager || a.Args == nil
	if argsCopied {
		copy.Args = make([]any, len(a.Args))
	}
	for i, value := range a.Args {
		child, lowered := n.value(value)
		if lowered && !argsCopied {
			copy.Args = append([]any(nil), a.Args...)
			argsCopied, changed = true, true
		}
		if argsCopied {
			copy.Args[i] = child
		}
	}
	keywordsCopied := n.eager && a.KwArgs != nil
	if keywordsCopied {
		copy.KwArgs = make(map[string]any, len(a.KwArgs))
	}
	for name, value := range a.KwArgs {
		child, lowered := n.value(value)
		if lowered && !keywordsCopied {
			copy.KwArgs = cloneAttributeKeywords(a.KwArgs)
			keywordsCopied, changed = true, true
		}
		if keywordsCopied {
			copy.KwArgs[name] = child
		}
	}
	if len(a.KeywordOrder) == 0 {
		copy.KeywordOrder = nil
	} else if n.eager {
		copy.KeywordOrder = append([]string(nil), a.KeywordOrder...)
	}
	if !changed {
		return a
	}
	return &copy
}

func cloneAttributeKeywords(values map[string]any) map[string]any {
	copy := make(map[string]any, len(values))
	for name, value := range values {
		copy[name] = value
	}
	return copy
}

func (n *attributeNormalizer) bind(a *grammar.Attribute, name string) *grammar.Attribute {
	if len(a.Args) == 0 {
		return a
	}
	if len(a.Args) != 1 || a.Args[0] == nil {
		n.error(a, "requires one nonempty positional argument")
		return a
	}
	if _, exists := a.KwArgs[name]; exists {
		n.error(a, "argument "+name+" is specified twice")
		return a
	}
	if !n.eager {
		copy := *a
		copy.KwArgs = cloneAttributeKeywords(a.KwArgs)
		a = &copy
	} else if a.KwArgs == nil {
		a.KwArgs = map[string]any{}
	}
	a.KwArgs[name] = a.Args[0]
	a.Args = nil
	return a
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
	return &grammar.Repeat{SourceInfo: grammar.SourceInfo{Pos: a.Diagnostic()}, Expr: e, Min: min, Max: max}
}

func (n *attributeNormalizer) expr(expr grammar.Expr) grammar.Expr {
	switch e := expr.(type) {
	case *grammar.Group:
		return n.expr(e.Expr)
	case *grammar.Predicate:
		term := e.Term
		if block, ok := term.(*grammar.Block); ok && len(block.Statements) > 0 {
			return predicateBlock(e, block)
		}
		term = grammaranalysis.UngroupTerm(term)
		if !n.eager && term == e.Term {
			return e
		}
		copy := *e
		copy.Term = term
		return &copy
	case *grammar.Seq:
		items, changed := normalizeItems(e.Items, n.eager, n.expr)
		items, expanded := expandPredicateBlocks(e.Items, items)
		changed = changed || expanded
		if !changed {
			return e
		}
		copy := *e
		copy.Items = items
		return &copy
	case *grammar.Choice:
		items, changed := normalizeItems(e.Alts, n.eager, n.expr)
		if !changed {
			return e
		}
		copy := *e
		copy.Alts = items
		return &copy
	case *grammar.Repeat:
		child := n.expr(e.Expr)
		if !n.eager && child == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = child
		return &copy
	case *grammar.Optional:
		child := n.expr(e.Expr)
		if !n.eager && child == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = child
		return &copy
	case *grammar.And:
		child := n.expr(e.Expr)
		if !n.eager && child == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = child
		return &copy
	case *grammar.Not:
		child := n.expr(e.Expr)
		if !n.eager && child == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = child
		return &copy
	case *grammar.Stream:
		return n.wrap(n.expr(e.Expr), &grammar.Attribute{SourceInfo: grammar.SourceInfo{Pos: e.Diagnostic()}, Name: "stream", Pragma: true})
	case *grammar.Recovery:
		return n.wrap(n.expr(e.Expr), &grammar.Attribute{SourceInfo: grammar.SourceInfo{Pos: e.Diagnostic()}, Name: "recover", Pragma: true, KwArgs: map[string]any{"skip": n.expr(e.Skip)}})
	case *grammar.Atomic:
		child := n.expr(e.Expr)
		if !n.eager && child == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = child
		return &copy
	case *grammar.Discard:
		child := n.expr(e.Expr)
		if !n.eager && child == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = child
		return &copy
	case *grammar.Capture:
		child := n.expr(e.Expr)
		if !n.eager && child == e.Expr {
			return e
		}
		copy := *e
		copy.Expr = child
		return &copy
	case *grammar.Attributed:
		inner := n.expr(e.Expr)
		seenDiagnostic := false
		for _, source := range e.Attrs {
			a := n.attribute(source)
			pragma := a.Name == "repeat" || a.Name == "atomic" || a.Name == "recover" || a.Name == "stream" || a.Name == "missing"
			if pragma && !a.Pragma {
				n.error(a, "is a pragma; use #"+a.Name)
				continue
			}
			if (a.Name == "error" || a.Name == "diagnostic") && a.Pragma {
				n.error(a, "is an attribute; use @"+a.Name)
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
				inner = &grammar.Atomic{SourceInfo: grammar.SourceInfo{Pos: a.Diagnostic()}, Expr: inner}
				continue
			case "error":
				a = n.bind(a, "message")
			case "diagnostic":
				if seenDiagnostic {
					n.error(a, "may occur only once on an expression")
				}
				seenDiagnostic = true
				if len(a.Args) != 0 || len(a.KwArgs) != 1 {
					n.error(a, "requires exactly one named argument, name")
				} else if _, ok := a.KwArgs["name"]; !ok {
					n.error(a, "requires a name argument")
				} else if lit, ok := a.Arg("name"); !ok {
					n.error(a, "name must be a string")
				} else if stringLit, ok := lit.(*grammar.Literal); !ok {
					n.error(a, "name must be a string")
				} else if stringLit.Value == "" || strings.ContainsAny(stringLit.Value, "\r\n\u0085\u2028\u2029") {
					n.error(a, "name must be nonempty and contain no line separator")
				}
			case "recover":
				a = n.bind(a, "skip")
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
		skip := n.expr(e.Skip)
		operands, operandsChanged := normalizeItems(e.Operands, n.eager, n.operand)
		levels, levelsChanged := normalizeItems(e.Levels, n.eager, n.level)
		if !n.eager && skip == e.Skip && !operandsChanged && !levelsChanged {
			return e
		}
		copy := *e
		copy.Skip, copy.Operands, copy.Levels = skip, operands, levels
		return &copy
	}
	return expr
}

func (n *attributeNormalizer) operand(o *grammar.OperatorsRuleOperand) *grammar.OperatorsRuleOperand {
	expr, action := n.expr(o.Expr), grammaranalysis.UngroupTerm(o.Action)
	if !n.eager && expr == o.Expr && action == o.Action {
		return o
	}
	copy := *o
	copy.Expr, copy.Action = expr, action
	return &copy
}

func (n *attributeNormalizer) level(l *grammar.OperatorsRuleLevel) *grammar.OperatorsRuleLevel {
	operators, changed := normalizeItems(l.Operators, n.eager, n.operator)
	if !changed {
		return l
	}
	copy := *l
	copy.Operators = operators
	return &copy
}

func (n *attributeNormalizer) operator(o *grammar.OperatorsRuleOperator) *grammar.OperatorsRuleOperator {
	expr, action := n.expr(o.Expr), grammaranalysis.UngroupTerm(o.Action)
	if !n.eager && expr == o.Expr && action == o.Action {
		return o
	}
	copy := *o
	copy.Expr, copy.Action = expr, action
	return &copy
}

func attributeMessage(a *grammar.Attribute) string {
	value, _ := a.Arg("message")
	return value.(*grammar.Literal).Value
}

func attributeDiagnosticName(a *grammar.Attribute) string {
	value, _ := a.Arg("name")
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

// Source grouping lowers before checking/generation; unchanged children stay
// shared so ordinary action and type trees need no extra copies.
func (n *attributeNormalizer) typeDef(td *grammar.TypeDef) *grammar.TypeDef {
	spec := td.Spec
	switch s := spec.(type) {
	case *grammar.AliasSpec:
		child := grammaranalysis.UngroupType(s.Type)
		if child != s.Type {
			v := *s
			v.Type = child
			spec = &v
		}
	case *grammar.StructSpec:
		var fields []*grammar.Field
		for i, f := range s.Fields {
			child := grammaranalysis.UngroupType(f.Type)
			if child == f.Type {
				continue
			}
			if fields == nil {
				fields = append([]*grammar.Field(nil), s.Fields...)
			}
			v := *f
			v.Type = child
			fields[i] = &v
		}
		if fields != nil {
			v := *s
			v.Fields = fields
			spec = &v
		}
	}
	if spec == td.Spec {
		return td
	}
	v := *td
	v.Spec = spec
	return &v
}
