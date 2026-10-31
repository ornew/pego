package exects

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/zeroparse/internal/ir/exec"
	"github.com/ornew/zeroparse/internal/ir/exec/pass"
)

// Grammar code lowered from Flow IR prints with Body, by the conventions of the TypeScript printer
// of Flow IR, so that the code printed from Exec IR is the code printed from Flow IR.
//
// A block whose label a printed jump names prints as a labeled block statement; any other block
// prints its statements in place. A loop prints as a for or while statement, labeled if a printed
// jump names its label. An Exit and a Break print as a labeled break, and a Continue as a labeled
// continue. Statements after a jump are not printed up to the end of the block, or up to the end of
// the labeled block a reachable jump exits; the point after an If is reachable if the point before
// it is, and the point after a loop is reachable unless the loop is unconditional and no reachable
// jump names its label. A label is named (BodyTarget.Label) where its first jump prints, and a
// Continue names its label even at an unreachable point. The operations print at unreachable points
// too, with their text left out, so that a target that names declarations as it prints them names
// them in the same order; an assignment at an unreachable point is not formed.

// BodyTarget is how Body prints the operations of lowered grammar code. Each hook takes whether the
// point is unreachable, where the text it returns is not printed.
type BodyTarget struct {
	// Reg returns the TypeScript expression of the register name, which must be assignable.
	Reg func(name string) string
	// Label returns the TypeScript label of the Exec IR label name; Body calls it where the label
	// prints and where a jump to it prints.
	Label func(name string) string
	// Op returns the TypeScript expression of a call of an operation.
	Op func(c *exec.Call, dead bool) string
	// Do returns the statements of a call of an operation that a Do makes, each ending with a
	// newline. args returns the printed arguments.
	Do func(c *exec.Call, args func() []string, dead bool) string
	// Set, if not nil, returns the statements of an assignment of one call of an operation, each
	// ending with a newline, or reports false to print the assignment as Body does. init reports
	// the Init of an If, which is formed at an unreachable point too.
	Set func(s *exec.Set, init, dead bool) (string, bool)
	// Test, if not nil, returns the condition of an If with an Init, which then is not printed
	// apart, or reports false to print the Init before the If.
	Test func(s *exec.If, dead bool) (string, bool)
	// Pure reports whether evaluating a call of an operation has no effect, for leaving out unread
	// assignments.
	Pure func(c *exec.Call) bool
}

// BodyResult is the printed body of a function (Body).
type BodyResult struct {
	// Text is the statements, indented by two spaces for each nested statement.
	Text string
	// End reports that the end of the body's statements is reachable, before its label.
	End bool
	// Exited reports that a reachable jump exits the body's labeled block, whose label the caller
	// prints.
	Exited bool
}

// Body prints the statements of the body of the function f of the module m, as the grammar code of
// a TypeScript function whose declaration, locals and returns the caller prints. A body with a
// label prints without it, and the caller prints the labeled block around the statements if the
// body is exited (BodyResult.Exited). st is what pass.DeadStores found about the locals of f, with
// the purity of Pure: assignments it removes are left out, and the places of locals that no
// statement that stays reads are left out of the assignments that stay, with their values
// printed alone unless pure.
//
// The values of a function's Return are a value and a constant bool: true returns the value, and
// false returns undefined.
func Body(m *exec.Module, f *exec.Func, t *BodyTarget, st *pass.Stores) BodyResult {
	p := &bodyPrinter{m: m, t: t, st: st, used: map[string]bool{}}
	p.stmts(f.Body.Stmts)
	return BodyResult{Text: p.b.String(), End: !p.dead, Exited: f.Body.Label != "" && p.used[f.Body.Label]}
}

// Pure reports whether evaluating x in TypeScript has no effect: locals, constants, registers, the
// operator ! and the binary operators other than division and shifts on operands that are pure, and
// the calls of operations that call reports pure.
func Pure(x exec.Expr, call func(*exec.Call) bool) bool {
	switch x := x.(type) {
	case *exec.Local, *exec.Const, *exec.Reg:
		return true
	case *exec.Unary:
		return x.Op == "!" && Pure(x.X, call)
	case *exec.Binary:
		switch x.Op {
		case "+", "-", "&&", "||", "==", "!=", "<", ">", "<=", ">=":
			return Pure(x.X, call) && Pure(x.Y, call)
		}
	case *exec.Call:
		return call != nil && call(x)
	}
	return false
}

