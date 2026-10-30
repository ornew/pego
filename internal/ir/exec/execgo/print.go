// Package execgo is the Go backend of Exec IR (design 065): it prints the functions of a module as
// Go source over a target's implementation of the operations.
//
// The printed code is structured as the function is. A block that declares locals prints in braces,
// so that its locals end with it; a labeled block that an Exit leaves prints its label after its
// statements, and the Exit prints as a goto, which never skips a declaration because every local a
// goto could skip is inside braces that end before the label. An Exit or a Break of a loop prints
// as a break, and a Continue as a continue, labeled only when the loop is not the innermost one.
// Statements after a jump in the same block are not printed. Labels are unique in a printed
// function. Arithmetic on i32, u32 and u64 maps to int32, uint32 and uint64, whose operations wrap
// as Exec IR's do; int maps to int.
package execgo

import (
	"fmt"
	"go/format"
	"strconv"
	"strings"

	"github.com/ornew/pego/internal/ir/exec"
)

// Target is how printed code reaches the parser state and the operations of one Go implementation.
type Target struct {
	// Recv is the receiver of printed functions, such as "p *Parser"; empty prints package
	// functions. A call of a function of the module goes through the receiver's name.
	Recv string
	// Op returns the Go expression of a call of an operation, given the call (with its immediate
	// operand) and its printed arguments. A call of an operation without results is printed as a
	// statement, so its expression must be a call.
	Op func(c *exec.Call, args []string) string
	// Reg returns the Go expression of the register name, which must be assignable.
	Reg func(name string) string
	// Handle returns the Go type of the handle type name.
	Handle func(name string) string
	// FuncName returns the Go name of the printed function name; nil keeps the name.
	FuncName func(name string) string
	// Source, if not empty, names the source of printed functions in their first comment line,
	// such as the package that builds the module.
	Source string
}

// Func returns the Go declaration of the function f of the verified module m, formatted.
func Func(m *exec.Module, f *exec.Func, t *Target) (string, error) {
	p := &printer{m: m, t: t, f: f, labels: map[string]int{}}
	src := p.fn()
	out, err := format.Source([]byte(src))
	if err != nil {
		return "", fmt.Errorf("execgo: function %s: %v\n%s", f.Name, err, src)
	}
	return string(out), nil
}

type printer struct {
	m      *exec.Module
	t      *Target
	f      *exec.Func
	b      strings.Builder
	labels map[string]int // the uses of each label name in the function so far
	// scope is the stack of the enclosing blocks and loops, innermost last.
	scope []*frame
}

// frame is an enclosing block or loop.
type frame struct {
	label  string // the Exec IR label
	goName string // the label printed
	loop   bool
	// used reports that a printed jump names the label.
	used bool
}

func (p *printer) w(format string, args ...any) { fmt.Fprintf(&p.b, format, args...) }

func (p *printer) name(n string) string {
	if p.t.FuncName != nil {
		return p.t.FuncName(n)
	}
	return n
}

func (p *printer) typ(t exec.Type) string {
	switch t.Kind {
	case exec.KBool:
		return "bool"
	case exec.KInt:
		return "int"
	case exec.KI32:
		return "int32"
	case exec.KU32:
		return "uint32"
	case exec.KU64:
		return "uint64"
	case exec.KHandle:
		return p.t.Handle(t.Handle)
	}
	panic("execgo: invalid type")
}

func (p *printer) fn() string {
	f := p.f
	for _, l := range strings.Split(strings.TrimSpace(f.Doc), "\n") {
		if l != "" {
			p.w("// %s\n", l)
		}
	}
	source := "printed from Exec IR function " + f.Name
	if p.t.Source != "" {
		source += " of " + p.t.Source
	}
	if len(f.Features) > 0 {
		source += ", specialized for " + strings.Join(f.Features, ", ")
	}
	if f.Doc != "" {
		p.w("//\n")
	}
	p.w("// %s.\n", strings.ToUpper(source[:1])+source[1:])
	p.w("func ")
	if p.t.Recv != "" {
		p.w("(%s) ", p.t.Recv)
	}
	params := make([]string, len(f.Params))
	for i, x := range f.Params {
		params[i] = x.Name + " " + p.typ(x.Type)
	}
	p.w("%s(%s)", p.name(f.Name), strings.Join(params, ", "))
	switch len(f.Results) {
	case 0:
	case 1:
		p.w(" %s", p.typ(f.Results[0]))
	default:
		rs := make([]string, len(f.Results))
		for i, r := range f.Results {
			rs[i] = p.typ(r)
		}
		p.w(" (%s)", strings.Join(rs, ", "))
	}
	p.w(" {\n")
	for _, l := range f.Locals {
		p.w("var %s %s\n", l.Name, p.typ(l.Type))
		if !reads(f.Body.Stmts, l.Name) {
			p.w("_ = %s\n", l.Name)
		}
	}
	if f.Body.Label == "" {
		p.stmts(f.Body)
	} else {
		p.block(f.Body)
	}
	p.w("}\n")
	return p.b.String()
}

