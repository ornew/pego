package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

// An inline action (e => A) is an anonymous rule embedded at its position: it behaves exactly as a
// reference to a rule whose body is e and whose action is A. lowerInlineActions makes that
// explicit. It replaces each inline action by a reference to a hidden rule of that form, placed
// after the rule that holds it, so that every backend runs the lowered grammar unchanged and the
// semantics of captures, items, positions, cuts, recovery and recursion are those of a rule.
//
// The hidden rule of the nth inline action of a rule r is named r_inline_n, with a further suffix
// when a rule of that name exists.

// inlineActionName returns a name for the hidden rule of the nth inline action of rule that no
// rule of the grammar has.
func inlineActionName(rule string, n int, taken map[string]bool) string {
	name := fmt.Sprintf("%s_inline_%d", rule, n)
	for taken[name] {
		name += "_"
	}
	taken[name] = true
	return name
}

// hasInlineAction reports whether g holds an inline action.
func hasInlineAction(g *grammar.Grammar, control *compileControl) bool {
	if g == nil {
		return false
	}
	found := false
	for _, rd := range control.rules(g) {
		walkExprControlled(rd.Expr, func(e grammar.Expr) {
			if _, ok := e.(*grammar.InlineAction); ok {
				found = true
			}
		}, control)
		if found {
			return true
		}
	}
	return false
}

type inlineLowerer struct {
	rule    string
	count   int
	taken   map[string]bool
	hidden  []grammar.Decl
	unused  int // depth of the expressions whose value is not used: lookaheads and discards
	errs    ErrorList
	control *compileControl
}

// lowerInlineActions returns g with every inline action replaced by a reference to a hidden rule.
// g itself does not change. Errors report inline actions in expressions that make no value.
func lowerInlineActions(g *grammar.Grammar, control *compileControl) (*grammar.Grammar, ErrorList) {
	var errs ErrorList
	taken := map[string]bool{}
	for _, rd := range control.rules(g) {
		taken[rd.Name] = true
	}
	decls := make([]grammar.Decl, 0, len(g.File.Decls))
	for _, d := range g.File.Decls {
		control.poll()
		rd, ok := d.(*grammar.RuleDef)
		if !ok {
			decls = append(decls, d)
			continue
		}
		l := &inlineLowerer{rule: rd.Name, taken: taken, control: control}
		expr := l.expr(rd.Expr)
		errs = append(errs, l.errs...)
		if expr == rd.Expr {
			decls = append(decls, rd)
			continue
		}
		lowered := *rd
		lowered.Expr = expr
		decls = append(decls, &lowered)
		decls = append(decls, l.hidden...)
	}
	if len(errs) != 0 {
		return nil, errs
	}
	lowered := *g
	lowered.File.Decls = decls
	return &lowered, nil
}

func (l *inlineLowerer) exprs(items []grammar.Expr) []grammar.Expr {
	var out []grammar.Expr
	for i, it := range items {
		child := l.expr(it)
		if child != it && out == nil {
			out = append([]grammar.Expr(nil), items...)
		}
		if out != nil {
			out[i] = child
		}
	}
	if out == nil {
		return items
	}
	return out
}

// quiet lowers e where its value is not used.
func (l *inlineLowerer) quiet(e grammar.Expr) grammar.Expr {
	l.unused++
	defer func() { l.unused-- }()
	return l.expr(e)
}