type bodyPrinter struct {
	m  *exec.Module
	t  *BodyTarget
	st *pass.Stores
	b  strings.Builder
	// dead reports that the current point is unreachable; used holds the labels that printed jumps
	// name.
	dead bool
	used map[string]bool
}

// write writes s at a reachable point.
func (p *bodyPrinter) write(s string) {
	if !p.dead {
		p.b.WriteString(s)
	}
}

// scoped prints the statements that body prints into a text of their own and returns it.
func (p *bodyPrinter) scoped(body func()) string {
	outer := p.b.String()
	p.b.Reset()
	body()
	out := p.b.String()
	p.b.Reset()
	p.b.WriteString(outer)
	return out
}

func (p *bodyPrinter) stmts(ss []exec.Stmt) {
	for _, s := range ss {
		p.stmt(s)
	}
}

func (p *bodyPrinter) stmt(s exec.Stmt) {
	switch s := s.(type) {
	case *exec.Block:
		enter := p.dead
		body := p.scoped(func() { p.stmts(s.Stmts) })
		end := p.dead
		p.dead = enter
		if s.Label != "" && p.used[s.Label] {
			p.write(fmt.Sprintf("%s: {\n%s}\n", p.t.Label(s.Label), indentTS(body)))
			p.dead = false
		} else {
			p.write(body)
			p.dead = end
		}
	case *exec.Loop:
		p.loop(s)
	case *exec.If:
		p.ifStmt(s)
	case *exec.Exit:
		p.jump("break", s.Label)
	case *exec.Break:
		p.jump("break", s.Label)
	case *exec.Continue:
		// The label is named even at an unreachable point.
		p.used[s.Label] = true
		p.write("continue " + p.t.Label(s.Label) + ";\n")
		p.dead = true
	case *exec.Set:
		if p.dead || p.st.Removed[s] {
			return
		}
		if p.t.Set != nil {
			if text, ok := p.t.Set(s, false, false); ok {
				p.write(text)
				return
			}
		}
		p.write(p.setText(s))
	case *exec.Do:
		dead := p.dead
		p.write(p.t.Do(s.Call, func() []string { return p.args(s.Call) }, dead))
	case *exec.Return:
		if p.dead {
			return
		}
		if len(s.Values) != 2 {
			panic(fmt.Sprintf("exects: a return of %d values in grammar code", len(s.Values)))
		}
		c, ok := s.Values[1].(*exec.Const)
		if !ok || c.Type != exec.TBool {
			panic("exects: a return whose bool is not a constant in grammar code")
		}
		if c.Value.(bool) {
			p.write("return " + p.expr(s.Values[0]) + ";\n")
		} else {
			p.write("return undefined;\n")
		}
		p.dead = true
	case *exec.Comment:
		p.write("// " + s.Text + "\n")
	default:
		panic(fmt.Sprintf("exects: statement %T in grammar code", s))
	}
}

// jump prints a break to the label at a reachable point.
func (p *bodyPrinter) jump(kw, label string) {
	if p.dead {
		return
	}
	p.used[label] = true
	p.write(kw + " " + p.t.Label(label) + ";\n")
	p.dead = true
}

func (p *bodyPrinter) loop(s *exec.Loop) {
	var head string
	switch {
	case s.Init != nil || s.Post != nil:
		var init, cond, post string
		if s.Init != nil {
			init = p.assignment(s.Init)
		}
		if s.Cond != nil {
			cond = p.expr(s.Cond)
		}
		if s.Post != nil {
			post = p.assignment(s.Post)
		}
		head = "for (" + init + "; " + cond + "; " + post + ")"
	case s.Cond != nil:
		head = "while (" + p.expr(s.Cond) + ")"
	default:
		head = "for (;;)"
	}
	enter := p.dead
	body := p.scoped(func() { p.stmts(s.Body.Stmts) })
	label := ""
	if p.used[s.Label] {
		label = p.t.Label(s.Label) + ": "
	}
	if !enter {
		p.b.WriteString(label + head + " {\n" + indentTS(body) + "}\n")
	}
	p.dead = enter || (s.Cond == nil && s.Init == nil && s.Post == nil && !p.used[s.Label])
}

