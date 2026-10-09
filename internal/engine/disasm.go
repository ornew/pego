package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// Disassemble renders the module as human-readable text (for debugging).
func (m *Module) Disassemble() string {
	var b strings.Builder
	str := func(i int) string {
		if i < 0 || i >= len(m.Strings) {
			return fmt.Sprintf("<%d>", i)
		}
		return strconv.Quote(m.Strings[i])
	}
	names := func(ids []int) string {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = m.Strings[id]
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	// Label the start positions of rules and Pratt sections.
	labels := map[int][]string{}
	elabels := map[int][]string{}
	for _, r := range m.Rules {
		name := m.Strings[r.Name]
		labels[r.Entry] = append(labels[r.Entry], name+":")
		if r.Action >= 0 {
			elabels[r.Action] = append(elabels[r.Action], name+".action:")
		}
	}
	line := func(l LineInfo, name string) {
		labels[l.Entry] = append(labels[l.Entry], name+":")
		if l.Action >= 0 {
			elabels[l.Action] = append(elabels[l.Action], name+".action:")
		}
	}
	for i, p := range m.Pratts {
		if p.Skip >= 0 {
			labels[p.Skip] = append(labels[p.Skip], fmt.Sprintf("pratt%d.skip:", i))
		}
		for k, o := range p.Operands {
			line(o, fmt.Sprintf("pratt%d.operand%d", i, k))
		}
		for _, o := range append(append([]OpInfo{}, p.Prefix...), p.Led...) {
			line(o.Line, fmt.Sprintf("pratt%d.op%d", i, o.ID))
		}
	}

	b.WriteString("rules:\n")
	for i, r := range m.Rules {
		fmt.Fprintf(&b, "  %d %s entry=%d scope=%s", i, m.Strings[r.Name], r.Entry, names(r.Scope))
		if r.Action >= 0 {
			fmt.Fprintf(&b, " action=%d", r.Action)
		}
		if r.TerminalType >= 0 {
			fmt.Fprintf(&b, " terminal=%s", m.Strings[r.TerminalType])
		}
		if r.Pratt >= 0 {
			fmt.Fprintf(&b, " pratt=%d", r.Pratt)
		}
		var flags []string
		for _, f := range []struct {
			on   bool
			name string
		}{{r.Memo, "memo"}, {r.Leader, "leader"}, {r.Positional, "positional"}, {r.BodyIsSeq, "seq"}, {r.NoValue, "novalue"}, {r.Transient, "transient"}} {
			if f.on {
				flags = append(flags, f.name)
			}
		}
		if len(flags) > 0 {
			fmt.Fprintf(&b, " %s", strings.Join(flags, ","))
		}
		b.WriteString("\n")
	}
	for i, p := range m.Pratts {
		fmt.Fprintf(&b, "pratt %d: skip=%d operands=%d\n", i, p.Skip, len(p.Operands))
		for _, o := range append(append([]OpInfo{}, p.Prefix...), p.Led...) {
			kind := []string{"prefix", "postfix", "infix"}[o.Kind]
			assoc := []string{"left", "right", "none"}[o.Assoc]
			fmt.Fprintf(&b, "  op%d %s %s level=%d entry=%d action=%d scope=%s\n", o.ID, kind, assoc, o.Level, o.Line.Entry, o.Line.Action, names(o.Line.Scope))
		}
	}
	b.WriteString("code:\n")
	for i, in := range m.Code {
		for _, l := range labels[i] {
			fmt.Fprintf(&b, "%s\n", l)
		}
		fmt.Fprintf(&b, "  %4d  %-10s %s\n", i, in.Op, m.operands(in, str, names))
	}
	b.WriteString("exprs:\n")
	for i, in := range m.Exprs {
		for _, l := range elabels[i] {
			fmt.Fprintf(&b, "%s\n", l)
		}
		fmt.Fprintf(&b, "  %4d  %-10s %s\n", i, in.Op, m.operands(in, str, names))
	}
	return b.String()
}

func (m *Module) operands(in Instr, str func(int) string, names func([]int) string) string {
	a, bb, c := int(in.A), int(in.B), int(in.C)
	switch in.Op {
	case OpStr:
		return fmt.Sprintf("%s build=%d", str(a), c)
	case OpClass:
		return fmt.Sprintf("%s build=%d", str(bb), c)
	case OpAny, OpTop, OpAtomic, OpEndSkip, OpEndRepeat:
		return fmt.Sprintf("build=%d", a)
	case OpAssert:
		return []string{"begin-input", "end-input", "begin-line", "end-line"}[a]
	case OpJump, OpChoice, OpCommit, OpIter, OpRecover, OpEndRecover, EAnd, EOr:
		return fmt.Sprintf("-> %d", a)
	case OpSeq:
		return fmt.Sprintf("n=%d", a)
	case OpCapture:
		if bb == 1 {
			return fmt.Sprintf("slot=%d pop", a)
		}
		return fmt.Sprintf("slot=%d", a)
	case OpGuard:
		return fmt.Sprintf("%s depth=%d -> %d", m.Strings[m.Classes[a].Desc], bb, c) // the expectation as written
	case OpScan, OpScanWide:
		if in.Op == OpScanWide {
			bb, c = int(m.Exprs[bb].integer()), int(m.Exprs[c].integer())
		}
		cl := "any"
		if a >= 0 {
			cl = str(m.Classes[a].Desc)
		}
		return fmt.Sprintf("%s min=%d max=%d", cl, bb, c)
	case OpRepeat, OpRepeatWide:
		if in.Op == OpRepeatWide {
			a, bb = int(m.Exprs[a].integer()), int(m.Exprs[bb].integer())
		}
		sc := "-"
		if c >= 0 {
			sc = names(m.Scopes[c])
		}
		return fmt.Sprintf("min=%d max=%d scope=%s", a, bb, sc)
	case OpNext:
		if bb == 3 {
			return fmt.Sprintf("-> %d mode=field slot=%d", a, c)
		}
		return fmt.Sprintf("-> %d mode=%s", a, []string{"drop", "keep", "stream"}[bb])
	case OpLook:
		if bb == 1 {
			return fmt.Sprintf("not -> %d", a)
		}
		return "and"
	case OpEndLook:
		if a == 1 {
			return "not"
		}
		return "and"
	case OpCall:
		return fmt.Sprintf("%s min=%d keep=%d", m.Strings[m.Rules[a].Name], bb, c)
	case OpPratt:
		return fmt.Sprintf("pratt%d", a)
	case OpPred:
		return fmt.Sprintf("expr=%d", a)
	case OpAssign:
		return fmt.Sprintf("%s expr=%d", m.Strings[a], bb)
	case OpLabel, EStr, EVar, EMember, EBin, EUnary, EBoolChk:
		return str(a)
	case EInt:
		return strconv.FormatInt(int64(a)+int64(bb)<<32, 10)
	case EItem, ELocal, ECap, EListPush:
		return strconv.Itoa(a)
	case EBool:
		return strconv.FormatBool(a == 1)
	case ETextEq:
		if a == 1 {
			return "!="
		}
		return "=="
	case ENew:
		return fmt.Sprintf("%s fields=%s", m.Strings[a], names(m.FieldLists[bb]))
	case EFunc:
		return fmt.Sprintf("entry=%d params=%d", a, bb)
	case ECall:
		return fmt.Sprintf("%s argc=%d", builtinNames[a], bb)
	}
	return ""
}
