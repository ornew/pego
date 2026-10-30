package core

import (
	"fmt"
	"strconv"
	"strings"
)

// Print returns a text form of the module: one line per rule and per node, children indented.
// With attrs, each node line ends with its attributes in brackets.
func Print(m *Module, attrs bool) string {
	p := &printer{m: m, attrs: attrs}
	for _, r := range m.Rules {
		if r != nil {
			p.rule(r)
		}
	}
	return p.b.String()
}

// PrintNode returns the text form of a node, as Print writes it.
func PrintNode(m *Module, n Node, attrs bool) string {
	p := &printer{m: m, attrs: attrs}
	p.node(n, 0)
	return p.b.String()
}

type printer struct {
	m     *Module
	attrs bool
	b     strings.Builder
}

func (p *printer) rule(r *Rule) {
	fmt.Fprintf(&p.b, "rule %d %s", r.ID, r.Name)
	if r.Want {
		p.b.WriteString(" want")
	}
	pol := r.Policy
	for _, f := range []struct {
		on   bool
		name string
	}{{pol.Memo, "memo"}, {pol.Transient, "transient"}, {pol.Plain, "plain"}, {pol.Leader, "leader"}, {pol.Splice, "splice"}, {pol.NoValue, "novalue"}} {
		if f.on {
			p.b.WriteString(" " + f.name)
		}
	}
	if pol.Seen >= 0 {
		fmt.Fprintf(&p.b, " seen=%d", pol.Seen)
	}
	if len(pol.Vars) > 0 {
		fmt.Fprintf(&p.b, " vars=%v", pol.Vars)
	}
	if r.Scope != nil {
		fmt.Fprintf(&p.b, " scope=%v", SlotNames(r.Scope))
	}
	if r.Twin != nil {
		fmt.Fprintf(&p.b, " twin=%d", r.Twin.ID)
	}
	switch r.Finish.Kind {
	case FinishAction:
		p.b.WriteString(" => " + TermString(r.Finish.Action))
	case FinishSplice:
		p.b.WriteString(" splice")
	case FinishScope:
		fmt.Fprintf(&p.b, " scope-value%s", shapeString(r.Finish.Shape))
	}
	if r.Finish.NodeType != "" {
		p.b.WriteString(" as " + r.Finish.NodeType)
	}
	p.b.WriteString("\n")
	p.node(r.Body, 1)
}

func (p *printer) callee(id int) string {
	if p.m != nil && id >= 0 && id < len(p.m.Rules) && p.m.Rules[id] != nil {
		return fmt.Sprintf("%d:%s", id, p.m.Rules[id].Name)
	}
	return strconv.Itoa(id)
}

func (p *printer) line(depth int, format string, args ...any) {
	p.b.WriteString(strings.Repeat("  ", depth))
	fmt.Fprintf(&p.b, format, args...)
}

func (p *printer) end(n Node) {
	if p.attrs {
		p.b.WriteString(" " + attrString(p.m, n.Attrs()))
	}
	p.b.WriteString("\n")
}

