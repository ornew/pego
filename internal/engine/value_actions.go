package engine

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
)

// This file checks actions in the value model: a capture has the value type of what it captured,
// a node exposes .Start, .End and .Value, and .$y reads a capture of a node.
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
	k := &typeResolver{prog: v.prog}
	return k.resolve(t)
}

// uncertain reports whether a value of type t may be one the value types do not know yet (tyAny),
// so that a typed location checks it at run time.
func (v *valueInference) uncertain(t ty) bool {
	return v.containsBasic(t, 0, false)
}

// needsCast reports whether a value of type t, which is assignable to target, may still not fit it
// because t is not known well enough where target is more specific: the value must be cast.
func (v *valueInference) needsCast(t, target ty) bool {
	return v.uncertain(t) && v.needsCastAt(t, target, 0)
}

func (v *valueInference) needsCastAt(t, target ty, depth int) bool {
	if depth > 32 || !v.uncertain(t) {
		return depth > 32
	}
	switch target := target.(type) {
	case basicTy:
		return target != tyAny && target != tyTop
	case listTy:
		if l, ok := v.expand(t).(listTy); ok {
			return v.needsCastAt(l.elem, target.elem, depth+1)
		}
	case optTy:
		if o, ok := v.expand(t).(optTy); ok {
			return v.needsCastAt(o.elem, target.elem, depth+1)
		}
	}
	return true
}

// narrowable reports whether a value of type t, which does not fit a location, may do so once it
// is cast: its type is not known (tyAny) or not specific (tyTop) in some part.
func (v *valueInference) narrowable(t ty) bool {
	return v.containsBasic(t, 0, true)
}

// castHint is the hint appended to the message about a value of type t that does not fit a
// location of type target, if a cast may make it fit, and "" otherwise.
func (v *valueInference) castHint(t ty, target any) string {
	if !v.narrowable(t) {
		return ""
	}
	return fmt.Sprintf(": the type of the value is not known; convert it with a cast, as in value.(%v)", target)
}

// containsBasic reports whether t has a part of type tyAny, or also of tyTop if top is set.
func (v *valueInference) containsBasic(t ty, depth int, top bool) bool {
	if depth > 32 {
		return true
	}
	switch t := v.expand(t).(type) {
	case basicTy:
		return t == tyAny || top && t == tyTop
	case listTy:
		return v.containsBasic(t.elem, depth+1, top)
	case optTy:
		return v.containsBasic(t.elem, depth+1, top)
	case nodeTy:
		return v.containsBasic(t.value, depth+1, top)
	case unionTy:
		return slices.ContainsFunc(t.alts, func(a ty) bool { return v.containsBasic(a, depth+1, top) })
	case tupleTy:
		return slices.ContainsFunc(t.elems, func(a ty) bool { return v.containsBasic(a, depth+1, top) })
	}
	return false
}

// checkResult rejects a value of type t that a rule or Operators Rule line (key) of rule rd
// produces when t is not known well enough to prove it fits the rule's declared type: the value
// must be converted explicitly.
func (v *valueInference) checkResult(key any, rd *grammar.RuleDef, t ty) {
	if rd.Type == nil || !v.uncertain(t) {
		return
	}
	d := v.resolve(rd.Type)
	if dn, ok := v.prog.convertsResult(key, rd); ok {
		d = dn.underlying() // checked before it is converted
	}
	if newFieldType(d) == nil || !v.needsCast(t, d) {
		return // every value fits
	}
	var pos grammar.Pos
	if k, ok := key.(interface{ Diagnostic() grammar.Pos }); ok {
		pos = k.Diagnostic()
	}
	v.errorf(pos, "cannot use %s as %s in result of rule %s: the type of the value is not known; convert it with a cast, as in value.(%s)", v.expand(t), d, rd.Name, d)
}

