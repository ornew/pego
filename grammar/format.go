package grammar

import (
	"fmt"
	"strconv"
	"strings"
)

// indentUnit is one level of indentation.
const indentUnit = "    "

// formatGrammar shares one control across the complete printing operation.
func formatGrammar(g *Grammar, control *metadataControl) string {
	pollFormat(control)
	p := &printer{start: true, headers: map[Pos][]*Comment{}, control: control}
	for _, comment := range g.File.HeaderComments {
		pollFormat(control)
		if comment != nil {
			p.headers[comment.Header] = append(p.headers[comment.Header], comment)
		}
	}
	if g.File.Pkg.Name != "" {
		p.lineBreak(g.File.Pkg.Break, "", false)
		p.header("package", g.File.Pkg.Pos)
		p.write(" " + g.File.Pkg.Name)
	}
	for _, s := range g.File.Decls {
		pollFormat(control)
		switch s := s.(type) {
		case *TypeDef:
			p.lineBreak(s.Break, "", true)
			p.typeDef(s)
		case *TypeAlias:
			p.lineBreak(s.Break, "", true)
			p.typeAlias(s)
		case *VarDef:
			p.lineBreak(s.Break, "", true)
			p.varDef(s)
		case *RuleDef:
			p.lineBreak(s.Break, "", true)
			p.ruleDef(s)
		}
	}
	p.comments(g.File.EndBreak, "", true)
	p.flush()
	return p.String()
}

// line is an output line.
type line struct {
	indent string
	// text is the code, or the comment of a comment line.
	text string
	// name is the field name of a struct field line; text is then its type.
	// Field types on consecutive field lines are aligned.
	name  string
	field bool
	// comment is a trailing comment. Trailing comments on consecutive lines
	// with the same indentation are aligned.
	comment string
	blank   bool
}

// printer collects output lines.
type printer struct {
	lines []*line
	cur   *line
	// start is true at the start of the file and of a block, where blank
	// lines are dropped.
	start bool
	// keepStart keeps a blank line at the start of the block. The blank line
	// before the first comment of a list decides what the comment belongs to.
	keepStart bool
	// trailing are the trailing comments of the current line.
	trailing []*Comment
	headers  map[Pos][]*Comment
	control  *metadataControl
}

func (p *printer) write(s string) {
	pollFormat(p.control)
	if p.cur == nil {
		p.newLine("")
	}
	p.cur.text = joinMinusTokens(p.cur.text, s)
}

// joinMinusTokens keeps adjacent minus operators from becoming the reserved
// token "--". It is also used when a complete expression is formatted.
func joinMinusTokens(left, right string) string {
	if strings.HasSuffix(left, "-") && strings.HasPrefix(right, "-") {
		return left + " " + right
	}
	return left + right
}

// flush writes the trailing comments of the current line.
func (p *printer) flush() {
	for _, c := range p.trailing {
		pollFormat(p.control)
		if p.cur != nil && p.cur.comment == "" {
			p.cur.comment = c.Text
			continue
		}
		indent := ""
		if p.cur != nil {
			indent = p.cur.indent
		}
		p.cur = &line{indent: indent, text: c.Text}
		p.lines = append(p.lines, p.cur)
	}
	p.trailing = nil
}

// inline records the trailing comment of b, for a node that b precedes but
// that is written on the current line.
func (p *printer) inline(b *LineBreak) {
	if b != nil && b.Trailing != nil && !b.Trailing.Header.IsValid() {
		p.trailing = append(p.trailing, b.Trailing)
	}
}

func (p *printer) newLine(indent string) {
	p.flush()
	p.cur = &line{indent: indent}
	p.lines = append(p.lines, p.cur)
	p.start = false
}

func (p *printer) blankLine() {
	if p.start && !p.keepStart || len(p.lines) == 0 || p.lines[len(p.lines)-1].blank {
		return
	}
	p.lines = append(p.lines, &line{blank: true})
}

func (p *printer) commentLine(indent string, c *Comment) {
	p.flush()
	if c.Blank {
		p.blankLine()
	}
	p.lines = append(p.lines, &line{indent: indent, text: c.Text})
	p.cur = nil
	p.start = false
}

// comments writes the comments of b on their own lines. It reports whether
// a blank line should precede the next node, which is never the case
// before a closing brace or the end of the file.
func (p *printer) comments(b *LineBreak, indent string, closing bool) bool {
	if b == nil {
		return false
	}
	for _, c := range b.Comments {
		pollFormat(p.control)
		if c.Header.IsValid() {
			continue
		}
		p.commentLine(indent, c)
	}
	return b.Blank && !closing
}

// lineBreak starts a new line for the node that b precedes. Without b, a
// blank line precedes the node if defaultBlank is set.
func (p *printer) lineBreak(b *LineBreak, indent string, defaultBlank bool) {
	blank := defaultBlank
	if b != nil {
		blank = p.comments(b, indent, false)
	}
	if blank {
		p.flush()
		p.blankLine()
	}
	p.newLine(indent)
	p.inline(b)
}

// inlineBlock returns the trailing comment of b if it is a block comment that
// stays between the tokens of a line.
func (b *LineBreak) inlineBlock() *Comment {
	if b == nil || b.Trailing == nil || b.TrailingEnds || b.Trailing.Header.IsValid() {
		return nil
	}
	if c := b.Trailing; strings.HasPrefix(c.Text, "/*") && !strings.ContainsAny(c.Text, "\r\n") {
		return c
	}
	return nil
}

// inlineLead returns the block comments in b, each followed by a space, when b
// holds nothing else: the comments between an opening delimiter and the token
// after it on one line. They lead what follows, so they are written before it.
//
// next is where the token after them begins, or the zero Pos when none follows
// on the line: a comment that is not on that line before it is some other
// comment that Format moved into b.
func inlineLead(b *LineBreak, next Pos) (string, bool) {
	if b == nil {
		return "", true
	}
	if b.Blank {
		return "", false
	}
	var s strings.Builder
	for _, c := range b.Comments {
		if c.Header.IsValid() || !strings.HasPrefix(c.Text, "/*") || strings.ContainsAny(c.Text, "\r\n") {
			return "", false
		}
		if next.IsValid() && (c.End.Line != next.Line || c.End.Col > next.Col) {
			return "", false
		}
		s.WriteString(c.Text + " ")
	}
	return s.String(), true
}

// exprPos returns where e begins in the source, if known.
func exprPos(e Expr) Pos {
	if n, ok := e.(SourceNode); ok {
		return n.Source().Pos
	}
	return Pos{}
}

// termPos returns where t begins in the source, if known.
func termPos(t Term) Pos {
	if n, ok := t.(SourceNode); ok {
		return n.Source().Pos
	}
	return Pos{}
}

// noComments reports whether b holds no comments before its node and no
// blank line, so that the node can stay on the current line.
func noComments(b *LineBreak) bool {
	return b == nil || len(b.Comments) == 0 && !b.Blank
}

func (p *printer) openBlock() {
	p.write("{")
	p.start = true
}

// closeBlock writes the comments before a closing brace and the brace.
func (p *printer) closeBlock(b *LineBreak, indent string) {
	p.comments(b, indent+indentUnit, true)
	p.newLine(indent)
	p.write("}")
	p.inline(b)
}

func (p *printer) typeDef(t *TypeDef) {
	pollFormat(p.control)
	p.header("type", t.Pos)
	p.write(" " + t.Name + " ")
	p.write("= ")
	if t.Type != nil && t.Struct() == nil {
		p.write(formatType(t.Type, p.control))
		return
	}
	st := t.Struct()
	if st == nil {
		st = &StructType{OneLine: true}
	}
	p.structBody(st.Fields, st.CloseBreak, st.OneLine, "")
}

func (p *printer) typeAlias(t *TypeAlias) {
	pollFormat(p.control)
	p.header("type", t.Pos)
	p.write(" " + t.Name + " ~ " + formatType(t.Type, p.control))
}

