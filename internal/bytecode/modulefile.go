package bytecode

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

// Reading and writing modules and validating loaded modules.

func (w *Encoder) ints(xs []int) {
	w.Uint(len(xs))
	for _, x := range xs {
		w.Int(x)
	}
}

func (w *Encoder) code(code []Instr) {
	w.Uint(len(code))
	for _, in := range code {
		w.Byte(byte(in.Op))
		w.Int(int(in.A))
		w.Int(int(in.B))
		w.Int(int(in.C))
	}
}

func (w *Encoder) line(l *LineInfo) {
	w.Int(l.Entry)
	w.ints(l.Scope)
	w.Int(l.Action)
	w.Bool(l.IsSeq)
}

// WriteModule writes m.
func (w *Encoder) WriteModule(m *Module) {
	w.Uint(len(m.Strings))
	w.Bool(m.SourceInfo)
	w.Bool(m.DefaultValue)
	w.Uint(len(m.Classes))
	for _, c := range m.Classes {
		w.Bool(c.Negated)
		w.Int(c.Desc)
		w.Uint(len(c.Ranges) / 2)
		for _, r := range c.Ranges {
			w.Int(int(r))
		}
	}
	w.Uint(len(m.Types))
	for _, t := range m.Types {
		w.Int(t.Name)
		w.ints(t.Fields)
		w.Int(t.Node)
	}
	for _, lists := range [][][]int{m.FieldLists, m.Scopes} {
		w.Uint(len(lists))
		for _, l := range lists {
			w.ints(l)
		}
	}
	w.Uint(len(m.Vars))
	for _, v := range m.Vars {
		w.Int(v.Name)
		w.Int(v.Type)
		w.Int(v.Int)
		w.Int(v.Str)
	}
	w.Uint(len(m.Rules))
	for _, r := range m.Rules {
		w.Int(r.Name)
		w.Int(r.Entry)
		w.ints(r.Scope)
		w.Int(r.Action)
		w.Int(r.NodeType)
		w.Bool(r.Memo)
		w.Bool(r.Leader)
		w.Bool(r.Positional)
		w.Int(r.OperatorsRule)
		w.Bool(r.NoValue)
		w.Bool(r.Transient)
		w.Bool(r.Rec)
		w.Int(r.Where)
		w.Bool(r.Splice)
		w.ints(r.Vars)
	}
	w.ints(m.Starts)
	w.Int(m.DefaultStart)
	w.Uint(len(m.OperatorsRules))
	for _, p := range m.OperatorsRules {
		w.Int(p.Skip)
		w.Uint(len(p.Operands))
		for i := range p.Operands {
			w.line(&p.Operands[i])
		}
		for _, ops := range [][]OpInfo{p.Prefix, p.Led} {
			w.Uint(len(ops))
			for i := range ops {
				o := &ops[i]
				w.Int(o.ID)
				w.Int(o.Kind)
				w.Int(o.Assoc)
				w.Int(o.Level)
				w.line(&o.Line)
			}
		}
	}
	w.code(m.Code)
	w.code(m.Exprs)
}

// int32 reads an instruction operand.
func (r *Decoder) int32() int32 {
	if r.Err != nil {
		return 0
	}
	n, k := binary.Varint(r.Data[r.Pos:])
	if k <= 0 {
		r.Fail("invalid integer")
		return 0
	}
	r.Pos += k
	if n < -1<<31 || n >= 1<<31 {
		r.Fail("integer %d out of range", n)
		return 0
	}
	return int32(n)
}

func (r *Decoder) ints() []int {
	n := r.Count()
	xs := make([]int, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		xs = append(xs, int(r.int32()))
	}
	return xs
}

func (r *Decoder) code() []Instr {
	n := r.Count()
	code := make([]Instr, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		code = append(code, Instr{Op: Op(r.Byte()), A: r.int32(), B: r.int32(), C: r.int32()})
	}
	return code
}

func (r *Decoder) line() LineInfo {
	return LineInfo{Entry: int(r.int32()), Scope: r.ints(), Action: int(r.int32()), IsSeq: r.Bool()}
}

