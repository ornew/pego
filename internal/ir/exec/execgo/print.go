// Package execgo is the Go backend of Exec IR (design 065): it prints the functions of a module as
// Go source over a target's implementation of the operations.
//
// The printed code is structured as the function is. A block that declares locals prints in braces,
// so that its locals end with it; a labeled block that an Exit leaves prints its label after its
// statements, and the Exit prints as a goto, which never skips a declaration because every local a
// goto could skip is inside braces that end before the label. An Exit or a Break of a loop prints
// as a break, and a Continue as a continue, labeled only when the loop is not the innermost one.
// Statements at an unreachable point are not printed, but their expressions are still formed, so
// that a target that names declarations as it prints them names them in the same order: after a
// jump the point is unreachable up to the end of the block, the point after a labeled block is
// reachable again if a reachable jump exits it, the point after an If is reachable if the point
// before it is, and the point after a loop always is; a loop prints its braces whatever the
// reachability. Labels are unique in a printed function. Arithmetic on i32, u32 and u64 maps to int32, uint32 and uint64, whose operations wrap
// as Exec IR's do; int maps to int.
package execgo

import (
	"fmt"
	"go/format"
	"go/token"
	"strconv"
	"strings"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/pass"
)

// Target is how printed code reaches the parser state and the operations of one Go implementation.
type Target struct {
	// Recv is the receiver of printed functions, such as "p *Parser"; empty prints package
	// functions. A call of a function of the module goes through the receiver's name.
	Recv string
	// Op returns the Go expression of a call of an operation, given the call (with its immediate
	// operand) and its printed arguments. A call of an operation without results is printed as a
	// statement, which may be any simple statement, or nothing when Op returns "".
	Op func(c *exec.Call, args []string) string
	// Stmt, if not nil, prints a call of an operation as statements of its own, given the call, its
	// printed arguments and the places it sets: the names of a Let (define is true) or the places of
	// a Set, with "_" for a discarded result, or none for a call without results. It reports false
	// to print the call with Op.
	Stmt func(c *exec.Call, args []string, dst []string, define bool) (string, bool)
	// Reg returns the Go expression of the register name, which must be assignable.
	Reg func(name string) string
	// Handle returns the Go type of the handle type name.
	Handle func(name string) string
	// FuncName returns the Go name of the printed function name; nil keeps the name.
	FuncName func(name string) string
	// Source, if not empty, names the source of printed functions in their first comment line,
	// such as the package that builds the module.
	Source string
	// Negate, if not nil, returns the Go negation of a call of an operation that the operator !
	// applies to, given its printed arguments, or reports false to print the operator.
	Negate func(c *exec.Call, args []string) (string, bool)
	// Prec, if not nil, returns the precedence of the Go text of a call of an operation: that of its
	// loosest binary operator outside parentheses, or 6 for an operand. Without it, the text of an
	// operation is parenthesized wherever it is an operand, unless it is atomic.
	Prec func(text string) int
	// Pure, if not nil, reports whether evaluating a call of an operation has no effect and cannot
	// panic, for leaving out unread assignments (Body).
	Pure func(c *exec.Call) bool
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

// BodyResult is the printed body of a function (Body).
type BodyResult struct {
	// Text is the statements, unformatted.
	Text string
	// Stores is what pass.DeadStores found about the function's locals.
	Stores *pass.Stores
	// End reports that the end of the body's statements is reachable, before its label.
	End bool
	// Exited reports that a reachable jump exits the body's labeled block, whose label the caller
	// prints.
	Exited bool
}

// Body prints the statements of the body of the function f of the verified module m without the
// function's declaration, its locals or the label of its body, which the caller prints around them,
// as the function of a rule that the generator wraps. Assignments to the locals cand of f that
// nothing reads are left out, and dead places print as the blank identifier (pass.DeadStores, with
// the purity of Target.Pure).
func Body(m *exec.Module, f *exec.Func, t *Target, cand []string) BodyResult {
	st := pass.DeadStores(f, cand, func(x exec.Expr) bool { return Pure(x, t.Pure) })
	p := &printer{m: m, t: t, f: f, labels: map[string]int{}, stores: st, outer: f.Body.Label}
	fr := &frame{label: f.Body.Label, goName: f.Body.Label}
	p.scope = append(p.scope, fr)
	for i, s := range f.Body.Stmts {
		if l, ok := s.(*exec.Let); ok {
			p.let(l, f.Body.Stmts[i+1:])
		} else {
			p.stmt(s)
		}
	}
	return BodyResult{Text: p.b.String(), Stores: st, End: !p.dead, Exited: fr.used}
}

// Pure reports whether evaluating x in Go has no effect and cannot panic: locals, constants,
// registers, negations and the binary operators that cannot panic on operands that are pure, and the
// calls of operations that call reports pure. Comparing for equality can panic on interfaces, and
// dividing and shifting can panic.
func Pure(x exec.Expr, call func(*exec.Call) bool) bool {
	switch x := x.(type) {
	case *exec.Local, *exec.Const, *exec.Reg:
		return true
	case *exec.Unary:
		return (x.Op == "!" || x.Op == "-" || x.Op == "^") && Pure(x.X, call)
	case *exec.Binary:
		switch x.Op {
		case "+", "-", "*", "&", "|", "^", "&^", "&&", "||", "<", ">", "<=", ">=":
			return Pure(x.X, call) && Pure(x.Y, call)
		}
	case *exec.Call:
		return call != nil && call(x)
	}
	return false
}

// Var is a local of a printed function: its name and Go type.
type Var struct{ Name, Type string }

// VarLines declares the locals vs, those of one type together (var x1, x2 int) in the order of the
// first of each type, without the locals in dead, which still give the declarations their order:
// the first local of each type, dead or not, places the type. A declaration that declares a local
// of quiet is followed by the statement that assigns the quiet locals of it to the blank
// identifier.
func VarLines(vs []Var, dead, quiet map[string]bool) string {
	var types []string
	names := map[string][]string{}
	seen := map[string]bool{}
	for _, v := range vs {
		if !seen[v.Type] {
			seen[v.Type] = true
			types = append(types, v.Type)
		}
		if !dead[v.Name] {
			names[v.Type] = append(names[v.Type], v.Name)
		}
	}
	var b strings.Builder
	for _, typ := range types {
		if len(names[typ]) > 0 {
			fmt.Fprintf(&b, "var %s %s\n", strings.Join(names[typ], ", "), typ)
			var q []string
			for _, n := range names[typ] {
				if quiet[n] {
					q = append(q, n)
				}
			}
			if len(q) > 0 {
				fmt.Fprintf(&b, "%s_ = %s\n", strings.Repeat("_, ", len(q)-1), strings.Join(q, ", "))
			}
		}
	}
	return b.String()
}

type printer struct {
	m      *exec.Module
	t      *Target
	f      *exec.Func
	b      strings.Builder
	labels map[string]int // the uses of each label name in the function so far
	// scope is the stack of the enclosing blocks and loops, innermost last.
	scope []*frame
	// dead reports that the current point is unreachable.
	dead bool
	// stores, if not nil, holds the dead locals and the assignments left out (Body).
	stores *pass.Stores
	// outer is the label of the function's body whose label the caller prints (Body), if any.
	outer string
}

// frame is an enclosing block or loop.
type frame struct {
	label  string // the Exec IR label
	goName string // the label printed
	loop   bool
	// used reports that a printed jump names the label.
	used bool
}

// w writes at a reachable point.
func (p *printer) w(format string, args ...any) {
	if !p.dead {
		fmt.Fprintf(&p.b, format, args...)
	}
}

// always writes whatever the reachability.
func (p *printer) always(format string, args ...any) { fmt.Fprintf(&p.b, format, args...) }

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
	// A doc that starts with the function's name starts with its printed name.
	doc := strings.TrimSpace(f.Doc)
	if rest, ok := strings.CutPrefix(doc, f.Name+" "); ok {
		doc = p.name(f.Name) + " " + rest
	}
	for _, l := range strings.Split(doc, "\n") {
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
	p.always("}\n")
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
	}
	p.scope = p.scope[:len(p.scope)-1]
	p.end(fr)
}

