package gen

import (
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// Vrule is a reference to a rule whose Go type is that of its value in the value model; the rule's
// resolved value type, with references replaced, is ValueResolver.Vres[name].
type Vrule struct{ Name string }

func (r Vrule) String() string { return r.Name }

// ValueResolver resolves the value types of the rules whose typed values come from them.
type ValueResolver struct {
	Prog *compile.Program
	info *compile.TypeInfo
	// values holds the value types of the rules and ruleDefs their definitions; Vres holds the
	// resolved value types of the rules whose typed values come from them, and resolving the rules
	// being resolved.
	values    map[string]compile.Ty
	ruleDefs  map[string]*grammar.RuleDef
	Vres      map[string]compile.Ty
	resolving map[string]bool
	// ownType reports that a legacy type has a typed representation of its own (a named join).
	ownType func(compile.Ty) bool
}

func NewValueResolver(p *compile.Program, ownType func(compile.Ty) bool) ValueResolver {
	r := ValueResolver{Prog: p, info: p.Typed, values: p.ValueTypes(), ruleDefs: map[string]*grammar.RuleDef{}, Vres: map[string]compile.Ty{}, resolving: map[string]bool{}, ownType: ownType}
	for _, rl := range p.RuleList {
		r.ruleDefs[rl.Name] = rl.Def
	}
	return r
}

func (t *ValueResolver) RuleRef(name string) compile.Ty {
	var legacy compile.Ty = compile.TyAny
	if def := t.ruleDefs[name]; def != nil && def.Type != nil {
		legacy = (&compile.TypeResolver{Prog: t.Prog}).Resolve(def.Type)
	}
	if !t.usesValue(name, legacy) {
		return legacy
	}
	if _, ok := t.Vres[name]; ok {
		return Vrule{name}
	}
	if t.resolving[name] {
		return compile.TyTop // a cycle: the reference becomes any
	}
	t.resolving[name] = true
	res := t.ResolveValue(t.values[name])
	delete(t.resolving, name)
	t.Vres[name] = res
	return Vrule{name}
}

// A rule has the Go type of its value if it has no declared type; a declared type is the rule's Go
// type, node[_] included.
func (t *ValueResolver) usesValue(name string, legacy compile.Ty) bool {
	v, ok := t.values[name]
	def := t.ruleDefs[name]
	if !ok || v == nil || v == compile.TyAny || def == nil {
		return false
	}
	if def.Type != nil && def.Action != nil {
		return false // the action's value has the declared type
	}
	if _, operators := def.Expr.(*grammar.OperatorsRule); operators && def.Type != nil {
		return false
	}
	if t.ownType(legacy) {
		return false
	}
	return def.Type == nil
}

// ResolveValue replaces the rule references in a value type.
func (t *ValueResolver) ResolveValue(x compile.Ty) compile.Ty {
	switch x := x.(type) {
	case compile.RuleTy:
		return t.RuleRef(x.Name)
	case compile.TupleTy:
		elems := make([]compile.Ty, len(x.Elems))
		for i, e := range x.Elems {
			elems[i] = t.ResolveValue(e)
		}
		return compile.TupleTy{Elems: elems}
	case compile.NodeTy:
		return compile.NodeTy{Value: t.ResolveValue(x.Value)}
	case compile.ListTy:
		return compile.ListTy{Elem: t.ResolveValue(x.Elem)}
	case compile.OptTy:
		return compile.OptTy{Elem: t.ResolveValue(x.Elem)}
	case compile.JoinTy:
		alts := make([]compile.Ty, len(x.Alts))
		for i, a := range x.Alts {
			alts[i] = t.ResolveValue(a)
		}
		return compile.JoinTy{Alts: alts}
	case compile.BasicTy:
		if x == compile.TyBottom || x == compile.TyNil {
			return compile.TyTop
		}
	}
	return x
}

// anonJoin is a join of declared node types that the typed output writes where no alias declares
// it: the type of a field, of a list element or option, or the value type of a rule.
type anonJoin struct {
	// Members are the distinct declared node types in the order written, nested joins flattened.
	Members compile.JoinTy
	// Alias is the alias that declares the same members, or empty if none does; then the join is
	// the type of that alias.
	Alias string
	// Spellings are the written forms (joinTy.String) of the type, which the generators look it up
	// by.
	Spellings []string
}

// AnonJoins returns the joins that the typed output of the program writes without naming them: the
// types of fields, of list elements and options inside them, of aliases, and the value types of
// rules, in the order of the declarations of the file and of the fields in them. A join is
// returned once for the same set of members, in the order of its first appearance there. Each has at least two distinct members, all of them declared node types (a struct or
// a named node type), besides error, or one declared node type and error; a join with a member that is not a declared node type, such as
// node[string] or a list, has no interface to give and keeps its Go type.
func (r *ValueResolver) AnonJoins() []anonJoin {
	aliasOf := map[string]string{} // the set of the members of an alias join -> the alias
	var aliasNames []string
	for name := range r.info.Aliases {
		aliasNames = append(aliasNames, name)
	}
	sort.Strings(aliasNames)
	for _, name := range aliasNames {
		if u, ok := r.info.Aliases[name].(compile.JoinTy); ok {
			if m, ok := joinKey(u); ok {
				if _, dup := aliasOf[joinSetKey(m)]; !dup {
					aliasOf[joinSetKey(m)] = name
				}
			}
		}
	}
	var out []anonJoin
	index := map[string]int{}
	var collect func(x compile.Ty)
	collect = func(x compile.Ty) {
		switch x := x.(type) {
		case compile.ListTy:
			collect(x.Elem)
		case compile.OptTy:
			collect(x.Elem)
		case compile.NodeTy:
			collect(x.Value)
		case compile.TupleTy:
			for _, e := range x.Elems {
				collect(e)
			}
		case compile.JoinTy:
			for _, a := range x.Alts {
				collect(a)
			}
			m, ok := joinKey(x)
			if !ok {
				return
			}
			i, seen := index[joinSetKey(m)]
			if !seen {
				i = len(out)
				index[joinSetKey(m)] = i
				out = append(out, anonJoin{Members: m, Alias: aliasOf[joinSetKey(m)]})
			}
			for _, s := range out[i].Spellings {
				if s == x.String() {
					return
				}
			}
			out[i].Spellings = append(out[i].Spellings, x.String())
		}
	}
	// The joins are collected in source order, so the first spelling of a set of members, which
	// names its interface, does not depend on the names of the types.
	collectRule := func(name string) {
		if ref, ok := r.RuleRef(name).(Vrule); ok {
			collect(r.Vres[ref.Name])
		} else {
			collect(r.RuleRef(name))
		}
	}
	visited := map[string]bool{}
	for _, d := range r.Prog.Grammar.File.Decls {
		switch d := d.(type) {
		case *grammar.TypeDef:
			for _, f := range r.info.Fields[d.Name] {
				collect(f.T)
			}
		case *grammar.TypeAlias:
			// The members of an alias are joined by the alias itself, but a member can be a join.
			if u, ok := r.info.Aliases[d.Name].(compile.JoinTy); ok {
				for _, a := range u.Alts {
					collect(a)
				}
			} else {
				collect(r.info.Aliases[d.Name])
			}
		case *grammar.RuleDef:
			if _, ok := r.ruleDefs[d.Name]; ok {
				visited[d.Name] = true
				collectRule(d.Name)
			}
		}
	}
	for _, rl := range r.Prog.RuleList {
		// Rules that the lowering of the grammar added follow the declared ones.
		if _, ok := r.ruleDefs[rl.Name]; ok && !visited[rl.Name] {
			collectRule(rl.Name)
		}
	}
	return out
}

// joinKey returns the members of a join as anonJoins compares them, and whether the join is one
// that has an interface: at least two distinct members, or one with error, all of them declared
// node types. Error is no member to compare: it implements every interface of a join.
func joinKey(u compile.JoinTy) (compile.JoinTy, bool) {
	var alts []compile.Ty
	seen := map[string]bool{}
	hasError := false
	var add func(x compile.Ty) bool
	add = func(x compile.Ty) bool {
		if m, ok := compile.JoinMembers(x).(compile.JoinTy); ok {
			for _, a := range m.Alts {
				if !add(a) {
					return false
				}
			}
			return true
		}
		x = compile.JoinMembers(compile.JoinTy{Alts: []compile.Ty{x}}).(compile.JoinTy).Alts[0]
		switch a := x.(type) {
		case compile.NamedTy:
			if a == compile.TyError {
				hasError = true // every interface of a join has the Error member
				return true
			}
			if a.Kind != 's' {
				return false
			}
		case compile.NodeTy:
			if a.Name == "" {
				return false
			}
		default:
			return false
		}
		if !seen[x.String()] {
			seen[x.String()] = true
			alts = append(alts, x)
		}
		return true
	}
	// A declared node type with error is a join, too: the value of a rule that recovers.
	if !add(u) || len(alts) < 2 && !(len(alts) == 1 && hasError) {
		return compile.JoinTy{}, false
	}
	return compile.JoinTy{Alts: alts}, true
}

// joinSetKey returns a string that is the same for joins with the same members in any order.
func joinSetKey(u compile.JoinTy) string {
	names := make([]string, len(u.Alts))
	for i, a := range u.Alts {
		names[i] = a.String()
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}
