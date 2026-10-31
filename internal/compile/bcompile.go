package compile

import (
	"slices"
	"unicode/utf8"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/bytecode"
)

// BytecodeModule is Module.
func (prog *Program) BytecodeModule() *bytecode.Module {
	if prog.HasMissing {
		panic(MissingUnsupported("bytecode module construction"))
	}
	prog.BytecodeOnce.Do(func() {
		c := &bcompiler{prog: prog, m: &bytecode.Module{SourceInfo: prog.RecordsSourceInfo, DefaultValue: prog.MakesDefaultValue}, strs: map[string]int{}}
		c.compile()
		prog.Bytecode = c.m
	})
	return prog.Bytecode
}

// bcompiler compiles a program to a bytecode module (Program.BytecodeModule): m is the module
// it fills and strs indexes the module's string table.
type bcompiler struct {
	prog *Program
	cur  *Rule             // rule being compiled
	proj map[string]string // projected captures of its body (ruleProjections)
	m    *bytecode.Module
	strs map[string]int
	// lambdas are the lambdas whose body compilation has been deferred.
	lambdas []pendingLambda
}

type pendingLambda struct {
	at     int // index of the EFUNC instruction
	body   grammar.Term
	scope  *Scope
	locals []string
}

func (c *bcompiler) str(s string) int32 {
	if i, ok := c.strs[s]; ok {
		return int32(i)
	}
	c.strs[s] = len(c.m.Strings)
	c.m.Strings = append(c.m.Strings, s)
	return int32(len(c.m.Strings) - 1)
}

func (c *bcompiler) strs2(names []string) []int {
	out := make([]int, len(names))
	for i, n := range names {
		out[i] = int(c.str(n))
	}
	return out
}

func (c *bcompiler) emit(op bytecode.Op, a, b, cc int32) int {
	c.m.Code = append(c.m.Code, bytecode.Instr{Op: op, A: a, B: b, C: cc})
	return len(c.m.Code) - 1
}

func (c *bcompiler) here() int32 { return int32(len(c.m.Code)) }

func (c *bcompiler) eemit(op bytecode.Op, a, b int32) int {
	return c.eemit3(op, a, b, 0)
}

func (c *bcompiler) eemit3(op bytecode.Op, a, b, cc int32) int {
	c.m.Exprs = append(c.m.Exprs, bytecode.Instr{Op: op, A: a, B: b, C: cc})
	return len(c.m.Exprs) - 1
}

// integer stores a full implementation-int value using the EINT representation.
func (c *bcompiler) integer(value int) int32 {
	lo := int32(value)
	return int32(c.eemit(bytecode.EInt, lo, int32((uint64(value)-uint64(int64(lo)))>>32)))
}

