package compile

import (
	"slices"

	"github.com/ornew/pego/grammar"
)

// --- Exclusion by the leading text of a unit ---

// word is the text with which a path of a unit begins: a case-sensitive literal, optionally
// followed by a negative lookahead of one character class, as a keyword "let" !idchar is. After
// the unit matches its word, the input at its start is the text followed, when after is set, by a
// character that is not in after or by the end of the input.
type word struct {
	text  []rune
	after charset
	// bounded reports whether after is known.
	bounded bool
}

// wordBudget bounds the steps of one search for words or through a continuation.
const wordBudget = leadBudget

// excludedByWord reports whether every path of the continuation c is excluded at the start of the
// unit u by the words with which the paths of u begin. A unit that fails within a word examines
// no more than the word and the character after it. After the unit matches a word, the input at
// its start begins with that word, and each path of the continuation fails at a cost that the
// word bounds: it fails on the text of the word, or it evaluates a negative lookahead of a rule
// that is proven to match there. Either way the continuation repeats no scan of u.
func (k *classifier) excludedByWord(u, c []*cont, keep func(*site) bool) bool {
	words, ok := k.unitWords(u)
	if !ok {
		return false
	}
	for _, w := range words {
		if !k.wordExcludes(c, w, keep) {
			return false
		}
	}
	return true
}

