package syntax

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/ornew/pego/grammar"
)

// fixPositional rewrites the positional captures of rule, operand and inline actions to named
// captures. An element referred to by $n is labeled unless it already has a label; a rule call
// is labeled with the rule's name when that name is free, and anything else with v1, v2, ....
// $0 becomes list($a, $b, ...) of every element with a value.
func fixPositional(src string) (string, error) {
	g, err := ParseLegacy(src)
	if err != nil {
		return "", fmt.Errorf("positional captures cannot be rewritten until the grammar parses: %w", err)
	}
	o := newFixOffsets(src)
	var edits []fixEdit
	for _, s := range g.Statements {
		rd, ok := s.(*grammar.RuleDef)
		if !ok {
			continue
		}
		taken := map[string]bool{}
		inspectAST(rd, func(n any) {
			switch n := n.(type) {
			case *grammar.Capture:
				taken[n.Name] = true
			case *grammar.CaptureRef:
				taken[n.Name] = true
			case *grammar.Lambda:
				// A capture named like a parameter would be shadowed inside the lambda.
				for _, p := range n.Params {
					taken[p] = true
				}
			}
		})
		var err error
		handled := map[*grammar.IndexRef]bool{}
		site := func(e grammar.Expr, action grammar.Term) {
			if err == nil {
				var site []fixEdit
				site, err = positionalSite(o, e, action, taken, handled)
				edits = append(edits, site...)
			}
		}
		inspectAST(rd.Expr, func(n any) {
			if ia, ok := n.(*grammar.InlineAction); ok {
				site(ia.Expr, ia.Action)
			}
		})
		if or, ok := rd.Expr.(*grammar.OperatorsRule); ok {
			for _, op := range or.Operands {
				site(op.Expr, op.Action)
			}
		} else {
			site(rd.Expr, rd.Action)
		}
		if err == nil {
			// The others are in predicates or operator actions, which never had positional
			// captures to refer to.
			inspectAST(rd, func(n any) {
				if r, ok := n.(*grammar.IndexRef); ok && !handled[r] && err == nil {
					err = fmt.Errorf("%d:%d: $%d cannot be rewritten: only rule, operand and inline actions have positional captures", r.Pos.Line, r.Pos.Col, r.Index)
				}
			})
		}
		if err != nil {
			return "", err
		}
	}
	if len(edits) == 0 {
		return src, nil
	}
	return applyFixEdits(src, edits), nil
}

// positionalSite returns the edits for the positional captures of one action over expression
// e. taken holds the capture names in use in the rule, and gains the names it adds; handled
// gains the positional captures of the action. Groups are looked through as the compiler does:
// the elements of ("a" "b") are those of "a" "b".
func positionalSite(o *fixOffsets, e grammar.Expr, action grammar.Term, taken map[string]bool, handled map[*grammar.IndexRef]bool) ([]fixEdit, error) {
	var refs []*grammar.IndexRef
	inspectAST(action, func(n any) {
		if r, ok := n.(*grammar.IndexRef); ok {
			refs = append(refs, r)
			handled[r] = true
		}
	})
	if len(refs) == 0 {
		return nil, nil
	}
	var items []grammar.Expr
	if seq, ok := ungroup(e).(*grammar.Seq); ok {
		for _, it := range seq.Items {
			if valued(it) {
				items = append(items, it)
			}
		}
	} else if valued(e) {
		items = []grammar.Expr{e}
	}
	var edits []fixEdit
	names := make([]string, len(items))
	// name returns the capture name of item i, labeling the item when it has none.
	name := func(i int) string {
		if names[i] != "" {
			return names[i]
		}
		it := items[i]
		if c, ok := ungroup(it).(*grammar.Capture); ok {
			names[i] = c.Name
			return c.Name
		}
		n := ""
		if r, ok := ungroup(it).(*grammar.Ref); ok && !taken[r.Name] && !strings.HasPrefix(r.Name, "_") {
			n = r.Name
		}
		for k := i + 1; n == ""; k++ {
			if c := fmt.Sprintf("v%d", k); !taken[c] {
				n = c
			}
		}
		taken[n] = true
		names[i] = n
		src := it.(grammar.SourceNode).Source()
		start := o.at(src.Pos)
		if _, ok := it.(*grammar.Choice); ok {
			// A choice that is not parenthesized is a whole body; a label would bind to its
			// first alternative.
			end := o.at(src.End)
			edits = append(edits, fixEdit{start, start, n + ":("}, fixEdit{end, end, ")"})
		} else {
			edits = append(edits, fixEdit{start, start, n + ":"})
		}
		return n
	}
	for _, r := range refs {
		var text string
		switch {
		case r.Index == 0:
			var args []string
			for i := range items {
				args = append(args, "$"+name(i))
			}
			text = "list(" + strings.Join(args, ", ") + ")"
		case r.Index <= len(items):
			text = "$" + name(r.Index-1)
		default:
			return nil, fmt.Errorf("%d:%d: $%d is out of range: there are %d elements with a value", r.Pos.Line, r.Pos.Col, r.Index, len(items))
		}
		edits = append(edits, fixEdit{o.at(r.Pos), o.at(r.End), text})
	}
	return edits, nil
}

// ungroup returns e without its enclosing parentheses.
func ungroup(e grammar.Expr) grammar.Expr {
	for {
		g, ok := e.(*grammar.Group)
		if !ok {
			return e
		}
		e = g.Expr
	}
}

// valued reports whether a sequence element has a value, as positional captures count them.
func valued(e grammar.Expr) bool {
	switch e := ungroup(e).(type) {
	case *grammar.And, *grammar.Not, *grammar.Discard, *grammar.Cut, *grammar.Bottom,
		*grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine, *grammar.EndLine,
		*grammar.Predicate:
		return false
	case *grammar.Capture:
		return valued(e.Expr)
	case *grammar.Attributed:
		return valued(e.Expr)
	}
	return true
}

var sourceInfoType = reflect.TypeFor[grammar.SourceInfo]()

// inspectAST calls f for every pointer reachable from v, skipping source information.
func inspectAST(v any, f func(any)) {
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			f(v.Interface())
			walk(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			if v.Type() == sourceInfoType {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(v))
}