func (c *bcompiler) repetition(op bytecode.Op, min, max int, other int32) {
	// Public ASTs allow any negative maximum. Canonicalize before narrowing
	// so large negative values cannot wrap into finite bytecode bounds.
	if max < 0 {
		max = -1
	}
	if int64(min) <= 1<<31-1 && int64(max) <= 1<<31-1 {
		if op == bytecode.OpScan {
			c.emit(op, other, int32(min), int32(max))
		} else {
			c.emit(op, int32(min), int32(max), other)
		}
		return
	}
	lo, hi := c.integer(min), c.integer(max)
	if op == bytecode.OpScan {
		c.emit(bytecode.OpScanWide, other, lo, hi)
	} else {
		c.emit(bytecode.OpRepeatWide, lo, hi, other)
	}
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// compile fills M with the type table, the rule table and the code of the rules of Prog.
func (c *bcompiler) compile() {
	// Type table
	for _, td := range c.prog.Grammar.Types() {
		switch spec := td.(type) {
		case *grammar.TypeDef:
			ti := bytecode.TypeInfo{Name: int(c.str(td.DeclName())), Node: -1}
			if n, ok := c.prog.NodeTypeOf(td.DeclName()); ok {
				ti.Node = int(c.str(NewFieldType(n.Underlying()).Text))
			}
			for _, f := range DefFields(spec) {
				ti.Fields = append(ti.Fields, int(c.str(f.Name)))
			}
			c.m.Types = append(c.m.Types, ti)
		}
	}
	if t := c.prog.Vars; t != nil {
		for _, d := range t.List {
			vi := bytecode.VarInfo{Name: int(c.str(d.Name)), Str: -1}
			switch v := d.Init.(type) {
			case int:
				vi.Type, vi.Int = bytecode.VarInt, v
			case string:
				vi.Type, vi.Str = bytecode.VarString, int(c.str(v))
			case bool:
				vi.Type, vi.Int = bytecode.VarBool, int(b2i(v))
			}
			c.m.Vars = append(c.m.Vars, vi)
		}
	}
	// Rule table (body positions are filled in later)
	all := append(append([]*Rule(nil), c.prog.RuleList...), c.prog.Twins...)
	for _, r := range all {
		ri := bytecode.RuleInfo{Name: int(c.str(r.Name)), Entry: -1, Action: -1, NodeType: -1, OperatorsRule: -1,
			Memo: r.Memo, Leader: r.Leader, Member: r.Member, Positional: r.Positional, NoValue: r.Novalue, Transient: r.Transient, Rec: r.Rec, Where: -1, Splice: r.Splice, Vars: c.strs2(r.Vars)}
		if r.Where != "" && r.Where != r.Name {
			ri.Where = int(c.str(r.Where))
		}
		if r.NodeType != "" {
			ri.NodeType = int(c.str(r.NodeType))
		} else if r.OpNodeType != "" {
			ri.NodeType = int(c.str(r.OpNodeType)) // that of the operator nodes of an Operators Rule
		}
		c.m.Rules = append(c.m.Rules, ri)
	}
	for i, r := range c.prog.RuleList {
		if slices.Contains(c.prog.StartRules, r.Name) {
			c.m.Starts = append(c.m.Starts, i)
		}
		if r.Name == c.prog.DefaultStartRule {
			c.m.DefaultStart = i
		}
	}
	for i, r := range all {
		c.cur = r
		ri := &c.m.Rules[i]
		s := NewScope()
		if pr, ok := r.Def.Expr.(*grammar.OperatorsRule); ok {
			ri.OperatorsRule = c.operatorsRule(pr)
			ri.Entry = int(c.here())
			c.emit(bytecode.OpOperatorsRule, int32(ri.OperatorsRule), 0, 0)
			c.emit(bytecode.OpReturn, 0, 0, 0)
		} else {
			ri.Entry = int(c.here())
			c.proj = RuleProjections(c.prog, r)
			if r.Lean {
				c.match(r.BodyExpr(), s, false)
			} else if r.Splice {
				c.spliceScope(r.BodyExpr(), s)
			} else {
				c.scope(r.BodyExpr(), s)
			}
			c.proj = nil
			c.emit(bytecode.OpReturn, 0, 0, 0)
		}
		ri.Scope = c.strs2(s.Names)
		if r.Action != nil {
			var locals []string
			if ri.OperatorsRule >= 0 {
				locals = OperatorsRuleActionLocals // $value is local 0
			}
			ri.Action = c.expr(r.Action, s, locals, true)
		}
	}
}

// The value shapes of value_shape.go: each pushes values as the closure backend builds them.

// tuple emits code that pushes the tuple node of the elements of e.
func (c *bcompiler) tuple(e grammar.Expr, s *Scope) {
	c.emit(bytecode.OpPushPos, 0, 0, 0)
	c.elems(e, s)
	c.emit(bytecode.OpSeq, int32(ValuedCount(c.prog, e)), 0, 0)
}

// single emits code that pushes the value of e where it takes one element: nil without a value.
func (c *bcompiler) single(e grammar.Expr, s *Scope) {
	switch SingleModeOf(c.prog, e) {
	case singleNone:
		c.elems(e, s)
		c.emit(bytecode.OpPushNil, 0, 0, 0)
	case SinglePass:
		c.elems(e, s)
	default:
		c.tuple(e, s)
	}
}

// captured emits code that pushes the value of e where it is captured or recovered: the unit
// node of its range without a value.
func (c *bcompiler) captured(e grammar.Expr, s *Scope) {
	if SingleModeOf(c.prog, e) == SinglePass {
		c.elems(e, s)
		return
	}
	c.tuple(e, s)
}

// scope emits code that pushes the value of a scope with the body e.
func (c *bcompiler) scope(e grammar.Expr, s *Scope) {
	if ScopeTuple(c.prog, e) {
		c.tuple(e, s)
		return
	}
	c.elems(e, s)
}

// spliceScope emits code that pushes the value of the body e of a splice rule: the tuple node of
// its elements if e builds one, else its one element or nil, as OpSpread takes it.
func (c *bcompiler) spliceScope(e grammar.Expr, s *Scope) {
	switch {
	case IsElems(c.prog, e):
		c.tuple(e, s)
	case ValuedCount(c.prog, e) == 1:
		c.elems(e, s)
	default:
		c.elems(e, s)
		c.emit(bytecode.OpPushNil, 0, 0, 0)
	}
}

// class adds the character class to the character class table and returns its index.
// guard emits a GUARD for an alternative of a choice that must begin with a given terminal
// (firstTerminal) and returns its index, or -1. Its jump target is set by the caller.
func (c *bcompiler) guard(alt grammar.Expr) int {
	t, depth := firstTerminal(c.prog, alt, 0, map[string]bool{})
	var cl int32
	switch t := t.(type) {
	case *grammar.CharClass:
		cl = c.class(t)
	case *grammar.Literal:
		r, _ := utf8.DecodeRuneInString(t.Value)
		ranges := []rune{r, r}
		if t.IgnoreCase {
			// A case-insensitive literal's first character matches either case of an ASCII letter.
			f := FoldASCIIRune(r)
			ranges = []rune{f, f}
			if 'a' <= f && f <= 'z' {
				upper := f - ('a' - 'A')
				ranges = []rune{upper, upper, f, f}
			}
		}
		c.m.Classes = append(c.m.Classes, bytecode.Class{Ranges: ranges, Desc: int(c.str(LiteralDesc(t)))})
		cl = int32(len(c.m.Classes) - 1)
	default:
		return -1
	}
	return c.emit(bytecode.OpGuard, cl, int32(depth), 0)
}

func (c *bcompiler) class(e *grammar.CharClass) int32 {
	cl := bytecode.Class{Negated: e.Negated, Desc: int(c.str(CharClassString(e)))}
	for _, r := range e.Ranges {
		cl.Ranges = append(cl.Ranges, r.Lo, r.Hi)
	}
	c.m.Classes = append(c.m.Classes, cl)
	return int32(len(c.m.Classes) - 1)
}

// match emits code for the expression. If build is true, it pushes the elements of the expression
// (elems); otherwise it pushes nothing.
func (c *bcompiler) match(e grammar.Expr, s *Scope, build bool) {
	if build {
		c.elems(e, s)
		return
	}
	c.void(e, s)
}

// elems emits code that pushes the valuedCount(c.prog, e) elements of e, flattened through sequences.
func (c *bcompiler) elems(e grammar.Expr, s *Scope) {
	switch e := e.(type) {
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Ref, *grammar.Atomic:
		c.leaf(e, s, true)
	case *grammar.Seq:
		for _, it := range e.Items {
			c.elems(it, s)
		}
	case *grammar.Choice:
		valued := ValuedCount(c.prog, e) == 1
		var ends []int
		for i, alt := range e.Alts {
			emit := func() {
				if valued {
					c.single(alt, s)
				} else {
					c.elems(alt, s)
				}
			}
			if i == len(e.Alts)-1 {
				emit()
				break
			}
			guard := c.guard(alt)
			ch := c.emit(bytecode.OpChoice, 0, 0, 0)
			emit()
			ends = append(ends, c.emit(bytecode.OpCommit, 0, 0, 0))
			c.m.Code[ch].A = c.here()
			if guard >= 0 {
				c.m.Code[guard].C = c.here()
			}
		}
		for _, j := range ends {
			c.m.Code[j].A = c.here()
		}
	case *grammar.Repeat:
		c.repeat(e, s, true, false)
	case *grammar.Optional:
		valued := ValuedCount(c.prog, e) == 1
		ch := c.emit(bytecode.OpChoice, 0, 0, 0)
		if valued {
			c.single(e.Expr, s)
		} else {
			c.elems(e.Expr, s)
		}
		end := c.emit(bytecode.OpCommit, 0, 0, 0)
		c.m.Code[ch].A = c.here()
		if valued {
			c.emit(bytecode.OpPushNil, 0, 0, 0)
		}
		c.m.Code[end].A = c.here()
	case *grammar.Capture:
		c.capture(e, s, true)
	case *grammar.Attributed:
		c.attrElems(e, len(e.Attrs)-1, s)
	default:
		// The other expressions have no value: lookaheads keep only their captures.
		c.void(e, s)
	}
}

// leaf emits a literal, a character class, ., a rule call or #atomic, pushing its value if build.
func (c *bcompiler) leaf(e grammar.Expr, s *Scope, build bool) {
	switch e := e.(type) {
	case *grammar.Literal:
		if e.IgnoreCase {
			c.emit(bytecode.OpStrFold, c.str(string(FoldedRunes([]rune(e.Value)))), c.str(LiteralDesc(e)), b2i(build))
		} else {
			c.emit(bytecode.OpStr, c.str(e.Value), c.str(quote(e.Value)), b2i(build))
		}
	case *grammar.CharClass:
		c.emit(bytecode.OpClass, c.class(e), c.str(CharClassString(e)), b2i(build))
	case *grammar.Any:
		c.emit(bytecode.OpAny, b2i(build), 0, 0)
	case *grammar.Ref:
		r := c.prog.ByName[e.Name]
		min := 0
		if e.Level != "" {
			for i, l := range r.Def.Expr.(*grammar.OperatorsRule).Levels {
				if l.Name == e.Level {
					min = i
				}
			}
		}
		splice := r.Splice
		if !build && r.Twin != nil {
			r = r.Twin
		}
		c.emit(bytecode.OpCall, int32(r.ID), int32(min), b2i(build))
		if build && splice {
			// The elements of the rule are elements of the sequence that calls it.
			c.emit(bytecode.OpSpread, int32(ValuedCount(c.prog, e)), b2i(IsElems(c.prog, e)), 0)
		}
	case *grammar.Atomic:
		if build {
			c.emit(bytecode.OpPushPos, 0, 0, 0)
		}
		c.void(e.Expr, s)
		if build {
			c.emit(bytecode.OpAtomic, 1, 0, 0)
		}
	}
}

// void emits code for the expression without its value.
func (c *bcompiler) void(e grammar.Expr, s *Scope) {
	switch e := e.(type) {
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Ref, *grammar.Atomic:
		c.leaf(e, s, false)
	case *grammar.Seq:
		for _, it := range e.Items {
			c.void(it, s)
		}
	case *grammar.Choice:
		var ends []int
		for i, alt := range e.Alts {
			if i == len(e.Alts)-1 {
				c.void(alt, s)
				break
			}
			guard := c.guard(alt)
			ch := c.emit(bytecode.OpChoice, 0, 0, 0)
			c.void(alt, s)
			ends = append(ends, c.emit(bytecode.OpCommit, 0, 0, 0))
			c.m.Code[ch].A = c.here()
			if guard >= 0 {
				c.m.Code[guard].C = c.here()
			}
		}
		for _, j := range ends {
			c.m.Code[j].A = c.here()
		}
	case *grammar.Repeat:
		c.repeat(e, s, false, false)
	case *grammar.Optional:
		ch := c.emit(bytecode.OpChoice, 0, 0, 0)
		c.void(e.Expr, s)
		end := c.emit(bytecode.OpCommit, 0, 0, 0)
		c.m.Code[ch].A = c.here()
		c.m.Code[end].A = c.here()
	case *grammar.And:
		// The values inside are dropped by ENDLOOK; the captures are kept.
		c.emit(bytecode.OpLook, 0, 0, 0)
		c.void(e.Expr, s)
		c.emit(bytecode.OpEndLook, 0, 0, 0)
	case *grammar.Not:
		look := c.emit(bytecode.OpLook, 0, 1, 0)
		c.void(e.Expr, s)
		c.emit(bytecode.OpEndLook, 1, 0, 0)
		c.m.Code[look].A = c.here()
	case *grammar.Discard:
		c.void(e.Expr, s)
	case *grammar.Capture:
		c.capture(e, s, false)
	case *grammar.Cut:
		c.emit(bytecode.OpCut, 0, 0, 0)
	case *grammar.Top:
		c.emit(bytecode.OpTop, 0, 0, 0)
	case *grammar.Bottom:
		c.emit(bytecode.OpFail, 0, 0, 0)
	case *grammar.BeginInput:
		c.emit(bytecode.OpAssert, 0, 0, 0)
	case *grammar.EndInput:
		c.emit(bytecode.OpAssert, 1, 0, 0)
	case *grammar.BeginLine:
		c.emit(bytecode.OpAssert, 2, 0, 0)
	case *grammar.EndLine:
		c.emit(bytecode.OpAssert, 3, 0, 0)
	case *grammar.Predicate:
		if a, ok := e.Term.(*grammar.Bind); ok {
			c.emit(bytecode.OpBind, int32(c.prog.Vars.Index(a.Name)), int32(c.expr(a.Value, s, nil, false)), 0)
		} else {
			c.emit(bytecode.OpPred, int32(c.expr(e.Term, s, nil, false)), 0, 0)
		}
	case *grammar.Attributed:
		c.attributed(e, len(e.Attrs)-1, s)
	}
}

// capture emits a capture, leaving its value pushed if keep.
func (c *bcompiler) capture(e *grammar.Capture, s *Scope, keep bool) {
	if !keep && c.cur.PredCaps != nil && !c.cur.PredCaps[e.Name] {
		c.void(e.Expr, s) // a capture not referenced by the predicate, in a program that builds no tree
		return
	}
	slot := s.Slot(e.Name)
	if field := c.proj[e.Name]; field != "" {
		c.projectRepeat(e.Expr.(*grammar.Repeat), field)
	} else {
		c.captured(e.Expr, s)
	}
	c.emit(bytecode.OpCapture, int32(slot), b2i(!keep), 0)
}

func (c *bcompiler) repeat(e *grammar.Repeat, s *Scope, build, stream bool) {
	if !build && !stream { // value-free repetition of a single character (same as Walker.scanRepeat)
		switch x := e.Expr.(type) {
		case *grammar.CharClass:
			c.repetition(bytecode.OpScan, e.Min, e.Max, c.class(x))
			return
		case *grammar.Any:
			c.repetition(bytecode.OpScan, e.Min, e.Max, -1)
			return
		}
	}
	elemScope := s
	sc := int32(-1)
	if ElementScoped(e.Expr) {
		elemScope = NewScope()
		c.m.Scopes = append(c.m.Scopes, nil)
		sc = int32(len(c.m.Scopes) - 1)
	}
	if build {
		c.emit(bytecode.OpPushPos, 0, 0, 0)
	}
	c.repetition(bytecode.OpRepeat, e.Min, e.Max, sc)
	loop := c.here()
	iter := c.emit(bytecode.OpIter, 0, 0, 0)
	if build {
		c.scope(e.Expr, elemScope)
	} else {
		c.void(e.Expr, elemScope)
	}
	flag := b2i(build)
	if stream && build {
		flag = 2
	}
	c.emit(bytecode.OpNext, loop, flag, 0)
	c.m.Code[iter].A = c.here()
	c.emit(bytecode.OpEndRepeat, b2i(build), 0, 0)
	if sc >= 0 {
		c.m.Scopes[sc] = c.strs2(elemScope.Names)
	}
}

// attrElems emits code that pushes the elements of the expression with attributes up to i applied
// (inner attributes are applied first). What recovers is one element: its value or the error.
func (c *bcompiler) attrElems(e *grammar.Attributed, i int, s *Scope) {
	if i < 0 {
		if rep, ok := e.Expr.(*grammar.Repeat); ok && IsStream(e) {
			c.repeat(rep, s, true, true)
		} else {
			c.elems(e.Expr, s)
		}
		return
	}
	a := e.Attrs[i]
	switch a.Name {
	case "error", "diagnostic":
		c.label(a)
		c.attrElems(e, i-1, s)
		c.emit(bytecode.OpEndLabel, 0, 0, 0)
	case "recover":
		skip, _ := a.Arg("skip")
		rec := c.emit(bytecode.OpRecover, 0, 0, 0)
		// The value of the inner expression where it is captured (captured), as in captureMatcher.
		mode := SingleModeOf(c.prog, e.Expr)
		if hasRecover(e.Attrs[:i]) {
			mode = SinglePass // an inner recovery is one element already
		}
		if mode != SinglePass {
			c.emit(bytecode.OpPushPos, 0, 0, 0)
		}
		c.attrElems(e, i-1, s)
		if mode != SinglePass {
			n := 0
			if mode == SingleTuple {
				n = ValuedCount(c.prog, e.Expr)
			}
			c.emit(bytecode.OpSeq, int32(n), 0, 0)
		}
		end := c.emit(bytecode.OpEndRecover, 0, 0, 0)
		c.m.Code[rec].A = c.here()
		c.void(skip, s)
		c.emit(bytecode.OpEndSkip, 1, 0, 0)
		c.m.Code[end].A = c.here()
	default: // #stream is handled by the repetition
		c.attrElems(e, i-1, s)
	}
}

// hasRecover reports whether attrs has #recover.
func hasRecover(attrs []*grammar.Attribute) bool {
	for _, a := range attrs {
		if a.Name == "recover" {
			return true
		}
	}
	return false
}

// label emits the LABEL of @error or @diagnostic.
func (c *bcompiler) label(a *grammar.Attribute) {
	if a.Name == "error" {
		c.emit(bytecode.OpLabel, c.str(AttributeMessage(a)), 0, 0)
	} else {
		c.emit(bytecode.OpLabel, c.str(AttributeDiagnosticName(a)), 1, 0)
	}
}

// attributed emits code for the expression with attributes up to i applied (inner attributes
// are applied first), without its value.
func (c *bcompiler) attributed(e *grammar.Attributed, i int, s *Scope) {
	if i < 0 {
		if rep, ok := e.Expr.(*grammar.Repeat); ok && IsStream(e) {
			c.repeat(rep, s, false, true)
		} else {
			c.void(e.Expr, s)
		}
		return
	}
	a := e.Attrs[i]
	switch a.Name {
	case "error", "diagnostic":
		c.label(a)
		c.attributed(e, i-1, s)
		c.emit(bytecode.OpEndLabel, 0, 0, 0)
	case "recover":
		skip, _ := a.Arg("skip")
		rec := c.emit(bytecode.OpRecover, 0, 0, 0)
		c.attributed(e, i-1, s)
		end := c.emit(bytecode.OpEndRecover, 0, 0, 0)
		c.m.Code[rec].A = c.here()
		c.void(skip, s)
		c.emit(bytecode.OpEndSkip, 0, 0, 0)
		c.m.Code[end].A = c.here()
	default: // #stream is handled by the repetition
		c.attributed(e, i-1, s)
	}
}

func (c *bcompiler) operatorsRule(e *grammar.OperatorsRule) int {
	pi := bytecode.OperatorsRuleInfo{Skip: -1}
	if e.Skip != nil {
		pi.Skip = int(c.here())
		c.match(e.Skip, NewScope(), false)
		c.emit(bytecode.OpEnd, 0, 0, 0)
	}
	for _, o := range e.Operands {
		pi.Operands = append(pi.Operands, c.line(o.Expr, o.Action, nil, o))
	}
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			oi := bytecode.OpInfo{ID: id, Level: i + 1}
			id++
			var locals []string
			switch op.Kind {
			case grammar.Prefix:
				oi.Kind = bytecode.OperatorPrefix
			case grammar.Postfix:
				oi.Kind = bytecode.OperatorPostfix
			default:
				oi.Kind = bytecode.OperatorInfix
			}
			switch op.Assoc {
			case grammar.AssocRight:
				oi.Assoc = bytecode.AssocRight
			case grammar.AssocNone:
				oi.Assoc = bytecode.AssocNone
			}
			// Locals of an operator action: 0 $lhs, 1 $rhs, 2 $op
			locals = []string{"lhs", "rhs", "op"}
			oi.Line = c.line(op.Expr, op.Action, locals, op)
			if oi.Kind == bytecode.OperatorPrefix {
				pi.Prefix = append(pi.Prefix, oi)
			} else {
				pi.Led = append(pi.Led, oi)
			}
		}
	}
	c.m.OperatorsRules = append(c.m.OperatorsRules, pi)
	return len(c.m.OperatorsRules) - 1
}

