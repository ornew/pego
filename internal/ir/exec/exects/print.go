// Package exects is the TypeScript backend of Exec IR (design 065): it prints the functions of a
// module as methods of a TypeScript class over a target's implementation of the operations.
//
// The printed code is structured as the function is. A labeled block prints as a labeled block
// statement and an Exit to it as a labeled break; a block that declares locals prints in braces.
// An Exit or a Break of a loop prints as a break, and a Continue as a continue, labeled only when
// the loop is not the innermost one. Statements after a jump in the same block are not printed.
// Labels are unique in a printed function.
//
// Values follow the representation that keeps engines on small integers: bool is boolean, int is
// number (exact up to 2^53), i32 and u32 are numbers normalized with "| 0" and ">>> 0" after
// every operation that can leave their range (Math.imul for products), and u64 is bigint reduced
// with BigInt.asUintN. A function with several results returns them as an array.
package exects

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/internal/ir/exec"
)

// Target is how printed code reaches the parser state and the operations of one TypeScript
// implementation.
type Target struct {
	// Op returns the TypeScript expression of a call of an operation, given the call (with its
	// immediate operand) and its printed arguments.
	Op func(c *exec.Call, args []string) string
	// Reg returns the TypeScript expression of the register name, which must be assignable.
	Reg func(name string) string
	// Handle returns the TypeScript type of the handle type name, without null; a handle may be
	// null.
	Handle func(name string) string
	// FuncName returns the name of the printed method name; nil keeps the name.
	FuncName func(name string) string
	// Source, if not empty, names the source of printed functions in their first comment line.
	Source string
}

// Method returns the TypeScript method of the function f of the verified module m, indented for
// a class body by two spaces. Functions of the module are called as methods of this.
func Method(m *exec.Module, f *exec.Func, t *Target) (string, error) {
	p := &printer{m: m, t: t, f: f, labels: map[string]int{}, depth: 1, types: map[string]exec.Type{}}
	for _, x := range append(append([]exec.Param(nil), f.Params...), f.Locals...) {
		p.types[x.Name] = x.Type
	}
	if err := p.fn(); err != nil {
		return "", fmt.Errorf("exects: function %s: %v", f.Name, err)
	}
	return p.b.String(), nil
}

type printer struct {
	// types holds the types of the locals declared so far; a later declaration of a name in a
	// sibling block replaces the earlier one, whose reads are printed by then.
	types  map[string]exec.Type
	m      *exec.Module
	t      *Target
	f      *exec.Func
	b      strings.Builder
	depth  int
	labels map[string]int
	scope  []*frame
	err    error
}

type frame struct {
	label, tsName string
	loop          bool
	used          bool
}

func (p *printer) line(format string, args ...any) {
	p.b.WriteString(strings.Repeat("  ", p.depth))
	fmt.Fprintf(&p.b, format, args...)
	p.b.WriteString("\n")
}

func (p *printer) name(n string) string {
	if p.t.FuncName != nil {
		return p.t.FuncName(n)
	}
	return strings.ReplaceAll(n, ".", "_")
}

func (p *printer) typ(t exec.Type) string {
	switch t.Kind {
	case exec.KBool:
		return "boolean"
	case exec.KInt, exec.KI32, exec.KU32:
		return "number"
	case exec.KU64:
		return "bigint"
	case exec.KHandle:
		return p.t.Handle(t.Handle) + " | null"
	}
	panic("exects: invalid type")
}

func zero(t exec.Type) string {
	switch t.Kind {
	case exec.KBool:
		return "false"
	case exec.KU64:
		return "0n"
	case exec.KHandle:
		return "null"
	}
	return "0"
}

// results returns the result types of the call c.
func (p *printer) results(c *exec.Call) []exec.Type {
	if o := p.m.Op(c.Callee); o != nil {
		return o.Results
	}
	return p.m.Func(c.Callee).Results
}

func (p *printer) resultType() string {
	switch len(p.f.Results) {
	case 0:
		return "void"
	case 1:
		return p.typ(p.f.Results[0])
	}
	rs := make([]string, len(p.f.Results))
	for i, r := range p.f.Results {
		rs[i] = p.typ(r)
	}
	return "[" + strings.Join(rs, ", ") + "]"
}

