package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

// Reading and writing modules (.pegoc version 2) and validating loaded modules.

func (w *encoder) ints(xs []int) {
	w.uint(len(xs))
	for _, x := range xs {
		w.int(x)
	}
}

func (w *encoder) code(code []Instr) {
	w.uint(len(code))
	for _, in := range code {
		w.byte(byte(in.Op))
		w.int(int(in.A))
		w.int(int(in.B))
		w.int(int(in.C))
	}
}

func (w *encoder) line(l *LineInfo) {
	w.int(l.Entry)
	w.ints(l.Scope)
	w.int(l.Action)
	w.bool(l.IsSeq)
}

func (w *encoder) module(m *Module) {
	w.uint(len(m.Strings))
	w.uint(len(m.Classes))
	for _, c := range m.Classes {
		w.bool(c.Negated)
		w.int(c.Desc)
		w.uint(len(c.Ranges) / 2)
		for _, r := range c.Ranges {
			w.int(int(r))
		}
	}
	w.uint(len(m.Types))
	for _, t := range m.Types {
		w.int(t.Name)
		w.bool(t.Terminal)
		w.ints(t.Fields)
	}
	for _, lists := range [][][]int{m.FieldLists, m.Scopes} {
		w.uint(len(lists))
		for _, l := range lists {
			w.ints(l)
		}
	}
	w.uint(len(m.Rules))
	for _, r := range m.Rules {
		w.int(r.Name)
		w.int(r.Entry)
		w.ints(r.Scope)
		w.int(r.Action)
		w.bool(r.BodyIsSeq)
		w.int(r.TerminalType)
		w.bool(r.Memo)
		w.bool(r.Leader)
		w.bool(r.Positional)
		w.int(r.Pratt)
		w.bool(r.NoValue)
		w.bool(r.Transient)
		w.ints(r.Vars)
	}
	w.uint(len(m.Pratts))
	for _, p := range m.Pratts {
		w.int(p.Skip)
		w.uint(len(p.Operands))
		for i := range p.Operands {
			w.line(&p.Operands[i])
		}
		for _, ops := range [][]OpInfo{p.Prefix, p.Led} {
			w.uint(len(ops))
			for i := range ops {
				o := &ops[i]
				w.int(o.ID)
				w.int(o.Kind)
				w.int(o.Assoc)
				w.int(o.Level)
				w.line(&o.Line)
			}
		}
	}
	w.code(m.Code)
	w.code(m.Exprs)
}

// int32 reads an instruction operand.
func (r *decoder) int32() int32 {
	n := r.int()
	if n < -1<<31 || n >= 1<<31 {
		r.fail("integer %d out of range", n)
		return 0
	}
	return int32(n)
}

func (r *decoder) ints() []int {
	n := r.count()
	xs := make([]int, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		xs = append(xs, int(r.int32()))
	}
	return xs
}

func (r *decoder) code() []Instr {
	n := r.count()
	code := make([]Instr, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		code = append(code, Instr{Op: Op(r.byte()), A: r.int32(), B: r.int32(), C: r.int32()})
	}
	return code
}

func (r *decoder) line() LineInfo {
	return LineInfo{Entry: int(r.int32()), Scope: r.ints(), Action: int(r.int32()), IsSeq: r.bool()}
}

