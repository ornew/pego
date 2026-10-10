package sample

import (
	"slices"
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

// prattStableSuccess is private to RHS proofs: success is certain but its final extent needs more
// input. It is propagated through recursive calls without using the witness endpoint as a tail start.
const prattStableSuccess status = unknown + 1

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
	active []activeCall // rule calls in progress, to detect left recursion
}

type activeCall struct {
	rule *ruleInfo
	pos  int
}

// run matches e at pos in text. m.steps is then the work it took.
func (m *matcher) run(e grammar.Expr, text []byte, pos int, final bool) (status, int) {
	m.text, m.final, m.steps, m.active = text, final, 0, m.active[:0]
	return m.match(e, pos)
}

// runPrattStop shares one allowance across skip, selection, scope walks and complete RHS matching.
// Unsupported or unresolved matching cannot justify rejecting a caller continuation.
func (m *matcher) runPrattStop(c *prattCheck, text []byte, pos int, final bool) (status, int) {
	m.text, m.final, m.steps, m.active = text, final, 0, m.active[:0]
	s := c.stop
	st, start := m.prattSkip(s, pos)
	if st != matched {
		return st, pos
	}
	st, best, end := m.prattLongest(s.ops, start)
	if st != matched {
		return st, pos
	}
	owner, work, complete := c.scope.owner(*best, matchSteps-m.steps)
	m.steps += work
	if !complete {
		return unknown, pos
	}
	if owner == nil {
		return failed, pos
	}
	if best.op.Kind == grammar.Postfix {
		return matched, end
	}
	st, end = m.prattExpr(s, prattRHSMin(best), end)
	if st == prattStableSuccess {
		st = matched
	}
	if st != matched {
		return st, pos
	}
	return st, end
}

// prattLongest selects by consumed length, breaking ties in declaration order. A nullable prefix
// or postfix is eligible only when its actual match consumes input. Incomplete competitors defer
// the selection, and cuts or other unsupported parts leave it inconclusive.
func (m *matcher) prattLongest(ops []prattOp, pos int) (status, *prattOp, int) {
	var best *prattOp
	end := pos
	incomplete := false
	for i := range ops {
		o := &ops[i]
		st, p := m.match(o.op.Expr, pos)
		switch st {
		case unknown:
			return unknown, nil, pos
		case more:
			incomplete = true
		case matched:
			if p == pos && o.op.Kind != grammar.Infix {
				continue
			}
			if best == nil || p > end {
				best, end = o, p
			}
		}
	}
	if incomplete {
		return more, nil, pos
	}
	if best == nil {
		return failed, nil, pos
	}
	return matched, best, end
}

// prattSkip ignores a definite skip failure, as the runtime does. The recursive RHS performs its
// own initial skip; stopping a tail restores the position before that tail's skip.
func (m *matcher) prattSkip(s *prattStop, pos int) (status, int) {
	if s.pr.Skip != nil {
		st, end := m.match(s.pr.Skip, pos)
		if st != failed {
			return st, end
		}
	}
	return matched, pos
}

func prattRHSMin(o *prattOp) int {
	if o.op.Assoc == grammar.AssocRight {
		return o.level
	}
	return o.level + 1
}

// prattExpr follows a complete RHS for matcher-supported syntax. Each invocation has fresh none
// state and spends shared work even if its parts are nullable. More/unknown never imply a plain
// RHS failure: a later cut-bearing operator can invalidate an otherwise successful primary.
func (m *matcher) prattExpr(s *prattStop, minLevel, pos int) (status, int) {
	m.steps++
	if m.steps > matchSteps {
		return unknown, pos
	}
	st, start := m.prattSkip(s, pos)
	if st != matched {
		return st, pos
	}
	st, prefix, end := m.prattLongest(s.prefixes, start)
	switch st {
	case more, unknown:
		return st, pos
	case matched:
		st, end = m.prattExpr(s, prefix.level+1, end)
		if st == prattStableSuccess {
			return st, end
		}
		if st == more || st == unknown {
			return st, pos
		}
	}
	if st != matched {
		// A selected ordinary prefix whose whole RHS fails falls back to operands, never to
		// a shorter prefix. Cut-bearing parts are already unknown in prattLongest.
		for _, o := range s.pr.Operands {
			st, end = m.match(o.Expr, start)
			if st != failed {
				break
			}
		}
		if st != matched {
			return st, pos
		}
	}
	lastNone := -1
	for {
		lhsEnd := end
		st, start := m.prattSkip(s, lhsEnd)
		if st != matched {
			return st, pos
		}
		st, op, partEnd := m.prattLongest(s.ops, start)
		switch st {
		case more:
			// Plain led parts cannot make an established primary fail: a failed future
			// uncommitted infix RHS only stops this frame. Do not guess its final extent.
			if s.uncommitted {
				return prattStableSuccess, lhsEnd
			}
			return more, pos
		case unknown:
			return unknown, pos
		case failed:
			return matched, lhsEnd
		}
		if op.level < minLevel || op.op.Assoc == grammar.AssocNone && lastNone == op.level {
			return matched, lhsEnd
		}
		end = partEnd
		if op.op.Kind == grammar.Infix {
			st, end = m.prattExpr(s, prattRHSMin(op), partEnd)
			if st == prattStableSuccess {
				return st, end
			}
			if st == failed {
				// A known uncommitted infix failure ends this invocation before its skip/part.
				return matched, lhsEnd
			}
			if st != matched {
				return st, pos
			}
		}
		lastNone = -1
		if op.op.Kind == grammar.Infix && op.op.Assoc == grammar.AssocNone {
			lastNone = op.level
		}
		if end == lhsEnd {
			return matched, end
		}
	}
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
			n++
			if p == pos {
				break
			}
			pos = p
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
		if slices.Contains(m.active, key) {
			return unknown, pos // left recursion
		}
		m.active = append(m.active, key)
		st, p := m.match(ri.def.Expr, pos)
		m.active = m.active[:len(m.active)-1]
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