// structBody writes the braces and fields of a struct type.
func (p *printer) structBody(fields []*Field, closeBreak *LineBreak, oneLine bool, indent string) {
	// On one line, a block comment after the opening brace leads the first
	// field, or precedes the closing brace of an empty struct.
	lead := ""
	var empty bool
	switch {
	case oneLine && len(fields) > 0:
		lead, empty = inlineLead(fields[0].Break, fields[0].Pos)
		empty = empty && noComments(closeBreak)
		for _, f := range fields[1:] {
			pollFormat(p.control)
			empty = empty && noComments(f.Break)
		}
	case oneLine:
		lead, empty = inlineLead(closeBreak, Pos{})
	default:
		empty = noComments(closeBreak)
		for _, f := range fields {
			pollFormat(p.control)
			empty = empty && noComments(f.Break)
		}
	}
	if empty && (len(fields) == 0 || oneLine) {
		parts := make([]string, len(fields))
		for i, f := range fields {
			pollFormat(p.control)
			parts[i] = f.Name + " " + formatType(f.Type, p.control)
			if hasFormatNewline(parts[i], p.control) {
				// A field type over several lines breaks the struct too.
				p.structBody(fields, closeBreak, false, indent)
				return
			}
		}
		// A block comment between two tokens of the line stays where it is: it
		// follows the separator of its field, and the last one precedes the brace.
		for i, f := range fields {
			pollFormat(p.control)
			if c := f.Break.inlineBlock(); c != nil {
				parts[i] += " " + c.Text
			} else {
				p.inline(f.Break)
			}
			if i+1 < len(fields) {
				parts[i] += ";"
			}
		}
		p.inline(closeBreak)
		if len(parts) == 0 {
			p.write("{" + strings.TrimSpace(lead) + "}")
		} else {
			p.write("{ " + lead + joinFormatParts(parts, " ", p.control) + " }")
		}
		return
	}
	p.openBlock()
	for _, f := range fields {
		pollFormat(p.control)
		p.lineBreak(f.Break, indent+indentUnit, false)
		p.cur.field = true
		p.cur.name = f.Name
		// A type over several lines continues at the field's indentation.
		p.write(indentTypeLines(formatType(f.Type, p.control), indent+indentUnit, p.control))
	}
	p.closeBlock(closeBreak, indent)
}

func (p *printer) varDef(v *VarDef) {
	pollFormat(p.control)
	p.header("var", v.Pos)
	p.write(" " + v.Name)
	if v.Type != nil {
		p.write(" " + formatType(v.Type, p.control))
	}
	if v.Value != nil {
		p.write(joinLayout(" = ", formatTerm(v.Value, 0, p.control)))
	}
}

func (p *printer) ruleDef(r *RuleDef) {
	pollFormat(p.control)
	p.header("rule", r.Pos)
	p.write(" " + r.Name)
	if r.Type != nil {
		p.write(" " + formatType(r.Type, p.control))
	}
	p.write(" = ")
	if pr, ok := r.Expr.(*OperatorsRule); ok {
		p.write("operators ")
		p.operatorsRule(pr, "")
	} else {
		if r.BodyBreak != nil || r.ActionBreak != nil || expressionLayoutControlled(r.Expr, p.control) {
			p.write("(")
			p.lineBreak(r.BodyBreak, indentUnit, false)
			p.body(r.Expr, indentUnit)
			p.comments(r.ActionBreak, indentUnit, false)
			p.newLine("")
			p.write(")")
			p.inline(r.ActionBreak)
		} else {
			p.body(r.Expr, "")
		}
	}
	if r.Action != nil || r.DefaultAction {
		if r.DefaultAction {
			p.write(" => ...")
		} else {
			p.write(joinLayout(" => ", formatTerm(r.Action, 0, p.control)))
		}
	}
	p.comments(r.CloseBreak, indentUnit, true)
	p.inline(r.CloseBreak)
}

func expressionLayoutControlled(e Expr, control *metadataControl) bool {
	pollFormat(control)
	switch e := e.(type) {
	case *Seq:
		for _, b := range e.Breaks {
			pollFormat(control)
			if b != nil {
				return true
			}
		}
		for _, it := range e.Items {
			pollFormat(control)
			if expressionLayoutControlled(it, control) {
				return true
			}
		}
	case *Choice:
		for _, b := range e.Breaks {
			pollFormat(control)
			if b != nil {
				return true
			}
		}
		for _, alt := range e.Alts {
			pollFormat(control)
			if expressionLayoutControlled(alt, control) {
				return true
			}
		}
	case *Attributed:
		for _, b := range e.Breaks {
			pollFormat(control)
			if b != nil {
				return true
			}
		}
	case *Atomic:
		return e.Break != nil
	case *Repeat:
		return e.PragmaBreak != nil
	case *Recovery:
		return e.Break != nil
	case *Stream:
		return e.Break != nil
	case *Capture:
		return expressionLayoutControlled(e.Expr, control)
	case *And:
		return expressionLayoutControlled(e.Expr, control)
	case *Not:
		return expressionLayoutControlled(e.Expr, control)
	case *Discard:
		return expressionLayoutControlled(e.Expr, control)
	}
	return false
}

func undecorate(e Expr, attrs []*Attribute, control *metadataControl) (Expr, bool) {
	pollFormat(control)
	for _, a := range attrs {
		pollFormat(control)
		if a.Name == "atomic" {
			x, ok := e.(*Atomic)
			if !ok {
				return nil, false
			}
			e = x.Expr
			continue
		}
		x, ok := e.(*Attributed)
		if !ok || len(x.Attrs) == 0 || x.Attrs[len(x.Attrs)-1] != a {
			return nil, false
		}
		if len(x.Attrs) == 1 {
			e = x.Expr
		} else {
			copy := *x
			copy.Attrs = x.Attrs[:len(x.Attrs)-1]
			if len(copy.Breaks) > len(copy.Attrs) {
				copy.Breaks = copy.Breaks[:len(copy.Attrs)]
			}
			e = &copy
		}
	}
	return e, true
}

// body writes the expression of a rule body, keeping the line breaks
// recorded in its top-level choice and sequences.
func (p *printer) body(e Expr, indent string) {
	pollFormat(p.control)
	c, ok := e.(*Choice)
	if !ok {
		p.sequence(e, indent, precChoice)
		return
	}
	for i, a := range c.Alts {
		pollFormat(p.control)
		if i == 0 {
			p.sequence(a, indent, precSeq)
			continue
		}
		if b := breakAt(c.Breaks, i); b != nil {
			p.lineBreak(b, indent, false)
		} else {
			p.write(" ")
		}
		p.write("/ ")
		// Continuation lines of an alternative line up after "/ ".
		p.sequence(a, indent+"  ", precSeq)
	}
}

func (p *printer) sequence(e Expr, indent string, outer int) {
	pollFormat(p.control)
	switch e := e.(type) {
	case *Choice:
		p.write(formatExpr(e, outer, p.control))
	case *Seq:
		for i, it := range e.Items {
			pollFormat(p.control)
			if i > 0 {
				if b := breakAt(e.Breaks, i); b != nil {
					p.lineBreak(b, indent, false)
				} else {
					p.write(" ")
				}
			}
			p.item(it, indent)
		}
	default:
		p.item(e, indent)
	}
}

// item writes an item of a top-level sequence of a rule body, keeping the
// line breaks before its attributes.
func (p *printer) item(e Expr, indent string) {
	pollFormat(p.control)
	switch e := e.(type) {
	case *Group:
		p.group(e, indent)
	case *Capture:
		p.write(e.Name + ":")
		p.item(e.Expr, indent)
	case *And:
		p.write("&")
		p.item(e.Expr, indent)
	case *Not:
		p.write("!")
		p.item(e.Expr, indent)
	case *Atomic:
		p.write(formatExpr(e.Expr, precSuffix, p.control))
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write("#atomic")
	case *Stream:
		p.write(formatExpr(e.Expr, precSuffix, p.control))
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write("#stream")
	case *Recovery:
		p.write(formatExpr(e.Expr, precSuffix, p.control))
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write("#recover(skip=" + formatExpr(e.Skip, precChoice, p.control) + ")")
	case *Repeat:
		if e.PragmaBreak == nil {
			p.write(indentTermLines(formatExpr(e, precPrefix, p.control), indent, p.control))
			break
		}
		p.write(formatExpr(e.Expr, precSuffix, p.control))
		p.lineBreak(e.PragmaBreak, indent, false)
		p.write(indentTermLines(formatRepeatPragma(e, p.control), indent, p.control))
	case *Discard:
		p.write("-")
		p.item(e.Expr, indent)
	case *Attributed:
		p.write(formatExpr(e.Expr, precSuffix, p.control))
		for i, a := range e.Attrs {
			pollFormat(p.control)
			if b := breakAt(e.Breaks, i); b != nil {
				p.lineBreak(b, indent, false)
			} else {
				p.write(" ")
			}
			// The lines of an attribute with a trailing comma continue at the indentation of its line.
			p.write(indentTermLines(formatAttribute(a, p.control), indent, p.control))
		}
	default:
		p.write(formatExpr(e, precPrefix, p.control))
	}
}

