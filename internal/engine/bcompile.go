package engine

import (
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Module compiles the program to bytecode. The result is stored in the program and reused on
// subsequent calls.
func (prog *Program) Module() *Module {
	prog.moduleOnce.Do(func() {
		c := &bcompiler{prog: prog, m: &Module{}, strs: map[string]int{}}
		c.compile()
		prog.module = c.m
	})
	return prog.module
}

type bcompiler struct {
	prog *Program
	cur  *rule             // rule being compiled
	proj map[string]string // projected captures of its body (ruleProjections)
	m    *Module
	strs map[string]int
	// lambdas are the lambdas whose body compilation has been deferred.
	lambdas []pendingLambda
}

type pendingLambda struct {
	at     int // index of the EFUNC instruction
	body   grammar.Term
	scope  *scope
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

func (c *bcompiler) emit(op Op, a, b, cc int32) int {
	c.m.Code = append(c.m.Code, Instr{Op: op, A: a, B: b, C: cc})
	return len(c.m.Code) - 1
}

func (c *bcompiler) here() int32 { return int32(len(c.m.Code)) }

func (c *bcompiler) eemit(op Op, a, b int32) int {
	c.m.Exprs = append(c.m.Exprs, Instr{Op: op, A: a, B: b})
	return len(c.m.Exprs) - 1
}

// integer stores a full implementation-int value using the EINT representation.
func (c *bcompiler) integer(value int) int32 {
	lo := int32(value)
	return int32(c.eemit(EInt, lo, int32((uint64(value)-uint64(int64(lo)))>>32)))
}

func (c *bcompiler) repetition(op Op, min, max int, other int32) {
	// Public ASTs allow any negative maximum. Canonicalize before narrowing
	// so large negative values cannot wrap into finite bytecode bounds.
	if max < 0 {
		max = -1
	}
	if int64(min) <= 1<<31-1 && int64(max) <= 1<<31-1 {
		if op == OpScan {
			c.emit(op, other, int32(min), int32(max))
		} else {
			c.emit(op, int32(min), int32(max), other)
		}
		return
	}
	lo, hi := c.integer(min), c.integer(max)
	if op == OpScan {
		c.emit(OpScanWide, other, lo, hi)
	} else {
		c.emit(OpRepeatWide, lo, hi, other)
	}
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func (c *bcompiler) compile() {
	// Type table
	for _, td := range c.prog.Grammar.Types() {
		switch spec := td.Spec.(type) {
		case *grammar.StructSpec:
			ti := TypeInfo{Name: int(c.str(td.Name))}
			for _, f := range spec.Fields {
				ti.Fields = append(ti.Fields, int(c.str(f.Name)))
			}
			c.m.Types = append(c.m.Types, ti)
		case *grammar.TerminalSpec:
			c.m.Types = append(c.m.Types, TypeInfo{Name: int(c.str(td.Name)), Terminal: true})
		}
	}
	// Rule table (body positions are filled in later)
	all := append(append([]*rule(nil), c.prog.rules...), c.prog.twins...)
	for _, r := range all {
		ri := RuleInfo{Name: int(c.str(r.name)), Entry: -1, Action: -1, TerminalType: -1, Pratt: -1,
			BodyIsSeq: r.bodyIsSeq, Memo: r.memo, Leader: r.leader, Positional: r.positional, NoValue: r.novalue, Transient: r.transient, Vars: c.strs2(r.vars)}
		if r.terminalType != "" {
			ri.TerminalType = int(c.str(r.terminalType))
		}
		c.m.Rules = append(c.m.Rules, ri)
	}
	for i, r := range all {
		c.cur = r
		ri := &c.m.Rules[i]
		s := newScope()
		if pr, ok := r.def.Expr.(*grammar.Pratt); ok {
			ri.Pratt = c.pratt(pr)
			ri.Entry = int(c.here())
			c.emit(OpPratt, int32(ri.Pratt), 0, 0)
			c.emit(OpReturn, 0, 0, 0)
		} else {
			ri.Entry = int(c.here())
			c.proj = ruleProjections(c.prog, r)
			c.value(r.def.Expr, s, !r.lean)
			c.proj = nil
			c.emit(OpReturn, 0, 0, 0)
		}
		ri.Scope = c.strs2(s.names)
		if r.action != nil {
			ri.Action = c.expr(r.action, s, nil, true)
		}
	}
}

// value emits code that pushes one value (nil if the expression has no value).
func (c *bcompiler) value(e grammar.Expr, s *scope, build bool) {
	c.match(e, s, build)
	if build && !visible(e) {
		c.emit(OpPushNil, 0, 0, 0)
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
		c.m.Classes = append(c.m.Classes, Class{Ranges: []rune{r, r}, Desc: int(c.str(quote(t.Value)))})
		cl = int32(len(c.m.Classes) - 1)
	default:
		return -1
	}
	return c.emit(OpGuard, cl, int32(depth), 0)
}

func (c *bcompiler) class(e *grammar.CharClass) int32 {
	cl := Class{Negated: e.Negated, Desc: int(c.str(charClassString(e)))}
	for _, r := range e.Ranges {
		cl.Ranges = append(cl.Ranges, r.Lo, r.Hi)
	}
	c.m.Classes = append(c.m.Classes, cl)
	return int32(len(c.m.Classes) - 1)
}

// match emits code for the expression. If build is true, an expression with a value pushes one
// value.
func (c *bcompiler) match(e grammar.Expr, s *scope, build bool) {
	switch e := e.(type) {
	case *grammar.Literal:
		c.emit(OpStr, c.str(e.Value), c.str(quote(e.Value)), b2i(build))
	case *grammar.CharClass:
		c.emit(OpClass, c.class(e), c.str(charClassString(e)), b2i(build))
	case *grammar.Any:
		c.emit(OpAny, b2i(build), 0, 0)
	case *grammar.Ref:
		r := c.prog.byName[e.Name]
		min := 0
		if e.Level != "" {
			for i, l := range r.def.Expr.(*grammar.Pratt).Levels {
				if l.Name == e.Level {
					min = i
				}
			}
		}
		if !build && r.twin != nil {
			r = r.twin
		}
		c.emit(OpCall, int32(r.id), int32(min), b2i(build))
	case *grammar.Seq:
		if build {
			c.emit(OpPushPos, 0, 0, 0)
		}
		n := 0
		for _, it := range e.Items {
			c.match(it, s, build)
			if build && visible(it) {
				n++
			}
		}
		if build {
			c.emit(OpSeq, int32(n), 0, 0)
		}
	case *grammar.Choice:
		var ends []int
		for i, alt := range e.Alts {
			if i == len(e.Alts)-1 {
				c.value(alt, s, build)
				break
			}
			guard := c.guard(alt)
			ch := c.emit(OpChoice, 0, 0, 0)
			c.value(alt, s, build)
			ends = append(ends, c.emit(OpCommit, 0, 0, 0))
			c.m.Code[ch].A = c.here()
			if guard >= 0 {
				c.m.Code[guard].C = c.here()
			}
		}
		for _, j := range ends {
			c.m.Code[j].A = c.here()
		}
	case *grammar.Repeat:
		c.repeat(e, s, build, false)
	case *grammar.Optional:
		ch := c.emit(OpChoice, 0, 0, 0)
		c.value(e.Expr, s, build)
		end := c.emit(OpCommit, 0, 0, 0)
		c.m.Code[ch].A = c.here()
		if build {
			c.emit(OpPushNil, 0, 0, 0)
		}
		c.m.Code[end].A = c.here()
	case *grammar.And:
		c.emit(OpLook, 0, 0, 0)
		c.match(e.Expr, s, build && hasCaptures(e.Expr))
		c.emit(OpEndLook, 0, 0, 0)
	case *grammar.Not:
		look := c.emit(OpLook, 0, 1, 0)
		c.match(e.Expr, s, false)
		c.emit(OpEndLook, 1, 0, 0)
		c.m.Code[look].A = c.here()
	case *grammar.Atomic:
		if build {
			c.emit(OpPushPos, 0, 0, 0)
		}
		c.match(e.Expr, s, false)
		if build {
			c.emit(OpAtomic, 1, 0, 0)
		}
	case *grammar.Discard:
		c.match(e.Expr, s, false)
	case *grammar.Capture:
		if !build && c.cur.predCaps != nil && !c.cur.predCaps[e.Name] {
			c.match(e.Expr, s, false) // a capture not referenced by the predicate, in a program that builds no tree
			return
		}
		slot := s.slot(e.Name)
		if field := c.proj[e.Name]; field != "" {
			c.projectRepeat(e.Expr.(*grammar.Repeat), field)
		} else {
			c.match(e.Expr, s, true)
		}
		c.emit(OpCapture, int32(slot), b2i(!build), 0)
	case *grammar.Cut:
		c.emit(OpCut, 0, 0, 0)
	case *grammar.Top:
		c.emit(OpTop, b2i(build), 0, 0)
	case *grammar.Bottom:
		c.emit(OpFail, 0, 0, 0)
	case *grammar.BeginInput:
		c.emit(OpAssert, 0, 0, 0)
	case *grammar.EndInput:
		c.emit(OpAssert, 1, 0, 0)
	case *grammar.BeginLine:
		c.emit(OpAssert, 2, 0, 0)
	case *grammar.EndLine:
		c.emit(OpAssert, 3, 0, 0)
	case *grammar.Predicate:
		if a, ok := e.Term.(*grammar.Assign); ok {
			c.emit(OpAssign, c.str(a.Name), int32(c.expr(a.Value, s, nil, false)), 0)
		} else {
			c.emit(OpPred, int32(c.expr(e.Term, s, nil, false)), 0, 0)
		}
	case *grammar.Attributed:
		c.attributed(e, len(e.Attrs)-1, s, build)
	}
}

func (c *bcompiler) repeat(e *grammar.Repeat, s *scope, build, stream bool) {
	if !build && !stream { // value-free repetition of a single character (same as compiler.scanRepeat)
		switch x := e.Expr.(type) {
		case *grammar.CharClass:
			c.repetition(OpScan, e.Min, e.Max, c.class(x))
			return
		case *grammar.Any:
			c.repetition(OpScan, e.Min, e.Max, -1)
			return
		}
	}
	elemScope := s
	sc := int32(-1)
	if elementScoped(e.Expr) {
		elemScope = newScope()
		c.m.Scopes = append(c.m.Scopes, nil)
		sc = int32(len(c.m.Scopes) - 1)
	}
	if build {
		c.emit(OpPushPos, 0, 0, 0)
	}
	c.repetition(OpRepeat, e.Min, e.Max, sc)
	loop := c.here()
	iter := c.emit(OpIter, 0, 0, 0)
	if build {
		c.value(e.Expr, elemScope, true)
	} else {
		c.match(e.Expr, elemScope, false)
	}
	flag := b2i(build)
	if stream {
		flag = 2
	}
	c.emit(OpNext, loop, flag, 0)
	c.m.Code[iter].A = c.here()
	c.emit(OpEndRepeat, b2i(build), 0, 0)
	if sc >= 0 {
		c.m.Scopes[sc] = c.strs2(elemScope.names)
	}
}

// attributed emits code for the expression with attributes up to i applied (inner attributes
// are applied first).
func (c *bcompiler) attributed(e *grammar.Attributed, i int, s *scope, build bool) {
	if i < 0 {
		if rep, ok := e.Expr.(*grammar.Repeat); ok && isStream(e) {
			c.repeat(rep, s, build, true)
		} else {
			c.match(e.Expr, s, build)
		}
		return
	}
	a := e.Attrs[i]
	switch a.Name {
	case "error":
		msg := a.Args[0].Value.(*grammar.Literal).Value
		c.emit(OpLabel, c.str(msg), 0, 0)
		c.attributed(e, i-1, s, build)
		c.emit(OpEndLabel, 0, 0, 0)
	case "recover":
		skip, _ := a.Arg("skip")
		rec := c.emit(OpRecover, 0, 0, 0)
		c.attributed(e, i-1, s, build)
		end := c.emit(OpEndRecover, 0, 0, 0)
		c.m.Code[rec].A = c.here()
		c.match(skip, s, false)
		c.emit(OpEndSkip, b2i(build && visible(e.Expr)), 0, 0) // as in compiler.attributed
		c.m.Code[end].A = c.here()
	default: // #stream is handled by the repetition
		c.attributed(e, i-1, s, build)
	}
}

func (c *bcompiler) pratt(e *grammar.Pratt) int {
	pi := PrattInfo{Skip: -1}
	if e.Skip != nil {
		pi.Skip = int(c.here())
		c.match(e.Skip, newScope(), false)
		c.emit(OpEnd, 0, 0, 0)
	}
	for _, o := range e.Operands {
		pi.Operands = append(pi.Operands, c.line(o.Expr, o.Action, nil))
	}
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			oi := OpInfo{ID: id, Level: i + 1}
			id++
			var locals []string
			switch op.Kind {
			case grammar.Prefix:
				oi.Kind = opPrefix
			case grammar.Postfix:
				oi.Kind = opPostfix
			default:
				oi.Kind = opInfix
			}
			switch op.Assoc {
			case grammar.AssocRight:
				oi.Assoc = assocRight
			case grammar.AssocNone:
				oi.Assoc = assocNone
			}
			// Locals of an operator action: 0 $lhs, 1 $rhs, 2 $op
			locals = []string{"lhs", "rhs", "op"}
			oi.Line = c.line(op.Expr, op.Action, locals)
			if oi.Kind == opPrefix {
				pi.Prefix = append(pi.Prefix, oi)
			} else {
				pi.Led = append(pi.Led, oi)
			}
		}
	}
	c.m.Pratts = append(c.m.Pratts, pi)
	return len(c.m.Pratts) - 1
}

func (c *bcompiler) line(e grammar.Expr, action grammar.Term, locals []string) LineInfo {
	s := newScope()
	li := LineInfo{Entry: int(c.here()), Action: -1}
	_, li.IsSeq = e.(*grammar.Seq)
	c.value(e, s, !c.cur.novalue && !leanLine(action, locals))
	c.emit(OpEnd, 0, 0, 0)
	li.Scope = c.strs2(s.names)
	if action != nil && !c.cur.novalue {
		li.Action = c.expr(action, s, locals, locals == nil)
	}
	return li
}

// expr compiles the value expression into expression code and returns its start position.
func (c *bcompiler) expr(t grammar.Term, s *scope, locals []string, allowIndex bool) int {
	start := len(c.m.Exprs)
	c.term(t, s, locals)
	c.eemit(ERet, 0, 0)
	// Lambda bodies are placed after the expression that contains them.
	for len(c.lambdas) > 0 {
		l := c.lambdas[0]
		c.lambdas = c.lambdas[1:]
		c.m.Exprs[l.at].A = int32(len(c.m.Exprs))
		c.term(l.body, l.scope, l.locals)
		c.eemit(ERet, 0, 0)
	}
	return start
}

// projectRepeat emits a repetition that gathers the values of the capture field of its elements
// (project.go): the elements are matched without values, and NEXT pushes the field's slot.
func (c *bcompiler) projectRepeat(e *grammar.Repeat, field string) {
	elemScope := newScope()
	c.m.Scopes = append(c.m.Scopes, nil)
	sc := int32(len(c.m.Scopes) - 1)
	c.emit(OpPushPos, 0, 0, 0)
	c.repetition(OpRepeat, e.Min, e.Max, sc)
	loop := c.here()
	iter := c.emit(OpIter, 0, 0, 0)
	c.match(e.Expr, elemScope, false)
	c.emit(OpNext, loop, 3, int32(elemScope.slots[field]))
	c.m.Code[iter].A = c.here()
	c.emit(OpEndRepeat, 1, 0, 0)
	c.m.Scopes[sc] = c.strs2(elemScope.names)
}

// identity is the lambda (e) => $e, which takes the place of the projection in a map call of a
// projected repetition: the list holds the values already.
var identity = &grammar.Lambda{Params: []string{"e"}, Body: &grammar.CaptureRef{Name: "e"}}

// concatParts emits the code that gathers the elements of the arguments of a fusable concat call.
func (c *bcompiler) concatParts(t *grammar.Call, s *scope, locals []string) {
	for _, a := range t.Args {
		call := a.(*grammar.Call)
		switch call.Func {
		case "list":
			for _, x := range call.Args {
				c.term(x, s, locals)
			}
			c.eemit(EListPush, int32(len(call.Args)), 0)
		case "map":
			c.term(call.Args[0], s, locals)
			if c.prog.projected[call] {
				c.lambda(identity, s, locals)
			} else {
				c.lambda(call.Args[1], s, locals)
			}
			c.eemit(EMapPush, 0, 0)
		case "concat":
			c.concatParts(call, s, locals)
		}
	}
}

// lambda emits a function value for the lambda t.
func (c *bcompiler) lambda(t grammar.Term, s *scope, locals []string) {
	l, ok := t.(*grammar.Lambda)
	if !ok {
		c.term(t, s, locals)
		return
	}
	at := c.eemit(EFunc, 0, int32(len(l.Params)))
	c.lambdas = append(c.lambdas, pendingLambda{at: at, body: l.Body, scope: s,
		locals: append(append([]string(nil), locals...), l.Params...)})
}

func (c *bcompiler) term(t grammar.Term, s *scope, locals []string) {
	switch t := t.(type) {
	case *grammar.IntLit:
		// A holds the low 32 bits and B the rest (see EInt), so B is 0 for the values of int32.
		lo := int32(t.Value)
		c.eemit(EInt, lo, int32((uint64(t.Value)-uint64(int64(lo)))>>32))
	case *grammar.StringLit:
		c.eemit(EStr, c.str(t.Value), 0)
	case *grammar.BoolLit:
		c.eemit(EBool, b2i(t.Value), 0)
	case *grammar.NilLit:
		c.eemit(ENil, 0, 0)
	case *grammar.CaptureRef:
		for i := len(locals) - 1; i >= 0; i-- {
			if locals[i] == t.Name {
				c.eemit(ELocal, int32(i), 0)
				return
			}
		}
		c.eemit(ECap, int32(s.slots[t.Name]), 0)
	case *grammar.IndexRef:
		c.eemit(EItem, int32(t.Index), 0)
	case *grammar.VarRef:
		c.eemit(EVar, c.str(t.Name), 0)
	case *grammar.Member:
		c.term(t.X, s, locals)
		c.eemit(EMember, c.str(t.Name), 0)
	case *grammar.New:
		name, _ := c.prog.structType(t.Type)
		names := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			c.term(f.Value, s, locals)
			names[i] = f.Name
		}
		c.m.FieldLists = append(c.m.FieldLists, c.strs2(names))
		c.eemit(ENew, c.str(name), int32(len(c.m.FieldLists)-1))
	case *grammar.Call:
		if t.Func == "concat" && fusable(t) {
			// Gather the elements of list, map and concat arguments directly (see fusable).
			c.eemit(EListBegin, 0, 0)
			c.concatParts(t, s, locals)
			c.eemit(EListEnd, 0, 0)
			return
		}
		fn := 0
		for i, n := range builtinNames {
			if n == t.Func {
				fn = i
			}
		}
		for _, a := range t.Args {
			if l, ok := a.(*grammar.Lambda); ok {
				if c.prog.projected[t] {
					l = identity
				}
				at := c.eemit(EFunc, 0, int32(len(l.Params)))
				c.lambdas = append(c.lambdas, pendingLambda{at: at, body: l.Body, scope: s,
					locals: append(append([]string(nil), locals...), l.Params...)})
				continue
			}
			c.term(a, s, locals)
		}
		c.eemit(ECall, int32(fn), int32(len(t.Args)))
	case *grammar.Binary:
		switch t.Op {
		case "&&", "||":
			c.term(t.L, s, locals)
			op := EAnd
			if t.Op == "||" {
				op = EOr
			}
			j := c.eemit(op, 0, 0)
			c.term(t.R, s, locals)
			c.eemit(EBoolChk, c.str(t.Op), 0)
			c.m.Exprs[j].A = int32(len(c.m.Exprs))
		default:
			if lx, ok := textArg(t.L); ok && (t.Op == "==" || t.Op == "!=") {
				if rx, ok := textArg(t.R); ok {
					// Compare the texts without making strings values (see evalCtx.binary). Each
					// argument is checked right after it is evaluated, as text(...) would be.
					c.term(lx, s, locals)
					c.eemit(ETextChk, 0, 0)
					c.term(rx, s, locals)
					c.eemit(ETextChk, 0, 0)
					c.eemit(ETextEq, b2i(t.Op == "!="), 0)
					return
				}
			}
			c.term(t.L, s, locals)
			c.term(t.R, s, locals)
			c.eemit(EBin, c.str(t.Op), 0)
		}
	case *grammar.Unary:
		c.term(t.X, s, locals)
		c.eemit(EUnary, c.str(t.Op), 0)
	}
}