func (r *decoder) module() *Module {
	m := &Module{}
	n := r.uint()
	if r.err == nil && n > len(r.strs) {
		r.fail("module has %d strings, file has %d", n, len(r.strs))
	}
	if r.err != nil {
		return m
	}
	m.Strings = r.strs[:n]
	n = r.count()
	for i := 0; i < n && r.err == nil; i++ {
		c := Class{Negated: r.bool(), Desc: int(r.int32())}
		k := r.count()
		for j := 0; j < 2*k && r.err == nil; j++ {
			c.Ranges = append(c.Ranges, rune(r.int32()))
		}
		m.Classes = append(m.Classes, c)
	}
	n = r.count()
	for i := 0; i < n && r.err == nil; i++ {
		m.Types = append(m.Types, TypeInfo{Name: int(r.int32()), Terminal: r.bool(), Fields: r.ints()})
	}
	for _, lists := range []*[][]int{&m.FieldLists, &m.Scopes} {
		n = r.count()
		for i := 0; i < n && r.err == nil; i++ {
			*lists = append(*lists, r.ints())
		}
	}
	n = r.count()
	for i := 0; i < n && r.err == nil; i++ {
		m.Rules = append(m.Rules, RuleInfo{
			Name: int(r.int32()), Entry: int(r.int32()), Scope: r.ints(), Action: int(r.int32()),
			BodyIsSeq: r.bool(), TerminalType: int(r.int32()),
			Memo: r.bool(), Leader: r.bool(), Positional: r.bool(), Pratt: int(r.int32()), NoValue: r.bool(), Transient: r.bool(), Vars: r.ints(),
		})
	}
	n = r.count()
	for i := 0; i < n && r.err == nil; i++ {
		p := PrattInfo{Skip: int(r.int32())}
		k := r.count()
		for j := 0; j < k && r.err == nil; j++ {
			p.Operands = append(p.Operands, r.line())
		}
		for _, ops := range []*[]OpInfo{&p.Prefix, &p.Led} {
			k = r.count()
			for j := 0; j < k && r.err == nil; j++ {
				o := OpInfo{ID: int(r.int32()), Kind: int(r.int32()), Assoc: int(r.int32()), Level: int(r.int32())}
				o.Line = r.line()
				*ops = append(*ops, o)
			}
		}
		m.Pratts = append(m.Pratts, p)
	}
	m.Code = r.code()
	m.Exprs = r.code()
	return m
}

