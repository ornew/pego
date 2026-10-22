package engine

import (
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
)

// Ordinary compilation and code generation keep their existing construction
// path. Controlled construction polls without capturing the compile context
// in the returned parser-time matchers.
func firstTerminalControlled(prog *Program, e grammar.Expr, depth int, seen map[string]bool, control *compileControl) (grammar.Expr, int) {
	if control == nil {
		return firstTerminal(prog, e, depth, seen)
	}
	control.poll()
	switch e := e.(type) {
	case *grammar.Literal:
		if e.Value != "" {
			return e, depth
		}
	case *grammar.CharClass:
		return e, depth
	case *grammar.Seq:
		if len(e.Items) > 0 {
			return firstTerminalControlled(prog, e.Items[0], depth, seen, control)
		}
	case *grammar.Capture:
		return firstTerminalControlled(prog, e.Expr, depth, seen, control)
	case *grammar.Atomic:
		return firstTerminalControlled(prog, e.Expr, depth, seen, control)
	case *grammar.Discard:
		return firstTerminalControlled(prog, e.Expr, depth, seen, control)
	case *grammar.Ref:
		r := prog.byName[e.Name]
		if r == nil || r.leader || seen[e.Name] || r.def == nil {
			break
		}
		if _, operatorsRule := r.def.Expr.(*grammar.OperatorsRule); operatorsRule {
			break
		}
		seen[e.Name] = true
		return firstTerminalControlled(prog, r.def.Expr, depth+1, seen, control)
	}
	return nil, 0
}

func charClassStringControlled(e *grammar.CharClass, control *compileControl) string {
	if control == nil {
		return charClassString(e)
	}
	var b strings.Builder
	b.WriteString("[")
	if e.Negated {
		b.WriteString("^")
	}
	esc := func(r rune) string {
		switch r {
		case '-', '[', ']', '^', '\\':
			return `\` + string(r)
		}
		s := strconv.QuoteRune(r)
		return s[1 : len(s)-1]
	}
	for _, rg := range e.Ranges {
		control.poll()
		b.WriteString(esc(rg.Lo))
		if rg.Hi != rg.Lo {
			b.WriteString("-" + esc(rg.Hi))
		}
	}
	b.WriteString("]")
	return b.String()
}

func classAcceptControlled(e *grammar.CharClass, control *compileControl) func(*parser, rune) bool {
	if control == nil || len(e.Ranges) <= 2 {
		return classAccept(e)
	}
	ranges := copyClassRangesControlled(e.Ranges, control)
	neg := e.Negated
	slow := func(r rune) bool {
		for _, rg := range ranges {
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
	}
	ascii := classASCIIControlled(ranges, neg, control)
	return func(p *parser, r rune) bool {
		if uint32(r) < 128 {
			return ascii[r>>6]&(1<<(r&63)) != 0
		}
		if !workControlsEnabled || p.control == nil {
			return slow(r)
		}
		for _, rg := range ranges {
			p.stepWork()
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
	}
}

// Copies and bitmap construction share the compiler invocation's polling
// counter. Only immutable range data and bitmap values reach runtime closures.
func copyClassRangesControlled(ranges []grammar.CharRange, control *compileControl) []grammar.CharRange {
	var result []grammar.CharRange
	if len(ranges) != 0 {
		result = make([]grammar.CharRange, len(ranges))
	}
	for i, rg := range ranges {
		control.poll()
		result[i] = rg
	}
	return result
}

func classASCIIControlled(ranges []grammar.CharRange, neg bool, control *compileControl) (ascii [2]uint64) {
	for r := rune(0); r < 128; r++ {
		matched := false
		for at := 0; at < len(ranges); {
			// Batch to the invocation's next checkpoint. Keep the inner
			// range scan free of a function call per comparison, without
			// allowing more than compilePollStride visits between context checks.
			start := at
			end := min(len(ranges), at+compilePollStride-int(control.steps%compilePollStride))
			for at < end {
				rg := ranges[at]
				at++
				if rg.Lo <= r && r <= rg.Hi {
					matched = true
					break
				}
			}
			control.steps += uint32(at - start)
			if control.steps%compilePollStride == 0 {
				control.check()
			}
			if matched {
				break
			}
		}
		if matched != neg {
			ascii[r>>6] |= 1 << (r & 63)
		}
	}
	return ascii
}