// reads reports whether the statements read the local name.
func reads(ss []exec.Stmt, name string) bool {
	found := false
	exec.Walk(&exec.Block{Stmts: ss}, nil, func(x exec.Expr) {
		if l, ok := x.(*exec.Local); ok && l.Name == name {
			found = true
		}
	})
	return found
}

// stmts prints the statements of the block b in place, with b's label after them if a jump
// named it.
func (p *printer) stmts(b *exec.Block) {
	fr := &frame{label: b.Label}
	if b.Label != "" {
		fr.goName = p.label(b.Label)
	}
	p.scope = append(p.scope, fr)
	for i, st := range b.Stmts {
		if l, ok := st.(*exec.Let); ok {
			p.let(l, b.Stmts[i+1:])
		} else {
			p.stmt(st)
		}
		if jumps(st) {
			break
		}
	}
	p.scope = p.scope[:len(p.scope)-1]
	if fr.used {
		p.w("%s:\n", fr.goName)
	}
}

// label returns a Go label for the Exec IR label l that no other block or loop of the function has.
func (p *printer) label(l string) string {
	p.labels[l]++
	if n := p.labels[l]; n > 1 {
		return l + "_" + strconv.Itoa(n)
	}
	return l
}

func jumps(st exec.Stmt) bool {
	switch st.(type) {
	case *exec.Exit, *exec.Break, *exec.Continue, *exec.Return:
		return true
	}
	return false
}

func declares(b *exec.Block) bool {
	for _, st := range b.Stmts {
		if _, ok := st.(*exec.Let); ok {
			return true
		}
	}
	return false
}

// block prints a nested block: in braces if it declares locals.
func (p *printer) block(b *exec.Block) {
	if declares(b) {
		// The label of a braced block follows its braces.
		inner := &exec.Block{Stmts: b.Stmts}
		fr := &frame{label: b.Label}
		if b.Label != "" {
			fr.goName = p.label(b.Label)
		}
		p.scope = append(p.scope, fr)
		p.w("{\n")
		p.stmts(inner)
		p.w("}\n")
		p.scope = p.scope[:len(p.scope)-1]
		if fr.used {
			p.w("%s:\n", fr.goName)
		}
		return
	}
	p.stmts(b)
}

// let prints the declaration l, which the statements rest follow in its block: a name that
// nothing reads prints as the blank identifier, as Go requires.
func (p *printer) let(l *exec.Let, rest []exec.Stmt) {
	names := make([]string, len(l.Names))
	declared := false
	var unread []string
	for i, n := range l.Names {
		names[i] = "_"
		switch {
		case reads(rest, n):
			names[i], declared = n, true
		case assigns(rest, n):
			names[i], declared = n, true
			unread = append(unread, n)
		}
	}
	op := " := "
	if !declared {
		op = " = "
	}
	p.w("%s%s%s\n", strings.Join(names, ", "), op, p.expr(l.Value, 0))
	for _, n := range unread {
		p.w("_ = %s\n", n)
	}
}

// assigns reports whether the statements assign the local name.
func assigns(ss []exec.Stmt, name string) bool {
	found := false
	exec.Walk(&exec.Block{Stmts: ss}, func(st exec.Stmt) {
		if s, ok := st.(*exec.Set); ok {
			for _, d := range s.Dst {
				if !d.Reg && d.Name == name {
					found = true
				}
			}
		}
	}, nil)
	return found
}

