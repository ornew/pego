package compile

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
	caps   []CapIndex
	locals map[string]Ty
}

func (e *valueEnv) with(names []string, ts []Ty) *valueEnv {
	n := *e
	n.locals = map[string]Ty{}
	for k, t := range e.locals {
		n.locals[k] = t
	}
	for i, name := range names {
		n.locals[name] = ts[i]
	}
	return &n
}

func (v *ValueInference) errorf(pos grammar.Pos, format string, args ...any) {
	if v.Quiet {
		return
	}
	if !pos.IsValid() && v.Rule != nil {
		pos = v.Rule.Diagnostic()
	}
	for i, a := range args {
		if t, ok := a.(Ty); ok {
			args[i] = v.shown(t)
		}
	}
	v.errs = append(v.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// shown returns t as a message shows it. The rule of an inline action has no name the author
// wrote, so inside a type it stands for the type of its result.
func (v *ValueInference) shown(t Ty) Ty {
	return v.shownSeen(t, nil)
}

func (v *ValueInference) shownSeen(t Ty, seen map[string]bool) Ty {
	if v.Prog == nil {
		return t
	}
	return mapTy(t, func(u Ty) (Ty, bool) {
		r, ok := u.(RuleTy)
		if !ok || seen[r.Name] {
			return nil, false
		}
		if rl := v.Prog.ByName[r.Name]; rl == nil || rl.Where == "" {
			return nil, false
		}
		next := maps.Clone(seen)
		if next == nil {
			next = map[string]bool{}
		}
		next[r.Name] = true
		return v.shownSeen(v.ruleValue(r.Name), next), true
	})
}

// resolve maps a type expression of the grammar to the value model.
func (v *ValueInference) resolve(t grammar.TypeExpr) Ty {
	k := &TypeResolver{Prog: v.Prog}
	return k.Resolve(t)
}

// uncertain reports whether a value of type t may be one the value types do not know yet (tyAny),
// so that a typed location checks it at run time.
func (v *ValueInference) uncertain(t Ty) bool {
	return v.containsBasic(t, 0, false, true)
}

// needsCast reports whether a value of type t, which is assignable to target, may still not fit it
// because t is not known well enough where target is more specific: the value must be cast.
func (v *ValueInference) needsCast(t, target Ty) bool {
	return v.uncertain(t) && v.needsCastAt(t, target, 0)
}

func (v *ValueInference) needsCastAt(t, target Ty, depth int) bool {
	if depth > 32 || !v.uncertain(t) {
		return depth > 32
	}
	switch target := target.(type) {
	case BasicTy:
		return target != TyAny && target != TyTop
	case ListTy:
		if l, ok := v.Expand(t).(ListTy); ok {
			return v.needsCastAt(l.Elem, target.Elem, depth+1)
		}
	case OptTy:
		if o, ok := v.Expand(t).(OptTy); ok {
			return v.needsCastAt(o.Elem, target.Elem, depth+1)
		}
	}
	return true
}

// narrowable reports whether a value of type t, which does not fit a location, may do so once it
// is cast: its type is not known (tyAny) or not specific (tyTop) in some part.
func (v *ValueInference) narrowable(t Ty) bool {
	return v.containsBasic(t, 0, true, true)
}

// castHint is the hint appended to the message about a value of type t that does not fit a
// location of type target, if a cast may make it fit, and "" otherwise.
func (v *ValueInference) castHint(t Ty, target any) string {
	// A type that nests without end, as the type of a recursive rule does, is known: only a part
	// of type _ or unknown makes the hint true.
	if !v.narrowable(t) || !v.containsBasic(t, 0, true, false) {
		return ""
	}
	return fmt.Sprintf(": the type of the value is not known; convert it with a cast, as in value.(%v)", target)
}

// containsBasic reports whether t has a part of type tyAny, or also of tyTop if top is set. A
// type nested deeper than the limit is taken to have such a part if deep is set.
func (v *ValueInference) containsBasic(t Ty, depth int, top, deep bool) bool {
	if depth > 32 {
		return deep
	}
	switch t := v.Expand(t).(type) {
	case BasicTy:
		return t == TyAny || top && t == TyTop
	case ListTy:
		return v.containsBasic(t.Elem, depth+1, top, deep)
	case OptTy:
		return v.containsBasic(t.Elem, depth+1, top, deep)
	case NodeTy:
		return v.containsBasic(t.Value, depth+1, top, deep)
	case JoinTy:
		return slices.ContainsFunc(t.Alts, func(a Ty) bool { return v.containsBasic(a, depth+1, top, deep) })
	case TupleTy:
		return slices.ContainsFunc(t.Elems, func(a Ty) bool { return v.containsBasic(a, depth+1, top, deep) })
	}
	return false
}

// checkResult rejects a value of type t that a rule or Operators Rule line (key) of rule rd
// produces when t is not known well enough to prove it fits the rule's declared type: the value
// must be converted explicitly.
func (v *ValueInference) checkResult(key any, rd *grammar.RuleDef, t Ty) {
	if rd.Type == nil || !v.uncertain(t) {
		return
	}
	d := v.resolve(rd.Type)
	if dn, ok := v.Prog.convertsResult(key, rd); ok {
		d = dn.Underlying() // checked before it is converted
	}
	if NewFieldType(d) == nil || !v.needsCast(t, d) {
		return // every value fits
	}
	var pos grammar.Pos
	if k, ok := key.(interface{ Diagnostic() grammar.Pos }); ok {
		pos = k.Diagnostic()
	}
	v.errorf(pos, "cannot use %s as %s in result of rule %s: the type of the value is not known; convert it with a cast, as in value.(%s)", v.display(t), d, rd.Name, d)
}

// declared checks the value t of the rule rd against its declared type, which is then the rule's
// type. A declared node[T] keeps the inferred captures, which a type expression cannot write.
func (v *ValueInference) declared(rd *grammar.RuleDef, t Ty) Ty {
	if rd.Type == nil {
		return t
	}
	d := v.resolve(rd.Type)
	if dn, ok := v.Prog.ruleNodeType(rd); ok {
		// The rule converts its value to its named node type.
		if !v.convertible(t, dn) {
			v.errorf(rd.Diagnostic(), "rule %s produces %s, which cannot be converted to %s", rd.Name, v.display(t), dn)
			return dn
		}
		v.checkResult(rd, rd, t)
		return dn
	}
	if !v.assignable(t, d) {
		v.errorf(rd.Diagnostic(), "rule %s produces %s, which is not assignable to %s%s", rd.Name, v.display(t), d, v.castHint(t, d))
		return d
	}
	if _, ok := rd.Expr.(*grammar.OperatorsRule); !ok {
		v.checkResult(rd, rd, t)
	}
	if dn, ok := d.(NodeTy); ok && dn.Name == "" && !anyNode(d) && len(dn.Caps) == 0 {
		if tn, ok := v.Expand(t).(NodeTy); ok {
			return NodeTy{Value: dn.Value, Caps: tn.Caps}
		}
	}
	return d
}

// Expand returns t with rule types replaced by the rules' value types, at the top level and in
// the arguments of joins and optional types.
func (v *ValueInference) Expand(t Ty) Ty {
	return v.expandSeen(t, nil)
}

// expandSeen is expand inside the rules seen. A chain of rules that only refer to each other has
// no value.
func (v *ValueInference) expandSeen(t Ty, seen map[string]bool) Ty {
	for {
		switch u := t.(type) {
		case RuleTy:
			if seen[u.Name] {
				return TyBottom
			}
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[u.Name] = true
			t = v.ruleValue(u.Name)
			continue
		case JoinTy:
			alts := make([]Ty, len(u.Alts))
			for i, a := range u.Alts {
				alts[i] = v.expandSeen(a, maps.Clone(seen))
			}
			return Vjoin(alts...)
		case OptTy:
			return vopt(v.expandSeen(u.Elem, seen))
		}
		return t
	}
}

// display returns t as a diagnostic prints it: expanded at the top level, with a reference to a
// rule that declares a type, at any depth, replaced by that type. A reference to a rule without a
// declared type keeps the rule's name.
func (v *ValueInference) display(t Ty) Ty {
	return mapTy(v.Expand(t), func(u Ty) (Ty, bool) {
		r, ok := u.(RuleTy)
		if !ok {
			return nil, false
		}
		if rd := v.Rules[r.Name]; rd != nil && rd.Type != nil {
			return v.resolve(rd.Type), true
		}
		return nil, false
	})
}

// alts returns the arguments of the expanded join t, or t itself.
func (v *ValueInference) alts(t Ty) []Ty {
	if u, ok := v.Expand(t).(JoinTy); ok {
		return u.Alts
	}
	return []Ty{v.Expand(t)}
}

// everyAlt reports whether ok holds for every argument of the expanded join t, looking through
// optional types. nil, unknown values and _|_ are accepted.
func (v *ValueInference) everyAlt(t Ty, ok func(Ty) bool) bool {
	for _, x := range v.alts(t) {
		if o, isOpt := x.(OptTy); isOpt {
			if !v.everyAlt(o.Elem, ok) {
				return false
			}
			continue
		}
		if x != TyNil && x != TyAny && x != TyBottom && !ok(x) {
			return false
		}
	}
	return true
}

// assignable reports whether a value of type s fits a location of type d. Subtyping is minimal:
// _|_ <= T <= _, T <= *T, nil <= *T, a join argument is below its join, lists, optional types
// and nodes are covariant, tuples are covariant element-wise, and structs compare by identity.
// node[_] is every node, and nodes whose captures differ are distinct.
func (v *ValueInference) assignable(s, d Ty) bool {
	s = v.Expand(s)
	if s == TyAny || d == TyAny || d == TyTop || s == TyBottom {
		return true
	}
	if u, ok := s.(JoinTy); ok {
		for _, a := range u.Alts {
			if !v.assignable(a, d) {
				return false
			}
		}
		return true
	}
	if o, ok := d.(OptTy); ok {
		if s == TyNil {
			return true
		}
		if so, ok := s.(OptTy); ok {
			return v.assignable(so.Elem, o.Elem)
		}
		return v.assignable(s, o.Elem)
	}
	if u, ok := d.(JoinTy); ok {
		for _, a := range u.Alts {
			if v.assignable(s, a) {
				return true
			}
		}
		return false
	}
	switch d := d.(type) {
	case ListTy:
		sl, ok := s.(ListTy)
		return ok && v.assignable(sl.Elem, d.Elem)
	case TupleTy:
		st, ok := s.(TupleTy)
		if !ok || len(st.Elems) != len(d.Elems) {
			return false
		}
		for i := range st.Elems {
			if !v.assignable(st.Elems[i], d.Elems[i]) {
				return false
			}
		}
		return true
	case NodeTy:
		sn, ok := s.(NodeTy)
		if !ok {
			return false
		}
		if d.Name != "" {
			// A named node type takes only its own values; others need a conversion.
			return sn.Name == d.Name
		}
		if anyNode(d) {
			return true
		}
		// A node type written in the grammar has no captures and takes nodes with any.
		return (len(d.Caps) == 0 || sameCaps(sn.Caps, d.Caps)) && v.assignable(v.nodeValue(sn), v.nodeValue(d))
	}
	if d == TyErr && s == TyMissing {
		return true // a missing token is an error value
	}
	return s == d
}

// anyNode reports whether t is node[_], the type of every node.
func anyNode(t Ty) bool {
	n, ok := t.(NodeTy)
	return ok && n.Value == TyTop && len(n.Caps) == 0 && n.Name == ""
}

// convertTerm returns the type of the node type constructor t, N{x}, which converts x to the
// named node type n.
func (v *ValueInference) convertTerm(t *grammar.New, n NodeTy, env *valueEnv) Ty {
	for _, fi := range t.Fields {
		v.term(fi.Value, env)
	}
	args := make([]Ty, len(t.Args))
	for i, x := range t.Args {
		args[i] = v.term(x, env)
	}
	if len(t.Fields) != 0 || len(t.Args) != 1 {
		return n // reported where the constructor is checked (compiler.checkTerm)
	}
	x := args[0]
	if v.rec != nil && !v.Quiet {
		v.rec.Proven[t] = v.convertible(x, n) && !v.needsCast(x, n.Underlying())
	}
	if !v.convertible(x, n) {
		v.errorf(t.Diagnostic(), "cannot convert %s to %s%s", v.display(x), n, v.castHint(x, n.Underlying()))
	} else if v.needsCast(x, n.Underlying()) {
		v.errorf(t.Diagnostic(), "cannot convert %s to %s: the type of the value is not known; convert it with a cast first, as in %s{value.(%s)}", v.display(x), n, t.Type, n.Underlying())
	}
	return n
}

// convertible reports whether a value of type s converts to the named node type d: every value of
// s is a node, named or not, whose underlying node type is assignable to that of d. A value not
// known statically is checked when it is converted.
func (v *ValueInference) convertible(s Ty, d NodeTy) bool {
	under := d.Underlying()
	for _, x := range v.alts(s) {
		switch x := x.(type) {
		case NodeTy:
			if !v.assignable(NodeTy{Value: v.nodeValue(x), Caps: x.Caps}, under) {
				return false
			}
		case NamedTy:
			if x.Kind != 'c' || x == TyError || x == TyMissing {
				return false
			}
			if n, ok := FromLegacy(x).(NodeTy); !ok || !v.assignable(n, under) {
				return false
			}
		default:
			if x != TyAny && x != TyBottom {
				return false
			}
		}
	}
	return true
}

// nodeValue returns the type of the value of a node of type n. The value of a named node type is
// read from its definition, which also gives the value of the type where it occurs inside itself.
func (v *ValueInference) nodeValue(n NodeTy) Ty {
	return v.Prog.nodeValueOf(n)
}

// nodeValueOf returns the type of the value of a node of type n, reading the value of a named
// node type from its definition.
func (prog *Program) nodeValueOf(n NodeTy) Ty {
	if n.Name != "" {
		if d, ok := prog.NodeTypeOf(n.Name); ok {
			return d.Value
		}
	}
	return n.Value
}

// NodeTypeOf returns the named node type that the type name names, through aliases. Its value is
// resolved once and cached.
func (prog *Program) NodeTypeOf(name string) (NodeTy, bool) {
	switch prog.Types[name].(type) {
	case *grammar.TypeDef, *grammar.TypeAlias:
	default:
		return NodeTy{}, false
	}
	if c, ok := prog.nodeTypeCache.Load(name); ok {
		n, ok := c.(NodeTy)
		return n, ok
	}
	k := &TypeResolver{Prog: prog}
	n, ok := k.ResolveNamed(name, grammar.Pos{}).(NodeTy)
	if !ok || n.Name == "" {
		prog.nodeTypeCache.Store(name, false)
		return NodeTy{}, false
	}
	prog.nodeTypeCache.Store(name, n)
	return n, true
}

// ruleNodeType returns the named node type that the value of rule rd is converted to: its declared
// type, through aliases, when that is exactly a named node type. An Operators Rule converts the
// nodes of its operators instead (operatorsNodeType), never its value as a whole.
func (prog *Program) ruleNodeType(rd *grammar.RuleDef) (NodeTy, bool) {
	if _, ok := rd.Expr.(*grammar.OperatorsRule); ok {
		return NodeTy{}, false
	}
	return prog.declaredNodeType(rd)
}

// operatorsNodeType returns the named node type that every operator node of the Operators Rule
// rd is converted to: the rule's declared type, through aliases, when that is exactly a named node
// type. The values of its operand lines are not converted.
func (prog *Program) operatorsNodeType(rd *grammar.RuleDef) (NodeTy, bool) {
	if _, ok := rd.Expr.(*grammar.OperatorsRule); !ok {
		return NodeTy{}, false
	}
	return prog.declaredNodeType(rd)
}

// declaredNodeType returns the named node type that is the declared type of rule rd, through
// aliases.
func (prog *Program) declaredNodeType(rd *grammar.RuleDef) (NodeTy, bool) {
	if rd.Type == nil {
		return NodeTy{}, false
	}
	k := &TypeResolver{Prog: prog}
	n, ok := k.Resolve(rd.Type).(NodeTy)
	return n, ok && n.Name != ""
}

// convertsResult reports whether the value that key produces (see checkInfo.results) is converted
// to a named node type, which its run-time check then sees converted: a rule's value, or the value
// of an operator of an Operators Rule.
func (prog *Program) convertsResult(key any, rd *grammar.RuleDef) (NodeTy, bool) {
	switch key.(type) {
	case *grammar.RuleDef:
		if key == any(rd) {
			return prog.ruleNodeType(rd)
		}
	case *grammar.OperatorsRuleOperator:
		return prog.operatorsNodeType(rd)
	}
	return NodeTy{}, false
}

func sameCaps(a, b []CapIndex) bool {
	return slices.EqualFunc(a, b, func(x, y CapIndex) bool {
		return x.Name == y.Name && x.Side == y.Side && slices.EqualFunc(x.Paths, y.Paths, func(p, q []int) bool { return slices.Equal(p, q) })
	})
}

// term returns the type of the value expression t.
func (v *ValueInference) term(t grammar.Term, env *valueEnv) Ty {
	r := v.termType(t, env)
	if v.rec != nil && !v.Quiet {
		v.rec.Terms[t] = r
	}
	return r
}

func (v *ValueInference) termType(t grammar.Term, env *valueEnv) Ty {
	v.control.poll()
	switch t := t.(type) {
	case *grammar.TermGroup:
		return v.term(t.Term, env)
	case *grammar.IntLit:
		return TyInt
	case *grammar.StringLit:
		return TyString
	case *grammar.BoolLit:
		return TyBool
	case *grammar.NilLit:
		return TyNil
	case *grammar.CaptureRef:
		if lt, ok := env.locals[t.Name]; ok {
			return lt
		}
		if ct, ok := CapType(env.caps, t.Name); ok {
			return ct
		}
		// Undefined captures were reported when names were resolved; this one has no value.
		v.errorf(t.Diagnostic(), "$%s has no value in its scope", t.Name)
		return TyAny
	case *grammar.VarRef:
		if d := v.Prog.Vars.lookup(t.Name); d != nil {
			return d.Typ
		}
		return TyAny // an undeclared variable was reported when names were resolved
	case *grammar.Member:
		return v.member(v.term(t.X, env), t)
	case *grammar.New:
		if nt, ok := v.Prog.NodeTypeOf(t.Type); ok {
			return v.convertTerm(t, nt, env)
		}
		for _, x := range t.Args {
			v.term(x, env)
		}
		name, st := v.Prog.StructType(t.Type)
		for _, fi := range t.Fields {
			vt := v.term(fi.Value, env)
			if st == nil {
				continue
			}
			if f := FieldOf(st, fi.Name); f != nil {
				if ft := v.resolve(f.Type); ft == TyBottom {
					// A field without values is reported where it is declared.
				} else if !v.assignable(vt, ft) {
					v.errorf(fi.Diagnostic(), "cannot use %s as %s in field %s of %s%s", v.display(vt), ft, fi.Name, t.Type, v.castHint(vt, ft))
				} else if NewFieldType(ft) != nil && v.needsCast(vt, ft) {
					v.errorf(fi.Diagnostic(), "cannot use %s as %s in field %s of %s: the type of the value is not known; convert it with a cast, as in value.(%s)", v.display(vt), ft, fi.Name, t.Type, ft)
				}
			}
		}
		if st == nil {
			return TyAny
		}
		return NamedTy{name, 's'}
	case *grammar.TupleLit:
		elems := make([]Ty, len(t.Elems))
		for i, x := range t.Elems {
			elems[i] = v.term(x, env)
		}
		return TupleTy{elems}
	case *grammar.ListConstructor:
		elem := v.resolve(t.Elem)
		for _, item := range t.Items {
			it := v.term(item, env)
			if elem != TyAny && v.unknown(it) || !v.assignable(it, elem) {
				v.errorf(termPos(item, t.Diagnostic()), "cannot use %s as %s in list constructor%s", v.display(it), elem, v.castHint(it, elem))
			}
		}
		return ListTy{elem}
	case *grammar.Call:
		return v.call(t, env)
	case *grammar.Cast:
		x, to := v.term(t.X, env), v.resolve(t.Type)
		if to != TyBottom && !v.overlaps(x, to, 0) { // a cast to _|_ is reported where names are resolved
			v.errorf(t.Diagnostic(), "cannot cast %s to %s", v.display(x), to)
		}
		return to
	case *grammar.Lambda:
		v.errorf(grammar.Pos{}, "functions can only be passed to filter, foldl, foldr, or map")
		return TyAny
	case *grammar.Unary:
		x := v.term(t.X, env)
		want := TyInt
		if t.Op == "!" {
			want = TyBool
		}
		if !v.assignable(x, want) {
			v.errorf(t.Diagnostic(), "invalid operand %s for unary %s", v.display(x), t.Op)
		}
		return want
	case *grammar.Binary:
		return v.binary(t, env)
	}
	// Positional captures are rejected when names are resolved, bindings are checked with the
	// predicates that make them, and statement blocks are rejected when the program is compiled.
	return TyAny
}

func (v *ValueInference) binary(t *grammar.Binary, env *valueEnv) Ty {
	l, r := v.term(t.L, env), v.term(t.R, env)
	both := func(want Ty) bool { return v.assignable(l, want) && v.assignable(r, want) }
	bad := func() Ty {
		v.errorf(t.Diagnostic(), "invalid operands %s and %s for %s", v.display(l), v.display(r), t.Op)
		return TyAny
	}
	switch t.Op {
	case "[]":
		return v.index(l, r, t)
	case "==", "!=":
		return TyBool
	case "&&", "||":
		if !both(TyBool) {
			return bad()
		}
		return TyBool
	case "<", "<=", ">", ">=":
		if !both(TyInt) && !both(TyString) {
			return bad()
		}
		return TyBool
	case "+":
		if v.Expand(l) == TyAny || v.Expand(r) == TyAny {
			return TyAny
		}
		if both(TyInt) {
			return TyInt
		}
		if both(TyString) {
			return TyString
		}
		return bad()
	}
	if !both(TyInt) {
		return bad()
	}
	return TyInt
}

// index returns the type of x[i]: an element of a list or of a tuple. A node is indexed through
// its Value.
func (v *ValueInference) index(x, i Ty, t *grammar.Binary) Ty {
	if !v.assignable(i, TyInt) {
		v.errorf(t.Diagnostic(), "index must be int, got %s", i)
	}
	var out []Ty
	var elems func(a Ty) bool
	elems = func(a Ty) bool {
		switch a := a.(type) {
		case ListTy:
			out = append(out, a.Elem)
			return true
		case TupleTy:
			if lit, ok := ungroupTerm(t.R).(*grammar.IntLit); ok {
				if lit.Value < 0 || lit.Value >= len(a.Elems) {
					v.errorf(t.Diagnostic(), "index %d is out of range for %s", lit.Value, a)
					return false
				}
				out = append(out, a.Elems[lit.Value])
			} else {
				out = append(out, a.Elems...)
			}
			return true
		case NodeTy:
			// A node indexes the elements of its tuple or list value.
			for _, b := range v.alts(v.nodeValue(a)) {
				switch b.(type) {
				case ListTy, TupleTy:
				default:
					if b != TyAny && b != TyBottom {
						v.errorf(t.Diagnostic(), "cannot index %s", a)
						return false
					}
				}
				if !elems(b) {
					return false
				}
			}
			return true
		case BasicTy:
			switch a {
			case TyAny:
				out = append(out, TyAny)
				return true
			case TyBottom:
				return true
			}
		}
		v.errorf(t.Diagnostic(), "cannot index %s", a)
		return false
	}
	for _, a := range v.alts(x) {
		if !elems(a) {
			return TyAny
		}
	}
	return Vjoin(out...)
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

// member returns the type of x.Name. On a join every argument must have the member, and the
// result is the join of their types (optional if a recovered value may be read); .$y needs every argument to capture y.
func (v *ValueInference) member(x Ty, m *grammar.Member) Ty {
	x = v.Expand(x)
	if x == TyAny || x == TyBottom {
		v.noteMember(m, x == TyBottom)
		return x
	}
	switch m.Name {
	case "Source":
		v.errorf(m.Diagnostic(), "the source information of a node is not accessible from actions")
		return TyAny
	case "startPos", "endPos":
		v.errorf(m.Diagnostic(), ".%s is removed: use .Start or .End", m.Name)
		return TyInt
	case "children":
		v.errorf(m.Diagnostic(), ".children is removed: use .Value")
		return TyAny
	}
	if _, ok := x.(OptTy); ok {
		v.errorf(m.Diagnostic(), "cannot access .%s of %s: the value may be nil", m.Name, x)
		return TyAny
	}
	if name, ok := strings.CutPrefix(m.Name, "$"); ok {
		return v.captureAccess(x, name, m)
	}
	alts := v.alts(x)
	v.noteMember(m, !slices.ContainsFunc(alts, func(a Ty) bool {
		_, node := a.(NodeTy)
		return !node && a != TyErr && a != TyMissing && a != TyBottom
	}))
	var found, missing []Ty
	recovered := false
	for _, a := range alts {
		n := len(found)
		switch a := a.(type) {
		case NodeTy:
			switch m.Name {
			case "Start", "End":
				found = append(found, TyInt)
			case "Value":
				found = append(found, v.nodeValue(a))
			}
		case BasicTy:
			switch {
			case a == TyAny:
				return TyAny
			case a == TyErr && (m.Name == "Start" || m.Name == "End"):
				found = append(found, TyInt)
			}
		case NamedTy:
			if a == TyMissing && (m.Name == "Start" || m.Name == "End") {
				found = append(found, TyInt)
			}
			if st, ok := v.Prog.Types[a.Name].(*grammar.TypeDef); ok {
				if f := FieldOf(st, m.Name); f != nil {
					found = append(found, v.resolve(f.Type))
				}
			}
		}
		if len(found) == n && a != TyBottom {
			if a == TyErr || a == TyMissing {
				recovered = true // a recovered value reads as nil: the result is optional
			} else {
				missing = append(missing, a)
			}
		}
	}
	if len(found) == 0 {
		if n, ok := x.(NodeTy); ok && slices.ContainsFunc(n.Caps, func(c CapIndex) bool { return c.Name == m.Name }) {
			v.errorf(m.Diagnostic(), "%s has no field %s: read the capture with .$%s", x, m.Name, m.Name)
		} else {
			v.errorf(m.Diagnostic(), "%s has no field %s", x, m.Name)
		}
		return TyAny
	}
	if len(missing) > 0 {
		names := make([]string, len(missing))
		for i, a := range missing {
			names[i] = a.String()
		}
		v.errorf(m.Diagnostic(), "%s has no field %s in every member: %s lacks it; cast the value to a member that has it, as in x.(T).%s",
			x, m.Name, strings.Join(names, ", "), m.Name)
		return TyAny
	}
	if recovered {
		return vopt(Vjoin(found...))
	}
	return Vjoin(found...)
}

// noteMember records whether the member access m reads only node members (.Start, .End and
// .Value), never a struct field: an access typed several times (in a lambda body) must read node
// members every time.
func (v *ValueInference) noteMember(m *grammar.Member, nodeOnly bool) {
	if v.nodeMembers == nil || v.Quiet {
		return
	}
	if prev, ok := v.nodeMembers[m]; ok && !prev {
		return
	}
	v.nodeMembers[m] = nodeOnly
}

// captureAccess returns the type of x.$name: every argument of x must be a node that captures
// name, and the type is the join of their capture types.
func (v *ValueInference) captureAccess(x Ty, name string, m *grammar.Member) Ty {
	var found []Ty
	for _, a := range v.alts(x) {
		if a == TyAny {
			return TyAny
		}
		if a == TyBottom {
			continue
		}
		n, _ := a.(NodeTy)
		ct, ok := CapType(n.Caps, name)
		if !ok {
			v.errorf(m.Diagnostic(), "%s has no capture %s", a, name)
			return TyAny
		}
		found = append(found, ct)
	}
	return Vjoin(found...)
}

// CapType returns the type of the capture name in caps. A name captured several times in one
// match holds the last capture that matched, which may be any of them, so its type is the join of
// their types; it is not nil if one of them always matches.
func CapType(caps []CapIndex, name string) (Ty, bool) {
	var ts, sets []Ty
	always := false // some capture of the name is always made, so the name is always set
	for _, c := range caps {
		if c.Name == name {
			ts = append(ts, c.T)
			sets = append(sets, setType(c))
			always = always || !c.maybe
		}
	}
	if len(ts) == 0 {
		return nil, false
	}
	if always {
		return Vjoin(sets...), true
	}
	return Vjoin(ts...), true
}

// elemOf returns the type of the elements that list functions read from x: the elements of a
// list, or of the list a node holds, as for the node of a repetition. nil has no elements.
func (v *ValueInference) elemOf(fn string, x Ty, pos grammar.Pos) Ty {
	t, ok := v.elemsOf(x)
	if !ok {
		v.errorf(pos, "%s: expected a list, got %s", fn, v.display(x))
		return TyAny
	}
	return t
}

func (v *ValueInference) elemsOf(x Ty) (Ty, bool) {
	switch x := v.Expand(x).(type) {
	case ListTy:
		return x.Elem, true
	case NodeTy:
		if l, ok := v.Expand(v.nodeValue(x)).(ListTy); ok {
			return l.Elem, true
		}
	case OptTy:
		return v.elemsOf(x.Elem)
	case JoinTy:
		ts := make([]Ty, len(x.Alts))
		for i, a := range x.Alts {
			t, ok := v.elemsOf(a)
			if !ok {
				return nil, false
			}
			ts[i] = t
		}
		return Vjoin(ts...), true
	case BasicTy:
		switch x {
		case TyAny:
			return TyAny, true
		case TyNil, TyBottom:
			return TyBottom, true
		}
	}
	return nil, false
}

// lambda returns the function argument of the call t with n parameters.
func (v *ValueInference) lambda(t *grammar.Call, arg, n int) (*grammar.Lambda, bool) {
	l, ok := t.Args[arg].(*grammar.Lambda)
	if !ok || len(l.Params) != n {
		v.errorf(t.Diagnostic(), "%s: expected a function of %d parameters", t.Func, n)
		return nil, false
	}
	return l, true
}

// unknown reports whether a value of type t may be one the value types do not know (tyAny), as
// itself or as an element of a list, an optional value or a join. A node is known to be a node.
func (v *ValueInference) unknown(t Ty) bool {
	for range 32 {
		switch u := v.Expand(t).(type) {
		case BasicTy:
			return u == TyAny
		case ListTy:
			t = u.Elem
			continue
		case OptTy:
			t = u.Elem
			continue
		case JoinTy:
			return slices.ContainsFunc(u.Alts, v.unknown)
		}
		return false
	}
	return true
}

// overlaps reports whether some value has both type a and type b, so that a cast from one to the
// other can succeed. Nodes overlap when their values may: the captures of a node are static and do
// not decide what a cast accepts. Lists overlap when their elements may, since an empty list has
// every list type, and a list node is cast to a list type through its list.
func (v *ValueInference) overlaps(a, b Ty, depth int) bool {
	if depth > 16 || v.assignable(a, b) || v.assignable(b, a) {
		return true
	}
	split := func(t Ty) []Ty {
		var out []Ty
		for _, x := range v.alts(t) {
			if o, ok := x.(OptTy); ok {
				out = append(out, v.alts(o.Elem)...)
				out = append(out, TyNil)
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
			_, okx := x.(ListTy)
			_, oky := y.(ListTy)
			if okx && oky {
				return true // both admit the empty list
			}
			xn, ok1 := x.(NodeTy)
			yn, ok2 := y.(NodeTy)
			if ok1 && ok2 && (xn.Name == "" || yn.Name == "" || xn.Name == yn.Name) && v.overlaps(v.nodeValue(xn), v.nodeValue(yn), depth+1) {
				return true
			}
			if _, list := y.(ListTy); ok1 && list && v.overlaps(xn.Value, y, depth+1) {
				return true
			}
			if _, list := x.(ListTy); ok2 && list && v.overlaps(x, yn.Value, depth+1) {
				return true
			}
		}
	}
	return false
}

func (v *ValueInference) call(t *grammar.Call, env *valueEnv) Ty {
	if n, ok := builtins[t.Func]; !ok || n >= 0 && n != len(t.Args) {
		return TyAny // reported when the action is compiled
	}
	arg := func(i int) Ty { return v.term(t.Args[i], env) }
	switch t.Func {
	case "len":
		a := arg(0)
		if !v.everyAlt(a, func(x Ty) bool {
			switch x.(type) {
			case NodeTy, ListTy, TupleTy:
				return true
			}
			return x == TyString
		}) {
			v.errorf(t.Diagnostic(), "len: invalid argument %s", a)
		}
		return TyInt
	case "text":
		// text(x) is the input that node x spans; nil and strings are kept as they are.
		a := arg(0)
		if !v.everyAlt(a, func(x Ty) bool {
			_, ok := x.(NodeTy)
			return ok || x == TyString
		}) {
			v.errorf(t.Diagnostic(), "text: invalid argument %s", a)
		}
		return TyString
	case "foldl", "foldr":
		acc := arg(0)
		elem := v.elemOf(t.Func, arg(1), t.Diagnostic())
		l, ok := v.lambda(t, 2, 2)
		if !ok {
			return TyAny
		}
		// If the body's type does not fit the initial value's type, take the join and check again.
		converged := false
		for range 3 {
			saved := v.Quiet
			v.Quiet = true
			body := v.term(l.Body, env.with(l.Params, []Ty{acc, elem}))
			v.Quiet = saved
			widened := Vjoin(acc, body)
			// The accumulator is stable when the body fits it, or when widening it adds nothing
			// (a rule type that assignable compares by name does not always fit its own value).
			if v.assignable(body, acc) || widened.String() == acc.String() {
				converged = true
				break
			}
			acc = widened
		}
		body := v.term(l.Body, env.with(l.Params, []Ty{acc, elem}))
		if !converged && !v.assignable(body, acc) && Vjoin(acc, body).String() != acc.String() {
			v.errorf(t.Diagnostic(), "%s: the accumulator type does not converge (the callback produces %s, which is not assignable to %s); cast the result of the callback", t.Func, body, acc)
		}
		return acc
	case "filter":
		elem := v.elemOf("filter", arg(0), t.Diagnostic())
		l, ok := v.lambda(t, 1, 1)
		if !ok {
			return TyAny
		}
		if body := v.term(l.Body, env.with(l.Params, []Ty{elem})); !v.assignable(body, TyBool) {
			v.errorf(t.Diagnostic(), "filter: predicate must produce a bool, got %s", body)
		}
		return ListTy{elem}
	case "map":
		elem := v.elemOf("map", arg(0), t.Diagnostic())
		l, ok := v.lambda(t, 1, 1)
		if !ok {
			return TyAny
		}
		return ListTy{v.term(l.Body, env.with(l.Params, []Ty{elem}))}
	case "list":
		ts := make([]Ty, len(t.Args))
		for i := range t.Args {
			ts[i] = arg(i)
		}
		return ListTy{Vjoin(ts...)}
	case "concat":
		ts := make([]Ty, len(t.Args))
		for i := range t.Args {
			ts[i] = v.elemOf("concat", arg(i), t.Diagnostic())
		}
		return ListTy{Vjoin(ts...)}
	}
	return TyAny
}
