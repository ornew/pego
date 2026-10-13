package grammaranalysis

import "github.com/ornew/pego/grammar"

// UngroupType removes source-only grouping without changing its input.
func UngroupType(t grammar.TypeExpr) grammar.TypeExpr {
	switch t := t.(type) {
	case *grammar.TypeGroup:
		return UngroupType(t.Type)
	case *grammar.ListType:
		child := UngroupType(t.Elem)
		if child != t.Elem {
			v := *t
			v.Elem = child
			return &v
		}
	case *grammar.OptionalType:
		child := UngroupType(t.Elem)
		if child != t.Elem {
			v := *t
			v.Elem = child
			return &v
		}
	case *grammar.UnionType:
		var children []grammar.TypeExpr
		for i, x := range t.Types {
			child := UngroupType(x)
			if child != x {
				if children == nil {
					children = append([]grammar.TypeExpr(nil), t.Types...)
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
	switch t := t.(type) {
	case *grammar.TermGroup:
		return UngroupTerm(t.Term)
	case *grammar.Member:
		child := UngroupTerm(t.X)
		if child != t.X {
			v := *t
			v.X = child
			return &v
		}
	case *grammar.ListConstructor:
		elem, items := UngroupType(t.Elem), ungroupTerms(t.Items)
		if elem != t.Elem || items != nil {
			v := *t
			v.Elem = elem
			if items != nil {
				v.Items = items
			}
			return &v
		}
	case *grammar.TerminalConstructor:
		args := ungroupTerms(t.Args)
		if args != nil {
			v := *t
			v.Args = args
			return &v
		}
	case *grammar.New:
		var fields []*grammar.FieldInit
		for i, f := range t.Fields {
			child := UngroupTerm(f.Value)
			if child != f.Value {
				if fields == nil {
					fields = append([]*grammar.FieldInit(nil), t.Fields...)
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
		args := ungroupTerms(t.Args)
		if args != nil {
			v := *t
			v.Args = args
			return &v
		}
	case *grammar.Lambda:
		child := UngroupTerm(t.Body)
		if child != t.Body {
			v := *t
			v.Body = child
			return &v
		}
	case *grammar.Binary:
		l, r := UngroupTerm(t.L), UngroupTerm(t.R)
		if l != t.L || r != t.R {
			v := *t
			v.L, v.R = l, r
			return &v
		}
	case *grammar.Unary:
		child := UngroupTerm(t.X)
		if child != t.X {
			v := *t
			v.X = child
			return &v
		}
	case *grammar.Assign:
		child := UngroupTerm(t.Value)
		if child != t.Value {
			v := *t
			v.Value = child
			return &v
		}
	case *grammar.Block:
		children := ungroupTerms(t.Statements)
		if children != nil {
			v := *t
			v.Statements = children
			return &v
		}
	}
	return t
}

func ungroupTerms(ts []grammar.Term) []grammar.Term {
	var children []grammar.Term
	for i, x := range ts {
		child := UngroupTerm(x)
		if child != x {
			if children == nil {
				children = append([]grammar.Term(nil), ts...)
			}
			children[i] = child
		}
	}
	return children
}
