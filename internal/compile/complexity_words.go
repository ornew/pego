package compile

import (
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// --- Exclusion by a negative lookahead ---

// word is the prefix with which every path of a unit begins: the case-sensitive literal text
// followed by a negative lookahead of one character that is not in chars, as a keyword
// "let" !idchar is.
type word struct {
	text  string
	chars charset
}

// wordBudget bounds the steps of one search for a word or through a continuation.
const wordBudget = leadBudget

// excludedByWord reports whether every path of the continuation c is excluded at the start of the
// unit u by a word with which u begins. After u matches its word w followed by a character c that
// is not in the set of the word, or by the end of the input, each path of the continuation fails
// at bounded cost: it fails on the first character of w, or it evaluates a negative lookahead of
// a rule K that is proven to match w followed by such a character. If u fails within its word,
// it examined bounded input. Either way the continuation repeats no scan of u.
func (k *classifier) excludedByWord(u, c []*cont, keep func(*site) bool) bool {
	w, ok := k.unitWord(u)
	if !ok {
		return false
	}
	return k.wordExcludes(c, w, keep)
}

// unitWord returns the word with which every path of u begins, if all begin with the same one.
// It looks only through expressions that evaluate their content first, and through calls of rules
// that read no variable.
func (k *classifier) unitWord(u []*cont) (word, bool) {
	budget := wordBudget
	var found *word
	ok := true
	var visit func(p *cont)
	visit = func(p *cont) {
		for ok {
			k.control.poll()
			budget--
			if budget < 0 || p == nil {
				ok = false
				return
			}
			if len(p.items) == 0 {
				if p.next == nil {
					ok = false
					return
				}
				p = p.next
				continue
			}
			rest := p.rest()
			switch e := p.items[0].(type) {
			case *grammar.Group:
				p = rest.then(e.Expr)
			case *grammar.Capture:
				p = rest.then(e.Expr)
			case *grammar.Discard:
				p = rest.then(e.Expr)
			case *grammar.Atomic:
				p = rest.then(e.Expr)
			case *grammar.InlineAction:
				p = rest.then(e.Expr)
			case *grammar.Seq:
				p = rest.then(e.Items...)
			case *grammar.Choice:
				for _, alt := range e.Alts {
					visit(rest.then(alt))
				}
				return
			case *grammar.Ref:
				if !k.plainRef(e) {
					ok = false
					return
				}
				p = rest.then(k.byName[e.Name].Expr)
			case *grammar.Literal:
				w, good := k.wordAfter(e, rest)
				if !good || found != nil && (found.text != w.text || !found.chars.equal(w.chars)) {
					ok = false
					return
				}
				found = &w
				return
			default:
				ok = false
				return
			}
		}
	}
	for _, p := range u {
		visit(p)
	}
	if !ok || found == nil {
		return word{}, false
	}
	return *found, true
}

// wordAfter returns the word of the literal l when the path rest evaluates, before anything else,
// a negative lookahead of one character.
func (k *classifier) wordAfter(l *grammar.Literal, rest *cont) (word, bool) {
	if l.Value == "" || l.IgnoreCase {
		return word{}, false
	}
	p := rest
	for range wordBudget {
		if p == nil {
			return word{}, false
		}
		if len(p.items) == 0 {
			p = p.next
			continue
		}
		switch e := p.items[0].(type) {
		case *grammar.Group:
			p = p.rest().then(e.Expr)
		case *grammar.Seq:
			p = p.rest().then(e.Items...)
		case *grammar.Not:
			chars, ok := k.oneChar(e.Expr)
			if !ok {
				return word{}, false
			}
			return word{text: l.Value, chars: chars}, true
		default:
			return word{}, false
		}
	}
	return word{}, false
}

// oneChar returns the characters of e when e is a character class or a call of a rule whose body
// is one.
func (k *classifier) oneChar(e grammar.Expr) (charset, bool) {
	e = ungroup(e)
	if r, ok := e.(*grammar.Ref); ok {
		if !k.plainRef(r) {
			return nil, false
		}
		e = ungroup(k.byName[r.Name].Expr)
	}
	c, ok := e.(*grammar.CharClass)
	if !ok {
		return nil, false
	}
	return classChars(c), true
}

// plainRef reports whether r calls a rule of the grammar without a level restriction, and the
// rule reads no variable, so that the call matches as the rule body does.
func (k *classifier) plainRef(r *grammar.Ref) bool {
	return r.Level == "" && k.byName[r.Name] != nil && len(k.vars[r.Name]) == 0
}

// wordExcludes reports whether every path of the continuation fails at bounded cost at a position
// where the input begins with the word w.
func (k *classifier) wordExcludes(paths []*cont, w word, keep func(*site) bool) bool {
	budget := wordBudget
	r, _ := utf8.DecodeRuneInString(w.text)
	head := charOf(r)
	live := false
	ended := map[endKey]bool{}
	var visit func(p *cont)
	visit = func(p *cont) {
		for !live {
			k.control.poll()
			budget--
			if budget < 0 {
				live = true
				return
			}
			if p == nil {
				// The parse ends here and examines nothing more.
				return
			}
			if len(p.items) == 0 {
				if p.next != nil {
					p = p.next
					continue
				}
				if p.end != endNone {
					key := endKey{p.end, p.rule}
					if ended[key] {
						return
					}
					ended[key] = true
				}
				switch p.end {
				case endOps:
					k.opsLeads(p.rule, visit)
					k.endLeads(p.rule, keep, visit)
				case endRule:
					k.endLeads(p.rule, keep, visit)
				}
				return
			}
			rest := p.rest()
			it := p.items[0]
			if it == nil {
				p = rest
				continue
			}
			// An item that cannot examine the first character of w neither consumes input here nor
			// examines more than that character: it fails, or it continues at the same position.
			if !k.first(it).intersects(head) {
				if !k.nullable(it) {
					return
				}
				p = rest
				continue
			}
			switch e := it.(type) {
			case *grammar.Group:
				p = rest.then(e.Expr)
			case *grammar.Capture:
				p = rest.then(e.Expr)
			case *grammar.Discard:
				p = rest.then(e.Expr)
			case *grammar.Atomic:
				p = rest.then(e.Expr)
			case *grammar.Stream:
				p = rest.then(e.Expr)
			case *grammar.InlineAction:
				p = rest.then(e.Expr)
			case *grammar.Seq:
				p = rest.then(e.Items...)
			case *grammar.Attributed:
				if recoverSkip(e) != nil || hasPragma(e, "missing") {
					live = true
					return
				}
				p = rest.then(e.Expr)
			case *grammar.Choice:
				for _, alt := range e.Alts {
					visit(rest.then(alt))
				}
				return
			case *grammar.Optional:
				visit(rest.then(e.Expr))
				visit(rest)
				return
			case *grammar.Literal:
				// A literal that differs from w within their common length fails on it.
				if !e.IgnoreCase && !strings.HasPrefix(w.text, e.Value) && !strings.HasPrefix(e.Value, w.text) {
					return
				}
				live = true
				return
			case *grammar.Not:
				if k.provenWord(e.Expr, w) {
					return
				}
				live = true
				return
			case *grammar.Ref:
				if !k.plainRef(e) {
					live = true
					return
				}
				if pr := k.operatorsBody(e.Name); pr != nil {
					k.operatorsLeads(pr, e.Name, visit)
					return
				}
				p = rest.then(k.byName[e.Name].Expr)
			default:
				live = true
				return
			}
		}
	}
	for _, p := range paths {
		visit(p)
	}
	return !live
}

// provenWord reports whether the rule body or expression K matches wherever the input begins with
// the word w followed by a character outside its set or by the end of the input. K must be a
// sequence of a choice of case-sensitive literals and a negative lookahead of the same characters
// as the word, through calls of rules that read no variable, with nothing else that can depend on
// how the parser reached it: no predicate, other lookahead, cut, recovery or level restriction.
// The choice must reach w: no earlier literal of it may be a prefix of w or begin with w, so that
// the choice selects w itself.
func (k *classifier) provenWord(e grammar.Expr, w word) bool {
	e, ok := k.wordBody(e)
	if !ok {
		return false
	}
	s, ok := e.(*grammar.Seq)
	if !ok || len(s.Items) != 2 {
		return false
	}
	not, ok := ungroup(s.Items[1]).(*grammar.Not)
	if !ok {
		return false
	}
	chars, ok := k.oneChar(not.Expr)
	if !ok || !chars.equal(w.chars) {
		return false
	}
	alts, ok := k.wordBody(s.Items[0])
	if !ok {
		return false
	}
	var lits []grammar.Expr
	if c, ok := alts.(*grammar.Choice); ok {
		lits = c.Alts
	} else {
		lits = []grammar.Expr{alts}
	}
	for _, alt := range lits {
		alt, ok := k.wordBody(alt)
		if !ok {
			return false
		}
		l, ok := alt.(*grammar.Literal)
		if !ok || l.Value == "" || l.IgnoreCase {
			return false
		}
		if l.Value == w.text {
			return true
		}
		if strings.HasPrefix(w.text, l.Value) || strings.HasPrefix(l.Value, w.text) {
			return false
		}
	}
	return false
}

// wordBody returns e through groups, captures and calls of rules that read no variable.
func (k *classifier) wordBody(e grammar.Expr) (grammar.Expr, bool) {
	for range len(k.rules) + 1 {
		switch x := e.(type) {
		case *grammar.Group:
			e = x.Expr
		case *grammar.Capture:
			e = x.Expr
		case *grammar.Atomic:
			e = x.Expr
		case *grammar.Ref:
			if !k.plainRef(x) {
				return nil, false
			}
			e = k.byName[x.Name].Expr
		default:
			return e, true
		}
	}
	return nil, false
}