func (p *printer) node(n Node, d int) {
	if n == nil {
		p.line(d, "<nil>\n")
		return
	}
	switch n := n.(type) {
	case *Lit:
		if n.Fold {
			p.line(d, "lit %qi", n.Text)
		} else {
			p.line(d, "lit %q", n.Text)
		}
	case *ClassNode:
		p.line(d, "class %s", classString(n.Class))
	case *Any:
		p.line(d, "any")
	case *Anchor:
		p.line(d, "anchor %s", [...]string{"begin-input", "end-input", "begin-line", "end-line"}[n.Kind])
	case *Seq:
		p.line(d, "seq")
	case *Choice:
		p.line(d, "choice")
		p.end(n)
		for _, a := range n.Alts {
			p.line(d+1, "alt")
			if a.Guard != nil {
				p.b.WriteString(" guard=" + firstString(p.m, a.Guard))
				if a.GuardedClass {
					p.b.WriteString(" class")
				}
			}
			p.b.WriteString("\n")
			p.node(a.Body, d+2)
		}
		return
	case *Opt:
		p.line(d, "opt")
	case *Repeat:
		p.line(d, "repeat %d %d", n.Min, n.Max)
		if n.Scope != nil {
			fmt.Fprintf(&p.b, " scope=%v", SlotNames(n.Scope))
		}
		if n.Project != "" {
			p.b.WriteString(" project=" + n.Project)
		}
	case *Look:
		if n.Neg {
			p.line(d, "not")
		} else {
			p.line(d, "and")
		}
	case *Cut:
		p.line(d, "cut")
	case *Succeed:
		p.line(d, "succeed")
	case *FailNode:
		p.line(d, "fail")
	case *Call:
		p.line(d, "call %s", p.callee(n.Rule))
		if n.Level != 0 {
			fmt.Fprintf(&p.b, " level=%d", n.Level)
		}
	case *Nest:
		p.line(d, "nest %s", p.callee(n.Rule))
	case *OperatorsRule:
		p.line(d, "operators")
		p.end(n)
		if n.Skip != nil {
			p.opLine(d+1, "skip", n.Skip)
		}
		for _, l := range n.Operands {
			p.opLine(d+1, "operand", l)
		}
		for _, o := range n.Ops {
			p.opLine(d+1, fmt.Sprintf("op %d %s %s level=%d", o.ID, o.Kind, o.Assoc, o.Level), o.Line)
		}
		return
	case *Text:
		p.line(d, "text")
	case *Drop:
		p.line(d, "drop")
	case *Capture:
		p.line(d, "capture %d %s", n.Slot, n.Name)
		if n.Project != "" {
			p.b.WriteString(" project=" + n.Project)
		}
	case *Pred:
		p.line(d, "pred %s", TermString(n.Term))
	case *BindNode:
		p.line(d, "bind %s = %s", n.Var, TermString(n.Term))
	case *Label:
		if n.Diagnostic {
			p.line(d, "diagnostic %q", p.exp(n.Exp))
		} else {
			p.line(d, "error %q", p.exp(n.Exp))
		}
	case *Recover:
		p.line(d, "recover")
	case *Missing:
		p.line(d, "missing")
	case *Stream:
		p.line(d, "stream")
	case *Scan:
		if n.Class == nil {
			p.line(d, "scan any %d %d", n.Min, n.Max)
		} else {
			p.line(d, "scan %s %d %d", classString(n.Class), n.Min, n.Max)
		}
	case *ClassRun:
		cs := make([]string, len(n.Classes))
		for i, c := range n.Classes {
			cs[i] = classString(c)
		}
		p.line(d, "classrun %s", strings.Join(cs, " "))
	case *Terminal:
		p.line(d, "terminal")
	default:
		p.line(d, "%T", n)
	}
	p.end(n)
	for _, c := range Children(n) {
		p.node(c, d+1)
	}
}

func (p *printer) opLine(d int, head string, l *OpLine) {
	p.line(d, "%s scope=%v%s", head, SlotNames(l.Scope), shapeString(l.Shape))
	if l.Action != nil {
		p.b.WriteString(" => " + TermString(l.Action))
	}
	p.b.WriteString("\n")
	p.node(l.Body, d+1)
}

func attrString(m *Module, a *Attrs) string {
	var xs []string
	if a.Want {
		xs = append(xs, "want"+shapeString(a.Shape))
	}
	if a.Nullable {
		xs = append(xs, "nullable")
	}
	if a.First != nil {
		xs = append(xs, "first="+firstString(m, a.First))
	}
	if e := effectString(a.Eff); e != "" {
		xs = append(xs, e)
	}
	return "[" + strings.Join(xs, " ") + "]"
}

func shapeString(s Shape) string {
	if s == (Shape{}) {
		return ""
	}
	var xs []string
	if s.Elems {
		xs = append(xs, "elems")
	}
	xs = append(xs, fmt.Sprintf("k=%d", s.K), fmt.Sprintf("mode=%d", s.Mode))
	if s.Tuple {
		xs = append(xs, "tuple")
	}
	if s.Shared {
		xs = append(xs, "shared")
	}
	return "(" + strings.Join(xs, ",") + ")"
}

// exp returns the expectation id of the module, or its number if the module has none.
func (p *printer) exp(id ExpID) string { return expString(p.m, id) }

func expString(m *Module, id ExpID) string {
	if m != nil && id >= 0 && int(id) < len(m.Exps) {
		return m.Exps[id]
	}
	return fmt.Sprintf("exp%d", id)
}

func firstString(m *Module, f *First) string {
	s := classString(f.Class)
	if f.Lit {
		s = strconv.QuoteRune(f.Rune)
		if f.Fold {
			s += "i"
		}
	}
	if f.Depth > 0 {
		s += fmt.Sprintf("+%d", f.Depth)
	}
	return s
}