func (p *printer) fn() error {
	f := p.f
	for _, l := range strings.Split(strings.TrimSpace(f.Doc), "\n") {
		if l != "" {
			p.line("// %s", l)
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
		p.line("//")
	}
	p.line("// %s.", strings.ToUpper(source[:1])+source[1:])
	params := make([]string, len(f.Params))
	for i, x := range f.Params {
		params[i] = x.Name + ": " + p.typ(x.Type)
	}
	p.line("%s(%s): %s {", p.name(f.Name), strings.Join(params, ", "), p.resultType())
	p.depth++
	for _, l := range f.Locals {
		p.line("let %s: %s = %s;", l.Name, p.typ(l.Type), zero(l.Type))
		if !reads(f.Body.Stmts, l.Name) {
			p.line("void %s;", l.Name)
		}
	}
	if f.Body.Label == "" {
		p.stmts(f.Body)
	} else {
		p.block(f.Body)
	}
	p.depth--
	p.line("}")
	return p.err
}

func reads(ss []exec.Stmt, name string) bool {
	found := false
	exec.Walk(&exec.Block{Stmts: ss}, nil, func(x exec.Expr) {
		if l, ok := x.(*exec.Local); ok && l.Name == name {
			found = true
		}
	})
	return found
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

// label returns a TypeScript label for the Exec IR label l that no other block or loop of the
// function has and that is not a reserved word.
func (p *printer) label(l string) string {
	p.labels[l]++
	name := l
	if reserved[l] {
		name += "_"
	}
	if n := p.labels[l]; n > 1 {
		return name + "_" + strconv.Itoa(n)
	}
	return name
}

// reserved holds the reserved words of JavaScript and TypeScript that cannot be labels.
var reserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields("await break case catch class const continue debugger default delete do else enum " +
		"export extends false finally for function if implements import in instanceof interface let new null package " +
		"private protected public return static super switch this throw true try typeof var void while with yield") {
		reserved[w] = true
	}
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

// stmts prints the statements of b in the current block statement.
func (p *printer) stmts(b *exec.Block) {
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
}

// block prints a nested block: as a labeled block statement if a printed jump names its label, in
// braces if it declares locals, and in place otherwise.
func (p *printer) block(b *exec.Block) {
	fr := &frame{label: b.Label}
	labeled := b.Label != "" && exitsTo(b.Stmts, b.Label)
	if labeled {
		fr.tsName = p.label(b.Label)
	}
	p.scope = append(p.scope, fr)
	defer func() { p.scope = p.scope[:len(p.scope)-1] }()
	switch {
	case labeled:
		p.line("%s: {", fr.tsName)
	case declares(b):
		p.line("{")
	default:
		p.stmts(b)
		return
	}
	p.depth++
	p.stmts(b)
	p.depth--
	p.line("}")
}

// exitsTo reports whether a statement of ss that is printed (not after a jump in its block) exits
// to the label.
func exitsTo(ss []exec.Stmt, label string) bool {
	for _, st := range ss {
		switch s := st.(type) {
		case *exec.Exit:
			return s.Label == label
		case *exec.Block:
			if exitsTo(s.Stmts, label) {
				return true
			}
		case *exec.Loop:
			if exitsTo(s.Body.Stmts, label) {
				return true
			}
		case *exec.If:
			if exitsTo(s.Then.Stmts, label) || s.Else != nil && exitsTo(s.Else.Stmts, label) {
				return true
			}
		}
		if jumps(st) {
			return false
		}
	}
	return false
}

func (p *printer) let(l *exec.Let, rest []exec.Stmt) {
	kw := "const"
	names := make([]string, len(l.Names))
	bound := false
	var unread []string
	for i, n := range l.Names {
		switch {
		case reads(rest, n):
			names[i], bound = n, true
			if assigns(rest, n) {
				kw = "let"
			}
		case assigns(rest, n):
			names[i], bound, kw = n, true, "let"
			unread = append(unread, n)
		}
	}
	v := p.expr(l.Value, 0)
	if c, ok := l.Value.(*exec.Call); ok && len(l.Names) > 1 {
		rs := p.results(c)
		for i, n := range l.Names {
			p.types[n] = rs[i]
		}
	} else {
		p.types[l.Names[0]] = p.typeOf(l.Value)
	}
	switch {
	case !bound && !hasCall(l.Value):
		// Nothing reads the value and computing it has no effect.
	case !bound:
		p.line("%s;", v)
	case len(names) == 1:
		p.line("%s %s = %s;", kw, names[0], v)
	default:
		p.line("%s [%s] = %s;", kw, strings.Join(names, ", "), v)
	}
	for _, n := range unread {
		p.line("void %s;", n)
	}
}

func (p *printer) stmt(st exec.Stmt) {
	switch s := st.(type) {
	case *exec.Block:
		p.block(s)
	case *exec.Loop:
		p.loop(s)
	case *exec.If:
		if s.Init != nil {
			p.line("%s;", p.set(s.Init))
		}
		p.ifStmt(s, "if")
	case *exec.Set:
		p.line("%s;", p.set(s))
	case *exec.Do:
		p.line("%s;", p.expr(s.Call, 0))
	case *exec.Exit:
		p.jump(s.Label, "")
	case *exec.Break:
		p.jump(s.Label, "break")
	case *exec.Continue:
		p.jump(s.Label, "continue")
	case *exec.Return:
		switch len(s.Values) {
		case 0:
			p.line("return;")
		case 1:
			p.line("return %s;", p.expr(s.Values[0], 0))
		default:
			vals := make([]string, len(s.Values))
			for i, x := range s.Values {
				vals[i] = p.expr(x, 0)
			}
			p.line("return [%s];", strings.Join(vals, ", "))
		}
	case *exec.Assert:
		p.line("if (!(%s)) throw new Error(%q);", p.expr(s.Cond, 0), "assertion failed: "+s.Msg)
	case *exec.Comment:
		p.line("// %s", s.Text)
	default:
		panic(fmt.Sprintf("exects: statement %T", st))
	}
}

func (p *printer) jump(l, kw string) {
	inner := true
	for i := len(p.scope) - 1; i >= 0; i-- {
		fr := p.scope[i]
		if fr.label == l {
			switch {
			case fr.loop && kw == "continue":
			case fr.loop:
				kw = "break"
			default:
				fr.used = true
				p.line("break %s;", fr.tsName)
				return
			}
			if inner {
				p.line("%s;", kw)
				return
			}
			fr.used = true
			p.line("%s %s;", kw, fr.tsName)
			return
		}
		if fr.loop {
			inner = false
		}
	}
	panic("exects: jump to " + l + " outside it")
}

func (p *printer) loop(s *exec.Loop) {
	fr := &frame{label: s.Label, tsName: p.label(s.Label), loop: true}
	p.scope = append(p.scope, fr)
	mark := p.b.Len()
	var head string
	switch {
	case s.Init != nil || s.Post != nil:
		init, cond, post := "", "", ""
		if s.Init != nil {
			init = p.set(s.Init)
		}
		if s.Cond != nil {
			cond = " " + p.expr(s.Cond, 0)
		}
		if s.Post != nil {
			post = " " + p.set(s.Post)
		}
		head = "for (" + init + ";" + cond + ";" + post + ")"
	case s.Cond != nil:
		head = "while (" + p.expr(s.Cond, 0) + ")"
	default:
		head = "for (;;)"
	}
	p.line("%s {", head)
	p.depth++
	p.blockBody(s.Body)
	p.depth--
	p.line("}")
	p.scope = p.scope[:len(p.scope)-1]
	if fr.used {
		text := p.b.String()
		p.b.Reset()
		p.b.WriteString(text[:mark] + strings.Repeat("  ", p.depth) + fr.tsName + ":\n" + text[mark:])
	}
}

// blockBody prints a block that is the body of a statement with braces of its own: a loop or a
// branch.
func (p *printer) blockBody(b *exec.Block) {
	if b.Label != "" {
		p.block(b)
		return
	}
	p.stmts(b)
}

func (p *printer) ifStmt(s *exec.If, kw string) {
	p.line("%s (%s) {", kw, p.expr(s.Cond, 0))
	p.depth++
	p.blockBody(s.Then)
	p.depth--
	if s.Else != nil && len(s.Else.Stmts) == 1 && s.Else.Label == "" {
		if e, ok := s.Else.Stmts[0].(*exec.If); ok && e.Init == nil {
			p.ifStmt(e, "} else if")
			return
		}
	}
	if s.Else != nil {
		p.line("} else {")
		p.depth++
		p.blockBody(s.Else)
		p.depth--
	}
	p.line("}")
}

func (p *printer) set(s *exec.Set) string {
	dst := make([]string, len(s.Dst))
	for i, d := range s.Dst {
		switch {
		case d.Name == "":
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
	if len(dst) == 1 {
		if dst[0] == "" {
			return vals[0]
		}
		if b, ok := s.Values[0].(*exec.Binary); ok && !s.Dst[0].Reg && s.Values[0] != nil {
			if l, ok := b.X.(*exec.Local); ok && l.Name == s.Dst[0].Name && (b.Op == "+" || b.Op == "-") {
				if c, ok := b.Y.(*exec.Const); ok && c.Type == exec.TInt && c.Value == int64(1) {
					return dst[0] + b.Op + b.Op
				}
			}
		}
		return dst[0] + " = " + vals[0]
	}
	if len(vals) == 1 {
		return "[" + strings.Join(dst, ", ") + "] = " + vals[0]
	}
	return "[" + strings.Join(dst, ", ") + "] = [" + strings.Join(vals, ", ") + "]"
}

// Precedences of JavaScript's binary operators.
var prec = map[string]int{
	"||": 1, "&&": 2, "|": 3, "^": 4, "&": 5,
	"===": 6, "!==": 6, "<": 7, "<=": 7, ">": 7, ">=": 7,
	"<<": 8, ">>": 8, ">>>": 8, "+": 9, "-": 9, "*": 10, "/": 10, "%": 10,
}

func paren(s string, n, outer int) string {
	if n < outer {
		return "(" + s + ")"
	}
	return s
}

func (p *printer) typeOf(x exec.Expr) exec.Type {
	switch x := x.(type) {
	case *exec.Const:
		return x.Type
	case *exec.Local:
		if t, ok := p.types[x.Name]; ok {
			return t
		}
	case *exec.Reg:
		for _, r := range p.m.Registers {
			if r.Name == x.Name {
				return r.Type
			}
		}
	case *exec.Unary:
		return p.typeOf(x.X)
	case *exec.Binary:
		switch x.Op {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return exec.TBool
		}
		return p.typeOf(x.X)
	case *exec.Convert:
		return x.To
	case *exec.Call:
		return p.results(x)[0]
	case *exec.Feature:
		return exec.TBool
	}
	panic(fmt.Sprintf("exects: no type for %T", x))
}

// expr returns the TypeScript expression of x where an operator of precedence outer encloses it.
// An operator or a conversion of constants prints as its value.
func (p *printer) expr(x exec.Expr, outer int) string {
	if c, ok := exec.Fold(x); ok {
		x = c
	}
	switch x := x.(type) {
	case *exec.Const:
		s := constant(x)
		if strings.HasPrefix(s, "-") {
			return paren(s, 11, outer)
		}
		return s
	case *exec.Local:
		return x.Name
	case *exec.Reg:
		return p.t.Reg(x.Name)
	case *exec.Feature:
		panic("exects: feature " + x.Name + " in a printed function; specialize it first")
	case *exec.Unary:
		if x.Op == "!" {
			return "!" + p.expr(x.X, 12)
		}
		t := p.typeOf(x.X)
		neg := "-" + p.expr(x.X, 12)
		switch t.Kind {
		case exec.KI32:
			return paren(paren(neg, 11, 3)+" | 0", 3, outer)
		case exec.KU32:
			return paren(paren(neg, 11, 8)+" >>> 0", 8, outer)
		case exec.KU64:
			return "BigInt.asUintN(64, " + neg + ")"
		}
		return paren(neg, 11, outer)
	case *exec.Binary:
		return p.binary(x, outer)
	case *exec.Convert:
		return p.convert(p.typeOf(x.X), x.To, x.X, outer)
	case *exec.Call:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = p.expr(a, 0)
		}
		if p.m.Op(x.Callee) != nil {
			return p.t.Op(x, args)
		}
		return "this." + p.name(x.Callee) + "(" + strings.Join(args, ", ") + ")"
	}
	panic(fmt.Sprintf("exects: expression %T", x))
}

func (p *printer) binary(x *exec.Binary, outer int) string {
	t := p.typeOf(x.X)
	op := x.Op
	switch op {
	case "==":
		op = "==="
	case "!=":
		op = "!=="
	}
	n := prec[op]
	plain := func() string {
		return paren(p.expr(x.X, n)+" "+op+" "+p.expr(x.Y, n+1), n, outer)
	}
	switch op {
	case "===", "!==", "<", "<=", ">", ">=", "&&", "||":
		return plain()
	}
	switch t.Kind {
	case exec.KInt:
		if op == "/" {
			return "Math.trunc(" + p.expr(x.X, 10) + " / " + p.expr(x.Y, 11) + ")"
		}
		return plain()
	case exec.KI32:
		switch op {
		case "*":
			return "Math.imul(" + p.expr(x.X, 0) + ", " + p.expr(x.Y, 0) + ")"
		case "&", "|", "^", "<<", ">>":
			return plain()
		}
		// +, -, /, %: the result of the number operation, truncated to 32 bits.
		return paren(paren(p.expr(x.X, n)+" "+op+" "+p.expr(x.Y, n+1), n, 3)+" | 0", 3, outer)
	case exec.KU32:
		switch op {
		case "*":
			return paren("Math.imul("+p.expr(x.X, 0)+", "+p.expr(x.Y, 0)+") >>> 0", 8, outer)
		case ">>":
			return paren(p.expr(x.X, 8)+" >>> "+p.expr(x.Y, 9), 8, outer)
		case "%":
			return plain()
		}
		return paren(paren(p.expr(x.X, n)+" "+op+" "+p.expr(x.Y, n+1), n, 8)+" >>> 0", 8, outer)
	case exec.KU64:
		switch op {
		case "/", "%", "&", "|", "^", ">>":
			return plain()
		}
		return "BigInt.asUintN(64, " + p.expr(x.X, n) + " " + op + " " + p.expr(x.Y, n+1) + ")"
	}
	return plain()
}

func (p *printer) convert(from, to exec.Type, x exec.Expr, outer int) string {
	if from == to {
		return p.expr(x, outer)
	}
	if from.Kind == exec.KU64 {
		v := p.expr(x, 0)
		switch to.Kind {
		case exec.KInt:
			return "Number(BigInt.asIntN(64, " + v + "))"
		case exec.KI32:
			return "Number(BigInt.asIntN(32, " + v + "))"
		case exec.KU32:
			return "Number(BigInt.asUintN(32, " + v + "))"
		}
	}
	switch to.Kind {
	case exec.KInt:
		return p.expr(x, outer)
	case exec.KI32:
		return paren(p.expr(x, 4)+" | 0", 3, outer)
	case exec.KU32:
		return paren(p.expr(x, 9)+" >>> 0", 8, outer)
	case exec.KU64:
		return "BigInt.asUintN(64, BigInt(" + p.expr(x, 0) + "))"
	}
	panic("exects: conversion to " + to.String())
}

func constant(c *exec.Const) string {
	switch c.Type.Kind {
	case exec.KBool:
		return strconv.FormatBool(c.Value.(bool))
	case exec.KU64:
		return strconv.FormatUint(c.Value.(uint64), 10) + "n"
	case exec.KHandle:
		return "null"
	}
	return fmt.Sprint(c.Value)
}