// ReadModule reads a module.
func (r *Decoder) ReadModule() *Module {
	m := &Module{}
	n := r.Uint()
	if r.Err == nil && n > len(r.Strs) {
		r.Fail("module has %d strings, file has %d", n, len(r.Strs))
	}
	if r.Err != nil {
		return m
	}
	m.Strings = r.Strs[:n]
	m.SourceInfo = r.Bool()
	m.DefaultValue = r.Bool()
	n = r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		c := Class{Negated: r.Bool(), Desc: int(r.int32())}
		k := r.Count()
		for j := 0; j < 2*k && r.Err == nil; j++ {
			c.Ranges = append(c.Ranges, rune(r.int32()))
		}
		m.Classes = append(m.Classes, c)
	}
	n = r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		m.Types = append(m.Types, TypeInfo{Name: int(r.int32()), Fields: r.ints(), Node: int(r.int32())})
	}
	for _, lists := range []*[][]int{&m.FieldLists, &m.Scopes} {
		n = r.Count()
		for i := 0; i < n && r.Err == nil; i++ {
			*lists = append(*lists, r.ints())
		}
	}
	n = r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		m.Vars = append(m.Vars, VarInfo{Name: int(r.int32()), Type: int(r.int32()), Int: r.Int(), Str: int(r.int32())})
	}
	n = r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		ri := RuleInfo{
			Name: int(r.int32()), Entry: int(r.int32()), Scope: r.ints(), Action: int(r.int32()),
			NodeType: int(r.int32()),
			Memo:     r.Bool(), Leader: r.Bool(), Positional: r.Bool(), OperatorsRule: int(r.int32()), NoValue: r.Bool(), Transient: r.Bool(), Rec: r.Bool(), Where: int(r.int32()), Splice: r.Bool(), Vars: r.ints(),
		}
		m.Rules = append(m.Rules, ri)
	}
	m.Starts = r.ints()
	m.DefaultStart = int(r.int32())
	n = r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		p := OperatorsRuleInfo{Skip: int(r.int32())}
		k := r.Count()
		for j := 0; j < k && r.Err == nil; j++ {
			p.Operands = append(p.Operands, r.line())
		}
		for _, ops := range []*[]OpInfo{&p.Prefix, &p.Led} {
			k = r.Count()
			for j := 0; j < k && r.Err == nil; j++ {
				o := OpInfo{ID: int(r.int32()), Kind: int(r.int32()), Assoc: int(r.int32()), Level: int(r.int32())}
				o.Line = r.line()
				*ops = append(*ops, o)
			}
		}
		m.OperatorsRules = append(m.OperatorsRules, p)
	}
	m.Code = r.code()
	m.Exprs = r.code()
	return m
}