// declared checks the value t of the rule rd against its declared type, which is then the rule's
// type. A declared node[T] keeps the inferred captures, which a type expression cannot write.
func (v *valueInference) declared(rd *grammar.RuleDef, t ty) ty {
	if rd.Type == nil {
		return t
	}
	d := v.resolve(rd.Type)
	if dn, ok := v.prog.ruleNodeType(rd); ok {
		// The rule converts its value to its named node type.
		if !v.convertible(t, dn) {
			v.errorf(rd.Diagnostic(), "rule %s produces %s, which cannot be converted to %s", rd.Name, v.expand(t), dn)
			return dn
		}
		v.checkResult(rd, rd, t)
		return dn
	}
	if !v.assignable(t, d) {
		v.errorf(rd.Diagnostic(), "rule %s produces %s, which is not assignable to %s%s", rd.Name, v.expand(t), d, v.castHint(t, d))
		return d
	}
	if _, ok := rd.Expr.(*grammar.OperatorsRule); !ok {
		v.checkResult(rd, rd, t)
	}
	if dn, ok := d.(nodeTy); ok && dn.name == "" && !anyNode(d) && len(dn.caps) == 0 {
		if tn, ok := v.expand(t).(nodeTy); ok {
			return nodeTy{value: dn.value, caps: tn.caps}
		}
	}
	return d
}

// expand returns t with rule types replaced by the rules' value types, at the top level and in
// the arguments of unions and optional types.
func (v *valueInference) expand(t ty) ty {
	return v.expandSeen(t, nil)
}

// expandSeen is expand inside the rules seen. A chain of rules that only refer to each other has
// no value.
func (v *valueInference) expandSeen(t ty, seen map[string]bool) ty {
	for {
		switch u := t.(type) {
		case ruleTy:
			if seen[u.name] {
				return tyBottom
			}
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[u.name] = true
			t = v.ruleValue(u.name)
			continue
		case unionTy:
			alts := make([]ty, len(u.alts))
			for i, a := range u.alts {
				alts[i] = v.expandSeen(a, maps.Clone(seen))
			}
			return vunion(alts...)
		case optTy:
			return vopt(v.expandSeen(u.elem, seen))
		}
		return t
	}
}

// alts returns the arguments of the expanded union t, or t itself.
func (v *valueInference) alts(t ty) []ty {
	if u, ok := v.expand(t).(unionTy); ok {
		return u.alts
	}
	return []ty{v.expand(t)}
}

// everyAlt reports whether ok holds for every argument of the expanded union t, looking through
// optional types. nil, unknown values and _|_ are accepted.
func (v *valueInference) everyAlt(t ty, ok func(ty) bool) bool {
	for _, x := range v.alts(t) {
		if o, isOpt := x.(optTy); isOpt {
			if !v.everyAlt(o.elem, ok) {
				return false
			}
			continue
		}
		if x != tyNil && x != tyAny && x != tyBottom && !ok(x) {
			return false
		}
	}
	return true
}

// assignable reports whether a value of type s fits a location of type d. Subtyping is minimal:
// _|_ <= T <= _, T <= *T, nil <= *T, a union argument is below its union, lists, optional types
// and nodes are covariant, tuples are covariant element-wise, and structs compare by identity.
// node[_] is every node, and nodes whose captures differ are distinct.
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
		if d.name != "" {
			// A named node type takes only its own values; others need a conversion.
			return sn.name == d.name
		}
		if anyNode(d) {
			return true
		}
		// A node type written in the grammar has no captures and takes nodes with any.
		return (len(d.caps) == 0 || sameCaps(sn.caps, d.caps)) && v.assignable(v.nodeValue(sn), v.nodeValue(d))
	}
	if d == tyErr && s == tyMissing {
		return true // a missing token is an error value
	}
	return s == d
}

// anyNode reports whether t is node[_], the type of every node.
func anyNode(t ty) bool {
	n, ok := t.(nodeTy)
	return ok && n.value == tyTop && len(n.caps) == 0 && n.name == ""
}

