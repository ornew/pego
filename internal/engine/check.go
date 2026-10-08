package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

// maxInferredType is the length of the written form of an inferred rule type beyond which the
// rule's type is taken to be any.
const maxInferredType = 2048

// checker infers and checks the types of a grammar.
//
// The types of rules without a declared type are found by repeating inference until no rule's
// type changes (the least fixed point). The grammar is then checked once more, reporting errors.
type checker struct {
	prog   *Program
	errs   *ErrorList
	report bool
	// ruleTypes holds the type of each rule (declared, or being inferred).
	ruleTypes map[string]ty
	declared  map[string]bool
	// resolving holds the type aliases being resolved (for cycle detection).
	resolving map[string]bool
	rule      *grammar.RuleDef
}

func (k *checker) errorf(pos grammar.Pos, format string, args ...any) {
	if !k.report {
		return
	}
	if !pos.IsValid() && k.rule != nil {
		pos = k.rule.Pos
	}
	*k.errs = append(*k.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// checkTypes checks the types of the whole grammar.
func checkTypes(prog *Program, errs *ErrorList) {
	k := &checker{prog: prog, errs: errs, ruleTypes: map[string]ty{}, declared: map[string]bool{}, resolving: map[string]bool{}}

	// Check references in type definitions.
	k.report = true
	for _, td := range prog.Grammar.Types() {
		switch spec := td.Spec.(type) {
		case *grammar.StructSpec:
			seen := map[string]bool{}
			for _, f := range spec.Fields {
				if seen[f.Name] {
					k.errorf(f.Pos, "duplicate field %s in %s", f.Name, td.Name)
				}
				if !startsUpper(f.Name) {
					k.errorf(f.Pos, "field %s of %s must start with an uppercase letter", f.Name, td.Name)
				}
				seen[f.Name] = true
				k.resolve(f.Type)
			}
		case *grammar.AliasSpec:
			k.resolveNamed(td.Name, td.Pos)
		}
	}

	rules := prog.Grammar.Rules()
	for _, rd := range rules {
		if rd.Type != nil {
			t := k.resolve(rd.Type)
			if !isNodeOrNil(t) {
				k.errorf(rd.Pos, "rule %s must produce a node, but its type is %s", rd.Name, t)
				t = tyAny
			}
			k.ruleTypes[rd.Name] = t
			k.declared[rd.Name] = true
		} else {
			k.ruleTypes[rd.Name] = tyNever
		}
	}

	// Inference (errors are not reported).
	k.report = false
	for i := 0; i < 50; i++ {
		changed := false
		for _, rd := range rules {
			if k.declared[rd.Name] {
				continue
			}
			t := k.ruleType(rd)
			ts := t.String()
			if len(ts) > maxInferredType {
				// A type that keeps growing (such as lists of lists of the rule's own values) can
				// double in size at every round, which would take exponential time; give up on it.
				t, ts = tyAny, tyAny.String()
			}
			if ts != k.ruleTypes[rd.Name].String() {
				k.ruleTypes[rd.Name] = t
				changed = true
			}
		}
		if !changed {
			break
		}
		if i == 49 {
			// If inference does not converge (types grow without bound), use any.
			for _, rd := range rules {
				if !k.declared[rd.Name] {
					k.ruleTypes[rd.Name] = tyAny
				}
			}
		}
	}

	// Checking (errors are reported).
	k.report = true
	for _, rd := range rules {
		k.ruleType(rd)
	}
}

// resolve turns a type expression into a type.
func (k *checker) resolve(t grammar.TypeExpr) ty {
	switch t := t.(type) {
	case *grammar.TypeRef:
		return k.resolveNamed(t.Name, t.Pos)
	case *grammar.ListType:
		return listTy{k.resolve(t.Elem)}
	case *grammar.OptionalType:
		return optional(k.resolve(t.Elem))
	case *grammar.UnionType:
		ts := make([]ty, len(t.Types))
		for i, x := range t.Types {
			ts[i] = k.resolve(x)
		}
		return union(ts...)
	}
	return tyAny
}

func (k *checker) resolveNamed(name string, pos grammar.Pos) ty {
	switch name {
	case "int":
		return tyInt
	case "string":
		return tyString
	case "bool":
		return tyBool
	case "node":
		return tyNode
	case "terminal":
		return tyTerminal
	case "any":
		return tyAny
	case TypeMatch, TypeSeq, TypeList, TypeOperator, TypeError:
		return namedTy{name, 'c'}
	}
	switch spec := k.prog.types[name].(type) {
	case *grammar.StructSpec:
		return namedTy{name, 's'}
	case *grammar.TerminalSpec:
		return namedTy{name, 't'}
	case *grammar.AliasSpec:
		if k.resolving[name] {
			k.errorf(pos, "type %s refers to itself", name)
			return tyAny
		}
		k.resolving[name] = true
		defer delete(k.resolving, name)
		return k.resolve(spec.Type)
	}
	k.errorf(pos, "undefined type %s", name)
	return tyAny
}

// capInfo is the type of a capture in a scope. If always is false, the capture may not match
// (it may be nil).
type capInfo struct {
	t      ty
	always bool
}

type caps map[string]capInfo

func (c caps) typeOf(name string) (ty, bool) {
	ci, ok := c[name]
	if !ok {
		return nil, false
	}
	if ci.always {
		return ci.t, true
	}
	return optional(ci.t), true
}

// record returns the type of a node that holds the captures.
func (c caps) record() recordTy {
	r := recordTy{fields: map[string]ty{}}
	for n := range c {
		r.fields[n], _ = c.typeOf(n)
	}
	return r
}

// scopeCheck is the checking state of one scope (a rule body, a repetition element, or a Pratt
// line).
type scopeCheck struct {
	caps  caps
	preds []grammar.Term // predicates checked after the scope has been examined
}

// expr returns the type of the expression's value and records captures in sc. It returns nil for
// an expression without a value.
func (k *checker) expr(e grammar.Expr, sc *scopeCheck) ty {
	switch e := e.(type) {
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Top, *grammar.Atomic:
		return tyMatch
	case *grammar.Ref:
		if t, ok := k.ruleTypes[e.Name]; ok {
			return t
		}
		return tyAny
	case *grammar.Seq:
		for _, it := range e.Items {
			k.expr(it, sc)
		}
		return tySeq
	case *grammar.Choice:
		var ts []ty
		var all []caps
		for _, alt := range e.Alts {
			sub := &scopeCheck{caps: caps{}}
			ts = append(ts, k.expr(alt, sub))
			all = append(all, sub.caps)
			sc.preds = append(sc.preds, sub.preds...)
		}
		// Only names captured in every alternative always have a value.
		for _, c := range all {
			for n, ci := range c {
				inAll := true
				for _, o := range all {
					if oc, ok := o[n]; !ok || !oc.always {
						inAll = false
					}
				}
				k.addCapture(sc, n, capInfo{t: ci.t, always: inAll})
			}
		}
		return union(ts...)
	case *grammar.Repeat:
		inner := &scopeCheck{caps: caps{}}
		t := k.expr(e.Expr, inner)
		k.checkPreds(inner)
		if len(inner.caps) > 0 {
			t = inner.caps.record()
		}
		if t == nil {
			t = tyNever
		}
		return listTy{t}
	case *grammar.Optional:
		sub := &scopeCheck{caps: caps{}}
		t := k.expr(e.Expr, sub)
		for n, ci := range sub.caps {
			k.addCapture(sc, n, capInfo{t: ci.t, always: false})
		}
		sc.preds = append(sc.preds, sub.preds...)
		if t == nil {
			return tyNil
		}
		return optional(t)
	case *grammar.And:
		k.expr(e.Expr, sc)
		return nil
	case *grammar.Not, *grammar.Discard, *grammar.Cut, *grammar.Bottom, *grammar.BeginInput,
		*grammar.EndInput, *grammar.BeginLine, *grammar.EndLine:
		return nil
	case *grammar.Capture:
		t := k.expr(e.Expr, sc)
		if t == nil {
			t = tyAny
		}
		k.addCapture(sc, e.Name, capInfo{t: t, always: true})
		return t
	case *grammar.Predicate:
		sc.preds = append(sc.preds, e.Term)
		return nil
	case *grammar.Attributed:
		return k.attributed(e, sc)
	}
	return tyAny
}

func (k *checker) addCapture(sc *scopeCheck, name string, ci capInfo) {
	if old, ok := sc.caps[name]; ok {
		ci = capInfo{t: union(old.t, ci.t), always: old.always || ci.always}
	}
	sc.caps[name] = ci
}

func (k *checker) checkPreds(sc *scopeCheck) {
	env := &termEnv{caps: sc.caps}
	for _, p := range sc.preds {
		if a, ok := p.(*grammar.Assign); ok {
			t := k.term(a.Value, env)
			if !assignable(t, union(tyInt, tyString, tyBool)) {
				k.errorf(a.Pos, "variable %s must be int, string, or bool, got %s", a.Name, t)
			}
			continue
		}
		k.term(p, env)
	}
}

// ruleType computes the type of the rule's value (and reports errors if report is true).
func (k *checker) ruleType(rd *grammar.RuleDef) ty {
	k.rule = rd
	declared := k.ruleTypes[rd.Name]
	if !k.declared[rd.Name] {
		declared = nil
	}
	if pr, ok := rd.Expr.(*grammar.Pratt); ok {
		return k.prattType(rd, pr, declared)
	}
	sc := &scopeCheck{caps: caps{}}
	bodyT := k.expr(rd.Expr, sc)
	k.checkPreds(sc)
	var items []ty
	if seq, ok := rd.Expr.(*grammar.Seq); ok {
		for _, it := range seq.Items {
			if visible(it) {
				items = append(items, k.expr(it, &scopeCheck{caps: caps{}}))
			}
		}
	} else if bodyT != nil {
		items = []ty{bodyT}
	}
	var t ty
	if rd.Action != nil {
		t = k.term(rd.Action, &termEnv{caps: sc.caps, items: items, allowIndex: true})
		if !isNodeOrNil(t) {
			k.errorf(termPos(rd.Action, rd.Pos), "action of %s must produce a node, got %s", rd.Name, t)
			t = tyAny
		}
	} else {
		if bodyT == nil {
			bodyT = tyNil
		}
		t = bodyT
		if len(sc.caps) > 0 {
			t = sc.caps.record()
		}
		if declared != nil {
			if n, ok := declared.(namedTy); ok && n.kind == 't' {
				return declared // conversion to a terminal type
			}
		}
	}
	if declared != nil {
		if !assignable(t, declared) {
			k.errorf(rd.Pos, "rule %s produces %s, which is not assignable to %s", rd.Name, t, declared)
		}
		return declared
	}
	return t
}

func (k *checker) prattType(rd *grammar.RuleDef, pr *grammar.Pratt, declared ty) ty {
	self := k.ruleTypes[rd.Name]
	var ts []ty
	if pr.Skip != nil {
		k.expr(pr.Skip, &scopeCheck{caps: caps{}})
	}
	for _, o := range pr.Operands {
		ts = append(ts, k.lineType(o.Expr, o.Action, nil, rd.Pos))
	}
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			if op.Action == nil {
				ts = append(ts, tyOperator)
				k.lineType(op.Expr, nil, nil, op.Pos)
				continue
			}
			locals := map[string]ty{"op": tyMatch}
			if op.Kind != grammar.Prefix {
				locals["lhs"] = self
			}
			if op.Kind != grammar.Postfix {
				locals["rhs"] = self
			}
			ts = append(ts, k.lineType(op.Expr, op.Action, locals, op.Pos))
		}
	}
	if declared != nil {
		for _, t := range ts {
			if !assignable(t, declared) {
				if t == tyOperator {
					k.errorf(rd.Pos, "operators of %s without an action produce Operator, which is not assignable to %s", rd.Name, declared)
				} else {
					k.errorf(rd.Pos, "pratt %s produces %s, which is not assignable to %s", rd.Name, t, declared)
				}
			}
		}
		return declared
	}
	return union(ts...)
}