// ValidateModule checks that every table and instruction reference in the module is in range
// and that jumps go forward (only the NEXT of a repetition jumps backward).
// Recovery regions must not enclose a stream commitment. Other value-stack
// and entry-stack invariants are not checked. fieldType checks the text of a field type, which
// the type system above the module defines.
func ValidateModule(m *Module, fieldType func(text string) error) error {
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
		opt("string", t.Node, len(m.Strings))
		if err == nil && t.Node >= 0 && len(t.Fields) != 0 {
			fail("node type %q has fields", m.Strings[t.Name])
		}
	}
	for _, l := range m.FieldLists {
		strs(l)
	}
	for _, l := range m.Scopes {
		strs(l)
	}
	checkType := func(i int, where string) {
		if err == nil {
			if perr := fieldType(m.Strings[i]); perr != nil {
				fail("%v %s", perr, where)
			}
		}
	}
	for _, t := range m.Types {
		if t.Node >= 0 && err == nil {
			checkType(t.Node, "in node type "+m.Strings[t.Name])
			if err == nil && !strings.HasPrefix(m.Strings[t.Node], "node[") {
				fail("node type %q is not over a node type", m.Strings[t.Name])
			}
		}
	}
	// nodeType reports whether name is a declared named node type.
	nodeType := func(name string) bool {
		for _, t := range m.Types {
			if m.Strings[t.Name] == name {
				return t.Node >= 0
			}
		}
		return false
	}
	line := func(l *LineInfo) {
		in("entry", l.Entry, len(m.Code))
		strs(l.Scope)
		opt("action", l.Action, len(m.Exprs))
	}
	seenVars := map[string]bool{}
	for _, v := range m.Vars {
		str(v.Name)
		in("variable type", v.Type, 3)
		opt("string", v.Str, len(m.Strings))
		if err == nil {
			name := m.Strings[v.Name]
			switch {
			case seenVars[name]:
				fail("variable %q is defined twice", name)
			case (v.Type == VarString) != (v.Str >= 0):
				fail("invalid initial value of variable %q", name)
			case v.Type == VarBool && v.Int != 0 && v.Int != 1:
				fail("invalid initial value of variable %q", name)
			case v.Type == VarString && v.Int != 0:
				fail("invalid initial value of variable %q", name)
			}
			seenVars[name] = true
		}
	}
	// declared checks that a string index names a declared variable.
	declared := func(i int) {
		str(i)
		if err == nil && !seenVars[m.Strings[i]] {
			fail("variable %q is not declared", m.Strings[i])
		}
	}
	for _, r := range m.Rules {
		str(r.Name)
		in("entry", r.Entry, len(m.Code))
		strs(r.Scope)
		opt("action", r.Action, len(m.Exprs))
		opt("string", r.NodeType, len(m.Strings))
		if err == nil && r.NodeType >= 0 && !nodeType(m.Strings[r.NodeType]) {
			fail("type %q is not a named node type", m.Strings[r.NodeType])
		}
		opt("string", r.Where, len(m.Strings))
		opt("operatorsRule", r.OperatorsRule, len(m.OperatorsRules))
		for _, v := range r.Vars {
			declared(v)
		}
	}
	if len(m.Starts) == 0 {
		fail("no start rule")
	}
	startNames := map[int]bool{}
	for i, x := range m.Starts {
		in("start rule", x, len(m.Rules))
		if err == nil && (i > 0 && x <= m.Starts[i-1] || startNames[m.Rules[x].Name]) {
			fail("invalid start rule %d", x)
		}
		if err == nil {
			startNames[m.Rules[x].Name] = true
		}
	}
	if err == nil && !slices.Contains(m.Starts, m.DefaultStart) {
		fail("default start rule %d is not a start rule", m.DefaultStart)
	}
	for _, p := range m.OperatorsRules {
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
	bound := func(index int32) int64 {
		in("bound expression", int(index), len(m.Exprs))
		if index < 0 || int(index) >= len(m.Exprs) {
			return 0
		}
		x := m.Exprs[index]
		if x.Op != EInt {
			fail("bound expression %d is not EINT", index)
			return 0
		}
		n := x.Integer()
		if int64(int(n)) != n {
			fail("repetition bound %d exceeds the implementation int range", n)
		}
		return n
	}
	wideBounds := func(minIndex, maxIndex int32) {
		min, max := bound(minIndex), bound(maxIndex)
		if min < 0 {
			fail("invalid minimum %d", min)
		}
		if max < -1 || max >= 0 && max < min {
			fail("invalid maximum %d for minimum %d", max, min)
		}
	}
	for ip, x := range m.Code {
		n := len(m.Code)
		switch x.Op {
		case OpStr, OpStrFold:
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
			flag(x.B, 3)
			nonneg(x.C)
		case OpSeq:
			nonneg(x.A)
		case OpSpread:
			nonneg(x.A)
			flag(x.B, 1)
		case OpCapture:
			nonneg(x.A)
			flag(x.B, 1)
		case OpRepeat:
			nonneg(x.A)
			if x.B < -1 {
				fail("invalid maximum %d", x.B)
			}
			opt("scope", int(x.C), len(m.Scopes))
		case OpRepeatWide:
			wideBounds(x.A, x.B)
			opt("scope", int(x.C), len(m.Scopes))
		case OpCall:
			in("rule", int(x.A), len(m.Rules))
			nonneg(x.B)
			flag(x.C, 1)
		case OpOperatorsRule:
			in("operatorsRule", int(x.A), len(m.OperatorsRules))
		case OpPred:
			in("expression", int(x.A), len(m.Exprs))
		case OpBind:
			declared(int(x.A))
			in("expression", int(x.B), len(m.Exprs))
			flag(x.C, 0)
		case OpLabel:
			str(int(x.A))
			flag(x.B, 1)
		case OpGuard:
			in("class", int(x.A), len(m.Classes))
			nonneg(x.B)
			forward(ip, x.C, n)
		case OpScan:
			opt("class", int(x.A), len(m.Classes))
			nonneg(x.B)
			if x.C < -1 {
				fail("invalid maximum %d", x.C)
			}
		case OpScanWide:
			opt("class", int(x.A), len(m.Classes))
			wideBounds(x.B, x.C)
		default:
			fail("invalid instruction %d at %d", x.Op, ip)
		}
	}
	for ip, x := range m.Exprs {
		switch x.Op {
		case EInt, ENil, ERet, ETextChk, EListBegin, EMapPush, EListEnd:
		case ETextEq:
			flag(x.A, 1)
		case EListPush, ETuple:
			nonneg(x.A)
		case EBool:
			flag(x.A, 1)
		case EVar:
			declared(int(x.A))
		case EStr, EUnary, EBoolChk, EMember, EBin:
			str(int(x.A))
		case ECap, EItem, ELocal:
			nonneg(x.A)
		case ENew:
			str(int(x.A))
			in("field list", int(x.B), len(m.FieldLists))
		case ECast:
			str(int(x.A))
			checkType(int(x.A), fmt.Sprintf("at %d", ip))
			nonneg(x.B)
			nonneg(x.C)
		case ENode:
			str(int(x.A))
			if err == nil && !nodeType(m.Strings[x.A]) {
				fail("ENODE type %q is not a declared node type at %d", m.Strings[x.A], ip)
			}
			flag(x.B, 0)
		case EAnd, EOr:
			forward(ip, x.A, len(m.Exprs))
		case EFunc:
			forward(ip, x.A, len(m.Exprs))
			nonneg(x.B)
		case ECall:
			in("builtin", int(x.A), len(BuiltinNames))
			nonneg(x.B)
			if x.A == BuiltinFilter && x.B != 2 {
				fail("filter requires two arguments, got %d", x.B)
			}
		default:
			fail("invalid expression instruction %d at %d", x.Op, ip)
		}
	}
	if err != nil {
		return err
	}
	return ValidateStreamRecovery(m.Code)
}