// end ends the block of the frame fr: its label follows if a jump named it, and makes the point
// reachable.
func (p *printer) end(fr *frame) {
	if !fr.used {
		return
	}
	p.dead = false
	if fr.label != p.outer {
		p.w("%s:\n", fr.goName)
	}
}

// label returns a Go label for the Exec IR label l that no other block or loop of the function has
// and that is not a keyword.
func (p *printer) label(l string) string {
	p.labels[l]++
	name := l
	if token.IsKeyword(l) {
		name += "_"
	}
	if n := p.labels[l]; n > 1 {
		return name + "_" + strconv.Itoa(n)
	}
	return name
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

// body prints a block that is the body of a statement with braces of its own, a loop or a
// branch: those braces scope its locals.
func (p *printer) body(b *exec.Block) {
	if b.Label != "" {
		p.block(b)
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
		if !hasCall(l.Value) {
			return // nothing reads the value and computing it has no effect
		}
		op = " = "
	}
	if c, ok := l.Value.(*exec.Call); ok && p.opStmt(c, names, declared) {
		return
	}
	p.w("%s%s%s\n", strings.Join(names, ", "), op, p.expr(l.Value, 0))
	for _, n := range unread {
		p.w("_ = %s\n", n)
	}
}

// hasCall reports whether x calls an operation or a function.
func hasCall(x exec.Expr) bool {
	found := false
	exec.Walk(&exec.Block{Stmts: []exec.Stmt{&exec.Assert{Cond: x}}}, nil, func(e exec.Expr) {
		if _, ok := e.(*exec.Call); ok {
			found = true
		}
	})
	return found
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
		dead := p.dead
		if p.stores != nil && p.stores.Removed[s] {
			// The assignment is left out; its values are still formed.
			p.dead = true
		}
		if c, ok := s.Values[0].(*exec.Call); ok && len(s.Values) == 1 && p.opStmt(c, p.places(s), false) {
			p.dead = dead
			return
		}
		p.w("%s\n", p.set(s))
		p.dead = dead
	case *exec.Do:
		if p.opStmt(s.Call, nil, false) {
			return
		}
		if x := p.expr(s.Call, 0); x != "" {
			p.w("%s\n", x)
		}
	case *exec.Exit:
		p.jump(s.Label, "")
		p.dead = true
	case *exec.Break:
		p.jump(s.Label, "break")
		p.dead = true
	case *exec.Continue:
		p.jump(s.Label, "continue")
		p.dead = true
	case *exec.Return:
		vals := make([]string, len(s.Values))
		for i, x := range s.Values {
			vals[i] = p.expr(x, 0)
		}
		p.w("return %s\n", strings.Join(vals, ", "))
		p.dead = true
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
	if p.dead {
		return
	}
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
	// The braces of a loop are printed whatever the reachability.
	p.always("%s {\n", h)
	p.body(s.Body)
	p.always("}\n")
	p.dead = false
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
	dead := p.dead
	p.w("%s%s%s {\n", kw, head, p.expr(s.Cond, 0))
	p.body(s.Then)
	p.dead = dead
	if s.Else != nil && len(s.Else.Stmts) == 1 && s.Else.Label == "" {
		if e, ok := s.Else.Stmts[0].(*exec.If); ok {
			p.ifStmt(e, "} else if ")
			return
		}
	}
	if s.Else != nil {
		p.w("} else {\n")
		p.body(s.Else)
		p.dead = dead
	}
	p.w("}\n")
}

// opStmt prints the call c of an operation with Target.Stmt, if it takes the call, and reports
// whether it did.
func (p *printer) opStmt(c *exec.Call, dst []string, define bool) bool {
	if p.t.Stmt == nil || p.m.Op(c.Callee) == nil {
		return false
	}
	s, ok := p.t.Stmt(c, p.args(c), dst, define)
	if ok && s != "" {
		p.w("%s\n", s)
	}
	return ok
}

// places returns the printed places of the Set s.
func (p *printer) places(s *exec.Set) []string {
	dst := make([]string, len(s.Dst))
	for i, d := range s.Dst {
		switch {
		case d.Name == "":
			dst[i] = "_"
		case d.Reg:
			dst[i] = p.t.Reg(d.Name)
		case p.stores != nil && p.stores.Dead[d.Name]:
			dst[i] = "_"
		default:
			dst[i] = d.Name
		}
	}
	return dst
}

func (p *printer) set(s *exec.Set) string {
	dst := p.places(s)
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
	if c, ok := exec.Fold(x); ok {
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
		if c, ok := x.X.(*exec.Call); ok && x.Op == "!" && p.t.Negate != nil && p.m.Op(c.Callee) != nil {
			if s, ok := p.t.Negate(c, p.args(c)); ok {
				return p.paren(s, outer)
			}
		}
		s := p.expr(x.X, 6)
		if strings.HasPrefix(s, x.Op) || strings.HasPrefix(s, "-") {
			s = "(" + s + ")"
		}
		return x.Op + s
	case *exec.Binary:
		n := prec[x.Op]
		right := n + 1
		if y, ok := x.Y.(*exec.Binary); ok && y.Op == x.Op && (x.Op == "||" || x.Op == "&&") {
			right = n // || and && are associative
		}
		s := p.expr(x.X, n) + " " + x.Op + " " + p.expr(x.Y, right)
		if n < outer {
			return "(" + s + ")"
		}
		return s
	case *exec.Convert:
		return p.typ(x.To) + "(" + p.expr(x.X, 0) + ")"
	case *exec.Call:
		args := p.args(x)
		if p.m.Op(x.Callee) != nil {
			return p.paren(p.t.Op(x, args), outer)
		}
		name := p.name(x.Callee)
		if p.t.Recv != "" {
			name = strings.Fields(p.t.Recv)[0] + "." + name
		}
		return name + "(" + strings.Join(args, ", ") + ")"
	}
	panic(fmt.Sprintf("execgo: expression %T", x))
}

// paren returns the text s of an operation where an operator of precedence outer encloses it,
// parenthesized if it binds less tightly (Target.Prec).
func (p *printer) paren(s string, outer int) string {
	if outer == 0 {
		return s
	}
	if p.t.Prec != nil {
		if p.t.Prec(s) < outer {
			return "(" + s + ")"
		}
		return s
	}
	if !exec.Atomic(s) {
		return "(" + s + ")"
	}
	return s
}

func (p *printer) args(c *exec.Call) []string {
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		args[i] = p.expr(a, 0)
	}
	return args
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