func (c *bcompiler) line(e grammar.Expr, action grammar.Term, locals []string, key any) bytecode.LineInfo {
	s := NewScope()
	li := bytecode.LineInfo{Entry: int(c.here()), Action: -1}
	switch build := !c.cur.Novalue && !LeanLine(action, locals); {
	case !build:
		c.void(e, s)
	case locals == nil: // an operand line: the value of its scope
		c.scope(e, s)
	default: // an operator line: the operator's value, the node of its tuple if IsSeq
		li.IsSeq = SingleModeOf(c.prog, e) == SingleTuple
		c.captured(e, s)
	}
	c.emit(bytecode.OpEnd, 0, 0, 0)
	li.Scope = c.strs2(s.Names)
	if action != nil && !c.cur.Novalue {
		li.Action = c.expr(action, s, locals, locals == nil)
	}
	return li
}

// expr compiles the value expression into expression code and returns its start position.
func (c *bcompiler) expr(t grammar.Term, s *Scope, locals []string, allowIndex bool) int {
	start := len(c.m.Exprs)
	c.term(t, s, locals)
	c.eemit(bytecode.ERet, 0, 0)
	// Lambda bodies are placed after the expression that contains them.
	for len(c.lambdas) > 0 {
		l := c.lambdas[0]
		c.lambdas = c.lambdas[1:]
		c.m.Exprs[l.at].A = int32(len(c.m.Exprs))
		c.term(l.body, l.scope, l.locals)
		c.eemit(bytecode.ERet, 0, 0)
	}
	return start
}

