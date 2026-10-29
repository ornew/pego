package engine

import (
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// anonJoin is a join of declared node types that the typed output writes where no alias declares
// it: the type of a field, of a list element or option, or the value type of a rule.
type anonJoin struct {
	// members are the distinct declared node types in the order written, nested joins flattened.
	members compile.JoinTy
	// alias is the alias that declares the same members, or empty if none does; then the join is
	// the type of that alias.
	alias string
	// spellings are the written forms (joinTy.String) of the type, which the generators look it up
	// by.
	spellings []string
}

// anonJoins returns the joins that the typed output of the program writes without naming them: the
// types of fields, of list elements and options inside them, of aliases, and the value types of
// rules, in the order of the declarations of the file and of the fields in them. A join is
// returned once for the same set of members, in the order of its first appearance there. Each has at least two distinct members, all of them declared node types (a struct or
// a named node type), besides error, or one declared node type and error; a join with a member that is not a declared node type, such as
// node[string] or a list, has no interface to give and keeps its Go type.
func (r *valueResolver) anonJoins() []anonJoin {
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
				out = append(out, anonJoin{members: m, alias: aliasOf[joinSetKey(m)]})
			}
			for _, s := range out[i].spellings {
				if s == x.String() {
					return
				}
			}
			out[i].spellings = append(out[i].spellings, x.String())
		}
	}
	// The joins are collected in source order, so the first spelling of a set of members, which
	// names its interface, does not depend on the names of the types.
	collectRule := func(name string) {
		if ref, ok := r.ruleRef(name).(vrule); ok {
			collect(r.vres[ref.name])
		} else {
			collect(r.ruleRef(name))
		}
	}
	visited := map[string]bool{}
	for _, d := range r.prog.Grammar.File.Decls {
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
	for _, rl := range r.prog.RuleList {
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

// anonJoinName returns the Go name of the interface of an undeclared join: its members in the order
// written, joined by Or; one declared node type and error is that type and Error.
func anonJoinName(u compile.JoinTy) string {
	parts := make([]string, len(u.Alts))
	for i, a := range u.Alts {
		switch a := a.(type) {
		case compile.NamedTy:
			parts[i] = a.Name
		case compile.NodeTy:
			parts[i] = a.Name
		}
	}
	if len(parts) == 1 {
		parts = append(parts, "Error")
	}
	return strings.Join(parts, "Or")
}

// anonJoinText returns the join u of anonJoins as written: one declared node type is that type and
// error.
func anonJoinText(u compile.JoinTy) string {
	if len(u.Alts) == 1 {
		return u.String() + " | error"
	}
	return u.String()
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
