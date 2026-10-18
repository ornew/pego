package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
)

// This file checks actions in the value model of Record 060: a capture has the value type of what
// it captured, a node exposes .Start, .End and .Value, and .$y reads a capture of a node. Since
// the grammars are written for the current model, the errors are collected but not reported.
//
// tyAny stands for a value the value model does not know yet, such as a variable of a predicate
// or the value of an inline action; it fits everywhere without an error. tyTop is the type _,
// which fits only _.

// valueEnv is the environment of an action: the captures of its scope, and the lambda parameters
// and Operators Rule names $lhs, $op and $rhs.
type valueEnv struct {
	caps   []capIndex
	locals map[string]ty
}

func (e *valueEnv) with(names []string, ts []ty) *valueEnv {
	n := *e
	n.locals = map[string]ty{}
	for k, t := range e.locals {
		n.locals[k] = t
	}
	for i, name := range names {
		n.locals[name] = ts[i]
	}
	return &n
}

func (v *valueInference) errorf(pos grammar.Pos, format string, args ...any) {
	if v.quiet {
		return
	}
	if !pos.IsValid() && v.rule != nil {
		pos = v.rule.Diagnostic()
	}
	v.errs = append(v.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// resolve maps a type expression of the grammar to the value model.
func (v *valueInference) resolve(t grammar.TypeExpr) ty {
	k := &checker{prog: v.prog, resolving: map[string]bool{}}
	return fromLegacy(k.resolve(t))
}

// declared checks the value t of the rule rd against its declared type, which is then the rule's
// type. A declared node keeps the inferred type arguments, and a rule of a terminal type without
// an action converts its value to a string node.
func (v *valueInference) declared(rd *grammar.RuleDef, t ty) ty {
	if rd.Type == nil {
		return t
	}
	k := &checker{prog: v.prog, resolving: map[string]bool{}}
	legacy := k.resolve(rd.Type)
	if n, ok := legacy.(namedTy); ok && n.kind == 't' && rd.Action == nil {
		return tyStringNode
	}
	d := fromLegacy(legacy)
	if !v.assignable(t, d) {
		v.errorf(rd.Diagnostic(), "rule %s produces %s, which is not assignable to %s", rd.Name, t, d)
		return d
	}
	if anyNode(d) {
		return t
	}
	return d
}

// expand returns t with rule types replaced by the rules' value types, at the top level and in
// the arguments of unions and optional types.
func (v *valueInference) expand(t ty) ty {
	for range 64 {
		switch u := t.(type) {
		case ruleTy:
			t = v.ruleValue(u.name)
			continue
		case unionTy:
			alts := make([]ty, len(u.alts))
			for i, a := range u.alts {
				alts[i] = v.expand(a)
			}
			return vunion(alts...)
		case optTy:
			return vopt(v.expand(u.elem))
		}
		return t
	}
	return tyAny
}

// alts returns the arguments of the expanded union t, or t itself.
func (v *valueInference) alts(t ty) []ty {
	if u, ok := v.expand(t).(unionTy); ok {
		return u.alts
	}
	return []ty{v.expand(t)}
}

// assignable reports whether a value of type s fits a location of type d. Subtyping is minimal:
// _|_ <= T <= _, T <= *T, nil <= *T, a union argument is below its union, lists, optional types
// and nodes are covariant, tuples are covariant element-wise, and structs compare by identity.
// node without arguments is every node, and nodes whose captures differ are distinct.
func (v *valueInference) assignable(s, d ty) bool {
	s = v.expand(s)
	if s == tyAny || d == tyAny || d == tyTop || s == tyBottom {
		return true
	}
	if u, ok := s.(unionTy); ok {
		for _, a := range u.alts {
			if !v.assignable(a, d) {
				return false
			}
		}
		return true
	}
	if o, ok := d.(optTy); ok {
		if s == tyNil {
			return true
		}
		if so, ok := s.(optTy); ok {
			return v.assignable(so.elem, o.elem)
		}
		return v.assignable(s, o.elem)
	}
	if u, ok := d.(unionTy); ok {
		for _, a := range u.alts {
			if v.assignable(s, a) {
				return true
			}
		}
		return false
	}
	switch d := d.(type) {
	case listTy:
		sl, ok := s.(listTy)
		return ok && v.assignable(sl.elem, d.elem)
	case tupleTy:
		st, ok := s.(tupleTy)
		if !ok || len(st.elems) != len(d.elems) {
			return false
		}
		for i := range st.elems {
			if !v.assignable(st.elems[i], d.elems[i]) {
				return false
			}
		}
		return true
	case nodeTy:
		sn, ok := s.(nodeTy)
		if !ok {
			return false
		}
		if anyNode(d) {
			return true
		}
		return sameCaps(sn.caps, d.caps) && v.assignable(sn.value, d.value)
	}
	return s == d
}

// anyNode reports whether t is node without type arguments, the type of every node.
func anyNode(t ty) bool {
	n, ok := t.(nodeTy)
	return ok && n.value == tyTop && len(n.caps) == 0
}

func sameCaps(a, b []capIndex) bool {
	return slices.EqualFunc(a, b, func(x, y capIndex) bool {
		return x.name == y.name && x.side == y.side && slices.EqualFunc(x.paths, y.paths, func(p, q []int) bool { return slices.Equal(p, q) })
	})
}

// term returns the type of the value expression t.
func (v *valueInference) term(t grammar.Term, env *valueEnv) ty {
	v.control.poll()
	switch t := t.(type) {
	case *grammar.TermGroup:
		return v.term(t.Term, env)
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
		for _, c := range env.caps {
			if c.name == t.Name {
				return c.t
			}
		}
		// Undefined captures were reported when names were resolved; this one has no value.
		v.errorf(t.Diagnostic(), "$%s has no value in its scope", t.Name)
		return tyAny
	case *grammar.Member:
		return v.member(v.term(t.X, env), t)
	case *grammar.New:
		name, st := v.prog.structType(t.Type)
		for _, fi := range t.Fields {
			vt := v.term(fi.Value, env)
			if st == nil {
				continue
			}
			if f := fieldOf(st, fi.Name); f != nil {
				if ft := v.resolve(f.Type); !v.assignable(vt, ft) {
					v.errorf(fi.Diagnostic(), "cannot use %s as %s in field %s of %s", vt, ft, fi.Name, t.Type)
				}
			}
		}
		if st == nil {
			return tyAny
		}
		return namedTy{name, 's'}
	case *grammar.ListConstructor:
		elem := v.resolve(t.Elem)
		for _, item := range t.Items {
			if it := v.term(item, env); !v.assignable(it, elem) {
				v.errorf(termPos(item, t.Diagnostic()), "cannot use %s as %s in list constructor", it, elem)
			}
		}
		return listTy{elem}
	case *grammar.TerminalConstructor:
		for _, arg := range t.Args {
			v.term(arg, env)
		}
		v.errorf(t.Diagnostic(), "%s{...} constructs a node, which actions cannot construct", t.Type)
		return tyStringNode
	case *grammar.Call:
		return v.call(t, env)
	case *grammar.Cast:
		v.term(t.X, env)
		return v.resolve(t.Type)
	case *grammar.Unary:
		x := v.term(t.X, env)
		want := tyInt
		if t.Op == "!" {
			want = tyBool
		}
		if !v.assignable(x, want) {
			v.errorf(t.Diagnostic(), "invalid operand %s for unary %s", v.expand(x), t.Op)
		}
		return want
	case *grammar.Binary:
		return v.binary(t, env)
	}
	// Positional captures are rejected when names are resolved; variables, assignments, lambdas
	// outside calls and blocks are checked by the current model.
	return tyAny
}

func (v *valueInference) binary(t *grammar.Binary, env *valueEnv) ty {
	l, r := v.term(t.L, env), v.term(t.R, env)
	both := func(want ty) bool { return v.assignable(l, want) && v.assignable(r, want) }
	bad := func() ty {
		v.errorf(t.Diagnostic(), "invalid operands %s and %s for %s", v.expand(l), v.expand(r), t.Op)
		return tyAny
	}
	switch t.Op {
	case "[]":
		return v.index(l, r, t)
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
		if v.expand(l) == tyAny || v.expand(r) == tyAny {
			return tyAny
		}
		if both(tyInt) {
			return tyInt
		}
		if both(tyString) {
			return tyString
		}
		return bad()
	}
	if !both(tyInt) {
		return bad()
	}
	return tyInt
}

// index returns the type of x[i]: an element of a list or of a tuple. A node is indexed through
// its Value.
func (v *valueInference) index(x, i ty, t *grammar.Binary) ty {
	if !v.assignable(i, tyInt) {
		v.errorf(t.Diagnostic(), "index must be int, got %s", i)
	}
	var out []ty
	var elems func(a ty) bool
	elems = func(a ty) bool {
		switch a := a.(type) {
		case listTy:
			out = append(out, a.elem)
			return true
		case tupleTy:
			if lit, ok := ungroupTerm(t.R).(*grammar.IntLit); ok {
				if lit.Value < 0 || lit.Value >= len(a.elems) {
					v.errorf(t.Diagnostic(), "index %d is out of range for %s", lit.Value, a)
					return false
				}
				out = append(out, a.elems[lit.Value])
			} else {
				out = append(out, a.elems...)
			}
			return true
		case nodeTy:
			for _, b := range v.alts(a.value) {
				if !elems(b) {
					return false
				}
			}
			return true
		case basicTy:
			switch a {
			case tyAny:
				out = append(out, tyAny)
				return true
			case tyBottom:
				return true
			}
		}
		v.errorf(t.Diagnostic(), "cannot index %s", a)
		return false
	}
	for _, a := range v.alts(x) {
		if !elems(a) {
			return tyAny
		}
	}
	return vunion(out...)
}

func ungroupTerm(t grammar.Term) grammar.Term {
	for {
		g, ok := t.(*grammar.TermGroup)
		if !ok {
			return t
		}
		t = g.Term
	}
}

// member returns the type of x.Name. A member that only some arguments of a union have is nil for
// the others, as for struct fields in the current model; .$y needs every argument to capture y.
func (v *valueInference) member(x ty, m *grammar.Member) ty {
	x = v.expand(x)
	if x == tyAny || x == tyBottom {
		return x
	}
	switch m.Name {
	case "Source":
		v.errorf(m.Diagnostic(), "the source information of a node is not accessible from actions")
		return tyAny
	case "startPos", "endPos":
		v.errorf(m.Diagnostic(), ".%s is removed: use .Start or .End", m.Name)
		return tyInt
	case "children":
		v.errorf(m.Diagnostic(), ".children is removed: use .Value")
		return tyAny
	}
	if _, ok := x.(optTy); ok {
		v.errorf(m.Diagnostic(), "cannot access .%s of %s: the value may be nil", m.Name, x)
		return tyAny
	}
	if name, ok := strings.CutPrefix(m.Name, "$"); ok {
		return v.captureAccess(x, name, m)
	}
	alts := v.alts(x)
	var found []ty
	for _, a := range alts {
		switch a := a.(type) {
		case nodeTy:
			switch m.Name {
			case "Start", "End":
				found = append(found, tyInt)
			case "Value":
				found = append(found, a.value)
			}
		case basicTy:
			switch {
			case a == tyAny:
				return tyAny
			case a == tyErr && (m.Name == "Start" || m.Name == "End"):
				found = append(found, tyInt)
			}
		case namedTy:
			if st, ok := v.prog.types[a.name].(*grammar.StructSpec); ok {
				if f := fieldOf(st, m.Name); f != nil {
					found = append(found, v.resolve(f.Type))
				}
			}
		}
	}
	if len(found) == 0 {
		if n, ok := x.(nodeTy); ok && slices.ContainsFunc(n.caps, func(c capIndex) bool { return c.name == m.Name }) {
			v.errorf(m.Diagnostic(), "%s has no field %s: read the capture with .$%s", x, m.Name, m.Name)
		} else {
			v.errorf(m.Diagnostic(), "%s has no field %s", x, m.Name)
		}
		return tyAny
	}
	if len(found) < len(alts) {
		return vopt(vunion(found...))
	}
	return vunion(found...)
}

// captureAccess returns the type of x.$name: every argument of x must be a node that captures
// name, and the type is the join of their capture types.
func (v *valueInference) captureAccess(x ty, name string, m *grammar.Member) ty {
	var found []ty
	for _, a := range v.alts(x) {
		if a == tyAny {
			return tyAny
		}
		if a == tyBottom {
			continue
		}
		n, ok := a.(nodeTy)
		i := -1
		if ok {
			i = slices.IndexFunc(n.caps, func(c capIndex) bool { return c.name == name })
		}
		if i < 0 {
			v.errorf(m.Diagnostic(), "%s has no capture %s", a, name)
			return tyAny
		}
		found = append(found, n.caps[i].t)
	}
	return vunion(found...)
}

// elemOf returns the type of the elements that list functions read from x: the elements of a
// list, or of the list a node holds, as for the node of a repetition. nil has no elements.
func (v *valueInference) elemOf(fn string, x ty, pos grammar.Pos) ty {
	t, ok := v.elemsOf(x)
	if !ok {
		v.errorf(pos, "%s: expected a list, got %s", fn, v.expand(x))
		return tyAny
	}
	return t
}

func (v *valueInference) elemsOf(x ty) (ty, bool) {
	switch x := v.expand(x).(type) {
	case listTy:
		return x.elem, true
	case nodeTy:
		if l, ok := v.expand(x.value).(listTy); ok {
			return l.elem, true
		}
	case optTy:
		return v.elemsOf(x.elem)
	case unionTy:
		ts := make([]ty, len(x.alts))
		for i, a := range x.alts {
			t, ok := v.elemsOf(a)
			if !ok {
				return nil, false
			}
			ts[i] = t
		}
		return vunion(ts...), true
	case basicTy:
		switch x {
		case tyAny:
			return tyAny, true
		case tyNil, tyBottom:
			return tyBottom, true
		}
	}
	return nil, false
}

// lambda returns the function argument of a call with n parameters.
func (v *valueInference) lambda(t grammar.Term, n int) (*grammar.Lambda, bool) {
	l, ok := t.(*grammar.Lambda)
	return l, ok && len(l.Params) == n
}

func (v *valueInference) call(t *grammar.Call, env *valueEnv) ty {
	if n, ok := builtins[t.Func]; !ok || n >= 0 && n != len(t.Args) {
		return tyAny // reported by the current model
	}
	arg := func(i int) ty { return v.term(t.Args[i], env) }
	switch t.Func {
	case "len":
		a := arg(0)
		for _, x := range v.alts(a) {
			if o, ok := x.(optTy); ok {
				x = v.expand(o.elem)
			}
			switch x.(type) {
			case nodeTy, listTy, tupleTy:
				continue
			}
			if x != tyString && x != tyNil && x != tyAny && x != tyBottom {
				v.errorf(t.Diagnostic(), "len: invalid argument %s", a)
				break
			}
		}
		return tyInt
	case "text":
		arg(0)
		v.errorf(t.Diagnostic(), "text is removed: read the string of a node with .Value")
		return tyString
	case "foldl", "foldr":
		acc := arg(0)
		elem := v.elemOf(t.Func, arg(1), t.Diagnostic())
		l, ok := v.lambda(t.Args[2], 2)
		if !ok {
			return tyAny
		}
		// If the body's type does not fit the initial value's type, take the union and check again.
		for range 3 {
			saved := v.quiet
			v.quiet = true
			body := v.term(l.Body, env.with(l.Params, []ty{acc, elem}))
			v.quiet = saved
			if v.assignable(body, acc) {
				break
			}
			acc = vunion(acc, body)
		}
		v.term(l.Body, env.with(l.Params, []ty{acc, elem}))
		return acc
	case "filter":
		elem := v.elemOf("filter", arg(0), t.Diagnostic())
		l, ok := v.lambda(t.Args[1], 1)
		if !ok {
			return tyAny
		}
		if body := v.term(l.Body, env.with(l.Params, []ty{elem})); !v.assignable(body, tyBool) {
			v.errorf(t.Diagnostic(), "filter: predicate must produce a bool, got %s", body)
		}
		return listTy{elem}
	case "map":
		elem := v.elemOf("map", arg(0), t.Diagnostic())
		l, ok := v.lambda(t.Args[1], 1)
		if !ok {
			return tyAny
		}
		return listTy{v.term(l.Body, env.with(l.Params, []ty{elem}))}
	case "list":
		ts := make([]ty, len(t.Args))
		for i := range t.Args {
			ts[i] = arg(i)
		}
		return listTy{vunion(ts...)}
	case "concat":
		ts := make([]ty, len(t.Args))
		for i := range t.Args {
			ts[i] = v.elemOf("concat", arg(i), t.Diagnostic())
		}
		return listTy{vunion(ts...)}
	}
	return tyAny
}
