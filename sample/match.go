package sample

import (
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// status is the result of matching an expression against a prefix of the input that is still being
// generated.
type status int

const (
	// matched: the expression matches, whatever follows.
	matched status = iota
	// failed: the expression does not match, whatever follows.
	failed
	// more: the answer depends on input that has not been generated yet.
	more
	// unknown: the matcher cannot tell (predicates, Pratt expressions, left recursion, cuts,
	// #recover, or too much work); only parsing the whole input can.
	unknown
)

// matchSteps bounds the work of one match.
const matchSteps = 4000

// matcher is a small backtracking PEG matcher over the grammar AST. It is used only to steer the
// generator away from inputs that the parser would read differently (an earlier alternative of an
// ordered choice matching, a greedy repetition taking one more iteration, a negative lookahead
// matching). Its answers are heuristics: a wrong answer costs a retry, never a wrong result,
// because every generated input is verified by the real parser.
type matcher struct {
	in     *info
	text   []byte
	final  bool // text is the whole input
	steps  int
	active map[activeCall]bool
}

type activeCall struct {
	rule *ruleInfo
	pos  int
}

func (in *info) match(e grammar.Expr, text []byte, pos int, final bool) (status, int) {
	m := &matcher{in: in, text: text, final: final}
	return m.match(e, pos)
}

// end is the status of reading past the end of the text.
func (m *matcher) end() status {
	if m.final {
		return failed
	}
	return more
}

func (m *matcher) match(e grammar.Expr, pos int) (status, int) {
	m.steps++
	if m.steps > matchSteps {
		return unknown, pos
	}
	if pos > len(m.text) {
		return m.end(), pos
	}
	switch e := e.(type) {
	case *grammar.Literal:
		rest := m.text[pos:]
		if len(rest) >= len(e.Value) {
			if string(rest[:len(e.Value)]) == e.Value {
				return matched, pos + len(e.Value)
			}
			return failed, pos
		}
		if strings.HasPrefix(e.Value, string(rest)) {
			return m.end(), pos
		}
		return failed, pos
	case *grammar.CharClass:
		if pos == len(m.text) {
			return m.end(), pos
		}
		r, n := utf8.DecodeRune(m.text[pos:])
		if inClass(e, r) {
			return matched, pos + n
		}
		return failed, pos
	case *grammar.Any:
		if pos == len(m.text) {
			return m.end(), pos
		}
		_, n := utf8.DecodeRune(m.text[pos:])
		return matched, pos + n
	case *grammar.Seq:
		for _, it := range e.Items {
			st, p := m.match(it, pos)
			if st != matched {
				return st, p
			}
			pos = p
		}
		return matched, pos
	case *grammar.Choice:
		for _, a := range e.Alts {
			st, p := m.match(a, pos)
			if st != failed {
				return st, p
			}
		}
		return failed, pos
	case *grammar.Repeat:
		n := 0
		for e.Max < 0 || n < e.Max {
			st, p := m.match(e.Expr, pos)
			if st == failed {
				break
			}
			if st != matched {
				return st, p
			}
			if p == pos {
				break
			}
			pos = p
			n++
		}
		if n < e.Min {
			return failed, pos
		}
		return matched, pos
	case *grammar.Optional:
		st, p := m.match(e.Expr, pos)
		if st == failed {
			return matched, pos
		}
		return st, p
	case *grammar.And:
		st, _ := m.match(e.Expr, pos)
		return st, pos
	case *grammar.Not:
		switch st, _ := m.match(e.Expr, pos); st {
		case matched:
			return failed, pos
		case failed:
			return matched, pos
		default:
			return st, pos
		}
	case *grammar.Atomic:
		return m.match(e.Expr, pos)
	case *grammar.Discard:
		return m.match(e.Expr, pos)
	case *grammar.Capture:
		return m.match(e.Expr, pos)
	case *grammar.Attributed:
		for _, a := range e.Attrs {
			if a.Name == "recover" {
				return unknown, pos
			}
		}
		return m.match(e.Expr, pos)
	case *grammar.Ref:
		ri := m.in.rules[e.Name]
		if ri == nil || ri.pratt != nil || e.Level != "" {
			return unknown, pos
		}
		key := activeCall{ri, pos}
		if m.active[key] {
			return unknown, pos // left recursion
		}
		if m.active == nil {
			m.active = map[activeCall]bool{}
		}
		m.active[key] = true
		st, p := m.match(ri.def.Expr, pos)
		delete(m.active, key)
		return st, p
	case *grammar.Top:
		return matched, pos
	case *grammar.Bottom:
		return failed, pos
	case *grammar.BeginInput:
		if pos == 0 {
			return matched, pos
		}
		return failed, pos
	case *grammar.EndInput:
		if pos < len(m.text) {
			return failed, pos
		}
		if m.final {
			return matched, pos
		}
		return more, pos
	case *grammar.BeginLine:
		if pos == 0 || m.text[pos-1] == '\n' {
			return matched, pos
		}
		return failed, pos
	case *grammar.EndLine:
		if pos == len(m.text) {
			if m.final {
				return matched, pos
			}
			return more, pos
		}
		if c := m.text[pos]; c == '\n' || c == '\r' {
			return matched, pos
		}
		return failed, pos
	}
	// Cut, predicates, Pratt expressions.
	return unknown, pos
}

// inClass reports whether the class matches r.
func inClass(c *grammar.CharClass, r rune) bool {
	in := false
	for _, rg := range c.Ranges {
		if rg.Lo <= r && r <= rg.Hi {
			in = true
			break
		}
	}
	return in != c.Negated
}
