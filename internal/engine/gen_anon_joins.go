package engine

import (
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
)

// anonJoin is a join of declared node types that the typed output writes where no alias declares
// it: the type of a field, of a list element or option, or the value type of a rule.
type anonJoin struct {
	// members are the distinct declared node types in the order written, nested joins flattened.
	members joinTy
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
// a named node type), besides error; a join with a member that is not a declared node type, such as
// node[string] or a list, has no interface to give and keeps its Go type.
func (r *valueResolver) anonJoins() []anonJoin {
	aliasOf := map[string]string{} // the set of the members of an alias join -> the alias
	var aliasNames []string
	for name := range r.info.aliases {
		aliasNames = append(aliasNames, name)
	}
	sort.Strings(aliasNames)
	for _, name := range aliasNames {
		if u, ok := r.info.aliases[name].(joinTy); ok {
			if m, ok := joinKey(u); ok {
				if _, dup := aliasOf[joinSetKey(m)]; !dup {
					aliasOf[joinSetKey(m)] = name
				}
			}
		}
	}
	var out []anonJoin
	index := map[string]int{}
	var collect func(x ty)
	collect = func(x ty) {
		switch x := x.(type) {
		case listTy:
			collect(x.elem)
		case optTy:
			collect(x.elem)
		case nodeTy:
			collect(x.value)
		case tupleTy:
			for _, e := range x.elems {
				collect(e)
			}
		case joinTy:
			for _, a := range x.alts {
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
			for _, f := range r.info.fields[d.Name] {
				collect(f.t)
			}
		case *grammar.TypeAlias:
			// The members of an alias are joined by the alias itself, but a member can be a join.
			if u, ok := r.info.aliases[d.Name].(joinTy); ok {
				for _, a := range u.alts {
					collect(a)
				}
			} else {
				collect(r.info.aliases[d.Name])
			}
		case *grammar.RuleDef:
			if _, ok := r.ruleDefs[d.Name]; ok {
				visited[d.Name] = true
				collectRule(d.Name)
			}
		}
	}
	for _, rl := range r.prog.rules {
		// Rules that the lowering of the grammar added follow the declared ones.
		if _, ok := r.ruleDefs[rl.name]; ok && !visited[rl.name] {
			collectRule(rl.name)
		}
	}
	return out
}

// joinKey returns the members of a join as anonJoins compares them, and whether the join is one
// that has an interface: at least two distinct members, all of them declared node types. Error is
// no member to compare: it implements every interface of a join.
func joinKey(u joinTy) (joinTy, bool) {
	var alts []ty
	seen := map[string]bool{}
	var add func(x ty) bool
	add = func(x ty) bool {
		if m, ok := joinMembers(x).(joinTy); ok {
			for _, a := range m.alts {
				if !add(a) {
					return false
				}
			}
			return true
		}
		x = joinMembers(joinTy{[]ty{x}}).(joinTy).alts[0]
		switch a := x.(type) {
		case namedTy:
			if a == tyError {
				return true // every interface of a join has the Error member
			}
			if a.kind != 's' {
				return false
			}
		case nodeTy:
			if a.name == "" {
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
	if !add(u) || len(alts) < 2 {
		return joinTy{}, false
	}
	return joinTy{alts}, true
}

// anonJoinName returns the Go name of the interface of an undeclared join: its members in the order
// written, joined by Or.
func anonJoinName(u joinTy) string {
	parts := make([]string, len(u.alts))
	for i, a := range u.alts {
		switch a := a.(type) {
		case namedTy:
			parts[i] = a.name
		case nodeTy:
			parts[i] = a.name
		}
	}
	return strings.Join(parts, "Or")
}

// joinSetKey returns a string that is the same for joins with the same members in any order.
func joinSetKey(u joinTy) string {
	names := make([]string, len(u.alts))
	for i, a := range u.alts {
		names[i] = a.String()
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}