// projectRepeat emits a repetition that gathers the values of the capture field of its elements
// (project.go): the elements are matched without values, and NEXT pushes the field's slot.
func (c *bcompiler) projectRepeat(e *grammar.Repeat, field string) {
	elemScope := NewScope()
	c.m.Scopes = append(c.m.Scopes, nil)
	sc := int32(len(c.m.Scopes) - 1)
	c.emit(bytecode.OpPushPos, 0, 0, 0)
	c.repetition(bytecode.OpRepeat, e.Min, e.Max, sc)
	loop := c.here()
	iter := c.emit(bytecode.OpIter, 0, 0, 0)
	c.match(e.Expr, elemScope, false)
	c.emit(bytecode.OpNext, loop, 3, int32(elemScope.Slots[field]))
	c.m.Code[iter].A = c.here()
	c.emit(bytecode.OpEndRepeat, 1, 0, 0)
	c.m.Scopes[sc] = c.strs2(elemScope.Names)
}

// identity is the lambda (e) => e, which takes the place of the projection in a map call of a
// projected repetition: the list holds the values already.
var identity = &grammar.Lambda{Params: []string{"e"}, Body: &grammar.CaptureRef{Name: "e"}}

// concatParts emits the code that gathers the elements of the arguments of a fusable concat call.
func (c *bcompiler) concatParts(t *grammar.Call, s *Scope, locals []string) {
	for _, a := range t.Args {
		call := a.(*grammar.Call)
		switch call.Func {
		case "list":
			for _, x := range call.Args {
				c.term(x, s, locals)
			}
			c.eemit(bytecode.EListPush, int32(len(call.Args)), 0)
		case "map":
			c.term(call.Args[0], s, locals)
			if c.prog.Projected[call] {
				c.lambda(identity, s, locals)
			} else {
				c.lambda(call.Args[1], s, locals)
			}
			c.eemit(bytecode.EMapPush, 0, 0)
		case "concat":
			c.concatParts(call, s, locals)
		}
	}
}

