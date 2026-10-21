package engine

import (
	"slices"
	"strings"
)

// RuleTypeWithPoll is RuleType with invocation-owned polling for editor display.
// It does not retain poll. Callback panics propagate unchanged to the caller.
// Individual allocations and string comparisons/concatenations may finish
// before the next callback. A nil callback uses the ordinary type renderer.
func (p *Program) RuleTypeWithPoll(name string, poll func()) (string, bool) {
	if poll == nil {
		return p.RuleType(name)
	}
	poll()
	if p.typed == nil {
		return "", false
	}
	t, ok := p.typed.shown[name]
	if !ok {
		return "", false
	}
	return renderRuleType(t, poll), true
}

func renderRuleType(t ty, poll func()) string {
	poll()
	finish := func(s string) string { poll(); return s }
	term := func(t ty) string {
		s := renderRuleType(t, poll)
		if _, ok := t.(joinTy); ok {
			return "(" + s + ")"
		}
		return s
	}
	switch t := t.(type) {
	case listTy:
		return finish("[]" + term(t.elem))
	case optTy:
		return finish("*" + term(t.elem))
	case joinTy:
		parts := make([]string, len(t.alts))
		for i, alt := range t.alts {
			poll()
			parts[i] = renderRuleType(alt, poll)
		}
		return finish(joinRuleType(parts, " | ", poll))
	case tupleTy:
		parts := make([]string, len(t.elems))
		for i, e := range t.elems {
			poll()
			parts[i] = renderRuleType(e, poll)
		}
		if len(parts) == 1 {
			return finish("(" + parts[0] + ",)")
		}
		return finish("(" + joinRuleType(parts, ", ", poll) + ")")
	case nodeTy:
		if t.name != "" {
			return finish(t.name)
		}
		if len(t.caps) == 0 && t.value == tyTop {
			return finish("node[_]")
		}
		value := renderRuleType(t.value, poll)
		if len(t.caps) == 0 {
			return finish("node[" + value + "]")
		}
		return finish("node[" + value + ", " + t.capsText() + "]")
	case recordTy:
		names := make([]string, 0, len(t.fields))
		for name := range t.fields {
			poll()
			names = append(names, name)
		}
		slices.SortFunc(names, func(a, b string) int { poll(); return strings.Compare(a, b) })
		parts := make([]string, len(names))
		for i, name := range names {
			poll()
			parts[i] = name + ": " + renderRuleType(t.fields[name], poll)
		}
		return finish("node{" + joinRuleType(parts, ", ", poll) + "}")
	default:
		return finish(t.String())
	}
}

func joinRuleType(parts []string, sep string, poll func()) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	maxInt := int(^uint(0) >> 1)
	if len(sep) > 0 && len(parts)-1 > maxInt/len(sep) {
		panic("strings: Join output length overflow")
	}
	n := len(sep) * (len(parts) - 1)
	for _, part := range parts {
		poll()
		if len(part) > maxInt-n {
			panic("strings: Join output length overflow")
		}
		n += len(part)
	}
	var out strings.Builder
	out.Grow(n)
	write := func(s string) {
		for len(s) > 0 {
			poll()
			n := min(4096, len(s))
			out.WriteString(s[:n])
			s = s[n:]
		}
	}
	for i, part := range parts {
		poll()
		if i > 0 {
			write(sep)
		}
		write(part)
	}
	return out.String()
}