// lineType returns the value type of a Pratt operand line or operator line.
func (k *checker) lineType(e grammar.Expr, action grammar.Term, locals map[string]ty, pos grammar.Pos) ty {
	sc := &scopeCheck{caps: caps{}}
	t := k.expr(e, sc)
	k.checkPreds(sc)
	if action == nil {
		if len(sc.caps) > 0 {
			return sc.caps.record()
		}
		if t == nil {
			return tyNil
		}
		return t
	}
	env := &termEnv{caps: sc.caps, locals: locals}
	if locals == nil {
		// Operand lines may use $n.
		env.allowIndex = true
		if seq, ok := e.(*grammar.Seq); ok {
			for _, it := range seq.Items {
				if visible(it) {
					env.items = append(env.items, k.expr(it, &scopeCheck{caps: caps{}}))
				}
			}
		} else if t != nil {
			env.items = []ty{t}
		}
	}
	at := k.term(action, env)
	if !isNodeOrNil(at) {
		k.errorf(termPos(action, pos), "action must produce a node, got %s", at)
		return tyAny
	}
	return at
}

func (k *checker) attributed(e *grammar.Attributed, sc *scopeCheck) ty {
	t := k.expr(e.Expr, sc)
	for _, a := range e.Attrs {
		for _, arg := range a.Args {
			k.expr(arg.Value, &scopeCheck{caps: caps{}})
		}
		if a.Name == "recover" && t != nil {
			t = union(t, tyError)
		}
	}
	return t
}