// lambda emits a function value for the lambda t.
func (c *bcompiler) lambda(t grammar.Term, s *Scope, locals []string) {
	l, ok := t.(*grammar.Lambda)
	if !ok {
		c.term(t, s, locals)
		return
	}
	at := c.eemit(bytecode.EFunc, 0, int32(len(l.Params)))
	c.lambdas = append(c.lambdas, pendingLambda{at: at, body: l.Body, scope: s,
		locals: append(append([]string(nil), locals...), l.Params...)})
}

func (c *bcompiler) term(t grammar.Term, s *Scope, locals []string) {
	switch t := t.(type) {
	case *grammar.IntLit:
		// A holds the low 32 bits and B the rest (see EInt), so B is 0 for the values of int32.
		lo := int32(t.Value)
		c.eemit(bytecode.EInt, lo, int32((uint64(t.Value)-uint64(int64(lo)))>>32))
	case *grammar.StringLit:
		c.eemit(bytecode.EStr, c.str(t.Value), 0)
	case *grammar.BoolLit:
		c.eemit(bytecode.EBool, b2i(t.Value), 0)
	case *grammar.NilLit:
		c.eemit(bytecode.ENil, 0, 0)
	case *grammar.CaptureRef:
		for i := len(locals) - 1; i >= 0; i-- {
			if locals[i] == t.Name {
				c.eemit(bytecode.ELocal, int32(i), 0)
				return
			}
		}
		c.eemit(bytecode.ECap, int32(s.Slots[t.Name]), 0)
	case *grammar.IndexRef:
		c.eemit(bytecode.EItem, int32(t.Index), 0)
	case *grammar.VarRef:
		c.eemit(bytecode.EVar, int32(c.prog.Vars.Index(t.Name)), 0)
	case *grammar.Member:
		c.term(t.X, s, locals)
		c.eemit(bytecode.EMember, c.str(t.Name), 0)
	case *grammar.New:
		if len(t.Args) != 0 {
			n, _ := c.prog.NodeTypeOf(t.Type)
			c.term(t.Args[0], s, locals)
			c.eemit(bytecode.ENode, c.str(n.Name), 0)
			return
		}
		name, _ := c.prog.StructType(t.Type)
		names := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			c.term(f.Value, s, locals)
			names[i] = f.Name
		}
		c.m.FieldLists = append(c.m.FieldLists, c.strs2(names))
		c.eemit(bytecode.ENew, c.str(name), int32(len(c.m.FieldLists)-1))
	case *grammar.ListConstructor:
		for _, item := range t.Items {
			c.term(item, s, locals)
		}
		c.eemit(bytecode.ECall, bytecode.BuiltinList, int32(len(t.Items)))
	case *grammar.TupleLit:
		for _, x := range t.Elems {
			c.term(x, s, locals)
		}
		c.eemit(bytecode.ETuple, int32(len(t.Elems)), 0)
	case *grammar.Call:
		if t.Func == "concat" && Fusable(t) {
			// Gather the elements of list, map and concat arguments directly (see fusable).
			c.eemit(bytecode.EListBegin, 0, 0)
			c.concatParts(t, s, locals)
			c.eemit(bytecode.EListEnd, 0, 0)
			return
		}
		fn := 0
		for i, n := range bytecode.BuiltinNames {
			if n == t.Func {
				fn = i
			}
		}
		for _, a := range t.Args {
			if l, ok := a.(*grammar.Lambda); ok {
				if c.prog.Projected[t] {
					l = identity
				}
				at := c.eemit(bytecode.EFunc, 0, int32(len(l.Params)))
				c.lambdas = append(c.lambdas, pendingLambda{at: at, body: l.Body, scope: s,
					locals: append(append([]string(nil), locals...), l.Params...)})
				continue
			}
			c.term(a, s, locals)
		}
		c.eemit(bytecode.ECall, int32(fn), int32(len(t.Args)))
	case *grammar.Binary:
		switch t.Op {
		case "&&", "||":
			c.term(t.L, s, locals)
			op := bytecode.EAnd
			if t.Op == "||" {
				op = bytecode.EOr
			}
			j := c.eemit(op, 0, 0)
			c.term(t.R, s, locals)
			c.eemit(bytecode.EBoolChk, c.str(t.Op), 0)
			c.m.Exprs[j].A = int32(len(c.m.Exprs))
		default:
			if lx, ok := TextArg(t.L); ok && (t.Op == "==" || t.Op == "!=") {
				if rx, ok := TextArg(t.R); ok {
					// Compare the texts without making strings values (see evalCtx.binary). Each
					// argument is checked right after it is evaluated, as text(...) would be.
					c.term(lx, s, locals)
					c.eemit(bytecode.ETextChk, 0, 0)
					c.term(rx, s, locals)
					c.eemit(bytecode.ETextChk, 0, 0)
					c.eemit(bytecode.ETextEq, b2i(t.Op == "!="), 0)
					return
				}
			}
			c.term(t.L, s, locals)
			c.term(t.R, s, locals)
			c.eemit(bytecode.EBin, c.str(t.Op), 0)
		}
	case *grammar.Unary:
		c.term(t.X, s, locals)
		c.eemit(bytecode.EUnary, c.str(t.Op), 0)
	case *grammar.Cast:
		c.term(t.X, s, locals)
		if ft := c.prog.CastType(t); ft != nil {
			line, col := c.prog.CastPos(t)
			c.eemit3(bytecode.ECast, c.str(ft.Text), int32(line), int32(col))
		}
	}
}