// validateModule checks that every table and instruction reference in the module is in range
// and that jumps go forward (only the NEXT of a repetition jumps backward). It does not check
// that the instruction sequence uses the value stack and the entry stack correctly.
func validateModule(m *Module) error {
	var err error
	fail := func(format string, args ...any) {
		if err == nil {
			err = fmt.Errorf(format, args...)
		}
	}
	in := func(what string, i, n int) {
		if i < 0 || i >= n {
			fail("%s %d out of range", what, i)
		}
	}
	opt := func(what string, i, n int) {
		if i != -1 {
			in(what, i, n)
		}
	}
	str := func(i int) { in("string", i, len(m.Strings)) }
	strs := func(xs []int) {
		for _, x := range xs {
			str(x)
		}
	}
	nonneg := func(i int32) {
		if i < 0 {
			fail("negative operand %d", i)
		}
	}
	for _, c := range m.Classes {
		if len(c.Ranges)%2 != 0 {
			fail("invalid class")
		}
		str(c.Desc)
	}
	for _, t := range m.Types {
		str(t.Name)
		strs(t.Fields)
	}
	for _, l := range m.FieldLists {
		strs(l)
	}
	for _, l := range m.Scopes {
		strs(l)
	}
	line := func(l *LineInfo) {
		in("entry", l.Entry, len(m.Code))
		strs(l.Scope)
		opt("action", l.Action, len(m.Exprs))
	}
	for _, r := range m.Rules {
		str(r.Name)
		in("entry", r.Entry, len(m.Code))
		strs(r.Scope)
		opt("action", r.Action, len(m.Exprs))
		opt("string", r.TerminalType, len(m.Strings))
		opt("pratt", r.Pratt, len(m.Pratts))
		strs(r.Vars)
	}
	for _, p := range m.Pratts {
		opt("entry", p.Skip, len(m.Code))
		for i := range p.Operands {
			line(&p.Operands[i])
		}
		for _, ops := range [][]OpInfo{p.Prefix, p.Led} {
			for i := range ops {
				o := &ops[i]
				in("operator kind", o.Kind, 3)
				in("associativity", o.Assoc, 3)
				line(&o.Line)
			}
		}
	}
	forward := func(ip int, l int32, n int) {
		if int(l) <= ip || int(l) >= n {
			fail("invalid jump from %d to %d", ip, l)
		}
	}
	flag := func(b int32, max int32) {
		if b < 0 || b > max {
			fail("invalid flag %d", b)
		}
	}
	for ip, x := range m.Code {
		n := len(m.Code)
		switch x.Op {
		case OpStr:
			str(int(x.A))
			str(int(x.B))
			flag(x.C, 1)
		case OpClass:
			in("class", int(x.A), len(m.Classes))
			str(int(x.B))
			flag(x.C, 1)
		case OpAny, OpTop, OpAtomic, OpEndRepeat, OpEndLook, OpEndSkip:
			flag(x.A, 1)
		case OpAssert:
			flag(x.A, 3)
		case OpFail, OpCut, OpPushPos, OpPushNil, OpEndLabel, OpReturn, OpEnd:
		case OpJump, OpChoice, OpCommit, OpIter, OpRecover, OpEndRecover:
			forward(ip, x.A, n)
		case OpLook:
			flag(x.B, 1)
			if x.B == 1 { // a positive lookahead does not use the jump target
				forward(ip, x.A, n)
			}
		case OpNext:
			if x.A < 0 || int(x.A) >= ip {
				fail("invalid loop from %d to %d", ip, x.A)
			}
			flag(x.B, 2)
		case OpSeq:
			nonneg(x.A)
		case OpCapture:
			nonneg(x.A)
			flag(x.B, 1)
		case OpRepeat:
			nonneg(x.A)
			if x.B < -1 {
				fail("invalid maximum %d", x.B)
			}
			opt("scope", int(x.C), len(m.Scopes))
		case OpCall:
			in("rule", int(x.A), len(m.Rules))
			nonneg(x.B)
			flag(x.C, 1)
		case OpPratt:
			in("pratt", int(x.A), len(m.Pratts))
		case OpPred:
			in("expression", int(x.A), len(m.Exprs))
		case OpAssign:
			str(int(x.A))
			in("expression", int(x.B), len(m.Exprs))
		case OpLabel:
			str(int(x.A))
		case OpScan:
			opt("class", int(x.A), len(m.Classes))
			nonneg(x.B)
			if x.C < -1 {
				fail("invalid maximum %d", x.C)
			}
		default:
			fail("invalid instruction %d at %d", x.Op, ip)
		}
	}
	for ip, x := range m.Exprs {
		switch x.Op {
		case EInt, ENil, ERet, ETextChk, EListBegin, EMapPush, EListEnd:
		case ETextEq:
			flag(x.A, 1)
		case EListPush:
			nonneg(x.A)
		case EBool:
			flag(x.A, 1)
		case EStr, EVar, EMember, EBin, EUnary, EBoolChk:
			str(int(x.A))
		case ECap, EItem, ELocal:
			nonneg(x.A)
		case ENew:
			str(int(x.A))
			in("field list", int(x.B), len(m.FieldLists))
		case EAnd, EOr:
			forward(ip, x.A, len(m.Exprs))
		case EFunc:
			forward(ip, x.A, len(m.Exprs))
			nonneg(x.B)
		case ECall:
			in("builtin", int(x.A), len(builtinNames))
			nonneg(x.B)
		default:
			fail("invalid expression instruction %d at %d", x.Op, ip)
		}
	}
	return err
}

// moduleProgram builds a program without an AST (it can only be run by the bytecode backend).
func moduleProgram(m *Module) *Program {
	prog := &Program{byName: map[string]*rule{}, types: map[string]grammar.TypeSpec{}}
	for _, t := range m.Types {
		if t.Terminal {
			prog.types[m.Strings[t.Name]] = &grammar.TerminalSpec{}
			continue
		}
		st := &grammar.StructSpec{}
		for _, f := range t.Fields {
			st.Fields = append(st.Fields, &grammar.Field{Name: m.Strings[f]})
		}
		prog.types[m.Strings[t.Name]] = st
	}
	prog.moduleOnce.Do(func() { prog.module = m })
	return prog
}