func (p *printer) stmt(st exec.Stmt) {
	switch s := st.(type) {
	case *exec.Block:
		p.block(s)
	case *exec.Loop:
		p.loop(s)
	case *exec.If:
		p.ifStmt(s, "if ")
	case *exec.Set:
		p.w("%s\n", p.set(s))
	case *exec.Do:
		p.w("%s\n", p.expr(s.Call, 0))
	case *exec.Exit:
		p.jump(s.Label, "")
	case *exec.Break:
		p.jump(s.Label, "break")
	case *exec.Continue:
		p.jump(s.Label, "continue")
	case *exec.Return:
		vals := make([]string, len(s.Values))
		for i, x := range s.Values {
			vals[i] = p.expr(x, 0)
		}
		p.w("return %s\n", strings.Join(vals, ", "))
	case *exec.Assert:
		p.w("if !(%s) {\npanic(%q)\n}\n", p.expr(s.Cond, 0), "assertion failed: "+s.Msg)
	case *exec.Comment:
		p.w("// %s\n", s.Text)
	default:
		panic(fmt.Sprintf("execgo: statement %T", st))
	}
}

// jump prints an exit (kw empty), break or continue of the block or loop labeled l.
func (p *printer) jump(l, kw string) {
	inner := true // no loop lies between the jump and its target
	for i := len(p.scope) - 1; i >= 0; i-- {
		fr := p.scope[i]
		if fr.label == l {
			switch {
			case fr.loop && kw == "continue":
			case fr.loop:
				kw = "break"
			default:
				fr.used = true
				p.w("goto %s\n", fr.goName)
				return
			}
			if inner {
				p.w("%s\n", kw)
				return
			}
			fr.used = true
			p.w("%s %s\n", kw, fr.goName)
			return
		}
		if fr.loop {
			inner = false
		}
	}
	panic("execgo: jump to " + l + " outside it")
}

func (p *printer) loop(s *exec.Loop) {
	fr := &frame{label: s.Label, goName: p.label(s.Label), loop: true}
	p.scope = append(p.scope, fr)
	mark := p.b.Len()
	var head []string
	if s.Init != nil || s.Post != nil {
		init, post := "", ""
		if s.Init != nil {
			init = p.set(s.Init)
		}
		cond := ""
		if s.Cond != nil {
			cond = p.expr(s.Cond, 0)
		}
		if s.Post != nil {
			post = p.set(s.Post)
		}
		head = append(head, init+"; "+cond+"; "+post)
	} else if s.Cond != nil {
		head = append(head, p.expr(s.Cond, 0))
	}
	h := "for"
	if len(head) > 0 {
		h += " " + head[0]
	}
	p.w("%s {\n", h)
	p.block(s.Body)
	p.w("}\n")
	p.scope = p.scope[:len(p.scope)-1]
	if fr.used {
		// A labeled break or continue names the loop: its label goes before it.
		text := p.b.String()
		p.b.Reset()
		p.b.WriteString(text[:mark] + fr.goName + ":\n" + text[mark:])
	}
}

func (p *printer) ifStmt(s *exec.If, kw string) {
	head := ""
	if s.Init != nil {
		head = p.set(s.Init) + "; "
	}
	p.w("%s%s%s {\n", kw, head, p.expr(s.Cond, 0))
	p.block(s.Then)
	if s.Else != nil && len(s.Else.Stmts) == 1 && s.Else.Label == "" {
		if e, ok := s.Else.Stmts[0].(*exec.If); ok {
			p.ifStmt(e, "} else if ")
			return
		}
	}
	if s.Else != nil {
		p.w("} else {\n")
		p.block(s.Else)
	}
	p.w("}\n")
}

func (p *printer) set(s *exec.Set) string {
	dst := make([]string, len(s.Dst))
	for i, d := range s.Dst {
		switch {
		case d.Name == "":
			dst[i] = "_"
		case d.Reg:
			dst[i] = p.t.Reg(d.Name)
		default:
			dst[i] = d.Name
		}
	}
	vals := make([]string, len(s.Values))
	for i, x := range s.Values {
		vals[i] = p.expr(x, 0)
	}
	if len(dst) == 1 && len(vals) == 1 {
		if b, ok := s.Values[0].(*exec.Binary); ok {
			// x = x + 1 prints as x++, x = x - 1 as x--.
			if l, ok := b.X.(*exec.Local); ok && !s.Dst[0].Reg && l.Name == s.Dst[0].Name {
				if c, ok := b.Y.(*exec.Const); ok && fmt.Sprint(c.Value) == "1" && (b.Op == "+" || b.Op == "-") {
					return dst[0] + b.Op + b.Op
				}
			}
		}
	}
	return strings.Join(dst, ", ") + " = " + strings.Join(vals, ", ")
}