var effectNames = [...]string{"consume", "fail", "expect", "capture", "bind", "recover", "cut", "raise", "call", "commit"}

func effectString(e Effects) string {
	var xs []string
	for i, name := range effectNames {
		if e.Set&(1<<i) != 0 {
			if Effect(1<<i) == EffCapture {
				name = fmt.Sprintf("capture%v", e.Slots)
			}
			xs = append(xs, name)
		}
	}
	return strings.Join(xs, ",")
}

func classString(c *Class) string {
	if c == nil {
		return "<nil>"
	}
	var b strings.Builder
	b.WriteString("[")
	if c.Negated {
		b.WriteString("^")
	}
	for _, r := range c.Ranges {
		if r.Lo == r.Hi {
			b.WriteString(runeString(r.Lo))
		} else {
			b.WriteString(runeString(r.Lo) + "-" + runeString(r.Hi))
		}
	}
	b.WriteString("]")
	return b.String()
}

func runeString(r rune) string {
	if r > ' ' && r < 0x7f && r != '-' && r != ']' && r != '\\' && r != '^' {
		return string(r)
	}
	return fmt.Sprintf("\\u%04x", r)
}

// TermString returns a text form of a term.
func TermString(t Term) string {
	switch t := t.(type) {
	case nil:
		return "<nil>"
	case *Const:
		if s, ok := t.Value.(string); ok {
			return strconv.Quote(s)
		}
		return fmt.Sprint(t.Value)
	case *SlotRef:
		return fmt.Sprintf("$%d:%s", t.Slot, t.Name)
	case *Local:
		return [...]string{"param:", "$lhs", "$rhs", "$op", "$value"}[t.Kind] + localName(t)
	case *VarRef:
		return "var:" + t.Name
	case *Member:
		return TermString(t.X) + "." + t.Name
	case *New:
		fs := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			fs[i] = f.Name + ": " + TermString(f.Value)
		}
		s := t.Type + "{" + strings.Join(fs, ", ") + "}"
		if t.Final {
			s += "!"
		}
		return s
	case *Convert:
		return t.NodeType + "(" + TermString(t.X) + ")"
	case *Tuple:
		return "(" + termList(t.Elems) + ")"
	case *Builtin:
		return t.Func + "(" + termList(t.Args) + ")"
	case *Lambda:
		return "fn(" + strings.Join(t.Params, ", ") + ") " + TermString(t.Body)
	case *Binary:
		return "(" + TermString(t.L) + " " + t.Op + " " + TermString(t.R) + ")"
	case *Unary:
		return t.Op + TermString(t.X)
	case *Cast:
		return TermString(t.X) + ".(" + fmt.Sprint(t.Check) + ")"
	case *TextEq:
		op := "=="
		if t.Neg {
			op = "!="
		}
		return "text-eq(" + TermString(t.L) + " " + op + " " + TermString(t.R) + ")"
	case *FieldCompare:
		return "field-cmp(" + operandString(t.L) + " " + t.Op + " " + operandString(t.R) + ")"
	case *TextIn:
		qs := make([]string, len(t.Texts))
		for i, s := range t.Texts {
			qs[i] = strconv.Quote(s)
		}
		return "text-in(" + TermString(t.X) + ", " + strings.Join(qs, " ") + ")"
	case *ConcatFused:
		ps := make([]string, len(t.Parts))
		for i, p := range t.Parts {
			switch p.Kind {
			case PartList:
				ps[i] = "list(" + termList(p.Args) + ")"
			case PartMap:
				ps[i] = "map(" + TermString(p.X) + ", " + TermString(p.Fn) + ")"
			default:
				ps[i] = "items(" + TermString(p.X) + ")"
			}
		}
		return "concat-fused(" + strings.Join(ps, ", ") + ")"
	case *ProjectedMap:
		return "projected-map(" + TermString(t.X) + ")"
	}
	return fmt.Sprintf("%T", t)
}

func localName(t *Local) string {
	if t.Kind == LocalParam {
		return t.Name
	}
	return ""
}

func termList(ts []Term) string {
	xs := make([]string, len(ts))
	for i, t := range ts {
		xs[i] = TermString(t)
	}
	return strings.Join(xs, ", ")
}

func operandString(o Operand) string {
	if o.Field {
		return TermString(o.X) + "." + o.Name
	}
	return TermString(o.X)
}