func (p *bodyPrinter) ifStmt(s *exec.If) {
	dead := p.dead
	var init, cond string
	tested := false
	if s.Init != nil && p.t.Test != nil {
		cond, tested = p.t.Test(s, dead)
	}
	if !tested {
		if s.Init != nil {
			// The Init is formed at an unreachable point too.
			text, ok := "", false
			if p.t.Set != nil {
				text, ok = p.t.Set(s.Init, true, dead)
			}
			if !ok {
				text = p.setText(s.Init)
			}
			init = text
		}
		cond = p.expr(s.Cond)
	}
	then := p.scoped(func() { p.stmts(s.Then.Stmts) })
	p.dead = dead
	var els string
	if s.Else != nil && len(s.Else.Stmts) > 0 {
		els = p.scoped(func() { p.stmts(s.Else.Stmts) })
		p.dead = dead
	}
	if dead {
		return
	}
	p.b.WriteString(init)
	if els == "" && strings.Count(then, "\n") == 1 {
		p.b.WriteString("if (" + cond + ") " + then) // one statement
		return
	}
	p.b.WriteString("if (" + cond + ") {\n" + indentTS(then) + "}")
	if els != "" {
		p.b.WriteString(" else {\n" + indentTS(els) + "}")
	}
	p.b.WriteString("\n")
}

// read reports whether a statement that stays reads the local name.
func (p *bodyPrinter) read(name string) bool { return p.st.Read[name] }

// setText returns the statements of the assignment s, one for each place, or "" if none stays: a
// place of a local that nothing reads is left out, with its value printed alone unless pure.
func (p *bodyPrinter) setText(s *exec.Set) string {
	if len(s.Dst) != len(s.Values) {
		panic("exects: an assignment of a call with several results in grammar code")
	}
	var parts []string
	for i, d := range s.Dst {
		x := s.Values[i]
		switch {
		case d.Reg:
			parts = append(parts, p.t.Reg(d.Name)+" = "+p.expr(x))
		case d.Name != "" && p.read(d.Name):
			parts = append(parts, p.assignment(&exec.Set{Dst: []exec.Place{d}, Values: []exec.Expr{x}}))
		case !Pure(x, p.t.Pure):
			parts = append(parts, p.expr(x))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ";\n") + ";\n"
}

// assignment returns the expression of the assignment s of one value: an increment of a local by
// one prints as ++.
func (p *bodyPrinter) assignment(s *exec.Set) string {
	d, x := s.Dst[0], s.Values[0]
	dst := d.Name
	if d.Reg {
		dst = p.t.Reg(d.Name)
	} else if b, ok := x.(*exec.Binary); ok && b.Op == "+" {
		if l, ok := b.X.(*exec.Local); ok && l.Name == d.Name {
			if c, ok := b.Y.(*exec.Const); ok && c.Type == exec.TInt && c.Value == int64(1) {
				return dst + "++"
			}
		}
	}
	return dst + " = " + p.expr(x)
}

func (p *bodyPrinter) args(c *exec.Call) []string {
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		args[i] = p.expr(a)
	}
	return args
}

// expr returns the TypeScript expression of x: a disjunction in parentheses, a comparison without,
// and a negation of anything but a local with its operand in parentheses.
func (p *bodyPrinter) expr(x exec.Expr) string {
	switch x := x.(type) {
	case *exec.Local:
		return x.Name
	case *exec.Reg:
		return p.t.Reg(x.Name)
	case *exec.Const:
		switch x.Type.Kind {
		case exec.KBool:
			return strconv.FormatBool(x.Value.(bool))
		case exec.KHandle:
			return "null"
		case exec.KInt:
			return fmt.Sprint(x.Value)
		}
	case *exec.Unary:
		if x.Op == "!" {
			inner := p.expr(x.X)
			if _, ok := x.X.(*exec.Local); ok {
				return "!" + inner
			}
			return "!" + wrapTS(inner)
		}
	case *exec.Binary:
		op := x.Op
		switch op {
		case "||":
			return "(" + p.expr(x.X) + " || " + p.expr(x.Y) + ")"
		case "==":
			op = "==="
		case "!=":
			op = "!=="
		}
		return p.expr(x.X) + " " + op + " " + p.expr(x.Y)
	case *exec.Call:
		if p.m.Op(x.Callee) == nil {
			panic("exects: a call of function " + x.Callee + " in grammar code")
		}
		return p.t.Op(x, p.dead)
	}
	panic(fmt.Sprintf("exects: expression %T in grammar code", x))
}

// wrapTS returns cond in parentheses, unless it starts and ends with them.
func wrapTS(cond string) string {
	if strings.HasPrefix(cond, "(") && strings.HasSuffix(cond, ")") {
		return cond
	}
	return "(" + cond + ")"
}

// indentTS indents each line of s by two spaces.
func indentTS(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, l := range strings.SplitAfter(s, "\n") {
		if l != "" {
			b.WriteString("  ")
			b.WriteString(l)
		}
	}
	return b.String()
}