// termEnv is the environment for type checking value expressions.
type termEnv struct {
	caps       caps
	items      []ty
	allowIndex bool
	locals     map[string]ty
}

func (e *termEnv) with(names []string, ts []ty) *termEnv {
	n := *e
	n.locals = map[string]ty{}
	for k, v := range e.locals {
		n.locals[k] = v
	}
	for i, name := range names {
		n.locals[name] = ts[i]
	}
	return &n
}

func termPos(t grammar.Term, def grammar.Pos) grammar.Pos {
	switch t := t.(type) {
	case *grammar.CaptureRef:
		return t.Pos
	case *grammar.IndexRef:
		return t.Pos
	case *grammar.VarRef:
		return t.Pos
	case *grammar.Member:
		return t.Pos
	case *grammar.New:
		return t.Pos
	case *grammar.Call:
		return t.Pos
	case *grammar.Binary:
		return t.Pos
	case *grammar.Unary:
		return t.Pos
	case *grammar.Assign:
		return t.Pos
	}
	return def
}

// term returns the type of the value expression.
func (k *checker) term(t grammar.Term, env *termEnv) ty {
	switch t := t.(type) {
	case *grammar.IntLit:
		return tyInt
	case *grammar.StringLit:
		return tyString
	case *grammar.BoolLit:
		return tyBool
	case *grammar.NilLit:
		return tyNil
	case *grammar.CaptureRef:
		if lt, ok := env.locals[t.Name]; ok {
			return lt
		}
		if ct, ok := env.caps.typeOf(t.Name); ok {
			return ct
		}
		return tyAny // undefined captures were already reported during name resolution
	case *grammar.IndexRef:
		if !env.allowIndex {
			return tyAny
		}
		if t.Index == 0 {
			return listTy{union(env.items...)}
		}
		if t.Index > len(env.items) {
			k.errorf(t.Pos, "$%d is out of range: the rule has %d elements with a value", t.Index, len(env.items))
			return tyAny
		}
		return env.items[t.Index-1]
	case *grammar.VarRef:
		return tyAny
	case *grammar.Member:
		return k.member(k.term(t.X, env), t)
	case *grammar.New:
		st, ok := k.prog.types[t.Type].(*grammar.StructSpec)
		for _, fi := range t.Fields {
			vt := k.term(fi.Value, env)
			if !ok {
				continue
			}
			if f := fieldOf(st, fi.Name); f != nil {
				ft := k.resolve(f.Type)
				if !assignable(vt, ft) {
					k.errorf(fi.Pos, "cannot use %s as %s in field %s of %s", vt, ft, fi.Name, t.Type)
				}
			}
		}
		if !ok {
			return tyAny
		}
		return namedTy{t.Type, 's'}
	case *grammar.Call:
		return k.call(t, env)
	case *grammar.Lambda:
		k.errorf(grammar.Pos{}, "functions can only be passed to foldl, foldr, or map")
		return tyAny
	case *grammar.Unary:
		x := k.term(t.X, env)
		want := tyInt
		if t.Op == "!" {
			want = tyBool
		}
		if !assignable(x, want) {
			k.errorf(t.Pos, "invalid operand %s for unary %s", x, t.Op)
		}
		return want
	case *grammar.Binary:
		l, r := k.term(t.L, env), k.term(t.R, env)
		bad := func() ty {
			k.errorf(t.Pos, "invalid operands %s and %s for %s", l, r, t.Op)
			return tyAny
		}
		both := func(want ty) bool { return assignable(l, want) && assignable(r, want) }
		switch t.Op {
		case "==", "!=":
			return tyBool
		case "&&", "||":
			if !both(tyBool) {
				return bad()
			}
			return tyBool
		case "<", "<=", ">", ">=":
			if !both(tyInt) && !both(tyString) {
				return bad()
			}
			return tyBool
		case "+":
			if l == tyAny || r == tyAny {
				return tyAny
			}
			if both(tyInt) {
				return tyInt
			}
			if both(tyString) {
				return tyString
			}
			return bad()
		default:
			if !both(tyInt) {
				return bad()
			}
			return tyInt
		}
	case *grammar.Assign:
		return tyAny
	}
	return tyAny
}

