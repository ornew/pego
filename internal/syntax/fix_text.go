package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// fixTextStructs rewrites the structs { Text string } that hold the text a rule matches to named
// node types, which hold the text and the range of the match themselves:
//
//	type T = { Text string }
//	rule r T = t:(e) #atomic => T{Text: text($t)}
//
// becomes
//
//	type T = node[string]
//	rule r T = (e) #atomic
//
// A struct type is rewritten when its only field is Text of type string, at least one rule makes
// it in that form and no other constructor makes it; a struct that terminals[name] marks, which
// a terminal type became, is rewritten when no other constructor makes it, even if no rule does.
// The rule may be declared with T or an alias of it, or without a type, in which case it is
// declared with T. A rule in that form may also have its body or its #atomic expression in
// parentheses, as in t:(e #atomic). Reads x.Text of the values become text(x), and len(x.Text)
// becomes len(x): every read when no struct left has a field Text, and otherwise the reads of
// captures of calls of the rules of the rewritten types, or of choices of such calls.
//
// marks are offsets in src that are kept, shifted, in the result; no edit removes a mark. src that
// does not parse is returned unchanged.
func fixTextStructs(src string, marks []int, terminals map[string]bool) (string, []int) {
	if !strings.Contains(src, "Text") {
		return src, marks
	}
	g, err := Parse(src)
	if err != nil {
		return src, marks
	}
	o := newFixOffsets(src)
	defs := map[string]grammar.TypeDecl{}
	for _, td := range g.Types() {
		defs[td.DeclName()] = td
	}
	// resolve returns the type definition that name stands for, through aliases, or "".
	resolve := func(name string) string {
		for seen := map[string]bool{}; !seen[name]; {
			seen[name] = true
			switch d := defs[name].(type) {
			case *grammar.TypeAlias:
				r, ok := d.Type.(*grammar.TypeRef)
				if !ok {
					return ""
				}
				name = r.Name
			case *grammar.TypeDef:
				return name
			default:
				return ""
			}
		}
		return ""
	}
	// textStruct reports whether d is a struct whose only field is Text of type string.
	textStruct := func(d *grammar.TypeDef) bool {
		st := d.Struct()
		if st == nil || len(st.Fields) != 1 || st.Fields[0].Name != "Text" {
			return false
		}
		r, ok := st.Fields[0].Type.(*grammar.TypeRef)
		return ok && r.Name == "string" && defs["string"] == nil
	}
	// textRule returns the type a rule in the form above makes, and the atomic expression of its
	// body.
	textRule := func(rd *grammar.RuleDef) (string, *grammar.Atomic) {
		n, ok := rd.Action.(*grammar.New)
		if !ok || len(n.Fields) != 1 || n.Fields[0].Name != "Text" {
			return "", nil
		}
		call, ok := ungroupTerm(n.Fields[0].Value).(*grammar.Call)
		if !ok || call.Func != "text" || len(call.Args) != 1 {
			return "", nil
		}
		ref, ok := ungroupTerm(call.Args[0]).(*grammar.CaptureRef)
		if !ok || ref.Param {
			return "", nil
		}
		capture, ok := ungroup(rd.Expr).(*grammar.Capture)
		if !ok || capture.Name != ref.Name {
			return "", nil
		}
		a, ok := ungroup(capture.Expr).(*grammar.Atomic)
		if !ok {
			return "", nil
		}
		t := resolve(n.Type)
		if rd.Type != nil {
			r, ok := rd.Type.(*grammar.TypeRef)
			if !ok || resolve(r.Name) != t {
				return "", nil
			}
		}
		return t, a
	}
	// made holds the types made in the form above, and other the types that other constructors
	// make.
	made, other := map[string]bool{}, map[string]bool{}
	rules := map[*grammar.RuleDef]*grammar.Atomic{}
	for _, s := range g.File.Decls {
		if rd, ok := s.(*grammar.RuleDef); ok {
			if t, a := textRule(rd); a != nil {
				made[t] = true
				rules[rd] = a
			}
		}
	}
	inspectAST(g, func(n any) {
		c, ok := n.(*grammar.New)
		if !ok {
			return
		}
		for rd := range rules {
			if rd.Action == grammar.Term(c) {
				return
			}
		}
		other[resolve(c.Type)] = true
	})
	converted := map[string]bool{}
	var edits []fixEdit
	for _, td := range g.Types() {
		d, ok := td.(*grammar.TypeDef)
		if !ok || !textStruct(d) || other[d.Name] || !made[d.Name] && !terminals[d.Name] {
			continue
		}
		converted[d.Name] = true
		span := d.Type.(grammar.SourceNode).Source()
		start, end := o.at(span.Pos), o.at(span.End)
		text := "node[string]"
		if strings.HasPrefix(src[start:end], "=") { // the span of a definition starts at "="
			text = "= " + text
		}
		edits = append(edits, fixEdit{start, end, text})
	}
	if len(converted) == 0 {
		return src, marks
	}
	// The rules of the rewritten types: the rewritten rules, and the rules declared with them.
	ruleTypes := map[string]bool{}
	for _, s := range g.File.Decls {
		rd, ok := s.(*grammar.RuleDef)
		if !ok {
			continue
		}
		if r, ok := rd.Type.(*grammar.TypeRef); ok && converted[resolve(r.Name)] {
			ruleTypes[rd.Name] = true
		}
		a := rules[rd]
		if a == nil {
			continue
		}
		t := resolve(rd.Action.(*grammar.New).Type)
		if !converted[t] {
			continue
		}
		ruleTypes[rd.Name] = true
		body, atom := rd.Expr.(grammar.SourceNode).Source(), a.Source()
		start := o.at(body.Pos)
		// Drop the capture and the parentheses around the #atomic expression, and the action
		// with the spaces before it, keeping comments.
		arrow := arrowAfter(src, o.at(body.End))
		if arrow < 0 {
			continue
		}
		cut := arrow
		for cut > o.at(body.End) && (src[cut-1] == ' ' || src[cut-1] == '\t') {
			cut--
		}
		edits = append(edits,
			fixEdit{start, o.at(atom.Pos), ""},
			fixEdit{o.at(atom.End), o.at(body.End), ""},
			fixEdit{cut, o.at(rd.Action.(grammar.SourceNode).Source().End), ""})
		if rd.Type == nil {
			// Declare the rule with the type, before the "=" of the body.
			eq := strings.LastIndex(src[:start], "=")
			text := t + " "
			if r, _ := utf8.DecodeLastRuneInString(src[:eq]); !unicode.IsSpace(r) {
				text = " " + text
			}
			edits = append(edits, fixEdit{eq, eq, text})
		}
	}
	// Reads x.Text of the values become text(x).
	textField := false
	for _, td := range g.Types() {
		if d, ok := td.(*grammar.TypeDef); ok && !converted[d.Name] {
			for _, f := range d.Fields() {
				textField = textField || f.Name == "Text"
			}
		}
	}
	var calls func(e grammar.Expr) bool
	calls = func(e grammar.Expr) bool {
		switch e := ungroup(e).(type) {
		case *grammar.Ref:
			return e.Level == "" && ruleTypes[e.Name]
		case *grammar.Choice:
			for _, alt := range e.Alts {
				if !calls(alt) {
					return false
				}
			}
			return true
		}
		return false
	}
	for _, s := range g.File.Decls {
		rd, ok := s.(*grammar.RuleDef)
		if !ok {
			continue
		}
		textCapture := map[string]bool{}
		inspectAST(rd, func(n any) {
			if c, ok := n.(*grammar.Capture); ok {
				ok, seen := textCapture[c.Name]
				textCapture[c.Name] = (ok || !seen) && calls(c.Expr)
			}
		})
		// len(x.Text) becomes len(x), the length of the text of a string node.
		lenArg := map[*grammar.Member]bool{}
		inspectAST(rd, func(n any) {
			if c, ok := n.(*grammar.Call); ok && c.Func == "len" && len(c.Args) == 1 {
				if m, ok := c.Args[0].(*grammar.Member); ok {
					lenArg[m] = true
				}
			}
		})
		inspectAST(rd, func(n any) {
			m, ok := n.(*grammar.Member)
			if !ok || m.Name != "Text" {
				return
			}
			if textField {
				r, ok := ungroupTerm(m.X).(*grammar.CaptureRef)
				if !ok || r.Param || !textCapture[r.Name] {
					return
				}
			}
			x, all := m.X.(grammar.SourceNode).Source(), m.Source()
			if lenArg[m] {
				edits = append(edits, fixEdit{o.at(x.End), o.at(all.End), ""})
				return
			}
			// x.Text becomes text(x): insert "text(" and replace ".Text" with ")".
			edits = append(edits, fixEdit{o.at(x.Pos), o.at(x.Pos), "text("}, fixEdit{o.at(x.End), o.at(all.End), ")"})
		})
	}
	out, _ := applyMarkedFixEdits(src, edits, nil)
	return out, shiftMarks(marks, edits)
}

// arrowAfter returns the offset of the first "=>" in src from offset i that is not in a comment,
// or -1.
func arrowAfter(src string, i int) int {
	for i < len(src) {
		switch {
		case strings.HasPrefix(src[i:], "=>"):
			return i
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				return -1
			}
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return -1
			}
			i += end + 4
		default:
			i++
		}
	}
	return -1
}

// shiftMarks returns the offsets marks moved by edits, which are sorted and none of which
// covers a mark.
func shiftMarks(marks []int, edits []fixEdit) []int {
	out := make([]int, len(marks))
	for i, m := range marks {
		out[i] = m
		for _, e := range edits {
			if e.end <= m {
				out[i] += len(e.text) - (e.end - e.start)
			}
		}
	}
	return out
}

// ungroupTerm returns t without its enclosing parentheses.
func ungroupTerm(t grammar.Term) grammar.Term {
	for {
		g, ok := t.(*grammar.TermGroup)
		if !ok {
			return t
		}
		t = g.Term
	}
}
