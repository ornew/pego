package compile

import (
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
)

// FirstTerminalControlled is firstTerminal polling control. Ordinary compilation and code
// generation keep their existing construction path. Controlled construction polls without
// capturing the compile context in the returned parser-time matchers.
func FirstTerminalControlled(prog *Program, e grammar.Expr, depth int, seen map[string]bool, control *Control) (grammar.Expr, int) {
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
			return FirstTerminalControlled(prog, e.Items[0], depth, seen, control)
		}
	case *grammar.Capture:
		return FirstTerminalControlled(prog, e.Expr, depth, seen, control)
	case *grammar.Atomic:
		return FirstTerminalControlled(prog, e.Expr, depth, seen, control)
	case *grammar.Discard:
		return FirstTerminalControlled(prog, e.Expr, depth, seen, control)
	case *grammar.Ref:
		r := prog.ByName[e.Name]
		if r == nil || r.Leader || seen[e.Name] || r.Def == nil {
			break
		}
		if _, operatorsRule := r.Def.Expr.(*grammar.OperatorsRule); operatorsRule {
			break
		}
		seen[e.Name] = true
		return FirstTerminalControlled(prog, r.Def.Expr, depth+1, seen, control)
	}
	return nil, 0
}

func CharClassStringControlled(e *grammar.CharClass, control *Control) string {
	if control == nil {
		return CharClassString(e)
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

// CopyClassRanges returns a copy of ranges, polling control. Copies and bitmap construction share
// the compiler invocation's polling counter. Only immutable range data and bitmap values reach
// runtime closures.
func CopyClassRanges(ranges []grammar.CharRange, control *Control) []grammar.CharRange {
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

func ClassASCII(ranges []grammar.CharRange, neg bool, control *Control) (ascii [2]uint64) {
	for r := rune(0); r < 128; r++ {
		matched := false
		for at := 0; at < len(ranges); {
			// Batch to the invocation's next checkpoint. Keep the inner
			// range scan free of a function call per comparison, without
			// allowing more than compilePollStride visits between context checks.
			start := at
			end := min(len(ranges), at+PollStride-int(control.Steps%PollStride))
			for at < end {
				rg := ranges[at]
				at++
				if rg.Lo <= r && r <= rg.Hi {
					matched = true
					break
				}
			}
			control.Steps += uint32(at - start)
			if control.Steps%PollStride == 0 {
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
