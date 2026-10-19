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
	// reportUnsupported reports type syntax that is not supported yet even while report is
	// false: without type checking such types would silently accept every value.
	reportUnsupported bool
	// sawUnsupported records that a resolved type used syntax that is not supported yet, whether
	// or not it was reported here, so that checks of the type are not reported on top.
	sawUnsupported bool
	// resolving holds the type aliases being resolved (for cycle detection).
	resolving map[string]bool
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

// resolve turns a type expression into a type.
func (k *typeResolver) resolve(t grammar.TypeExpr) ty {
	k.control.poll()
	switch t := t.(type) {
	case *grammar.TypeRef:
		return k.resolveNamed(t.Name, t.Diagnostic())
	case *grammar.ListType:
		// A constructor applied to _|_ is _|_.
		if e := k.resolve(t.Elem); e != tyNever {
			return listTy{e}
		}
		return tyNever
	case *grammar.OptionalType:
		if e := k.resolve(t.Elem); e != tyNever {
			return optional(e)
		}
		return tyNever
	case *grammar.UnionType:
		ts := make([]ty, len(t.Types))
		for i, x := range t.Types {
			ts[i] = k.resolve(x)
		}
		return union(ts...)
	case *grammar.TupleType:
		// Record 060 types: reserved syntax until values carry them.
		k.unsupported(t, t.Diagnostic(), "tuple types are not supported yet")
		return tyAny
	case *grammar.StructType:
		k.unsupported(t, t.Diagnostic(), "unnamed struct types are not supported yet; define one with type T = {...}")
		return tyAny
	case *grammar.TypeApp:
		if t.Name == "node" {
			// node[string] is a string node and node[_] a node of any value. Other type arguments
			// wait for tuple types.
			if len(t.Args) == 1 {
				switch k.resolve(t.Args[0]) {
				case tyString:
					return tyMatch
				case tyTop:
					return tyNode
				}
			}
			k.unsupported(t, t.Diagnostic(), "type arguments of node other than node[string] and node[_] are not supported yet")
		} else if k.defined(t.Name) {
			k.resolveNamed(t.Name, t.Diagnostic()) // reports an alias that refers to itself
			k.unsupported(t, t.Diagnostic(), "type %s takes no type arguments", t.Name)
		} else {
			k.unsupported(t, t.Diagnostic(), "undefined type %s", t.Name)
		}
		return tyAny
	case *grammar.IntersectionType:
		m := ty(tyTop) // the meet of no types
		for _, x := range t.Types {
			m = meet(m, k.resolve(x))
		}
		return m
	}
	return tyAny
}

// unsupported reports type syntax t that is not supported yet (reportUnsupported), once.
func (k *typeResolver) unsupported(t grammar.TypeExpr, pos grammar.Pos, format string, args ...any) {
	k.sawUnsupported = true
	if !k.report && !k.reportUnsupported || k.prog.unsupportedSeen[t] {
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
	if r == tyNever {
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
		// node without a type argument would accept every node where the value is known.
		k.errorf(pos, "node takes a type argument: write node[T], or node[_] for a node of any value")
		return tyNode
	case "any":
		return tyAny
	case "_":
		return tyTop
	case "_|_":
		return tyNever
	case TypeMissing:
		return namedTy{name, 'c'}
	case "error": // the value of recovery: a recovered error or a missing token
		return union(namedTy{TypeError, 'c'}, namedTy{TypeMissing, 'c'})
	}
	switch spec := k.prog.types[name].(type) {
	case *grammar.StructSpec:
		return namedTy{name, 's'}
	case *grammar.AliasSpec:
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
	"terminal": "node[string]",
	"Match":    "node[string]",
	"Seq":      "node[_]",
	"List":     "node[_]",
	"Operator": "node[_]",
	"Error":    "error",
}

// checkTypeDefinitions reports the errors of the type definitions: duplicate fields, field names
// that do not start with an uppercase letter, and types that do not resolve (undefined names,
// aliases that refer to themselves, unsupported syntax and fields of type _|_).
func (k *typeResolver) checkTypeDefinitions() {
	for _, td := range k.control.types(k.prog.Grammar) {
		k.control.poll()
		switch spec := td.Spec.(type) {
		case *grammar.StructSpec:
			seen := map[string]bool{}
			for _, f := range spec.Fields {
				k.control.poll()
				if seen[f.Name] {
					k.errorf(f.Diagnostic(), "duplicate field %s in %s", f.Name, td.Name)
				}
				if !startsUpper(f.Name) {
					k.errorf(f.Diagnostic(), "field %s of %s must start with an uppercase letter", f.Name, td.Name)
				}
				seen[f.Name] = true
				k.resolveLocation(f.Type, f.Diagnostic(), "field "+f.Name+" of "+td.Name)
			}
		case *grammar.AliasSpec:
			k.resolveNamed(td.Name, td.Diagnostic())
		}
	}
}
