package syntax

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
)

// fixParsed rewrites the parts of src that need the grammar parsed: positional captures,
// lambda parameters written $x and captures read as members x.y. If src does not parse, it is an error when positional captures
// need rewriting, and otherwise src is returned unchanged.
//
// The positional captures of rule, operand and inline actions become named captures. An element
// referred to by $n is labeled unless it already has a label; a rule call is labeled with the
// rule's name when that name is free, and anything else with v1, v2, .... $0 becomes
// list($a, $b, ...) of every element with a value.
func fixParsed(src string, positional bool) (string, error) {
	g, err := ParseLegacy(src)
	if err != nil {
		if !positional {
			return src, nil
		}
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
	params, err := paramEdits(o, g)
	if err != nil {
		return "", err
	}
	edits = append(edits, params...)
	inspectAST(g, func(n any) {
		m, ok := n.(*grammar.Member)
		if !ok {
			return
		}
		start := o.at(m.Diagnostic()) // the name
		switch {
		case captureMember(m.Name):
			edits = append(edits, fixEdit{start, start, "$"})
		case m.Name == "startPos":
			edits = append(edits, fixEdit{start, start + len(m.Name), "Start"})
		case m.Name == "endPos":
			edits = append(edits, fixEdit{start, start + len(m.Name), "End"})
		}
	})
	types, err := typeEdits(o, src, g)
	if err != nil {
		return "", err
	}
	edits = append(edits, types...)
	if len(edits) == 0 {
		return src, nil
	}
	return applyFixEdits(src, edits), nil
}

// valueTypeNames maps the reserved names of concrete syntax tree types to the value types that
// replace them.
var valueTypeNames = map[string]string{
	"terminal": "node[string]",
	"Match":    "node[string]",
	"Seq":      "node",
	"List":     "node",
	"Operator": "node",
	"Error":    "error",
}

// typeEdits returns the edits that replace terminal types and the reserved names of concrete
// syntax tree types with value types. A terminal type T becomes the struct { Text string }; a
// rule declared with it and without an action, which produced a terminal holding its matched
// text, builds the struct from that text instead; and T{s} becomes T{Text: s}.
func typeEdits(o *fixOffsets, src string, g *grammar.Grammar) ([]fixEdit, error) {
	defs := map[string]grammar.TypeSpec{}
	for _, td := range g.Types() {
		defs[td.Name] = td.Spec
	}
	// terminal returns the terminal type that name stands for, through aliases, or "".
	terminal := func(name string) string {
		for seen := map[string]bool{}; !seen[name]; {
			seen[name] = true
			switch spec := defs[name].(type) {
			case *grammar.TerminalSpec:
				return name
			case *grammar.AliasSpec:
				r, ok := spec.Type.(*grammar.TypeRef)
				if !ok {
					return ""
				}
				name = r.Name
			default:
				return ""
			}
		}
		return ""
	}
	var edits, last []fixEdit
	for _, td := range g.Types() {
		if spec, ok := td.Spec.(*grammar.TerminalSpec); ok {
			src := spec.Source()
			edits = append(edits, fixEdit{o.at(src.Pos), o.at(src.End), "= { Text string }"})
		}
	}
	for _, r := range grammar.TypeRefs(g) {
		if to, ok := valueTypeNames[r.Name]; ok && defs[r.Name] == nil && r.Pos.IsValid() {
			edits = append(edits, fixEdit{o.at(r.Pos), o.at(r.End), to})
		}
	}
	inspectAST(g, func(n any) {
		if c, ok := n.(*grammar.TerminalConstructor); ok && len(c.Args) == 1 {
			start := o.at(c.Args[0].(grammar.SourceNode).Source().Pos)
			edits = append(edits, fixEdit{start, start, "Text: "})
		}
	})
	// ruleTerminal returns the terminal type a rule is declared with, or "".
	ruleTerminal := map[string]string{}
	for _, s := range g.Statements {
		if rd, ok := s.(*grammar.RuleDef); ok {
			if ref, ok := rd.Type.(*grammar.TypeRef); ok {
				ruleTerminal[rd.Name] = terminal(ref.Name)
			}
		}
	}
	// A terminal of a rule held its matched text, which text and len read; the struct holds it
	// in Text. text($x) becomes $x.Text and len($x) becomes len($x.Text) where every capture x
	// in the rule is a call of a rule of a terminal type, or a choice of such calls.
	var terminalCall func(e grammar.Expr) bool
	terminalCall = func(e grammar.Expr) bool {
		switch e := ungroup(e).(type) {
		case *grammar.Ref:
			return e.Level == "" && ruleTerminal[e.Name] != ""
		case *grammar.Choice:
			for _, alt := range e.Alts {
				if !terminalCall(alt) {
					return false
				}
			}
			return true
		}
		return false
	}
	for _, s := range g.Statements {
		rd, ok := s.(*grammar.RuleDef)
		if !ok {
			continue
		}
		terminalCapture := map[string]bool{}
		inspectAST(rd, func(n any) {
			if c, ok := n.(*grammar.Capture); ok {
				ok, seen := terminalCapture[c.Name]
				terminalCapture[c.Name] = (ok || !seen) && terminalCall(c.Expr)
			}
		})
		inspectAST(rd, func(n any) {
			c, ok := n.(*grammar.Call)
			if !ok || (c.Func != "text" && c.Func != "len") || len(c.Args) != 1 {
				return
			}
			r, ok := c.Args[0].(*grammar.CaptureRef)
			if !ok || r.Param || !terminalCapture[r.Name] {
				return
			}
			if c.Func == "text" {
				src := c.Source()
				edits = append(edits, fixEdit{o.at(src.Pos), o.at(src.End), "$" + r.Name + ".Text"})
			} else {
				end := o.at(r.Source().End)
				edits = append(edits, fixEdit{end, end, ".Text"})
			}
		})
	}
	for _, s := range g.Statements {
		rd, ok := s.(*grammar.RuleDef)
		if !ok || rd.Action != nil {
			continue
		}
		ref, ok := rd.Type.(*grammar.TypeRef)
		if !ok {
			continue
		}
		t := terminal(ref.Name)
		if t == "" {
			continue
		}
		if _, ok := rd.Expr.(*grammar.OperatorsRule); ok {
			return nil, fmt.Errorf("%d:%d: rule %s of terminal type %s is an operators rule; give it an action before running pego fix", rd.Pos.Line, rd.Pos.Col, rd.Name, ref.Name)
		}
		taken := map[string]bool{}
		captures := false
		inspectAST(rd, func(n any) {
			switch n := n.(type) {
			case *grammar.Capture:
				taken[n.Name] = true
				captures = true
			case *grammar.Lambda:
				for _, p := range n.Params {
					taken[p] = true
				}
			}
		})
		name := "t"
		for i := 1; taken[name]; i++ {
			name = fmt.Sprintf("t%d", i)
		}
		body := rd.Expr.(grammar.SourceNode).Source()
		start, end := o.at(body.Pos), o.at(body.End)
		open, close := name+":(", ")"
		if _, ok := rd.Expr.(*grammar.Group); ok {
			open, close = name+":", ""
		}
		// The text of a match is its value only where nothing in it is captured: captures inside
		// #atomic have no effect.
		if !captures && !atomic(rd.Expr) {
			close += " #atomic"
		}
		action := fmt.Sprintf("%s{Text: text($%s)}", t, name)
		edits = append(edits, fixEdit{start, start, open})
		if rd.DefaultAction {
			// Replace the ... of "=> ...".
			at := strings.Index(src[end:], "...")
			if at < 0 {
				return nil, fmt.Errorf("%d:%d: cannot find the action of rule %s", rd.Pos.Line, rd.Pos.Col, rd.Name)
			}
			last = append(last, fixEdit{end, end, close}, fixEdit{end + at, end + at + 3, action})
		} else {
			last = append(last, fixEdit{end, end, close + " => " + action})
		}
	}
	return append(edits, last...), nil
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

// atomic reports whether e is an expression marked #atomic, in parentheses or not.
func atomic(e grammar.Expr) bool {
	_, ok := ungroup(e).(*grammar.Atomic)
	return ok
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

// paramEdits returns the edits that drop the $ of references to lambda parameters. In a lambda
// written with $x, a bare name x referred to a variable; it would refer to the parameter once
// rewritten, so it is an error.
func paramEdits(o *fixOffsets, g *grammar.Grammar) ([]fixEdit, error) {
	var edits []fixEdit
	var err error
	seen := map[*grammar.CaptureRef]bool{}
	inspectAST(g, func(n any) {
		l, ok := n.(*grammar.Lambda)
		if !ok || err != nil {
			return
		}
		for _, p := range l.Params {
			if p == "true" || p == "false" || p == "nil" {
				err = fmt.Errorf("%d:%d: the lambda parameter %s would be read as the literal %s; rename the parameter", l.Pos.Line, l.Pos.Col, p, p)
				return
			}
		}
		var old, bare *grammar.CaptureRef
		inspectAST(l.Body, func(n any) {
			r, ok := n.(*grammar.CaptureRef)
			if !ok || !slices.Contains(l.Params, r.Name) {
				return
			}
			if r.Param {
				bare = r
				return
			}
			old = r
			if !seen[r] {
				seen[r] = true
				start := o.at(r.Pos)
				edits = append(edits, fixEdit{start, start + 1, ""})
			}
		})
		if old != nil && bare != nil {
			err = fmt.Errorf("%d:%d: variable %s would refer to the lambda parameter %s; rename the parameter", bare.Pos.Line, bare.Pos.Col, bare.Name, bare.Name)
		}
	})
	return edits, err
}