func (p *printer) group(g *Group, indent string) {
	pollFormat(p.control)
	lead, plain := "", false
	if g.OneLine && (g.BodyBreak == nil || g.BodyBreak.Trailing == nil) {
		lead, plain = inlineLead(g.BodyBreak, exprPos(g.Expr))
	}
	if plain && noComments(g.CloseBreak) && p.tryOneLine(func() {
		p.write("(" + lead)
		p.groupBody(g.Expr, indent)
		p.write(")")
		p.inline(g.CloseBreak)
	}) {
		return
	}
	p.write("(")
	p.lineBreak(g.BodyBreak, indent+indentUnit, false)
	p.groupBody(g.Expr, indent+indentUnit)
	p.comments(g.CloseBreak, indent+indentUnit, false)
	p.newLine(indent)
	p.write(")")
	p.inline(g.CloseBreak)
}

// tryOneLine runs write and reports whether it stayed on one line. A text that
// holds a line break, such as a list with a trailing comma, is rolled back, so
// that the caller writes its lines instead; the next pass would read such a
// layout anyway.
func (p *printer) tryOneLine(write func()) bool {
	if p.cur == nil {
		p.newLine("")
	}
	cur, text, lines, trailing := p.cur, p.cur.text, len(p.lines), len(p.trailing)
	write()
	if len(p.lines) == lines && p.cur == cur && !hasFormatNewline(p.cur.text[len(text):], p.control) {
		return true
	}
	p.lines, p.cur, p.trailing = p.lines[:lines], cur, p.trailing[:trailing]
	cur.text = text
	return false
}

func (p *printer) groupBody(e Expr, indent string) {
	pollFormat(p.control)
	if action, ok := e.(*InlineAction); ok {
		p.body(action.Expr, indent)
		if action.DefaultAction {
			p.write(" => ...")
		} else {
			p.write(joinLayout(" => ", indentTermLines(formatTerm(action.Action, 0, p.control), indent, p.control)))
		}
		return
	}
	p.body(e, indent)
}

func breakAt(bs []*LineBreak, i int) *LineBreak {
	if i < len(bs) {
		return bs[i]
	}
	return nil
}

func (p *printer) operatorsRule(pr *OperatorsRule, indent string) {
	pollFormat(p.control)
	inner := indent + indentUnit
	p.openBlock()
	if pr.Skip != nil {
		p.lineBreak(pr.SkipBreak, inner, false)
		p.write("skip " + formatExpr(pr.Skip, precChoice, p.control))
	}
	for _, o := range pr.Operands {
		pollFormat(p.control)
		p.lineBreak(o.Break, inner, false)
		p.write("operand " + formatExpr(o.Expr, precChoice, p.control))
		if o.Action != nil {
			p.write(joinLayout(" => ", indentTermLines(formatTerm(o.Action, 0, p.control), inner, p.control)))
		} else if o.DefaultAction {
			p.write(" => ...")
		}
	}
	for _, l := range pr.Levels {
		pollFormat(p.control)
		p.lineBreak(l.Break, inner, false)
		p.header("level", l.Pos)
		p.write(" ")
		if l.Name != "" {
			p.write(l.Name + " ")
		}
		empty := noComments(l.CloseBreak)
		for _, op := range l.Operators {
			pollFormat(p.control)
			empty = empty && noComments(op.Break)
		}
		switch {
		case empty && (len(l.Operators) == 0 || l.OneLine) && p.tryOneLine(func() {
			p.write("{")
			for i, op := range l.Operators {
				pollFormat(p.control)
				if i > 0 {
					p.write(";")
				}
				p.write(" ")
				p.operator(op, inner)
				p.inline(op.Break)
			}
			if len(l.Operators) > 0 {
				p.write(" ")
			}
			p.write("}")
			p.inline(l.CloseBreak)
		}):
		default:
			p.openBlock()
			for _, op := range l.Operators {
				pollFormat(p.control)
				p.lineBreak(op.Break, inner+indentUnit, false)
				p.operator(op, inner+indentUnit)
			}
			p.closeBlock(l.CloseBreak, inner)
		}
	}
	p.closeBlock(pr.CloseBreak, indent)
}

func formatOperator(op *OperatorsRuleOperator, control *metadataControl) string {
	pollFormat(control)
	s := op.Kind + " "
	if op.Assoc != "" {
		s += op.Assoc + " "
	}
	s += formatExpr(op.Expr, precChoice, control)
	if op.Action != nil {
		s += joinLayout(" => ", formatTerm(op.Action, 0, control))
	} else if op.DefaultAction {
		s += " => ..."
	}
	return s
}

// String aligns the lines and joins them.
func (p *printer) String() string {
	ls := p.lines
	// Align the types of consecutive struct fields.
	for i := 0; i < len(ls); {
		pollFormat(p.control)
		j, w := i, 0
		for j < len(ls) && ls[j].field && ls[j].indent == ls[i].indent {
			pollFormat(p.control)
			w = max(w, formatRuneCount(ls[j].name, p.control))
			j++
		}
		for _, l := range ls[i:j] {
			pollFormat(p.control)
			l.text = pad(l.name, w, p.control) + " " + l.text
		}
		i = max(j, i+1)
	}
	// Align the trailing comments of consecutive lines.
	for i := 0; i < len(ls); {
		pollFormat(p.control)
		j, w := i, 0
		for j < len(ls) && ls[j].comment != "" && ls[j].indent == ls[i].indent {
			pollFormat(p.control)
			w = max(w, formatRuneCount(ls[j].text, p.control))
			j++
		}
		for _, l := range ls[i:j] {
			pollFormat(p.control)
			l.text = pad(l.text, w, p.control) + " " + l.comment
		}
		i = max(j, i+1)
	}
	var b strings.Builder
	for _, l := range ls {
		pollFormat(p.control)
		if !l.blank {
			writeFormatString(&b, trimFormatRight(l.indent+l.text, " ", p.control), p.control)
		}
		writeFormatString(&b, "\n", p.control)
	}
	return b.String()
}

func pad(s string, w int, control *metadataControl) string {
	pollFormat(control)
	return s + strings.Repeat(" ", w-formatRuneCount(s, control))
}