// unitWords returns the word with which each path of u begins. It looks only through
// expressions that evaluate their content first, and through calls of rules that read no variable
// and have no level restriction.
func (k *classifier) unitWords(u []*cont) ([]word, bool) {
	budget := wordBudget
	var words []word
	ok := true
	var visit func(p *cont)
	visit = func(p *cont) {
		for ok {
			k.control.poll()
			budget--
			if budget < 0 || p == nil || len(words) > leadMax {
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
				if e.Value == "" || e.IgnoreCase {
					ok = false
					return
				}
				w := word{text: []rune(e.Value)}
				w.after, w.bounded = k.notAfter(rest)
				words = append(words, w)
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
	return words, ok && len(words) > 0
}

// notAfter returns the characters of a negative lookahead of one character class that the path p
// evaluates before anything else, if it does.
func (k *classifier) notAfter(p *cont) (charset, bool) {
	for range wordBudget {
		if p == nil {
			return nil, false
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
			return k.oneChar(e.Expr)
		default:
			return nil, false
		}
	}
	return nil, false
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
// rule reads no variable, so that the call matches as the rule body does. Both checks are
// defensive: the body under a level restriction is an Operators Rule, whose start paths do not
// depend on the level, and variables change matching only through predicates, which no word or
// proven lookahead accepts. They keep the reasoning valid if either of those changes.
func (k *classifier) plainRef(r *grammar.Ref) bool {
	return r.Level == "" && k.byName[r.Name] != nil && len(k.vars[r.Name]) == 0
}

// wordKey is a rule end that a path of a continuation reaches at a position in a word.
type wordKey struct {
	endKey
	at int
}

// wordExcludes reports whether every path of the continuation fails at bounded cost at a position
// where the input begins with the word w. A path reads the text of w as it consumes it; past the
// text, nothing is known and the path is not excluded.
func (k *classifier) wordExcludes(paths []*cont, w word, keep func(*site) bool) bool {
	budget := wordBudget
	live := false
	ended := map[wordKey]bool{}
	var visit func(p *cont, at int)
	visit = func(p *cont, at int) {
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
					key := wordKey{endKey{p.end, p.rule}, at}
					if ended[key] {
						return
					}
					ended[key] = true
				}
				next := func(c *cont) { visit(c, at) }
				switch p.end {
				case endOps:
					k.opsLeads(p.rule, next)
					k.endLeads(p.rule, keep, next)
				case endRule:
					k.endLeads(p.rule, keep, next)
				}
				return
			}
			rest := p.rest()
			it := p.items[0]
			if it == nil {
				p = rest
				continue
			}
			if at == len(w.text) {
				// Past the text nothing is known.
				live = true
				return
			}
			// An item that cannot examine the next character of the text neither consumes input
			// here nor examines more than that character: it fails, or it continues at the same
			// position.
			if !k.first(it).intersects(charOf(w.text[at])) {
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
					visit(rest.then(alt), at)
				}
				return
			case *grammar.Optional:
				visit(rest.then(e.Expr), at)
				visit(rest, at)
				return
			case *grammar.Repeat:
				if e.Max == 0 {
					p = rest
					continue
				}
				again := &grammar.Repeat{SourceInfo: e.SourceInfo, Expr: e.Expr, Min: 0, Max: -1}
				visit(rest.then(e.Expr, again), at)
				if e.Min == 0 {
					visit(rest, at)
				}
				return
			case *grammar.CharClass, *grammar.Any:
				// The item matches the character of the text, which first has shown.
				p, at = rest, at+1
			case *grammar.Literal:
				n, ok := matchText(e, w.text[at:])
				switch {
				case !ok:
					return
				case n < 0:
					live = true
					return
				}
				p, at = rest, at+n
			case *grammar.Not:
				switch k.matchesWord(e.Expr, w, at) {
				case wordMatches:
					return
				case wordFails:
					p = rest
				default:
					live = true
					return
				}
			case *grammar.Ref:
				if !k.plainRef(e) {
					live = true
					return
				}
				if pr := k.operatorsBody(e.Name); pr != nil {
					k.operatorsLeads(pr, e.Name, func(c *cont) { visit(c, at) })
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
		visit(p, 0)
	}
	return !live
}

// matchText matches the literal l at the start of text. It returns the length that l matches and
// true, false when l fails within text, or -1 and true when the text ends before l does.
func matchText(l *grammar.Literal, text []rune) (int, bool) {
	if l.IgnoreCase {
		return -1, true
	}
	lit := []rune(l.Value)
	n := min(len(lit), len(text))
	if !slices.Equal(lit[:n], text[:n]) {
		return 0, false
	}
	if len(lit) > len(text) {
		return -1, true
	}
	return len(lit), true
}

// wordOutcome is what is known of an expression at a position in a word.
type wordOutcome uint8

const (
	wordUnknown wordOutcome = iota
	wordMatches
	wordFails
)

// matchesWord evaluates the expression K at position at in the word w. K must be a choice of
// case-sensitive literals that are not empty, or one such literal, optionally followed by a
// negative lookahead of a character class, through groups, captures, #atomic and calls of rules
// that read no variable and have no level restriction, with nothing else that can depend on how
// the parser reached it: no predicate, other lookahead, cut, recovery, action or level
// restriction. The literals are tried in order against the text; a lookahead after the text
// is known only when it has the characters that cannot follow the word.
func (k *classifier) matchesWord(e grammar.Expr, w word, at int) wordOutcome {
	e, ok := k.wordBody(e)
	if !ok {
		return wordUnknown
	}
	var not charset
	hasNot := false
	if s, ok := e.(*grammar.Seq); ok {
		if len(s.Items) != 2 {
			return wordUnknown
		}
		n, ok := ungroup(s.Items[1]).(*grammar.Not)
		if !ok {
			return wordUnknown
		}
		if not, ok = k.oneChar(n.Expr); !ok {
			return wordUnknown
		}
		hasNot = true
		if e, ok = k.wordBody(s.Items[0]); !ok {
			return wordUnknown
		}
	}
	alts := []grammar.Expr{e}
	if c, ok := e.(*grammar.Choice); ok {
		alts = c.Alts
	}
	text := w.text[at:]
	for _, alt := range alts {
		alt, ok := k.wordBody(alt)
		if !ok {
			return wordUnknown
		}
		l, ok := alt.(*grammar.Literal)
		if !ok || l.Value == "" || l.IgnoreCase {
			return wordUnknown
		}
		n, ok := matchText(l, text)
		switch {
		case !ok:
			continue
		case n < 0:
			return wordUnknown
		case !hasNot:
			return wordMatches
		case n < len(text):
			if not.intersects(charOf(text[n])) {
				return wordFails
			}
			return wordMatches
		case w.bounded && not.equal(w.after):
			return wordMatches
		}
		return wordUnknown
	}
	return wordFails
}

// wordBody returns e through groups, captures, #atomic and calls of rules that read no variable
// and have no level restriction.
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