// ValidateStreamRecovery checks recovery boundaries in lexical bytecode order.
// Both the protected body and its skip region must finish before a stream NEXT:
// recovery restores the saved input position, but streaming discards that input.
// Element-local recovery ends before the enclosing repetition's NEXT and is safe.
func ValidateStreamRecovery(code []Instr) error {
	type region struct{ skip, end int }
	var regions []region
	for ip, in := range code {
		for len(regions) > 0 && regions[len(regions)-1].end == ip {
			regions = regions[:len(regions)-1]
		}
		if in.Op == OpRecover {
			skip := int(in.A)
			if skip <= ip+1 || skip >= len(code) || code[skip-1].Op != OpEndRecover {
				return fmt.Errorf("invalid recovery body at %d", ip)
			}
			end := int(code[skip-1].A)
			if end <= skip || end >= len(code) || code[end-1].Op != OpEndSkip {
				return fmt.Errorf("invalid recovery skip at %d", ip)
			}
			if len(regions) > 0 {
				parent := regions[len(regions)-1]
				limit := parent.end
				if ip < parent.skip {
					limit = parent.skip
				}
				if end >= limit {
					return fmt.Errorf("crossing recovery regions at %d", ip)
				}
			}
			regions = append(regions, region{skip, end})
		} else if in.StreamsValue() && len(regions) > 0 {
			return fmt.Errorf("recovery region encloses a stream commitment at %d", ip)
		}
	}
	return nil
}
