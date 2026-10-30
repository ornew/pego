package exec

import (
	"fmt"
	"strings"
)

// String returns a readable listing of the module, for tests, golden files and the verifier's
// messages: its features, components, registers, handle types, operations with their contracts,
// and functions, one statement per line, indented by block.
func String(m *Module) string {
	p := &printer{}
	p.line(0, "module %s", m.Name)
	if len(m.Features) > 0 {
		p.line(0, "features %s", strings.Join(m.Features, ", "))
	}
	for _, c := range m.Components {
		p.line(0, "")
		p.line(0, "component %s", c.Name)
		p.doc(1, c.Doc)
	}
	for _, r := range m.Registers {
		p.line(0, "")
		p.line(0, "register %s %s", r.Name, r.Type)
		p.doc(1, r.Doc)
	}
	for _, h := range m.Handles {
		p.line(0, "")
		p.line(0, "handle %s of %s", h.Name, h.Component)
		p.doc(1, h.Doc)
	}
	for _, o := range m.Ops {
		p.line(0, "")
		p.op(o)
	}
	for _, f := range m.Funcs {
		p.line(0, "")
		p.fn(f)
	}
	return p.b.String()
}

// FuncString returns the listing of one function.
func FuncString(f *Func) string {
	p := &printer{}
	p.fn(f)
	return p.b.String()
}

type printer struct{ b strings.Builder }

func (p *printer) line(depth int, format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if s != "" {
		p.b.WriteString(strings.Repeat("  ", depth))
	}
	p.b.WriteString(s)
	p.b.WriteString("\n")
}

func (p *printer) doc(depth int, doc string) {
	if doc == "" {
		return
	}
	for _, l := range strings.Split(strings.TrimSpace(doc), "\n") {
		p.line(depth, "// %s", l)
	}
}

func params(ps []Param) string {
	s := make([]string, len(ps))
	for i, x := range ps {
		s[i] = x.Name + " " + x.Type.String()
	}
	return strings.Join(s, ", ")
}

func results(ts []Type) string {
	switch len(ts) {
	case 0:
		return ""
	case 1:
		return " " + ts[0].String()
	}
	s := make([]string, len(ts))
	for i, t := range ts {
		s[i] = t.String()
	}
	return " (" + strings.Join(s, ", ") + ")"
}

func (p *printer) op(o *Op) {
	kind := "op"
	if o.Transitional {
		kind = "transitional op"
	}
	p.line(0, "%s %s(%s)%s", kind, o.Name, params(o.Params), results(o.Results))
	p.line(1, "component %s", o.Component)
	if len(o.Reads) > 0 {
		p.line(1, "reads %s", strings.Join(o.Reads, ", "))
	}
	if len(o.Writes) > 0 {
		p.line(1, "writes %s", strings.Join(o.Writes, ", "))
	}
	if len(o.Features) > 0 {
		p.line(1, "features %s", strings.Join(o.Features, ", "))
	}
	if o.NoReturn {
		p.line(1, "noreturn")
	}
	p.line(1, "cost <= %s", o.Cost)
	if o.Contract != "" {
		p.line(1, "contract:")
		p.doc(2, o.Contract)
	}
	if o.Requires != "" {
		p.line(1, "requires:")
		p.doc(2, o.Requires)
	}
	for _, l := range o.Laws {
		p.line(1, "law: %s", l)
	}
}

func (p *printer) fn(f *Func) {
	feat := ""
	if len(f.Features) > 0 {
		feat = " [" + strings.Join(f.Features, ", ") + "]"
	}
	p.doc(0, f.Doc)
	p.line(0, "func %s(%s)%s%s {", f.Name, params(f.Params), results(f.Results), feat)
	for _, l := range f.Locals {
		p.line(1, "var %s %s", l.Name, l.Type)
	}
	p.stmts(f.Body.Stmts, 1)
	p.line(0, "}")
}

