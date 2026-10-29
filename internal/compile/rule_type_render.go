package compile

import (
	"slices"
	"strings"
)

// RuleTypeWithPoll is RuleTypeWithPoll.
func (p *Program) RuleTypeWithPoll(name string, poll func()) (string, bool) {
	if poll == nil {
		return p.RuleType(name)
	}
	poll()
	if p.Typed == nil {
		return "", false
	}
	t, ok := p.Typed.Shown[name]
	if !ok {
		return "", false
	}
	return RenderRuleType(t, poll), true
}

func RenderRuleType(t Ty, poll func()) string {
	poll()
	finish := func(s string) string { poll(); return s }
	term := func(t Ty) string {
		s := RenderRuleType(t, poll)
		if _, ok := t.(JoinTy); ok {
			return "(" + s + ")"
		}
		return s
	}
	switch t := t.(type) {
	case ListTy:
		return finish("[]" + term(t.Elem))
	case OptTy:
		return finish("*" + term(t.Elem))
	case JoinTy:
		parts := make([]string, len(t.Alts))
		for i, alt := range t.Alts {
			poll()
			parts[i] = RenderRuleType(alt, poll)
		}
		return finish(joinRuleType(parts, " | ", poll))
	case TupleTy:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			poll()
			parts[i] = RenderRuleType(e, poll)
		}
		if len(parts) == 1 {
			return finish("(" + parts[0] + ",)")
		}
		return finish("(" + joinRuleType(parts, ", ", poll) + ")")
	case NodeTy:
		if t.Name != "" {
			return finish(t.Name)
		}
		if len(t.Caps) == 0 && t.Value == TyTop {
			return finish("node[_]")
		}
		value := RenderRuleType(t.Value, poll)
		if len(t.Caps) == 0 {
			return finish("node[" + value + "]")
		}
		return finish("node[" + value + ", " + t.capsText() + "]")
	case RecordTy:
		names := make([]string, 0, len(t.Fields))
		for name := range t.Fields {
			poll()
			names = append(names, name)
		}
		slices.SortFunc(names, func(a, b string) int { poll(); return strings.Compare(a, b) })
		parts := make([]string, len(names))
		for i, name := range names {
			poll()
			parts[i] = name + ": " + RenderRuleType(t.Fields[name], poll)
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
