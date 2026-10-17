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
	for _, comment := range g.HeaderComments {
		pollFormat(control)
		if comment != nil {
			p.headers[comment.Header] = append(p.headers[comment.Header], comment)
		}
	}
	if g.Package != "" {
		p.lineBreak(g.PackageBreak, "", false)
		p.header("package", g.PackageSource.Pos)
		p.write(" " + g.Package)
	}
	for _, s := range g.Statements {
		pollFormat(control)
		switch s := s.(type) {
		case *TypeDef:
			p.lineBreak(s.Break, "", true)
			p.typeDef(s)
		case *RuleDef:
			p.lineBreak(s.Break, "", true)
			p.ruleDef(s)
		}
	}
	p.comments(g.EndBreak, "", true)
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
	if p.start || len(p.lines) == 0 || p.lines[len(p.lines)-1].blank {
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
	switch spec := t.Spec.(type) {
	case *StructSpec:
		p.structSpec(spec, "")
	case *AliasSpec:
		p.write("~ " + formatType(spec.Type, p.control))
	case *TerminalSpec:
		p.write("terminal")
	}
}

func (p *printer) structSpec(s *StructSpec, indent string) {
	pollFormat(p.control)
	p.write("= ")
	empty := noComments(s.CloseBreak)
	for _, f := range s.Fields {
		pollFormat(p.control)
		empty = empty && noComments(f.Break)
	}
	if empty && (len(s.Fields) == 0 || s.OneLine) {
		parts := make([]string, len(s.Fields))
		for i, f := range s.Fields {
			pollFormat(p.control)
			parts[i] = f.Name + " " + formatType(f.Type, p.control)
			p.inline(f.Break)
		}
		p.inline(s.CloseBreak)
		if len(parts) == 0 {
			p.write("{}")
		} else {
			p.write("{ " + joinFormatParts(parts, "; ", p.control) + " }")
		}
		return
	}
	p.openBlock()
	for _, f := range s.Fields {
		pollFormat(p.control)
		p.lineBreak(f.Break, indent+indentUnit, false)
		p.cur.field = true
		p.cur.name = f.Name
		p.write(formatType(f.Type, p.control))
	}
	p.closeBlock(s.CloseBreak, indent)
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
		p.write(" => ")
		if r.DefaultAction {
			p.write("...")
		} else {
			p.write(formatTerm(r.Action, 0, p.control))
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
			p.write(formatExpr(e, precPrefix, p.control))
			break
		}
		p.write(formatExpr(e.Expr, precSuffix, p.control))
		p.lineBreak(e.PragmaBreak, indent, false)
		if e.Min == e.Max {
			p.write(fmt.Sprintf("#repeat(%d)", e.Min))
		} else if e.Max < 0 {
			p.write(fmt.Sprintf("#repeat(%d, )", e.Min))
		} else {
			p.write(fmt.Sprintf("#repeat(%d, %d)", e.Min, e.Max))
		}
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
			p.write(formatAttribute(a, p.control))
		}
	default:
		p.write(formatExpr(e, precPrefix, p.control))
	}
}