func (p *printer) stmts(ss []Stmt, depth int) {
	for _, s := range ss {
		p.stmt(s, depth)
	}
}

func (p *printer) stmt(s Stmt, depth int) {
	switch s := s.(type) {
	case *Block:
		if s.Label != "" {
			p.line(depth, "block %s {", s.Label)
		} else {
			p.line(depth, "{")
		}
		p.stmts(s.Stmts, depth+1)
		p.line(depth, "}")
	case *Loop:
		var clauses []string
		if s.Init != nil {
			clauses = append(clauses, "init "+setString(s.Init))
		}
		if s.Cond != nil {
			clauses = append(clauses, "while "+expr(s.Cond))
		}
		if s.Post != nil {
			clauses = append(clauses, "post "+setString(s.Post))
		}
		head := ""
		if len(clauses) > 0 {
			head = " " + strings.Join(clauses, "; ")
		}
		p.line(depth, "loop %s%s {", s.Label, head)
		p.stmts(s.Body.Stmts, depth+1)
		p.line(depth, "}")
	case *If:
		init := ""
		if s.Init != nil {
			init = setString(s.Init) + "; "
		}
		p.line(depth, "if %s%s {", init, expr(s.Cond))
		p.stmts(s.Then.Stmts, depth+1)
		if s.Else != nil {
			p.line(depth, "} else {")
			p.stmts(s.Else.Stmts, depth+1)
		}
		p.line(depth, "}")
	case *Let:
		p.line(depth, "let %s = %s", strings.Join(s.Names, ", "), expr(s.Value))
	case *Set:
		p.line(depth, "%s", setString(s))
	case *Comment:
		p.line(depth, "// %s", s.Text)
	case *Do:
		p.line(depth, "%s", expr(s.Call))
	case *Exit:
		p.line(depth, "exit %s", s.Label)
	case *Break:
		p.line(depth, "break %s", s.Label)
	case *Continue:
		p.line(depth, "continue %s", s.Label)
	case *Return:
		vs := make([]string, len(s.Values))
		for i, x := range s.Values {
			vs[i] = expr(x)
		}
		p.line(depth, "return %s", strings.Join(vs, ", "))
	case *Assert:
		p.line(depth, "assert %s %q", expr(s.Cond), s.Msg)
	default:
		p.line(depth, "?%T", s)
	}
}

func setString(s *Set) string {
	dst := make([]string, len(s.Dst))
	for i, d := range s.Dst {
		switch {
		case d.Name == "":
			dst[i] = "_"
		case d.Reg:
			dst[i] = "$" + d.Name
		default:
			dst[i] = d.Name
		}
	}
	vals := make([]string, len(s.Values))
	for i, x := range s.Values {
		vals[i] = expr(x)
	}
	return strings.Join(dst, ", ") + " = " + strings.Join(vals, ", ")
}

func expr(x Expr) string {
	switch x := x.(type) {
	case *Const:
		if x.Type.Kind == KHandle {
			return "null(" + x.Type.Handle + ")"
		}
		switch x.Type {
		case TInt, TBool:
			return fmt.Sprint(x.Value)
		}
		return fmt.Sprintf("%s(%v)", x.Type, x.Value)
	case *Local:
		return x.Name
	case *Reg:
		return "$" + x.Name
	case *Feature:
		return "feature(" + x.Name + ")"
	case *Unary:
		return x.Op + expr(x.X)
	case *Binary:
		return "(" + expr(x.X) + " " + x.Op + " " + expr(x.Y) + ")"
	case *Convert:
		return x.To.String() + "(" + expr(x.X) + ")"
	case *Call:
		as := make([]string, len(x.Args))
		for i, a := range x.Args {
			as[i] = expr(a)
		}
		if x.Imm != nil {
			as = append([]string{fmt.Sprintf("[%v]", x.Imm)}, as...)
		}
		return x.Callee + "(" + strings.Join(as, ", ") + ")"
	}
	return fmt.Sprintf("?%T", x)
}