func (l *inlineLowerer) expr(e grammar.Expr) grammar.Expr {
	l.control.poll()
	switch e := e.(type) {
	case *grammar.InlineAction:
		if l.unused > 0 {
			l.errs = append(l.errs, &Error{Pos: e.Diagnostic(), Msg: "an inline action is not allowed in a lookahead or a discard, whose value is not used"})
		}
		inner := l.expr(e.Expr)
		l.count++
		name := inlineActionName(l.rule, l.count, l.taken)
		l.hidden = append(l.hidden, &grammar.RuleDef{SourceInfo: e.SourceInfo, Name: name, Expr: inner, Action: e.Action, DefaultAction: e.DefaultAction})
		return &grammar.Ref{SourceInfo: e.SourceInfo, Name: name}
	case *grammar.Group:
		return rewrap(e, e.Expr, l.expr(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Seq:
		items := l.exprs(e.Items)
		if len(items) > 0 && len(e.Items) > 0 && &items[0] == &e.Items[0] {
			return e
		}
		copy := *e
		copy.Items = items
		return &copy
	case *grammar.Choice:
		alts := l.exprs(e.Alts)
		if len(alts) > 0 && &alts[0] == &e.Alts[0] {
			return e
		}
		copy := *e
		copy.Alts = alts
		return &copy
	case *grammar.Repeat:
		return rewrap(e, e.Expr, l.expr(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Optional:
		return rewrap(e, e.Expr, l.expr(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.And:
		return rewrap(e, e.Expr, l.quiet(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Not:
		return rewrap(e, e.Expr, l.quiet(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Discard:
		return rewrap(e, e.Expr, l.quiet(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Atomic:
		return rewrap(e, e.Expr, l.expr(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Stream:
		return rewrap(e, e.Expr, l.expr(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Capture:
		return rewrap(e, e.Expr, l.expr(e.Expr), func(c grammar.Expr) grammar.Expr { copy := *e; copy.Expr = c; return &copy })
	case *grammar.Recovery:
		x, skip := l.expr(e.Expr), l.expr(e.Skip)
		if x == e.Expr && skip == e.Skip {
			return e
		}
		copy := *e
		copy.Expr, copy.Skip = x, skip
		return &copy
	case *grammar.Attributed:
		x := l.expr(e.Expr)
		attrs, changed := l.attributes(e.Attrs)
		if x == e.Expr && !changed {
			return e
		}
		copy := *e
		copy.Expr, copy.Attrs = x, attrs
		return &copy
	case *grammar.OperatorsRule:
		skip := l.expr(e.Skip)
		changed := skip != e.Skip
		operands := make([]*grammar.OperatorsRuleOperand, len(e.Operands))
		for i, o := range e.Operands {
			x := l.expr(o.Expr)
			operands[i] = o
			if x != o.Expr {
				copy := *o
				copy.Expr = x
				operands[i], changed = &copy, true
			}
		}
		levels := make([]*grammar.OperatorsRuleLevel, len(e.Levels))
		for i, lv := range e.Levels {
			levels[i] = lv
			ops := make([]*grammar.OperatorsRuleOperator, len(lv.Operators))
			levelChanged := false
			for j, o := range lv.Operators {
				x := l.expr(o.Expr)
				ops[j] = o
				if x != o.Expr {
					copy := *o
					copy.Expr = x
					ops[j], levelChanged = &copy, true
				}
			}
			if levelChanged {
				copy := *lv
				copy.Operators = ops
				levels[i], changed = &copy, true
			}
		}
		if !changed {
			return e
		}
		copy := *e
		copy.Skip, copy.Operands, copy.Levels = skip, operands, levels
		return &copy
	}
	return e
}

func (l *inlineLowerer) attributes(attrs []*grammar.Attribute) ([]*grammar.Attribute, bool) {
	var out []*grammar.Attribute
	for i, a := range attrs {
		var args []any
		for j, v := range a.Args {
			if x, ok := v.(grammar.Expr); ok {
				if c := l.expr(x); c != x {
					if args == nil {
						args = append([]any(nil), a.Args...)
					}
					args[j] = c
				}
			}
		}
		var kw map[string]any
		for name, v := range a.KwArgs {
			if x, ok := v.(grammar.Expr); ok {
				if c := l.expr(x); c != x {
					if kw == nil {
						kw = make(map[string]any, len(a.KwArgs))
						for k, o := range a.KwArgs {
							kw[k] = o
						}
					}
					kw[name] = c
				}
			}
		}
		if args == nil && kw == nil {
			continue
		}
		if out == nil {
			out = append([]*grammar.Attribute(nil), attrs...)
		}
		copy := *a
		if args != nil {
			copy.Args = args
		}
		if kw != nil {
			copy.KwArgs = kw
		}
		out[i] = &copy
	}
	if out == nil {
		return attrs, false
	}
	return out, true
}

// rewrap returns old if child is unchanged, and make(child) otherwise.
func rewrap(self, old, child grammar.Expr, make func(grammar.Expr) grammar.Expr) grammar.Expr {
	if child == old {
		return self
	}
	return make(child)
}

// LowerInlineActions returns g with every inline action replaced by a reference to the hidden rule
// that the compiler runs in its place, so that a tool working on the rules of a grammar, such as the
// input sampler, sees the rules the parser executes. g is not changed.
func LowerInlineActions(g *grammar.Grammar) (*grammar.Grammar, error) {
	if g == nil || !hasInlineAction(g, nil) {
		return g, nil
	}
	lowered, errs := lowerInlineActions(g, nil)
	if errs != nil {
		return nil, errs
	}
	return lowered, nil
}