func (k *checker) member(x ty, m *grammar.Member) ty {
	switch m.Name {
	case "startPos", "endPos":
		if !isNode(x) {
			k.errorf(m.Pos, "cannot access .%s of %s", m.Name, x)
		}
		return tyInt
	case "children":
		if !isNode(x) {
			k.errorf(m.Pos, "cannot access .%s of %s", m.Name, x)
		}
		return listTy{optional(tyNode)}
	}
	if x == tyAny {
		return tyAny
	}
	if _, ok := x.(optTy); ok {
		k.errorf(m.Pos, "cannot access .%s of %s: the value may be nil", m.Name, x)
		return tyAny
	}
	var alts []ty
	if u, ok := x.(unionTy); ok {
		alts = u.alts
	} else {
		alts = []ty{x}
	}
	var found []ty
	for _, a := range alts {
		switch a := a.(type) {
		case namedTy:
			if st, ok := k.prog.types[a.name].(*grammar.StructSpec); ok {
				if f := fieldOf(st, m.Name); f != nil {
					found = append(found, k.resolve(f.Type))
				}
			}
		case recordTy:
			if ft, ok := a.fields[m.Name]; ok {
				found = append(found, ft)
			}
		case basicTy:
			if a == tyNode {
				found = append(found, tyAny)
			}
		}
	}
	if len(found) == 0 {
		k.errorf(m.Pos, "%s has no field %s", x, m.Name)
		return tyAny
	}
	if len(found) < len(alts) {
		// A field present in only some of the types is nil for the others.
		return optional(union(found...))
	}
	return union(found...)
}

