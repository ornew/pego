package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

// typeResolver turns the type expressions of a grammar into types. It reports undefined names,
// aliases that refer to themselves and type syntax that is not supported yet.
type typeResolver struct {
	control *compileControl
	prog    *Program
	errs    *ErrorList
	report  bool
	// sawUnsupported records that a resolved type used syntax that is not supported yet, whether
	// or not it was reported here, so that checks of the type are not reported on top.
	sawUnsupported bool
	// resolving holds the type aliases being resolved (for cycle detection).
	resolving map[string]bool
	// inNode reports that the value of a named node type is being resolved: named node types
	// in it resolve to their names only.
	inNode bool
	// rule is the rule being checked, whose position stands in for a missing one.
	rule *grammar.RuleDef
}

func (k *typeResolver) errorf(pos grammar.Pos, format string, args ...any) {
	if !k.report {
		return
	}
	if !pos.IsValid() && k.rule != nil {
		pos = k.rule.Diagnostic()
	}
	*k.errs = append(*k.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// resolve turns a type expression into a value type.
func (k *typeResolver) resolve(t grammar.TypeExpr) ty {
	k.control.poll()
	switch t := t.(type) {
	case *grammar.TypeRef:
		return k.resolveNamed(t.Name, t.Diagnostic())
	case *grammar.TypeGroup:
		return k.resolve(t.Type)
	case *grammar.ListType:
		// A constructor applied to _|_ is _|_.
		if e := k.resolve(t.Elem); e != tyBottom {
			return listTy{e}
		}
		return tyBottom
	case *grammar.OptionalType:
		return vopt(k.resolve(t.Elem))
	case *grammar.JoinType:
		ts := make([]ty, len(t.Types))
		for i, x := range t.Types {
			ts[i] = k.resolve(x)
		}
		return vjoin(ts...)
	case *grammar.TupleType:
		elems := make([]ty, len(t.Types))
		for i, x := range t.Types {
			if elems[i] = k.resolve(x); elems[i] == tyBottom {
				return tyBottom
			}
		}
		return tupleTy{elems}
	case *grammar.StructType:
		k.unsupported(t, t.Diagnostic(), "unnamed struct types are not supported yet; define one with type T = {...}")
		return tyAny
	case *grammar.TypeApp:
		if t.Name == "node" {
			if len(t.Args) != 1 {
				// Reported like unsupported syntax.
				k.unsupported(t, t.Diagnostic(), "node takes one type argument, got %d", len(t.Args))
				return tyAny
			}
			// node[T] is a node whose value is of type T.
			if v := k.resolve(t.Args[0]); v != tyBottom {
				return nodeTy{value: v}
			}
			return tyBottom
		} else if k.defined(t.Name) {
			k.resolveNamed(t.Name, t.Diagnostic()) // reports an alias that refers to itself
			k.unsupported(t, t.Diagnostic(), "type %s takes no type arguments", t.Name)
		} else {
			k.unsupported(t, t.Diagnostic(), "undefined type %s", t.Name)
		}
		return tyAny
	case *grammar.MeetType:
		m := ty(tyTop) // the meet of no types
		for _, x := range t.Types {
			m = vmeet(m, k.resolve(x))
		}
		return m
	}
	return tyAny
}

// vmeet returns the greatest lower bound a & b of two declared types: the type of the values of
// both a and b, or _|_ if they have none in common.
func vmeet(a, b ty) ty {
	var v valueInference // declared types refer to no rules
	switch {
	case a == tyAny:
		return b
	case b == tyAny:
		return a
	case v.assignable(a, b):
		return a
	case v.assignable(b, a):
		return b
	}
	switch x := a.(type) {
	case listTy:
		if y, ok := b.(listTy); ok {
			if e := vmeet(x.elem, y.elem); e != tyBottom {
				return listTy{e}
			}
			return tyBottom
		}
	case nodeTy:
		if y, ok := b.(nodeTy); ok {
			if x.name != "" || y.name != "" {
				return tyBottom // distinct named node types, or one and a node type it is not below
			}
			caps := x.caps
			if len(caps) == 0 {
				caps = y.caps
			} else if len(y.caps) > 0 && !sameCaps(x.caps, y.caps) {
				return tyBottom
			}
			if e := vmeet(x.value, y.value); e != tyBottom {
				return nodeTy{value: e, caps: caps}
			}
			return tyBottom
		}
	case tupleTy:
		if y, ok := b.(tupleTy); ok {
			if len(x.elems) != len(y.elems) {
				return tyBottom
			}
			elems := make([]ty, len(x.elems))
			for i := range elems {
				if elems[i] = vmeet(x.elems[i], y.elems[i]); elems[i] == tyBottom {
					return tyBottom
				}
			}
			return tupleTy{elems: elems}
		}
	}
	xs, ys := valueAlternatives(a), valueAlternatives(b)
	if len(xs) == 1 && len(ys) == 1 {
		return tyBottom // unrelated, and neither is a join or optional to split
	}
	var common []ty
	for _, x := range xs {
		for _, y := range ys {
			common = append(common, vmeet(x, y))
		}
	}
	return vjoin(common...)
}

// valueAlternatives returns the types whose values make up the values of t.
func valueAlternatives(t ty) []ty {
	switch t := t.(type) {
	case joinTy:
		var alts []ty
		for _, a := range t.alts {
			alts = append(alts, valueAlternatives(a)...)
		}
		return alts
	case optTy:
		return append(valueAlternatives(t.elem), tyNil)
	}
	return []ty{t}
}

// unsupported reports type syntax t that is not supported yet, once.
func (k *typeResolver) unsupported(t grammar.TypeExpr, pos grammar.Pos, format string, args ...any) {
	k.sawUnsupported = true
	if !k.report || k.prog.unsupportedSeen[t] {
		return
	}
	if k.prog.unsupportedSeen == nil {
		k.prog.unsupportedSeen = map[grammar.TypeExpr]bool{}
	}
	k.prog.unsupportedSeen[t] = true
	report := k.report
	k.report = true
	k.errorf(pos, format, args...)
	k.report = report
}

// defined reports whether name is a built-in or declared type.
func (k *typeResolver) defined(name string) bool {
	if _, ok := k.prog.types[name]; ok {
		return true
	}
	r := &typeResolver{prog: k.prog, errs: &ErrorList{}, report: true}
	r.resolveNamed(name, grammar.Pos{})
	return len(*r.errs) == 0
}

// resolveLocation resolves the type of a location: a field, a rule, a list element or a cast.
// No value has the type _|_, so a location of that type is an error.
func (k *typeResolver) resolveLocation(t grammar.TypeExpr, pos grammar.Pos, what string) ty {
	r := k.resolve(t)
	if r == tyBottom {
		k.errorf(pos, "%s has type _|_, which has no values", what)
		return tyAny
	}
	return r
}

func (k *typeResolver) resolveNamed(name string, pos grammar.Pos) ty {
	k.control.poll()
	switch name {
	case "int":
		return tyInt
	case "string":
		return tyString
	case "bool":
		return tyBool
	case "node":
		k.errorf(pos, "node takes a type argument: write node[T] with the type T of the node's value")
		return nodeTy{value: tyTop}
	case "_":
		return tyTop
	case "_|_":
		return tyBottom
	case TypeMissing: // an inserted missing token, one kind of error
		return tyMissing
	case "error": // the value of recovery: a recovered error or a missing token
		return tyErr
	}
	switch spec := k.prog.types[name].(type) {
	case *grammar.TypeDef:
		value := spec.NodeValue()
		if value == nil {
			return namedTy{name, 's'}
		}
		// Within the value of a named node type, a named node type stands for itself by its
		// name, like a struct type: its value is read from its definition where it is needed
		// (Program.nodeValueOf). Types that refer to each other thus resolve in linear time,
		// and a type alias may refer to itself through a named node type.
		if k.inNode {
			return nodeTy{value: tyTop, name: name}
		}
		k.control.poll()
		resolving := k.resolving
		k.resolving, k.inNode = nil, true
		v := k.resolve(value)
		k.resolving, k.inNode = resolving, false
		if v == tyBottom {
			return tyBottom // node[_|_] has no values
		}
		return nodeTy{value: v, name: name}
	case *grammar.TypeAlias:
		if k.resolving == nil {
			k.resolving = map[string]bool{}
		}
		if k.resolving[name] {
			k.errorf(pos, "type %s refers to itself", name)
			return tyAny
		}
		k.resolving[name] = true
		defer delete(k.resolving, name)
		return k.resolve(spec.Type)
	}
	if to, ok := retiredTypeNames[name]; ok {
		k.errorf(pos, "%s is not a type; write %s", name, to)
		return tyAny
	}
	k.errorf(pos, "undefined type %s", name)
	return tyAny
}

// retiredTypeNames maps the names of the former terminal and concrete syntax tree types to the
// types that replace them.
var retiredTypeNames = map[string]string{
	"any":      "_",
	"terminal": "node[string]",
	"Match":    "node[string]",
	"Seq":      "node[_]",
	"List":     "node[_]",
	"Operator": "node[_]",
	"Error":    "error",
	"Missing":  "missing",
}

// defFields returns the fields of the struct type that spec defines, and nil for a named node type.
func defFields(spec *grammar.TypeDef) []*grammar.Field {
	if st := spec.Struct(); st != nil {
		return st.Fields
	}
	return nil
}

// checkTypeDefinitions reports the errors of the type definitions: duplicate fields, field names
// that do not start with an uppercase letter, and types that do not resolve (undefined names,
// aliases that refer to themselves, unsupported syntax and fields of type _|_).
func (k *typeResolver) checkTypeDefinitions() {
	for _, td := range k.control.types(k.prog.Grammar) {
		k.control.poll()
		switch spec := td.(type) {
		case *grammar.TypeDef:
			if value := spec.NodeValue(); value != nil {
				k.resolveLocation(value, spec.Diagnostic(), "the value of node type "+td.DeclName())
				continue
			}
			seen := map[string]bool{}
			for _, f := range defFields(spec) {
				k.control.poll()
				if seen[f.Name] {
					k.errorf(f.Diagnostic(), "duplicate field %s in %s", f.Name, td.DeclName())
				}
				if !startsUpper(f.Name) {
					k.errorf(f.Diagnostic(), "field %s of %s must start with an uppercase letter", f.Name, td.DeclName())
				}
				seen[f.Name] = true
				k.resolveLocation(f.Type, f.Diagnostic(), "field "+f.Name+" of "+td.DeclName())
			}
		case *grammar.TypeAlias:
			k.resolveNamed(td.DeclName(), td.Diagnostic())
		}
	}
}