// FormatType returns t in PEGO syntax.
func formatType(t TypeExpr, control *metadataControl) string {
	pollFormat(control)
	switch t := t.(type) {
	case *TypeGroup:
		body := formatType(t.Type, control)
		// A body that the formatter breaks into lines, such as a struct type with a comment, breaks
		// the group too.
		return finishFormat(formatTypeGroup(body, t.OpenBreak, t.BodyBreak, t.CloseBreak, t.OneLine && !hasFormatNewline(body, control), control), control)
	case *TypeRef:
		return finishFormat(t.Name, control)
	case *ListType:
		return finishFormat("[]"+formatTypeTerm(t.Elem, control), control)
	case *OptionalType:
		return finishFormat("*"+formatTypeTerm(t.Elem, control), control)
	case *JoinType:
		parts := make([]string, len(t.Types))
		for i, x := range t.Types {
			pollFormat(control)
			if _, ok := x.(*MeetType); ok {
				parts[i] = formatType(x, control) // & binds tighter than |
			} else {
				parts[i] = formatTypeTerm(x, control)
			}
		}
		return finishFormat(joinFormatParts(parts, " | ", control), control)
	case *MeetType:
		parts := make([]string, len(t.Types))
		for i, x := range t.Types {
			pollFormat(control)
			parts[i] = formatType(x, control)
			if _, ok := x.(*JoinType); ok {
				parts[i] = "(" + parts[i] + ")"
			}
		}
		return finishFormat(joinFormatParts(parts, " & ", control), control)
	case *TupleType:
		elems := make([]listElem, len(t.Types))
		for i, x := range t.Types {
			pollFormat(control)
			elems[i] = layoutElem(nodeLayout(x), formatType(x, control), control)
		}
		// The comma of a one-element tuple is required: (T) would be a group.
		return finishFormat(formatTypeList(elems, len(elems) == 1, t.TrailingComma && len(elems) > 1, "(", ")", t.OpenBreak, t.BodyBreak, t.CloseBreak, control), control)
	case *StructType:
		p := &printer{control: control}
		// Without source layout, as from JSON, the type stays on one line. A comment after a field
		// has no place on that line inside an expression, so it takes the block layout.
		oneLine := t.OneLine || t.CloseBreak == nil
		for _, f := range t.Fields {
			pollFormat(control)
			oneLine = oneLine && (f.Break == nil || f.Break.Trailing == nil)
		}
		oneLine = oneLine && (t.CloseBreak == nil || t.CloseBreak.Trailing == nil)
		p.structBody(t.Fields, t.CloseBreak, oneLine, "")
		p.flush()
		return finishFormat(strings.TrimSuffix(p.String(), "\n"), control)
	case *TypeApp:
		elems := make([]listElem, len(t.Args))
		for i, x := range t.Args {
			pollFormat(control)
			elems[i] = layoutElem(nodeLayout(x), formatType(x, control), control)
		}
		return finishFormat(t.Name+formatTypeList(elems, false, t.TrailingComma, "[", "]", t.OpenBreak, t.BodyBreak, t.CloseBreak, control), control)
	}
	return finishFormat("?", control)
}

func formatTypeTerm(t TypeExpr, control *metadataControl) string {
	pollFormat(control)
	switch t.(type) {
	case *JoinType, *MeetType:
		return "(" + formatType(t, control) + ")"
	}
	return formatType(t, control)
}

// formatSourceGroup keeps explicit delimiters and their boundary trivia.
func formatSourceGroup(body string, open, first, close *LineBreak, oneLine bool, control *metadataControl) string {
	return formatSourceDelimited(body, "(", ")", open, first, close, oneLine, control)
}

// formatSourceDelimited is formatSourceGroup for any pair of delimiters.
func formatSourceDelimited(body, openDelim, closeDelim string, open, first, close *LineBreak, oneLine bool, control *metadataControl) string {
	return formatSourceGroupIndented(body, openDelim, closeDelim, open, first, close, oneLine, func(s, indent string) string {
		return indentTermLines(s, indent, control)
	}, control)
}

// parenTerm parenthesizes a formatted term that needs it. A term over several
// lines gets the layout of a parenthesized term read from source, so that the
// next pass keeps it.
func parenTerm(s string, control *metadataControl) string {
	return formatSourceGroup(s, nil, nil, nil, !hasFormatNewline(s, control), control)
}

// listElem is an element of a delimited list: its text with the comments
// before it, and the comments between it and the comma that follows.
type listElem struct {
	head      string
	after     []*Comment
	elemLine  int        // the line the element ends on
	afterLine int        // the line of the comma that follows the element
	trail     []*Comment // comments on the line of the comma after the element, which end that line
}

// endsLine reports whether c ends its line in the source: a line comment, or a
// block comment before a token on a later line.
func endsLine(c *Comment, nextLine int) bool {
	return strings.HasPrefix(c.Text, "//") || nextLine > 0 && c.End.Line < nextLine
}

// formatSourceList formats the elements of a comma-separated list in
// brackets. A list with a trailing comma, or with a comment that needs its own
// line, puts one element on each line and ends each with a comma; any other
// list stays on one line. lines reports a trailing comma that counts; comma
// adds the comma that a one-element tuple needs on one line.
//
// A comment that needs its own line is a line comment, a comment that ends its
// line, a comment on a line of its own, and a block comment over several
// lines. A block comment between two tokens of one line stays there: its
// owner depends on that, so it keeps the list on one line.
//
// Where a comment is written decides which node owns it, so the comments
// around a comma keep their line (see writeListComma). A comment that ends the
// line of an element trails the element, and goes after its comma.
func formatSourceList(elems []listElem, comma, lines bool, openDelim, closeDelim string, open, first, close *LineBreak, indentLines func(s, indent string) string, control *metadataControl) string {
	pollFormat(control)
	multi := lines || !inlineGroupComments(open, first, close, control)
	for _, e := range elems {
		pollFormat(control)
		if hasFormatNewline(e.head, control) {
			multi = true
		}
		for _, c := range e.after {
			if c != nil && (endsLine(c, e.afterLine) || hasFormatNewline(c.Text, control)) {
				multi = true
			}
		}
		if len(e.trail) != 0 {
			multi = true
		}
	}
	var b strings.Builder
	if !multi {
		for i, e := range elems {
			pollFormat(control)
			head := e.head
			if i > 0 {
				if strings.HasPrefix(e.head, " ") {
					b.WriteString(",") // the element starts with a comment on the line of the comma
				} else {
					b.WriteString(", ")
				}
			} else {
				head = strings.TrimPrefix(head, " ") // no space after the opening delimiter
			}
			writeFormatString(&b, head, control)
			writeLayoutAfter(&b, e.after, "", e.afterLine, control)
		}
		if comma {
			b.WriteString(",")
		}
		if listHasComments(open, first, close) {
			return formatSourceGroupIndented(b.String(), openDelim, closeDelim, open, first, close, true, indentLines, control)
		}
		return openDelim + b.String() + closeDelim
	}
	if open != nil && open.Trailing != nil && !open.Trailing.Header.IsValid() && strings.HasPrefix(open.Trailing.Text, "/*") && !open.TrailingEnds && len(elems) > 0 {
		// A block comment on the line of the opening delimiter belongs to the first
		// element, not to the delimiter. Written after the delimiter it would have no
		// owner, so it takes a line of its own before the element.
		comment := open.Trailing
		moved := *open
		moved.Trailing = nil
		open = &moved
		lead := LineBreak{}
		if first != nil {
			lead = *first
		}
		lead.Comments = append([]*Comment{comment}, lead.Comments...)
		first = &lead
	}
	alone := false // the comma before the element starts its line
	for i, e := range elems {
		pollFormat(control)
		head := strings.TrimLeft(e.head, " \n")
		if i > 0 {
			if alone && strings.HasPrefix(e.head, " ") {
				// The comments after a comma that starts its line stay on its line,
				// where they belong to the element before.
				head = e.head
			} else {
				b.WriteByte('\n')
				if strings.HasPrefix(e.head, "\n\n") {
					// A blank line before the comments of an element keeps them from
					// trailing the element before.
					b.WriteByte('\n')
				}
			}
		}
		writeFormatString(&b, head, control)
		// The comments on the comma line after the comma belong to the element
		// before it. Comments that already follow the element in the source
		// could not tell them apart from these, so then the comma stays on the
		// line after them.
		followed := len(e.trail) != 0 || i+1 < len(elems) && strings.HasPrefix(elems[i+1].head, " ")
		if i+1 == len(elems) {
			// The comments before the closing delimiter: the one after the comma
			// trails the element.
			followed = first != nil && first.Trailing != nil && !first.Trailing.Header.IsValid() ||
				close != nil && len(close.Comments) != 0
		}
		alone = writeListComma(&b, e, followed, control)
		for _, c := range e.trail {
			b.WriteByte(' ')
			writeFormatString(&b, c.Text, control)
		}
	}
	return formatSourceGroupIndented(b.String(), openDelim, closeDelim, open, first, close, false, indentLines, control)
}