// elemType returns the element type of a list (or an optional list).
func (k *checker) elemType(fn string, t ty, pos grammar.Pos) ty {
	switch t := t.(type) {
	case listTy:
		return t.elem
	case optTy:
		return k.elemType(fn, t.elem, pos)
	case unionTy:
		// A union of list types, such as []A | []B, holds elements of either type.
		elems := make([]ty, len(t.alts))
		for i, alt := range t.alts {
			elems[i] = k.elemType(fn, alt, pos)
		}
		return union(elems...)
	}
	switch t {
	case tyNever:
		return tyNever
	case tyError:
		// An Error node from #recover has no children, so it behaves as an empty list.
		return tyNever
	case tyAny, tyNil, tyNode:
		return tyAny
	case tyList, tySeq, tyOperator:
		return optional(tyNode)
	}
	k.errorf(pos, "%s: expected a list, got %s", fn, t)
	return tyAny
}

func (k *checker) lambda(fn string, t grammar.Term, n int, pos grammar.Pos) (*grammar.Lambda, bool) {
	l, ok := t.(*grammar.Lambda)
	if !ok || len(l.Params) != n {
		k.errorf(pos, "%s: expected a function of %d parameters", fn, n)
		return nil, false
	}
	return l, true
}

func (k *checker) call(t *grammar.Call, env *termEnv) ty {
	if n, ok := builtins[t.Func]; !ok || n >= 0 && n != len(t.Args) {
		return tyAny // already reported during name resolution
	}
	argT := func(i int) ty { return k.term(t.Args[i], env) }
	switch t.Func {
	case "len":
		a := argT(0)
		if !assignable(a, union(tyString, optional(tyNode))) {
			k.errorf(t.Pos, "len: invalid argument %s", a)
		}
		return tyInt
	case "text":
		a := argT(0)
		if !assignable(a, union(tyString, optional(tyNode))) {
			k.errorf(t.Pos, "text: invalid argument %s", a)
		}
		return tyString
	case "foldl", "foldr":
		acc := argT(0)
		elem := k.elemType(t.Func, argT(1), t.Pos)
		l, ok := k.lambda(t.Func, t.Args[2], 2, t.Pos)
		if !ok {
			return tyAny
		}
		// If the body's type does not fit the initial value's type, take the union and check again.
		for i := 0; i < 3; i++ {
			saved := k.report
			k.report = false
			body := k.term(l.Body, env.with(l.Params, []ty{acc, elem}))
			k.report = saved
			if assignable(body, acc) {
				break
			}
			acc = union(acc, body)
		}
		k.term(l.Body, env.with(l.Params, []ty{acc, elem}))
		return acc
	case "map":
		elem := k.elemType("map", argT(0), t.Pos)
		l, ok := k.lambda("map", t.Args[1], 1, t.Pos)
		if !ok {
			return tyAny
		}
		body := k.term(l.Body, env.with(l.Params, []ty{elem}))
		if !isNodeOrNil(body) {
			k.errorf(t.Pos, "map: the function must produce a node, got %s", body)
		}
		return listTy{body}
	case "list":
		var elems []ty
		for i := range t.Args {
			a := argT(i)
			if !isNode(a) && !assignable(a, optional(tyNode)) {
				k.errorf(t.Pos, "list: elements must be nodes, got %s", a)
			}
			elems = append(elems, a)
		}
		return listTy{union(elems...)}
	case "concat":
		var elems []ty
		for i := range t.Args {
			elems = append(elems, k.elemType("concat", argT(i), t.Pos))
		}
		return listTy{union(elems...)}
	}
	return tyAny
}