func (p *printer) group(g *Group, indent string) {
	pollFormat(p.control)
	if g.OneLine && noComments(g.BodyBreak) && noComments(g.CloseBreak) {
		p.write("(")
		p.groupBody(g.Expr, indent)
		p.write(")")
		p.inline(g.CloseBreak)
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

func (p *printer) groupBody(e Expr, indent string) {
	pollFormat(p.control)
	if action, ok := e.(*InlineAction); ok {
		p.body(action.Expr, indent)
		p.write(" => ")
		if action.DefaultAction {
			p.write("...")
		} else {
			p.write(formatTerm(action.Action, 0, p.control))
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
			p.write(" => " + formatTerm(o.Action, 0, p.control))
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
		case empty && (len(l.Operators) == 0 || l.OneLine):
			p.write("{")
			for i, op := range l.Operators {
				pollFormat(p.control)
				if i > 0 {
					p.write(";")
				}
				p.write(" ")
				p.operator(op)
				p.inline(op.Break)
			}
			if len(l.Operators) > 0 {
				p.write(" ")
			}
			p.write("}")
			p.inline(l.CloseBreak)
		default:
			p.openBlock()
			for _, op := range l.Operators {
				pollFormat(p.control)
				p.lineBreak(op.Break, inner+indentUnit, false)
				p.operator(op)
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
		s += " => " + formatTerm(op.Action, 0, control)
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
		return finishFormat(formatSourceGroup(formatType(t.Type, control), t.OpenBreak, t.BodyBreak, t.CloseBreak, t.OneLine, control), control)
	case *TypeRef:
		return finishFormat(t.Name, control)
	case *ListType:
		return finishFormat("[]"+formatTypeTerm(t.Elem, control), control)
	case *OptionalType:
		return finishFormat("*"+formatTypeTerm(t.Elem, control), control)
	case *UnionType:
		parts := make([]string, len(t.Types))
		for i, x := range t.Types {
			pollFormat(control)
			parts[i] = formatTypeTerm(x, control)
		}
		return finishFormat(joinFormatParts(parts, " | ", control), control)
	}
	return finishFormat("?", control)
}

func formatTypeTerm(t TypeExpr, control *metadataControl) string {
	pollFormat(control)
	if _, ok := t.(*UnionType); ok {
		return "(" + formatType(t, control) + ")"
	}
	return formatType(t, control)
}

// formatSourceGroup keeps explicit delimiters and their boundary trivia.
func formatSourceGroup(body string, open, first, close *LineBreak, oneLine bool, control *metadataControl) string {
	pollFormat(control)
	if oneLine {
		leading, ok1 := groupInlineComments(open, true, control)
		opening, ok0 := groupInlineComments(open, false, control)
		before, ok2 := groupInlineComments(first, false, control)
		after, ok3 := groupInlineComments(first, true, control)
		closing, ok4 := groupInlineComments(close, false, control)
		if ok0 && ok1 && ok2 && ok3 && ok4 && (close == nil || close.Trailing == nil) {
			return "(" + leading + opening + before + body + after + closing + ")"
		}
	}
	p := &printer{control: control}
	p.write("(")
	p.inline(open)
	p.start = true
	p.comments(open, indentUnit, false)
	p.lineBreak(first, indentUnit, false)
	p.write(replaceFormatNewlines(body, "\n", "\n"+indentUnit, control))
	p.comments(close, indentUnit, true)
	p.newLine("")
	p.write(")")
	p.inline(close)
	p.flush()
	return strings.TrimSuffix(p.String(), "\n")
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
		action := "..."
		if !e.DefaultAction {
			action = formatTerm(e.Action, 0, control)
		}
		return finishFormat("("+formatExpr(e.Expr, precChoice, control)+" => "+action+")", control)
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
		case e.Min == 0 && e.Max < 0:
			s += "*"
		case e.Min == 1 && e.Max < 0:
			s += "+"
		case e.Max < 0:
			s += fmt.Sprintf(" #repeat(%d, )", e.Min)
		case e.Min == e.Max:
			s += fmt.Sprintf(" #repeat(%d)", e.Min)
		default:
			s += fmt.Sprintf(" #repeat(%d, %d)", e.Min, e.Max)
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

func formatAttribute(a *Attribute, control *metadataControl) string {
	pollFormat(control)
	marker := "@"
	if a.Pragma {
		marker = "#"
	}
	s := marker + a.Name
	if len(a.Args) == 0 && len(a.KwArgs) == 0 {
		return s
	}
	var args []string
	for _, value := range a.Args {
		pollFormat(control)
		if value == nil && len(a.Args) > 1 {
			args = append(args, "")
			continue
		}
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
	return s + "(" + joinFormatParts(args, ", ", control) + ")"
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

func formatTerm(t Term, outer int, control *metadataControl) (formatted string) {
	pollFormat(control)
	defer func() {
		// Do not run layout traversal while unwinding a cancellation or a
		// caller panic; it must not replace the original panic value.
		if failure := recover(); failure != nil {
			panic(failure)
		}
		if n, ok := t.(SourceNode); ok {
			formatted = formatSourceLayout(n.Source().Layout, formatted, control)
		}
	}()
	switch t := t.(type) {
	case *TermGroup:
		return formatSourceGroup(formatTerm(t.Term, 0, control), t.OpenBreak, t.BodyBreak, t.CloseBreak, t.OneLine, control)
	case *Block:
		p := &printer{control: control}
		if t.OneLine && noComments(t.CloseBreak) {
			plain := true
			for _, b := range t.Breaks {
				pollFormat(control)
				plain = plain && noComments(b)
			}
			if plain {
				parts := make([]string, len(t.Statements))
				for i, stmt := range t.Statements {
					pollFormat(control)
					parts[i] = formatTerm(stmt, 0, control)
				}
				return "{" + joinFormatParts(parts, "; ", control) + "}"
			}
		}
		p.openBlock()
		for i, stmt := range t.Statements {
			pollFormat(control)
			p.lineBreak(breakAt(t.Breaks, i), indentUnit, false)
			p.write(formatTerm(stmt, 0, control))
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
		return "$" + t.Name
	case *IndexRef:
		return "$" + strconv.Itoa(t.Index)
	case *VarRef:
		return t.Name
	case *Member:
		return formatTerm(t.X, 7, control) + "." + t.Name
	case *New:
		fields := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			pollFormat(control)
			fields[i] = formatSourceLayout(f.Layout, f.Name+": "+formatTerm(f.Value, 0, control), control)
		}
		return t.Type + "{" + joinFormatParts(fields, ", ", control) + "}"
	case *ListConstructor:
		items := make([]string, len(t.Items))
		for i, item := range t.Items {
			pollFormat(control)
			items[i] = formatTerm(item, 0, control)
		}
		return "[]" + formatTypeTerm(t.Elem, control) + "{" + joinFormatParts(items, ", ", control) + "}"
	case *TerminalConstructor:
		args := make([]string, len(t.Args))
		for i, arg := range t.Args {
			pollFormat(control)
			args[i] = formatTerm(arg, 0, control)
		}
		return t.Type + "{" + joinFormatParts(args, ", ", control) + "}"
	case *Call:
		args := make([]string, len(t.Args))
		for i, a := range t.Args {
			pollFormat(control)
			args[i] = formatTerm(a, 0, control)
		}
		return t.Func + "(" + joinFormatParts(args, ", ", control) + ")"
	case *Lambda:
		s := "(" + joinFormatParts(t.Params, ", ", control) + ") => " + formatTerm(t.Body, 0, control)
		if outer > 0 {
			return "(" + s + ")"
		}
		return s
	case *Binary:
		p := termPrec[t.Op]
		// Operators are left-associative, so the right operand is
		// parenthesized even at the same precedence.
		lp := p
		if p == termPrec["=="] {
			lp++ // comparison operators do not chain
		}
		s := formatTerm(t.L, lp, control) + " " + t.Op + " " + formatTerm(t.R, p+1, control)
		if p < outer {
			return "(" + s + ")"
		}
		return s
	case *Unary:
		return joinMinusTokens(t.Op, formatTerm(t.X, 6, control))
	case *Assign:
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
func (p *printer) operator(op *OperatorsRuleOperator) {
	pollFormat(p.control)
	p.header(op.Kind, op.Pos)
	p.write(" ")
	if op.Assoc != "" {
		p.write(op.Assoc + " ")
	}
	p.write(formatExpr(op.Expr, precChoice, p.control))
	if op.Action != nil {
		p.write(" => " + formatTerm(op.Action, 0, p.control))
	} else if op.DefaultAction {
		p.write(" => ...")
	}
}

// formatSourceLayout keeps the token-boundary line relationships that determine
// documentation ownership. Identical comment text at different boundaries is
// emitted independently; no content-based de-duplication is performed.
func formatSourceLayout(layout *SourceLayout, body string, control *metadataControl) string {
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