// convertTerm returns the type of the node type constructor t, N{x}, which converts x to the
// named node type n.
func (v *valueInference) convertTerm(t *grammar.New, n nodeTy, env *valueEnv) ty {
	for _, fi := range t.Fields {
		v.term(fi.Value, env)
	}
	args := make([]ty, len(t.Args))
	for i, x := range t.Args {
		args[i] = v.term(x, env)
	}
	if len(t.Fields) != 0 || len(t.Args) != 1 {
		return n // reported where the constructor is checked (compiler.checkTerm)
	}
	x := args[0]
	if !v.convertible(x, n) {
		v.errorf(t.Diagnostic(), "cannot convert %s to %s%s", v.expand(x), n, v.castHint(x, n.underlying()))
	} else if v.needsCast(x, n.underlying()) {
		v.errorf(t.Diagnostic(), "cannot convert %s to %s: the type of the value is not known; convert it with a cast first, as in %s{value.(%s)}", v.expand(x), n, t.Type, n.underlying())
	}
	return n
}

// convertible reports whether a value of type s converts to the named node type d: every value of
// s is a node, named or not, whose underlying node type is assignable to that of d. A value not
// known statically is checked when it is converted.
func (v *valueInference) convertible(s ty, d nodeTy) bool {
	under := d.underlying()
	for _, x := range v.alts(s) {
		switch x := x.(type) {
		case nodeTy:
			if !v.assignable(nodeTy{value: v.nodeValue(x), caps: x.caps}, under) {
				return false
			}
		case namedTy:
			if x.kind != 'c' || x == tyError || x == tyMissing {
				return false
			}
			if n, ok := fromLegacy(x).(nodeTy); !ok || !v.assignable(n, under) {
				return false
			}
		default:
			if x != tyAny && x != tyBottom {
				return false
			}
		}
	}
	return true
}

// nodeValue returns the type of the value of a node of type n. The value of a named node type is
// read from its definition, which also gives the value of the type where it occurs inside itself.
func (v *valueInference) nodeValue(n nodeTy) ty {
	return v.prog.nodeValueOf(n)
}

// nodeValueOf returns the type of the value of a node of type n, reading the value of a named
// node type from its definition.
func (prog *Program) nodeValueOf(n nodeTy) ty {
	if n.name != "" {
		if d, ok := prog.nodeTypeOf(n.name); ok {
			return d.value
		}
	}
	return n.value
}

// nodeTypeOf returns the named node type that the type name names, through aliases. Its value is
// resolved once and cached.
func (prog *Program) nodeTypeOf(name string) (nodeTy, bool) {
	switch prog.types[name].(type) {
	case *grammar.TypeDef, *grammar.TypeAlias:
	default:
		return nodeTy{}, false
	}
	if c, ok := prog.nodeTypeCache.Load(name); ok {
		n, ok := c.(nodeTy)
		return n, ok
	}
	k := &typeResolver{prog: prog}
	n, ok := k.resolveNamed(name, grammar.Pos{}).(nodeTy)
	if !ok || n.name == "" {
		prog.nodeTypeCache.Store(name, false)
		return nodeTy{}, false
	}
	prog.nodeTypeCache.Store(name, n)
	return n, true
}

// ruleNodeType returns the named node type that the value of rule rd is converted to: its declared
// type, through aliases, when that is exactly a named node type. An Operators Rule converts the
// nodes of its operators instead (operatorsNodeType), never its value as a whole.
func (prog *Program) ruleNodeType(rd *grammar.RuleDef) (nodeTy, bool) {
	if _, ok := rd.Expr.(*grammar.OperatorsRule); ok {
		return nodeTy{}, false
	}
	return prog.declaredNodeType(rd)
}

// operatorsNodeType returns the named node type that every operator node of the Operators Rule
// rd is converted to: the rule's declared type, through aliases, when that is exactly a named node
// type. The values of its operand lines are not converted.
func (prog *Program) operatorsNodeType(rd *grammar.RuleDef) (nodeTy, bool) {
	if _, ok := rd.Expr.(*grammar.OperatorsRule); !ok {
		return nodeTy{}, false
	}
	return prog.declaredNodeType(rd)
}