// writeListComma writes the comma after a list element and the comments
// between the two. Where a comment is written decides what it belongs to, so
// the comments keep the line they were on: one on the line of the element that
// ends its line trails the element, and goes after the comma when it is alone;
// a comment on a later line belongs to nothing, and stays between the element
// and the comma. followed reports that the comma line has a comment after the
// comma, which a comment after the comma would be taken for.
// It reports whether the comma starts its line.
func writeListComma(b *strings.Builder, e listElem, followed bool, control *metadataControl) (alone bool) {
	ends := false
	for _, c := range e.after {
		ends = ends || c != nil && endsLine(c, e.afterLine)
	}
	switch {
	case len(e.after) == 0:
		b.WriteByte(',')
	case !ends || len(e.after) == 1 && !followed && e.after[0].Pos.Line == e.elemLine:
		writeLayoutAfter(b, e.after, ",", e.afterLine, control)
		text := strings.TrimSuffix(b.String(), "\n")
		b.Reset()
		b.WriteString(text)
	default:
		previous := e.elemLine
		for _, c := range e.after {
			pollFormat(control)
			if c == nil {
				continue
			}
			if c.Pos.Line > previous {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
			writeFormatString(b, c.Text, control)
			previous = c.End.Line
		}
		if e.afterLine > previous {
			b.WriteByte('\n')
			alone = true
		}
		b.WriteByte(',')
	}
	return alone
}

// inlineGroupComments reports whether the comments of the delimiters of a list
// are block comments on the lines of the neighboring tokens, which can stay
// there.
func inlineGroupComments(open, first, close *LineBreak, control *metadataControl) bool {
	if !listHasComments(open, first, close) {
		return true
	}
	for _, b := range []*LineBreak{open, first, close} {
		if b != nil && len(b.Comments) != 0 {
			return false
		}
	}
	for _, trailing := range []bool{true, false} {
		for _, b := range []*LineBreak{open, first, close} {
			if _, ok := groupInlineComments(b, trailing, control); !ok {
				return false
			}
		}
	}
	return close == nil || close.Trailing == nil
}

// listHasComments reports whether any of the line breaks holds a comment.
func listHasComments(bs ...*LineBreak) bool {
	for _, b := range bs {
		if b != nil && (len(b.Comments) != 0 || b.Trailing != nil) {
			return true
		}
	}
	return false
}

// formatTypeGroup is formatSourceGroup for a type, whose block comments keep their lines.
func formatTypeGroup(body string, open, first, close *LineBreak, oneLine bool, control *metadataControl) string {
	return formatSourceGroupIndented(body, "(", ")", open, first, close, oneLine, func(s, indent string) string {
		return indentTypeLines(s, indent, control)
	}, control)
}

func formatSourceGroupIndented(body, openDelim, closeDelim string, open, first, close *LineBreak, oneLine bool, indentLines func(s, indent string) string, control *metadataControl) string {
	pollFormat(control)
	if oneLine {
		leading, ok1 := groupInlineComments(open, true, control)
		opening, ok0 := groupInlineComments(open, false, control)
		before, ok2 := groupInlineComments(first, false, control)
		after, ok3 := groupInlineComments(first, true, control)
		closing, ok4 := groupInlineComments(close, false, control)
		if ok0 && ok1 && ok2 && ok3 && ok4 && (close == nil || close.Trailing == nil) {
			inner := strings.TrimPrefix(leading+opening+before, " ") + body + strings.TrimSuffix(after, " ") + strings.TrimSuffix(closing, " ")
			if body == "" {
				inner = strings.TrimSpace(inner) // f(/* a */)
			}
			return openDelim + inner + closeDelim
		}
	}
	p := &printer{control: control, keepStart: true}
	p.write(openDelim)
	p.inline(open)
	p.start = true
	p.comments(open, indentUnit, false)
	if body != "" {
		p.lineBreak(first, indentUnit, false)
		p.write(indentLines(body, indentUnit))
	} else {
		// An empty group has no body line; one would print as a blank line that the next pass
		// reads back as a blank before the closing delimiter.
		p.comments(first, indentUnit, true)
		p.inline(first)
	}
	p.comments(close, indentUnit, true)
	p.newLine("")
	p.write(closeDelim)
	p.inline(close)
	p.flush()
	return strings.TrimSuffix(p.String(), "\n")
}

// indentTypeLines indents the lines of a formatted type that follow its first one, except the
// lines inside block comments, which keep their text. Formatted types hold no strings, so
// comments are the only text that can contain a comment delimiter.
func indentTypeLines(s, indent string, control *metadataControl) string {
	if !hasFormatNewline(s, control) {
		return s
	}
	var out strings.Builder
	depth, line := 0, false // block comment nesting; in a line comment
	for i := 0; i < len(s); i++ {
		if i%4096 == 0 {
			pollFormat(control)
		}
		switch {
		case s[i] == '\n':
			line = false
			out.WriteByte('\n')
			if depth == 0 && i+1 < len(s) && s[i+1] != '\n' {
				out.WriteString(indent)
			}
			continue
		case line:
		case strings.HasPrefix(s[i:], "//") && depth == 0:
			line = true
		case strings.HasPrefix(s[i:], "/*"):
			depth++
			out.WriteString("/*")
			i++
			continue
		case strings.HasPrefix(s[i:], "*/") && depth > 0:
			depth--
			out.WriteString("*/")
			i++
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

// indentTermLines indents the lines of a formatted term that follow its first
// one, except the lines inside block comments, which keep their text. Strings
// are skipped so that a comment delimiter inside one is not read as a comment.
func indentTermLines(s, indent string, control *metadataControl) string {
	if !hasFormatNewline(s, control) {
		return s
	}
	var out strings.Builder
	depth := 0 // block comment nesting
	line, str := false, false
	for i := 0; i < len(s); i++ {
		if i%4096 == 0 {
			pollFormat(control)
		}
		switch {
		case s[i] == '\n':
			line = false
			out.WriteByte('\n')
			if depth == 0 && i+1 < len(s) && s[i+1] != '\n' {
				out.WriteString(indent)
			}
			continue
		case line:
		case str:
			if s[i] == '\\' && i+1 < len(s) {
				out.WriteByte(s[i])
				i++
			} else if s[i] == '"' {
				str = false
			}
		case depth > 0:
			if strings.HasPrefix(s[i:], "/*") {
				depth++
				out.WriteString("/*")
				i++
				continue
			}
			if strings.HasPrefix(s[i:], "*/") {
				depth--
				out.WriteString("*/")
				i++
				continue
			}
		case s[i] == '"':
			str = true
		case strings.HasPrefix(s[i:], "//"):
			line = true
		case strings.HasPrefix(s[i:], "/*"):
			depth++
			out.WriteString("/*")
			i++
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

func groupInlineComments(b *LineBreak, trailing bool, control *metadataControl) (string, bool) {
	pollFormat(control)
	if b == nil {
		return "", true
	}
	comments := b.Comments
	if trailing {
		comments = nil
		if b.Trailing != nil {
			if b.TrailingEnds && !b.Trailing.Header.IsValid() {
				return "", false // the comment ends its line; its owner depends on that
			}
			comments = []*Comment{b.Trailing}
		}
	}
	var s strings.Builder
	for _, c := range comments {
		pollFormat(control)
		if c.Header.IsValid() {
			continue
		}
		if !strings.HasPrefix(c.Text, "/*") || hasFormatNewline(c.Text, control) {
			return "", false
		}
		s.WriteByte(' ')
		writeFormatString(&s, c.Text, control)
		s.WriteByte(' ')
	}
	return s.String(), !b.Blank
}

// Binding strengths of expressions.
const (
	precChoice = iota
	precSeq
	precPrefix
	precSuffix
)

// FormatExpr returns the parser expression e in PEGO syntax.
func FormatExpr(e Expr) string { return formatExpr(e, precChoice, nil) }

func paren(s string, inner, outer int) string {
	if inner < outer {
		return "(" + s + ")"
	}
	return s
}

func formatExpr(e Expr, outer int, control *metadataControl) string {
	pollFormat(control)
	switch e := e.(type) {
	case *InlineAction:
		action := " => ..."
		if !e.DefaultAction {
			action = joinLayout(" => ", formatTerm(e.Action, 0, control))
		}
		return finishFormat("("+formatExpr(e.Expr, precChoice, control)+action+")", control)
	case *Choice:
		parts := make([]string, len(e.Alts))
		for i, a := range e.Alts {
			pollFormat(control)
			parts[i] = formatExpr(a, precSeq, control)
		}
		return finishFormat(paren(joinFormatParts(parts, " / ", control), precChoice, outer), control)
	case *Seq:
		parts := make([]string, len(e.Items))
		for i, it := range e.Items {
			pollFormat(control)
			parts[i] = formatExpr(it, precPrefix, control)
		}
		return finishFormat(paren(joinFormatParts(parts, " ", control), precSeq, outer), control)
	case *Capture:
		return finishFormat(paren(e.Name+":"+formatExpr(e.Expr, precPrefix, control), precPrefix, outer), control)
	case *And:
		return finishFormat(paren("&"+formatExpr(e.Expr, precPrefix, control), precPrefix, outer), control)
	case *Not:
		return finishFormat(paren("!"+formatExpr(e.Expr, precPrefix, control), precPrefix, outer), control)
	case *Atomic:
		return finishFormat(paren(formatExpr(e.Expr, precSuffix, control)+" #atomic", precSuffix, outer), control)
	case *Discard:
		return finishFormat(paren(joinMinusTokens("-", formatExpr(e.Expr, precPrefix, control)), precPrefix, outer), control)
	case *Repeat:
		s := formatExpr(e.Expr, precSuffix, control)
		switch {
		case e.Spelling == nil && e.Min == 0 && e.Max < 0:
			s += "*"
		case e.Spelling == nil && e.Min == 1 && e.Max < 0:
			s += "+"
		default:
			s += " " + formatRepeatPragma(e, control)
		}
		return finishFormat(s, control)
	case *Optional:
		return finishFormat(formatExpr(e.Expr, precSuffix, control)+"?", control)
	case *Stream:
		return finishFormat(formatExpr(e.Expr, precSuffix, control)+" #stream", control)
	case *Recovery:
		return finishFormat(formatExpr(e.Expr, precSuffix, control)+" #recover(skip="+formatExpr(e.Skip, precChoice, control)+")", control)
	case *Attributed:
		s := formatExpr(e.Expr, precSuffix, control)
		for _, a := range e.Attrs {
			pollFormat(control)
			s += " " + formatAttribute(a, control)
		}
		return finishFormat(s, control)
	case *Group:
		p := &printer{control: control}
		p.group(e, "")
		p.flush()
		return finishFormat(strings.TrimSuffix(p.String(), "\n"), control)
	case *Ref:
		if e.Level != "" {
			return finishFormat("level("+e.Name+", "+e.Level+")", control)
		}
		return finishFormat(e.Name, control)
	case *Literal:
		return finishFormat(quote(e.Value, control), control)
	case *CharClass:
		return finishFormat(formatCharClass(e, control), control)
	case *Any:
		return finishFormat(".", control)
	case *Cut:
		return finishFormat("#cut", control)
	case *Top:
		return finishFormat("_", control)
	case *Bottom:
		return finishFormat("_|_", control)
	case *BeginInput:
		return finishFormat("^^", control)
	case *EndInput:
		return finishFormat("$$", control)
	case *BeginLine:
		return finishFormat("^", control)
	case *EndLine:
		return finishFormat("$", control)
	case *Predicate:
		if block, ok := e.Term.(*Block); ok {
			return finishFormat(formatTerm(block, 0, control), control)
		}
		return finishFormat(formatTerm(&Block{Statements: []Term{e.Term}, Breaks: e.Breaks, CloseBreak: e.CloseBreak, OneLine: e.OneLine || len(e.Breaks) == 0 && e.CloseBreak == nil}, 0, control), control)
	case *OperatorsRule:
		p := &printer{control: control}
		p.operatorsRule(e, "")
		p.flush()
		return finishFormat(strings.TrimSuffix(p.String(), "\n"), control)
	}
	return finishFormat(fmt.Sprintf("<%T>", e), control)
}

// formatRepeatPragma returns the pragma #repeat(...) that repeats e. It keeps
// the tokens the source had between the parentheses: #repeat(2,), #repeat(,3)
// and #repeat(2, 2) stay as written, and so do the comments between them. The
// comma is part of the syntax, so it is never a list separator and the pragma
// has no trailing comma. A comment that ends its line puts the tokens on lines.
func formatRepeatPragma(e *Repeat, control *metadataControl) string {
	pollFormat(control)
	var tokens []string
	var gaps []RepeatGap
	sp := e.Spelling
	if sp != nil && (sp.Min != e.Min || sp.Max != e.Max) {
		// Edited since it was read: the canonical form, with the old comments
		// before the bounds.
		var all []*Comment
		for _, g := range sp.Gaps {
			pollFormat(control)
			all = append(all, g.Comments...)
		}
		sp = nil
		gaps = []RepeatGap{{Comments: all}}
	}
	switch {
	case sp != nil:
		if sp.HasMin {
			tokens = append(tokens, strconv.Itoa(e.Min))
		}
		if sp.Comma {
			tokens = append(tokens, ",")
		}
		if sp.HasMax {
			tokens = append(tokens, strconv.Itoa(e.Max))
		}
		gaps = sp.Gaps
	case e.Min == e.Max:
		tokens = []string{strconv.Itoa(e.Min)}
	case e.Max < 0:
		tokens = []string{strconv.Itoa(e.Min), ","}
	default:
		tokens = []string{strconv.Itoa(e.Min), ",", strconv.Itoa(e.Max)}
	}
	if len(gaps) != len(tokens)+1 {
		var all []*Comment
		for _, g := range gaps {
			all = append(all, g.Comments...)
		}
		gaps = make([]RepeatGap, len(tokens)+1)
		gaps[0].Comments = all
	}
	lines := false
	for _, g := range gaps {
		for k, c := range g.Comments {
			pollFormat(control)
			lines = lines || endsLine(c, g.nextLine(k)) || hasFormatNewline(c.Text, control)
		}
	}
	// A maximum after the comma has a space before it, unless the minimum is
	// left out: #repeat(,3).
	space := func(i int) bool { return i == 2 }
	var b strings.Builder
	if !lines {
		b.WriteString("#repeat(")
		for i, tok := range tokens {
			pollFormat(control)
			g := gaps[i]
			for k, c := range g.Comments {
				if i > 0 || k > 0 {
					b.WriteByte(' ')
				}
				writeFormatString(&b, c.Text, control)
			}
			switch {
			case tok == ",":
			case len(g.Comments) > 0 || space(i):
				b.WriteByte(' ')
			}
			b.WriteString(tok)
		}
		for _, c := range gaps[len(tokens)].Comments {
			b.WriteByte(' ')
			writeFormatString(&b, c.Text, control)
		}
		b.WriteByte(')')
		return b.String()
	}
	// On lines: a comment on the line of the token before it stays there; the
	// others take lines of their own.
	head := "#repeat("
	var body []string
	cur := ""
	flush := func() {
		if cur != "" {
			body = append(body, indentUnit+cur)
		}
		cur = ""
	}
	gap := func(g RepeatGap, first bool) {
		for k, c := range g.Comments {
			pollFormat(control)
			var text strings.Builder
			writeFormatString(&text, c.Text, control)
			switch {
			case c.Pos.Line <= g.PrevLine && first:
				head += " " + text.String()
			case c.Pos.Line <= g.PrevLine && cur != "":
				cur += " " + text.String()
			default:
				flush()
				cur = text.String()
			}
			if endsLine(c, g.nextLine(k)) {
				if first && c.Pos.Line <= g.PrevLine {
					continue
				}
				flush()
			}
		}
	}
	for i, tok := range tokens {
		gap(gaps[i], i == 0)
		switch {
		case cur == "":
			cur = tok
		case tok == ",":
			cur += ","
		case len(gaps[i].Comments) > 0 || space(i):
			cur += " " + tok
		default:
			cur += tok
		}
	}
	gap(gaps[len(tokens)], false)
	flush()
	return head + "\n" + strings.Join(body, "\n") + "\n)"
}

// nextLine returns the line of what follows the k-th comment of g: the next
// comment, or else the token after the gap.
func (g RepeatGap) nextLine(k int) int {
	if k+1 < len(g.Comments) {
		return g.Comments[k+1].Pos.Line
	}
	return g.NextLine
}

func formatAttribute(a *Attribute, control *metadataControl) string {
	pollFormat(control)
	marker := "@"
	if a.Pragma {
		marker = "#"
	}
	s := marker + a.Name
	if len(a.Args) == 0 && len(a.KwArgs) == 0 && (a.ArgsLayout == nil || !listHasComments(a.ArgsLayout.OpenBreak, a.ArgsLayout.BodyBreak, a.ArgsLayout.CloseBreak)) {
		return s
	}
	var args []string
	for _, value := range a.Args {
		pollFormat(control)
		args = append(args, formatAttributeValue(value, control))
	}
	var keys []string
	seen := map[string]bool{}
	for _, key := range a.KeywordOrder {
		pollFormat(control)
		if _, exists := a.KwArgs[key]; exists && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	var remaining []string
	for key := range a.KwArgs {
		pollFormat(control)
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}
	sortFormatKeys(remaining, control)
	keys = appendMetadata(keys, remaining, control)
	for _, key := range keys {
		pollFormat(control)
		args = append(args, key+"="+formatAttributeValue(a.KwArgs[key], control))
	}
	list := a.ArgsLayout
	if list != nil && len(list.Elems) != len(args) {
		list = nil // the arguments changed since the source was read
	}
	if len(args) == 0 && (list == nil || !listHasComments(list.OpenBreak, list.BodyBreak, list.CloseBreak)) {
		return s
	}
	return s + formatItemList(args, a.TrailingComma, list, control)
}

// formatItemList formats a parenthesized list of items that are not nodes, such
// as the arguments of an attribute, with the comments in list, if any. A comma
// after the last item puts every item on its own line.
func formatItemList(items []string, trailing bool, list *ListLayout, control *metadataControl) string {
	elems := make([]listElem, len(items))
	for i, item := range items {
		pollFormat(control)
		var layout *SourceLayout
		if list != nil {
			layout = list.Elems[i]
		}
		elems[i] = layoutElem(layout, item, control)
	}
	var open, first, close *LineBreak
	if list != nil {
		open, first, close = list.OpenBreak, list.BodyBreak, list.CloseBreak
	}
	return formatTermList(elems, false, trailing, "(", ")", open, first, close, control)
}

func formatAttributeValue(value any, control *metadataControl) string {
	pollFormat(control)
	switch value := value.(type) {
	case nil:
		return "nil"
	case int:
		return strconv.Itoa(value)
	case string:
		return quote(value, control)
	case bool:
		return strconv.FormatBool(value)
	case Expr:
		return formatExpr(value, precChoice, control)
	}
	return fmt.Sprintf("<%T>", value)
}

// quote returns s as a PEGO string literal.
func quote(s string, control *metadataControl) string {
	pollFormat(control)
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		pollFormat(control)
		switch r {
		case '"':
			writeFormatString(&b, `\"`, control)
		case '\\':
			writeFormatString(&b, `\\`, control)
		case '\n':
			writeFormatString(&b, `\n`, control)
		case '\t':
			writeFormatString(&b, `\t`, control)
		case '\r':
			writeFormatString(&b, `\r`, control)
		default:
			if strconv.IsPrint(r) {
				b.WriteRune(r)
			} else {
				fmt.Fprintf(&b, `\u{%x}`, r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func formatCharClass(e *CharClass, control *metadataControl) string {
	pollFormat(control)
	var b strings.Builder
	writeFormatString(&b, "[", control)
	if e.Negated {
		writeFormatString(&b, "^", control)
	}
	esc := func(r rune) string {
		switch r {
		case '-', '[', ']', '^', '\\':
			return `\` + string(r)
		case '\n':
			return `\n`
		case '\t':
			return `\t`
		case '\r':
			return `\r`
		}
		if !strconv.IsPrint(r) {
			return fmt.Sprintf(`\u{%x}`, r)
		}
		return string(r)
	}
	for _, rg := range e.Ranges {
		pollFormat(control)
		writeFormatString(&b, esc(rg.Lo), control)
		if rg.Hi != rg.Lo {
			writeFormatString(&b, "-"+esc(rg.Hi), control)
		}
	}
	writeFormatString(&b, "]", control)
	return b.String()
}

var termPrec = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3,
	"+": 4, "-": 4,
	"*": 5, "/": 5, "%": 5,
}

// FormatTerm returns the value expression t in PEGO syntax.
func FormatTerm(t Term) string { return formatTerm(t, 0, nil) }

func formatTerm(t Term, outer int, control *metadataControl) string {
	pollFormat(control)
	return formatSourceLayout(termLayout(t), formatTermNode(t, outer, control), control)
}

// termLayout returns the comments around t.
func termLayout(t Term) *SourceLayout { return nodeLayout(t) }

// nodeLayout returns the comments around n, an element of a list.
func nodeLayout(n any) *SourceLayout {
	if n, ok := n.(SourceNode); ok {
		return n.Source().Layout
	}
	return nil
}

// formatListElem formats a list element without the comments after it, which
// the list places itself.
func formatListElem(t Term, control *metadataControl) listElem {
	return layoutElem(termLayout(t), formatTermNode(t, 0, control), control)
}

func layoutElem(layout *SourceLayout, body string, control *metadataControl) listElem {
	var e listElem
	if layout != nil {
		e.after, e.elemLine, e.afterLine = layout.After, layout.ElemLine, layout.AfterLine
		e.trail = layout.Trail
	}
	e.head = formatLayoutHead(layout, body, control)
	return e
}

// formatTermNode formats t without the comments around it.
func formatTermNode(t Term, outer int, control *metadataControl) string {
	pollFormat(control)
	switch t := t.(type) {
	case *TermGroup:
		return formatSourceGroup(formatTerm(t.Term, 0, control), t.OpenBreak, t.BodyBreak, t.CloseBreak, t.OneLine, control)
	case *Block:
		p := &printer{control: control}
		parts := make([]string, len(t.Statements))
		for i, stmt := range t.Statements {
			pollFormat(control)
			parts[i] = formatTerm(stmt, 0, control)
		}
		if t.OneLine && (len(t.Statements) > 0 && noComments(t.CloseBreak) || len(t.Statements) == 0) {
			plain := true
			lead := ""
			if len(t.Statements) == 0 {
				lead, plain = inlineLead(t.CloseBreak, Pos{})
				lead = strings.TrimSpace(lead)
			}
			for i, b := range t.Breaks {
				pollFormat(control)
				plain = plain && (b == nil || b.Trailing == nil)
				if i == 0 {
					var ok bool
					lead, ok = inlineLead(b, termPos(t.Statements[0]))
					plain = plain && ok
				} else {
					plain = plain && noComments(b)
				}
			}
			for _, part := range parts {
				// A statement over several lines breaks the block too.
				plain = plain && !hasFormatNewline(part, control)
			}
			if plain {
				return "{" + lead + joinFormatParts(parts, "; ", control) + "}"
			}
		}
		p.openBlock()
		for i := range t.Statements {
			pollFormat(control)
			p.lineBreak(breakAt(t.Breaks, i), indentUnit, false)
			p.write(indentTermLines(parts[i], indentUnit, control))
		}
		p.closeBlock(t.CloseBreak, "")
		p.flush()
		return strings.TrimSuffix(p.String(), "\n")
	case *IntLit:
		if t.Value == -int(^uint(0)>>1)-1 {
			// The absolute minimum cannot be a positive source token. Use
			// representable operands and honor the surrounding precedence.
			return formatTerm(&Binary{Op: "-", L: &IntLit{Value: t.Value + 1}, R: &IntLit{Value: 1}}, outer, control)
		}
		return strconv.Itoa(t.Value)
	case *StringLit:
		return quote(t.Value, control)
	case *BoolLit:
		return strconv.FormatBool(t.Value)
	case *NilLit:
		return "nil"
	case *CaptureRef:
		if t.Param {
			return t.Name
		}
		return "$" + t.Name
	case *IndexRef:
		return "$" + strconv.Itoa(t.Index)
	case *VarRef:
		return t.Name
	case *Member:
		return formatTerm(t.X, 7, control) + "." + t.Name
	case *New:
		elems := make([]listElem, 0, len(t.Fields)+len(t.Args))
		for _, f := range t.Fields {
			pollFormat(control)
			elems = append(elems, layoutElem(f.Layout, f.Name+": "+formatTerm(f.Value, 0, control), control))
		}
		for _, x := range t.Args {
			pollFormat(control)
			elems = append(elems, formatListElem(x, control))
		}
		return t.Type + formatTermList(elems, false, t.TrailingComma, "{", "}", t.OpenBreak, t.BodyBreak, t.CloseBreak, control)
	case *TupleLit:
		elems := make([]listElem, len(t.Elems))
		for i, x := range t.Elems {
			pollFormat(control)
			elems[i] = formatListElem(x, control)
		}
		// The comma of a one-element tuple is required: (x) would be a group.
		return formatTermList(elems, len(elems) == 1, t.TrailingComma && len(elems) > 1, "(", ")", t.OpenBreak, t.BodyBreak, t.CloseBreak, control)
	case *ListConstructor:
		items := make([]listElem, len(t.Items))
		for i, item := range t.Items {
			pollFormat(control)
			items[i] = formatListElem(item, control)
		}
		return "[]" + formatTypeTerm(t.Elem, control) + formatTermList(items, false, t.TrailingComma, "{", "}", t.OpenBreak, t.BodyBreak, t.CloseBreak, control)
	case *Call:
		args := make([]listElem, len(t.Args))
		for i, a := range t.Args {
			pollFormat(control)
			args[i] = formatListElem(a, control)
		}
		return t.Func + formatTermList(args, false, t.TrailingComma, "(", ")", t.OpenBreak, t.BodyBreak, t.CloseBreak, control)
	case *Lambda:
		list := t.ParamsLayout
		if list != nil && len(list.Elems) != len(t.Params) {
			list = nil // the parameters changed since the source was read
		}
		s := formatItemList(t.Params, t.TrailingComma, list, control) + " => " + formatTerm(t.Body, 0, control)
		if outer > 0 {
			return parenTerm(s, control)
		}
		return s
	case *Binary:
		if t.Op == "[]" {
			l := formatTerm(t.L, 7, control)
			switch t.L.(type) {
			case *IntLit, *StringLit, *IndexRef, *New, *ListConstructor, *Lambda:
				// "[" is an index only directly after a name, a capture, ")" or "]".
				l = parenTerm(l, control)
			}
			return l + "[" + strings.TrimPrefix(formatTerm(t.R, 0, control), " ") + "]"
		}
		p := termPrec[t.Op]
		// Operators are left-associative, so the right operand is
		// parenthesized even at the same precedence.
		lp := p
		if p == termPrec["=="] {
			lp++ // comparison operators do not chain
		}
		s := formatTerm(t.L, lp, control) + " " + t.Op + " " + formatTerm(t.R, p+1, control)
		if p < outer {
			return parenTerm(s, control)
		}
		return s
	case *Unary:
		return joinMinusTokens(t.Op, formatTerm(t.X, 6, control))
	case *Cast:
		x := formatTerm(t.X, 7, control)
		if _, ok := t.X.(*IntLit); ok {
			// 1.(int) would read as a number.
			x = parenTerm(x, control)
		}
		return x + ".(" + formatType(t.Type, control) + ")"
	case *Bind:
		return t.Name + " := " + formatTerm(t.Value, 0, control)
	}
	return fmt.Sprintf("<%T>", t)
}

// header keeps forbidden comments inside the header, so formatting never
// changes a semantic placement error into valid source.
func (p *printer) header(word string, pos Pos) {
	p.write(word)
	for _, comment := range p.headers[pos] {
		pollFormat(p.control)
		p.write(" " + comment.Text)
		if strings.HasPrefix(comment.Text, "//") {
			p.newLine(indentUnit)
		}
	}
}
func (p *printer) operator(op *OperatorsRuleOperator, indent string) {
	pollFormat(p.control)
	p.header(op.Kind, op.Pos)
	p.write(" ")
	if op.Assoc != "" {
		p.write(op.Assoc + " ")
	}
	p.write(formatExpr(op.Expr, precChoice, p.control))
	if op.Action != nil {
		p.write(joinLayout(" => ", indentTermLines(formatTerm(op.Action, 0, p.control), indent, p.control)))
	} else if op.DefaultAction {
		p.write(" => ...")
	}
}

// formatTypeList is formatSourceList for the elements of a type, whose block comments keep their lines.
func formatTypeList(elems []listElem, comma, lines bool, openDelim, closeDelim string, open, first, close *LineBreak, control *metadataControl) string {
	return formatSourceList(elems, comma, lines, openDelim, closeDelim, open, first, close, func(s, indent string) string {
		return indentTypeLines(s, indent, control)
	}, control)
}

// formatTermList is formatSourceList for the elements of an action expression.
func formatTermList(elems []listElem, comma, lines bool, openDelim, closeDelim string, open, first, close *LineBreak, control *metadataControl) string {
	return formatSourceList(elems, comma, lines, openDelim, closeDelim, open, first, close, func(s, indent string) string {
		return indentTermLines(s, indent, control)
	}, control)
}

// formatSourceLayout keeps the token-boundary line relationships that determine
// documentation ownership. Identical comment text at different boundaries is
// emitted independently; no content-based de-duplication is performed.
func formatSourceLayout(layout *SourceLayout, body string, control *metadataControl) string {
	pollFormat(control)
	if layout == nil || len(layout.Before) == 0 && len(layout.After) == 0 {
		return body
	}
	var out strings.Builder
	writeFormatString(&out, formatLayoutHead(layout, body, control), control)
	writeLayoutAfter(&out, layout.After, "", layout.AfterLine, control)
	return out.String()
}

// formatLayoutHead writes body after the comments before it.
func formatLayoutHead(layout *SourceLayout, body string, control *metadataControl) string {
	pollFormat(control)
	if layout == nil || len(layout.Before) == 0 {
		return body
	}
	var out strings.Builder
	previous := layout.PreviousLine
	for _, comment := range layout.Before {
		pollFormat(control)
		if comment == nil {
			continue
		}
		if comment.Pos.Line > previous {
			out.WriteByte('\n')
			if comment.Pos.Line-previous > 1 {
				out.WriteByte('\n')
			}
		} else {
			out.WriteByte(' ')
		}
		writeFormatString(&out, comment.Text, control)
		previous = comment.End.Line
	}
	if layout.NextLine > previous {
		out.WriteByte('\n')
		if layout.NextLine-previous > 1 {
			out.WriteByte('\n')
		}
	} else {
		out.WriteByte(' ')
	}
	writeFormatString(&out, body, control)
	return out.String()
}

// writeLayoutAfter writes the comments after a node and before the separator
// that follows it. A line comment ends its line, so the separator goes before
// the first of them, or after all the comments when there is none; with no
// separator it moves to the next line. It reports whether the text ends with a
// line comment and its line break.
func writeLayoutAfter(out *strings.Builder, comments []*Comment, separator string, nextLine int, control *metadataControl) (lastLine bool) {
	separated := separator == ""
	lineStart := false
	for _, comment := range comments {
		pollFormat(control)
		if comment == nil {
			continue
		}
		line := strings.HasPrefix(comment.Text, "//")
		if !separated && endsLine(comment, nextLine) {
			out.WriteString(separator)
			separated = true
		}
		if !lineStart {
			out.WriteByte(' ')
		}
		writeFormatString(out, comment.Text, control)
		lineStart = line
		if line {
			out.WriteByte('\n')
		}
	}
	if !separated {
		out.WriteString(separator)
	}
	return lineStart
}

// joinLayout appends body to a separator that ends in a space. A comment before body on the same
// line brings a space of its own, which would double the separator's.
func joinLayout(separator, body string) string {
	if strings.HasSuffix(separator, " ") && strings.HasPrefix(body, " ") {
		body = body[1:]
	}
	return separator + body
}
