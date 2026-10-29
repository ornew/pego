package compile

import (
	"fmt"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/value"
)

// TypeResolver turns the type expressions of a grammar into types. It reports undefined names,
// aliases that refer to themselves and type syntax that is not supported yet.
type TypeResolver struct {
	control *Control
	Prog    *Program
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

func (k *TypeResolver) errorf(pos grammar.Pos, format string, args ...any) {
	if !k.report {
		return
	}
	if !pos.IsValid() && k.rule != nil {
		pos = k.rule.Diagnostic()
	}
	*k.errs = append(*k.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// Resolve turns a type expression into a value type.
func (k *TypeResolver) Resolve(t grammar.TypeExpr) Ty {
	k.control.poll()
	switch t := t.(type) {
	case *grammar.TypeRef:
		return k.ResolveNamed(t.Name, t.Diagnostic())
	case *grammar.TypeGroup:
		return k.Resolve(t.Type)
	case *grammar.ListType:
		// A constructor applied to _|_ is _|_.
		if e := k.Resolve(t.Elem); e != TyBottom {
			return ListTy{e}
		}
		return TyBottom
	case *grammar.OptionalType:
		return vopt(k.Resolve(t.Elem))
	case *grammar.JoinType:
		ts := make([]Ty, len(t.Types))
		for i, x := range t.Types {
			ts[i] = k.Resolve(x)
		}
		return Vjoin(ts...)
	case *grammar.TupleType:
		elems := make([]Ty, len(t.Types))
		for i, x := range t.Types {
			if elems[i] = k.Resolve(x); elems[i] == TyBottom {
				return TyBottom
			}
		}
		return TupleTy{elems}
	case *grammar.StructType:
		k.unsupported(t, t.Diagnostic(), "unnamed struct types are not supported yet; define one with type T = {...}")
		return TyAny
	case *grammar.TypeApp:
		if t.Name == "node" {
			if len(t.Args) != 1 {
				// Reported like unsupported syntax.
				k.unsupported(t, t.Diagnostic(), "node takes one type argument, got %d", len(t.Args))
				return TyAny
			}
			// node[T] is a node whose value is of type T.
			if v := k.Resolve(t.Args[0]); v != TyBottom {
				return NodeTy{Value: v}
			}
			return TyBottom
		} else if k.defined(t.Name) {
			k.ResolveNamed(t.Name, t.Diagnostic()) // reports an alias that refers to itself
			k.unsupported(t, t.Diagnostic(), "type %s takes no type arguments", t.Name)
		} else {
			k.unsupported(t, t.Diagnostic(), "undefined type %s", t.Name)
		}
		return TyAny
	case *grammar.MeetType:
		m := Ty(TyTop) // the meet of no types
		for _, x := range t.Types {
			m = vmeet(m, k.Resolve(x))
		}
		return m
	}
	return TyAny
}

// vmeet returns the greatest lower bound a & b of two declared types: the type of the values of
// both a and b, or _|_ if they have none in common.
func vmeet(a, b Ty) Ty {
	var v ValueInference // declared types refer to no rules
	switch {
	case a == TyAny:
		return b
	case b == TyAny:
		return a
	case v.assignable(a, b):
		return a
	case v.assignable(b, a):
		return b
	}
	switch x := a.(type) {
	case ListTy:
		if y, ok := b.(ListTy); ok {
			if e := vmeet(x.Elem, y.Elem); e != TyBottom {
				return ListTy{e}
			}
			return TyBottom
		}
	case NodeTy:
		if y, ok := b.(NodeTy); ok {
			if x.Name != "" || y.Name != "" {
				return TyBottom // distinct named node types, or one and a node type it is not below
			}
			caps := x.Caps
			if len(caps) == 0 {
				caps = y.Caps
			} else if len(y.Caps) > 0 && !sameCaps(x.Caps, y.Caps) {
				return TyBottom
			}
			if e := vmeet(x.Value, y.Value); e != TyBottom {
				return NodeTy{Value: e, Caps: caps}
			}
			return TyBottom
		}
	case TupleTy:
		if y, ok := b.(TupleTy); ok {
			if len(x.Elems) != len(y.Elems) {
				return TyBottom
			}
			elems := make([]Ty, len(x.Elems))
			for i := range elems {
				if elems[i] = vmeet(x.Elems[i], y.Elems[i]); elems[i] == TyBottom {
					return TyBottom
				}
			}
			return TupleTy{Elems: elems}
		}
	}
	xs, ys := valueAlternatives(a), valueAlternatives(b)
	if len(xs) == 1 && len(ys) == 1 {
		return TyBottom // unrelated, and neither is a join or optional to split
	}
	var common []Ty
	for _, x := range xs {
		for _, y := range ys {
			common = append(common, vmeet(x, y))
		}
	}
	return Vjoin(common...)
}

// valueAlternatives returns the types whose values make up the values of t.
func valueAlternatives(t Ty) []Ty {
	switch t := t.(type) {
	case JoinTy:
		var alts []Ty
		for _, a := range t.Alts {
			alts = append(alts, valueAlternatives(a)...)
		}
		return alts
	case OptTy:
		return append(valueAlternatives(t.Elem), TyNil)
	}
	return []Ty{t}
}

// unsupported reports type syntax t that is not supported yet, once.
func (k *TypeResolver) unsupported(t grammar.TypeExpr, pos grammar.Pos, format string, args ...any) {
	k.sawUnsupported = true
	if !k.report || k.Prog.unsupportedSeen[t] {
		return
	}
	if k.Prog.unsupportedSeen == nil {
		k.Prog.unsupportedSeen = map[grammar.TypeExpr]bool{}
	}
	k.Prog.unsupportedSeen[t] = true
	report := k.report
	k.report = true
	k.errorf(pos, format, args...)
	k.report = report
}

// defined reports whether name is a built-in or declared type.
func (k *TypeResolver) defined(name string) bool {
	if _, ok := k.Prog.Types[name]; ok {
		return true
	}
	r := &TypeResolver{Prog: k.Prog, errs: &ErrorList{}, report: true}
	r.ResolveNamed(name, grammar.Pos{})
	return len(*r.errs) == 0
}

// resolveLocation resolves the type of a location: a field, a rule, a list element or a cast.
// No value has the type _|_, so a location of that type is an error.
func (k *TypeResolver) resolveLocation(t grammar.TypeExpr, pos grammar.Pos, what string) Ty {
	r := k.Resolve(t)
	if r == TyBottom {
		k.errorf(pos, "%s has type _|_, which has no values", what)
		return TyAny
	}
	return r
}

// ResolveNamed resolves the type name written at pos: a built-in type, a reserved node kind or a
// declared type, reporting names that are undefined or used without a required argument.
func (k *TypeResolver) ResolveNamed(name string, pos grammar.Pos) Ty {
	k.control.poll()
	switch name {
	case "int":
		return TyInt
	case "string":
		return TyString
	case "bool":
		return TyBool
	case "node":
		k.errorf(pos, "node takes a type argument: write node[T] with the type T of the node's value")
		return NodeTy{Value: TyTop}
	case "_":
		return TyTop
	case "_|_":
		return TyBottom
	case value.TypeMissing: // an inserted missing token, one kind of error
		return TyMissing
	case "error": // the value of recovery: a recovered error or a missing token
		return TyErr
	}
	switch spec := k.Prog.Types[name].(type) {
	case *grammar.TypeDef:
		val := spec.NodeValue()
		if val == nil {
			return NamedTy{name, 's'}
		}
		// Within the value of a named node type, a named node type stands for itself by its
		// name, like a struct type: its value is read from its definition where it is needed
		// (Program.nodeValueOf). Types that refer to each other thus resolve in linear time,
		// and a type alias may refer to itself through a named node type.
		if k.inNode {
			return NodeTy{Value: TyTop, Name: name}
		}
		k.control.poll()
		resolving := k.resolving
		k.resolving, k.inNode = nil, true
		v := k.Resolve(val)
		k.resolving, k.inNode = resolving, false
		if v == TyBottom {
			return TyBottom // node[_|_] has no values
		}
		return NodeTy{Value: v, Name: name}
	case *grammar.TypeAlias:
		if k.resolving == nil {
			k.resolving = map[string]bool{}
		}
		if k.resolving[name] {
			k.errorf(pos, "type %s refers to itself", name)
			return TyAny
		}
		k.resolving[name] = true
		defer delete(k.resolving, name)
		return k.Resolve(spec.Type)
	}
	if to, ok := retiredTypeNames[name]; ok {
		k.errorf(pos, "%s is not a type; write %s", name, to)
		return TyAny
	}
	k.errorf(pos, "undefined type %s", name)
	return TyAny
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

// DefFields returns the fields of the struct type that spec defines, and nil for a named node type.
func DefFields(spec *grammar.TypeDef) []*grammar.Field {
	if st := spec.Struct(); st != nil {
		return st.Fields
	}
	return nil
}

// checkTypeDefinitions reports the errors of the type definitions: duplicate fields, field names
// that do not start with an uppercase letter, and types that do not resolve (undefined names,
// aliases that refer to themselves, unsupported syntax and fields of type _|_).
func (k *TypeResolver) checkTypeDefinitions() {
	for _, td := range k.control.types(k.Prog.Grammar) {
		k.control.poll()
		switch spec := td.(type) {
		case *grammar.TypeDef:
			if val := spec.NodeValue(); val != nil {
				k.resolveLocation(val, spec.Diagnostic(), "the value of node type "+td.DeclName())
				continue
			}
			seen := map[string]bool{}
			for _, f := range DefFields(spec) {
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
			k.ResolveNamed(td.DeclName(), td.Diagnostic())
		}
	}
}