// Precedences of Go's binary operators.
var prec = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3,
	"+": 4, "-": 4, "|": 4, "^": 4,
	"*": 5, "/": 5, "%": 5, "<<": 5, ">>": 5, "&": 5,
}

// expr returns the Go expression of x where an operator of precedence outer encloses it. An
// operator or a conversion of constants prints as its value: Go evaluates constant expressions
// exactly and rejects one that overflows, where Exec IR wraps.
func (p *printer) expr(x exec.Expr, outer int) string {
	if c, ok := fold(x); ok {
		x = c
	}
	switch x := x.(type) {
	case *exec.Const:
		return constant(x)
	case *exec.Local:
		return x.Name
	case *exec.Reg:
		return p.t.Reg(x.Name)
	case *exec.Feature:
		panic("execgo: feature " + x.Name + " in a printed function; specialize it first")
	case *exec.Unary:
		s := p.expr(x.X, 6)
		if strings.HasPrefix(s, x.Op) || strings.HasPrefix(s, "-") {
			s = "(" + s + ")"
		}
		return x.Op + s
	case *exec.Binary:
		n := prec[x.Op]
		s := p.expr(x.X, n) + " " + x.Op + " " + p.expr(x.Y, n+1)
		if n < outer {
			return "(" + s + ")"
		}
		return s
	case *exec.Convert:
		return p.typ(x.To) + "(" + p.expr(x.X, 0) + ")"
	case *exec.Call:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = p.expr(a, 0)
		}
		if p.m.Op(x.Callee) != nil {
			return p.t.Op(x, args)
		}
		name := p.name(x.Callee)
		if p.t.Recv != "" {
			name = strings.Fields(p.t.Recv)[0] + "." + name
		}
		return name + "(" + strings.Join(args, ", ") + ")"
	}
	panic(fmt.Sprintf("execgo: expression %T", x))
}

// fold returns the value of x if x is an operator or a conversion whose operands are constants,
// evaluated as Exec IR evaluates it. A division by zero is not folded.
func fold(x exec.Expr) (*exec.Const, bool) {
	switch x := x.(type) {
	case *exec.Const:
		return x, true
	case *exec.Unary:
		c, ok := fold(x.X)
		if !ok {
			return nil, false
		}
		if x.Op == "!" {
			return &exec.Const{Type: exec.TBool, Value: !c.Value.(bool)}, true
		}
		return &exec.Const{Type: c.Type, Value: exec.Apply("-", exec.ConvertValue(int64(0), c.Type), c.Value)}, true
	case *exec.Binary:
		a, ok1 := fold(x.X)
		b, ok2 := fold(x.Y)
		if !ok1 || !ok2 || a.Type.Kind == exec.KHandle {
			return nil, false
		}
		switch x.Op {
		case "&&":
			return &exec.Const{Type: exec.TBool, Value: a.Value.(bool) && b.Value.(bool)}, true
		case "||":
			return &exec.Const{Type: exec.TBool, Value: a.Value.(bool) || b.Value.(bool)}, true
		case "/", "%":
			if b.Value == exec.ConvertValue(int64(0), b.Type) {
				return nil, false
			}
		}
		v := exec.Apply(x.Op, a.Value, b.Value)
		t := a.Type
		if _, isBool := v.(bool); isBool {
			t = exec.TBool
		}
		return &exec.Const{Type: t, Value: v}, true
	case *exec.Convert:
		c, ok := fold(x.X)
		if !ok {
			return nil, false
		}
		return &exec.Const{Type: x.To, Value: exec.ConvertValue(c.Value, x.To)}, true
	}
	return nil, false
}

func constant(c *exec.Const) string {
	switch c.Type.Kind {
	case exec.KBool:
		return strconv.FormatBool(c.Value.(bool))
	case exec.KInt:
		return strconv.FormatInt(c.Value.(int64), 10)
	case exec.KI32:
		return fmt.Sprintf("int32(%d)", c.Value)
	case exec.KU32:
		return fmt.Sprintf("uint32(%d)", c.Value)
	case exec.KU64:
		return fmt.Sprintf("uint64(%d)", c.Value)
	case exec.KHandle:
		return "nil"
	}
	panic("execgo: constant of an invalid type")
}
