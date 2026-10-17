package grammaranalysis

import "github.com/ornew/pego/grammar"

// UngroupType removes source-only grouping without changing its input.
func UngroupType(t grammar.TypeExpr) grammar.TypeExpr {
	return ungroupType(t, nil)
}

// UngroupTypeWithPoll also visits each traversed node and copied slice element.
// The callback may panic to interrupt lowering; the source is never modified.
func UngroupTypeWithPoll(t grammar.TypeExpr, poll func()) grammar.TypeExpr {
	return ungroupType(t, poll)
}

func ungroupType(t grammar.TypeExpr, poll func()) grammar.TypeExpr {
	if poll != nil {
		poll()
	}
	switch t := t.(type) {
	case *grammar.TypeGroup:
		return ungroupType(t.Type, poll)
	case *grammar.ListType:
		child := ungroupType(t.Elem, poll)
		if child != t.Elem {
			v := *t
			v.Elem = child
			return &v
		}
	case *grammar.OptionalType:
		child := ungroupType(t.Elem, poll)
		if child != t.Elem {
			v := *t
			v.Elem = child
			return &v
		}
	case *grammar.UnionType:
		var children []grammar.TypeExpr
		for i, x := range t.Types {
			child := ungroupType(x, poll)
			if child != x {
				if children == nil {
					children = cloneGroupItems(t.Types, poll)
				}
				children[i] = child
			}
		}
		if children != nil {
			v := *t
			v.Types = children
			return &v
		}
	case *grammar.IntersectionType:
		var children []grammar.TypeExpr
		for i, x := range t.Types {
			child := ungroupType(x, poll)
			if child != x {
				if children == nil {
					children = cloneGroupItems(t.Types, poll)
				}
				children[i] = child
			}
		}
		if children != nil {
			v := *t
			v.Types = children
			return &v
		}
	}
	return t
}

// UngroupTerm removes source-only grouping without changing its input.
func UngroupTerm(t grammar.Term) grammar.Term {
	return ungroupTerm(t, nil)
}

// UngroupTermWithPoll is UngroupTerm with the same polling contract as
// UngroupTypeWithPoll, including nested type expressions and slice copies.
func UngroupTermWithPoll(t grammar.Term, poll func()) grammar.Term {
	return ungroupTerm(t, poll)
}

func ungroupTerm(t grammar.Term, poll func()) grammar.Term {
	if poll != nil {
		poll()
	}
	switch t := t.(type) {
	case *grammar.TermGroup:
		return ungroupTerm(t.Term, poll)
	case *grammar.Member:
		child := ungroupTerm(t.X, poll)
		if child != t.X {
			v := *t
			v.X = child
			return &v
		}
	case *grammar.ListConstructor:
		elem, items := ungroupType(t.Elem, poll), ungroupTerms(t.Items, poll)
		if elem != t.Elem || items != nil {
			v := *t
			v.Elem = elem
			if items != nil {
				v.Items = items
			}
			return &v
		}
	case *grammar.TerminalConstructor:
		args := ungroupTerms(t.Args, poll)
		if args != nil {
			v := *t
			v.Args = args
			return &v
		}
	case *grammar.New:
		var fields []*grammar.FieldInit
		for i, f := range t.Fields {
			child := ungroupTerm(f.Value, poll)
			if child != f.Value {
				if fields == nil {
					fields = cloneGroupItems(t.Fields, poll)
				}
				v := *f
				v.Value = child
				fields[i] = &v
			}
		}
		if fields != nil {
			v := *t
			v.Fields = fields
			return &v
		}
	case *grammar.Call:
		args := ungroupTerms(t.Args, poll)
		if args != nil {
			v := *t
			v.Args = args
			return &v
		}
	case *grammar.Lambda:
		child := ungroupTerm(t.Body, poll)
		if child != t.Body {
			v := *t
			v.Body = child
			return &v
		}
	case *grammar.Binary:
		l, r := ungroupTerm(t.L, poll), ungroupTerm(t.R, poll)
		if l != t.L || r != t.R {
			v := *t
			v.L, v.R = l, r
			return &v
		}
	case *grammar.Cast:
		child, typ := ungroupTerm(t.X, poll), ungroupType(t.Type, poll)
		if child != t.X || typ != t.Type {
			v := *t
			v.X, v.Type = child, typ
			return &v
		}
	case *grammar.Unary:
		child := ungroupTerm(t.X, poll)
		if child != t.X {
			v := *t
			v.X = child
			return &v
		}
	case *grammar.Assign:
		child := ungroupTerm(t.Value, poll)
		if child != t.Value {
			v := *t
			v.Value = child
			return &v
		}
	case *grammar.Block:
		children := ungroupTerms(t.Statements, poll)
		if children != nil {
			v := *t
			v.Statements = children
			return &v
		}
	}
	return t
}

func ungroupTerms(ts []grammar.Term, poll func()) []grammar.Term {
	var children []grammar.Term
	for i, x := range ts {
		child := ungroupTerm(x, poll)
		if child != x {
			if children == nil {
				children = cloneGroupItems(ts, poll)
			}
			children[i] = child
		}
	}
	return children
}

// Ordinary callers retain the bulk copy; cancellable traversals count copied
// elements as well as visited nodes. Allocations remain nonpreemptible.
func cloneGroupItems[T any](src []T, poll func()) []T {
	if poll == nil {
		return append([]T(nil), src...)
	}
	poll()
	out := make([]T, len(src))
	for i, value := range src {
		poll()
		out[i] = value
	}
	return out
}
