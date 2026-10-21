package engine

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
)

// An inline action (e => A) is an anonymous rule embedded at its position: it behaves exactly as a
// reference to a rule whose body is e and whose action is A. lowerInlineActions makes that
// explicit. It replaces each inline action by a reference to a hidden rule of that form, placed
// after the rule that holds it, so that every backend runs the lowered grammar unchanged and the
// semantics of captures, items, positions, cuts, recovery and recursion are those of a rule.
//
// The hidden rule of the nth inline action of a rule r is named _r_n, with a further underscore
// added while a rule of that name exists. A name that begins with an underscore is a hidden rule,
// so a declared rule can be hidden in the same way. The generated names belong to the lowered
// grammar only: the grammar as written, which is what Program.SourceGrammar returns, holds the
// inline actions, and the messages of the compiler and of a parse describe an inline action by the
// rule that holds it (inlineNames).

// hiddenRule reports whether the rule name is hidden: it begins with an underscore. A hidden rule
// is not an entry point, and a hidden rule without an action makes no node of its own.
func hiddenRule(name string) bool { return strings.HasPrefix(name, "_") }

// EntryError returns the error for using the rule name as a start rule or another entry point of
// a parse, or nil if it can be one.
func EntryError(name string) error {
	if hiddenRule(name) {
		return fmt.Errorf("rule %s is hidden: a rule whose name begins with an underscore is not an entry point", name)
	}
	return nil
}

// inlineSite is the place of an inline action in the grammar as written.
type inlineSite struct {
	rule string // the rule that holds the inline action
	n    int    // its number among the inline actions of the rule, from 1
}

// inlineNames maps the name of each hidden rule that the lowering generated to the inline action
// it stands for.
type inlineNames map[string]inlineSite

// inlineActionName returns a name for the hidden rule of the nth inline action of rule that no
// rule of the grammar has.
func inlineActionName(rule string, n int, taken map[string]bool) string {
	name := fmt.Sprintf("_%s_%d", rule, n)
	for taken[name] {
		name += "_"
	}
	taken[name] = true
	return name
}

// describe names the inline action of the generated rule name as a user sees it. ok is false for
// any other name.
func (names inlineNames) describe(name string) (string, bool) {
	site, ok := names[name]
	if !ok {
		return "", false
	}
	return "the inline action in rule " + site.rule, true
}

// where names the rule name in the text of a failed action: an ordinary rule by its name and the
// hidden rule of an inline action by its place.
func (names inlineNames) where(name string) string {
	if site, ok := names[name]; ok {
		return fmt.Sprintf("%s (inline action %d)", site.rule, site.n)
	}
	return name
}

// rewrite replaces the generated names in the messages of err, which a compile reports for the
// lowered grammar, by the description of the inline action, so that no message names a rule that the
// grammar does not declare.
func (names inlineNames) rewrite(err error) error {
	list, ok := err.(ErrorList)
	if !ok || len(names) == 0 {
		return err
	}
	out := make(ErrorList, len(list))
	for i, e := range list {
		copy := *e
		copy.Msg = names.text(e.Msg)
		out[i] = &copy
	}
	return out
}

// text replaces the generated names in s, and the word "rule" before one.
func (names inlineNames) text(s string) string {
	for name := range names {
		desc, _ := names.describe(name)
		var b strings.Builder
		for {
			i := strings.Index(s, name)
			if i < 0 {
				break
			}
			end := i + len(name)
			if i > 0 && isNameByte(s[i-1]) || end < len(s) && isNameByte(s[end]) {
				b.WriteString(s[:end])
				s = s[end:]
				continue
			}
			b.WriteString(strings.TrimSuffix(s[:i], "rule "))
			b.WriteString(desc)
			s = s[end:]
		}
		b.WriteString(s)
		s = b.String()
	}
	return s
}

func isNameByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// undeclaredRules reports the references of g to a rule it does not declare. The lowering adds
// hidden rules, so a reference is checked against the grammar as written: a generated name can
// never satisfy one.
func undeclaredRules(g *grammar.Grammar, control *compileControl) ErrorList {
	declared := map[string]bool{}
	for _, rd := range control.rules(g) {
		declared[rd.Name] = true
	}
	var errs ErrorList
	for _, rd := range control.rules(g) {
		walkExprControlled(rd.Expr, func(e grammar.Expr) {
			if ref, ok := e.(*grammar.Ref); ok && !declared[ref.Name] {
				errs = append(errs, &Error{Pos: ref.Diagnostic(), Msg: "undefined rule " + ref.Name})
			}
		}, control)
	}
	return errs
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
	names   inlineNames
	hidden  []grammar.Decl
	unused  int // depth of the expressions whose value is not used: lookaheads and discards
	errs    ErrorList
	control *compileControl
}

// lowerInlineActions returns g with every inline action replaced by a reference to a hidden rule.
// g itself does not change. Errors report inline actions in expressions that make no value.
func lowerInlineActions(g *grammar.Grammar, names inlineNames, control *compileControl) (*grammar.Grammar, ErrorList) {
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
		l := &inlineLowerer{rule: rd.Name, taken: taken, names: names, control: control}
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
		if l.names != nil {
			l.names[name] = inlineSite{rule: l.rule, n: l.count}
		}
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
	lowered, errs := lowerInlineActions(g, nil, nil)
	if errs != nil {
		return nil, errs
	}
	return lowered, nil
}