// declaredNodeType returns the named node type that is the declared type of rule rd, through
// aliases.
func (prog *Program) declaredNodeType(rd *grammar.RuleDef) (nodeTy, bool) {
	if rd.Type == nil {
		return nodeTy{}, false
	}
	k := &typeResolver{prog: prog}
	n, ok := k.resolve(rd.Type).(nodeTy)
	return n, ok && n.name != ""
}

// convertsResult reports whether the value that key produces (see checkInfo.results) is converted
// to a named node type, which its run-time check then sees converted: a rule's value, or the value
// of an operator of an Operators Rule.
func (prog *Program) convertsResult(key any, rd *grammar.RuleDef) (nodeTy, bool) {
	switch key.(type) {
	case *grammar.RuleDef:
		if key == any(rd) {
			return prog.ruleNodeType(rd)
		}
	case *grammar.OperatorsRuleOperator:
		return prog.operatorsNodeType(rd)
	}
	return nodeTy{}, false
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
		if ct, ok := capType(env.caps, t.Name); ok {
			return ct
		}
		// Undefined captures were reported when names were resolved; this one has no value.
		v.errorf(t.Diagnostic(), "$%s has no value in its scope", t.Name)
		return tyAny
	case *grammar.VarRef:
		if d := v.prog.vars.lookup(t.Name); d != nil {
			return d.typ
		}
		return tyAny // an undeclared variable was reported when names were resolved
	case *grammar.Member:
		return v.member(v.term(t.X, env), t)
	case *grammar.New:
		if nt, ok := v.prog.nodeTypeOf(t.Type); ok {
			return v.convertTerm(t, nt, env)
		}
		for _, x := range t.Args {
			v.term(x, env)
		}
		name, st := v.prog.structType(t.Type)
		for _, fi := range t.Fields {
			vt := v.term(fi.Value, env)
			if st == nil {
				continue
			}
			if f := fieldOf(st, fi.Name); f != nil {
				if ft := v.resolve(f.Type); ft == tyBottom {
					// A field without values is reported where it is declared.
				} else if !v.assignable(vt, ft) {
					v.errorf(fi.Diagnostic(), "cannot use %s as %s in field %s of %s%s", v.expand(vt), ft, fi.Name, t.Type, v.castHint(vt, ft))
				} else if newFieldType(ft) != nil && v.needsCast(vt, ft) {
					v.errorf(fi.Diagnostic(), "cannot use %s as %s in field %s of %s: the type of the value is not known; convert it with a cast, as in value.(%s)", v.expand(vt), ft, fi.Name, t.Type, ft)
				}
			}
		}
		if st == nil {
			return tyAny
		}
		return namedTy{name, 's'}
	case *grammar.TupleLit:
		elems := make([]ty, len(t.Elems))
		for i, x := range t.Elems {
			elems[i] = v.term(x, env)
		}
		return tupleTy{elems}
	case *grammar.ListConstructor:
		elem := v.resolve(t.Elem)
		for _, item := range t.Items {
			it := v.term(item, env)
			if elem != tyAny && v.unknown(it) || !v.assignable(it, elem) {
				v.errorf(termPos(item, t.Diagnostic()), "cannot use %s as %s in list constructor%s", v.expand(it), elem, v.castHint(it, elem))
			}
		}
		return listTy{elem}
	case *grammar.Call:
		return v.call(t, env)
	case *grammar.Cast:
		x, to := v.term(t.X, env), v.resolve(t.Type)
		if to != tyBottom && !v.overlaps(x, to, 0) { // a cast to _|_ is reported where names are resolved
			v.errorf(t.Diagnostic(), "cannot cast %s to %s", v.expand(x), to)
		}
		return to
	case *grammar.Lambda:
		v.errorf(grammar.Pos{}, "functions can only be passed to filter, foldl, foldr, or map")
		return tyAny
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
	// Positional captures are rejected when names are resolved, assignments are checked with the
	// predicates that make them, and statement blocks are rejected when the program is compiled.
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
			// A node indexes the elements of its tuple or list value.
			for _, b := range v.alts(v.nodeValue(a)) {
				switch b.(type) {
				case listTy, tupleTy:
				default:
					if b != tyAny && b != tyBottom {
						v.errorf(t.Diagnostic(), "cannot index %s", a)
						return false
					}
				}
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

// member returns the type of x.Name. On a union every argument must have the member, and the
// result is the join of their types (optional if a recovered value may be read); .$y needs every argument to capture y.
func (v *valueInference) member(x ty, m *grammar.Member) ty {
	x = v.expand(x)
	if x == tyAny || x == tyBottom {
		v.noteMember(m, x == tyBottom)
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
	v.noteMember(m, !slices.ContainsFunc(alts, func(a ty) bool {
		_, node := a.(nodeTy)
		return !node && a != tyErr && a != tyMissing && a != tyBottom
	}))
	var found, missing []ty
	recovered := false
	for _, a := range alts {
		n := len(found)
		switch a := a.(type) {
		case nodeTy:
			switch m.Name {
			case "Start", "End":
				found = append(found, tyInt)
			case "Value":
				found = append(found, v.nodeValue(a))
			}
		case basicTy:
			switch {
			case a == tyAny:
				return tyAny
			case a == tyErr && (m.Name == "Start" || m.Name == "End"):
				found = append(found, tyInt)
			}
		case namedTy:
			if a == tyMissing && (m.Name == "Start" || m.Name == "End") {
				found = append(found, tyInt)
			}
			if st, ok := v.prog.types[a.name].(*grammar.TypeDef); ok {
				if f := fieldOf(st, m.Name); f != nil {
					found = append(found, v.resolve(f.Type))
				}
			}
		}
		if len(found) == n && a != tyBottom {
			if a == tyErr || a == tyMissing {
				recovered = true // a recovered value reads as nil: the result is optional
			} else {
				missing = append(missing, a)
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
	if len(missing) > 0 {
		names := make([]string, len(missing))
		for i, a := range missing {
			names[i] = a.String()
		}
		v.errorf(m.Diagnostic(), "%s has no field %s in every member: %s lacks it; cast the value to a member that has it, as in x.(T).%s",
			x, m.Name, strings.Join(names, ", "), m.Name)
		return tyAny
	}
	if recovered {
		return vopt(vunion(found...))
	}
	return vunion(found...)
}

// noteMember records whether the member access m reads only node members (.Start, .End and
// .Value), never a struct field: an access typed several times (in a lambda body) must read node
// members every time.
func (v *valueInference) noteMember(m *grammar.Member, nodeOnly bool) {
	if v.nodeMembers == nil || v.quiet {
		return
	}
	if prev, ok := v.nodeMembers[m]; ok && !prev {
		return
	}
	v.nodeMembers[m] = nodeOnly
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
		n, _ := a.(nodeTy)
		ct, ok := capType(n.caps, name)
		if !ok {
			v.errorf(m.Diagnostic(), "%s has no capture %s", a, name)
			return tyAny
		}
		found = append(found, ct)
	}
	return vunion(found...)
}

// capType returns the type of the capture name in caps. A name captured several times in one
// match holds the last capture that matched, which may be any of them, so its type is the join of
// their types; it is not nil if one of them always matches.
func capType(caps []capIndex, name string) (ty, bool) {
	var ts, sets []ty
	always := false // some capture of the name is always made, so the name is always set
	for _, c := range caps {
		if c.name == name {
			ts = append(ts, c.t)
			sets = append(sets, setType(c))
			always = always || !c.maybe
		}
	}
	if len(ts) == 0 {
		return nil, false
	}
	if always {
		return vunion(sets...), true
	}
	return vunion(ts...), true
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
		if l, ok := v.expand(v.nodeValue(x)).(listTy); ok {
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

// lambda returns the function argument of the call t with n parameters.
func (v *valueInference) lambda(t *grammar.Call, arg, n int) (*grammar.Lambda, bool) {
	l, ok := t.Args[arg].(*grammar.Lambda)
	if !ok || len(l.Params) != n {
		v.errorf(t.Diagnostic(), "%s: expected a function of %d parameters", t.Func, n)
		return nil, false
	}
	return l, true
}

// unknown reports whether a value of type t may be one the value types do not know (tyAny), as
// itself or as an element of a list, an optional value or a union. A node is known to be a node.
func (v *valueInference) unknown(t ty) bool {
	for range 32 {
		switch u := v.expand(t).(type) {
		case basicTy:
			return u == tyAny
		case listTy:
			t = u.elem
			continue
		case optTy:
			t = u.elem
			continue
		case unionTy:
			return slices.ContainsFunc(u.alts, v.unknown)
		}
		return false
	}
	return true
}

// overlaps reports whether some value has both type a and type b, so that a cast from one to the
// other can succeed. Nodes overlap when their values may: the captures of a node are static and do
// not decide what a cast accepts. Lists overlap when their elements may, since an empty list has
// every list type, and a list node is cast to a list type through its list.
func (v *valueInference) overlaps(a, b ty, depth int) bool {
	if depth > 16 || v.assignable(a, b) || v.assignable(b, a) {
		return true
	}
	split := func(t ty) []ty {
		var out []ty
		for _, x := range v.alts(t) {
			if o, ok := x.(optTy); ok {
				out = append(out, v.alts(o.elem)...)
				out = append(out, tyNil)
			} else {
				out = append(out, x)
			}
		}
		return out
	}
	for _, x := range split(a) {
		for _, y := range split(b) {
			if v.assignable(x, y) || v.assignable(y, x) {
				return true
			}
			_, okx := x.(listTy)
			_, oky := y.(listTy)
			if okx && oky {
				return true // both admit the empty list
			}
			xn, ok1 := x.(nodeTy)
			yn, ok2 := y.(nodeTy)
			if ok1 && ok2 && (xn.name == "" || yn.name == "" || xn.name == yn.name) && v.overlaps(v.nodeValue(xn), v.nodeValue(yn), depth+1) {
				return true
			}
			if _, list := y.(listTy); ok1 && list && v.overlaps(xn.value, y, depth+1) {
				return true
			}
			if _, list := x.(listTy); ok2 && list && v.overlaps(x, yn.value, depth+1) {
				return true
			}
		}
	}
	return false
}

func (v *valueInference) call(t *grammar.Call, env *valueEnv) ty {
	if n, ok := builtins[t.Func]; !ok || n >= 0 && n != len(t.Args) {
		return tyAny // reported when the action is compiled
	}
	arg := func(i int) ty { return v.term(t.Args[i], env) }
	switch t.Func {
	case "len":
		a := arg(0)
		if !v.everyAlt(a, func(x ty) bool {
			switch x.(type) {
			case nodeTy, listTy, tupleTy:
				return true
			}
			return x == tyString
		}) {
			v.errorf(t.Diagnostic(), "len: invalid argument %s", a)
		}
		return tyInt
	case "text":
		// text(x) is the input that node x spans; nil and strings are kept as they are.
		a := arg(0)
		if !v.everyAlt(a, func(x ty) bool {
			_, ok := x.(nodeTy)
			return ok || x == tyString
		}) {
			v.errorf(t.Diagnostic(), "text: invalid argument %s", a)
		}
		return tyString
	case "foldl", "foldr":
		acc := arg(0)
		elem := v.elemOf(t.Func, arg(1), t.Diagnostic())
		l, ok := v.lambda(t, 2, 2)
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
		l, ok := v.lambda(t, 1, 1)
		if !ok {
			return tyAny
		}
		if body := v.term(l.Body, env.with(l.Params, []ty{elem})); !v.assignable(body, tyBool) {
			v.errorf(t.Diagnostic(), "filter: predicate must produce a bool, got %s", body)
		}
		return listTy{elem}
	case "map":
		elem := v.elemOf("map", arg(0), t.Diagnostic())
		l, ok := v.lambda(t, 1, 1)
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
