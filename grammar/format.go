package grammar

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// indentUnit is one level of indentation.
const indentUnit = "    "

// Format converts g to PEGO source code.
//
// Comments and line breaks recorded in the AST (see LineBreak) are kept, so
// formatting parsed source preserves its comments. Struct field types and
// trailing comments on consecutive lines are aligned. A grammar without such
// layout information (for example, one decoded from JSON) is printed with a
// blank line between definitions and one item per line in blocks.
func Format(g *Grammar) string {
	p := &printer{start: true, headers: map[Pos][]*Comment{}}
	for _, comment := range g.HeaderComments {
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
}

func (p *printer) write(s string) {
	if p.cur == nil {
		p.newLine("")
	}
	p.cur.text = joinMinusTokens(p.cur.text, s)
}

// joinMinusTokens keeps adjacent minus operators from becoming the cut
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
	p.header("type", t.Pos)
	p.write(" " + t.Name + " ")
	switch spec := t.Spec.(type) {
	case *StructSpec:
		p.structSpec(spec, "")
	case *AliasSpec:
		p.write("= " + FormatType(spec.Type))
	case *TerminalSpec:
		p.write("terminal")
	}
}

func (p *printer) structSpec(s *StructSpec, indent string) {
	p.write("struct ")
	empty := noComments(s.CloseBreak)
	for _, f := range s.Fields {
		empty = empty && noComments(f.Break)
	}
	if empty && (len(s.Fields) == 0 || s.OneLine) {
		parts := make([]string, len(s.Fields))
		for i, f := range s.Fields {
			parts[i] = f.Name + " " + FormatType(f.Type)
			p.inline(f.Break)
		}
		p.inline(s.CloseBreak)
		if len(parts) == 0 {
			p.write("{}")
		} else {
			p.write("{ " + strings.Join(parts, "; ") + " }")
		}
		return
	}
	p.openBlock()
	for _, f := range s.Fields {
		p.lineBreak(f.Break, indent+indentUnit, false)
		p.cur.field = true
		p.cur.name = f.Name
		p.write(FormatType(f.Type))
	}
	p.closeBlock(s.CloseBreak, indent)
}

func (p *printer) ruleDef(r *RuleDef) {
	p.header("rule", r.Pos)
	p.write(" " + r.Name)
	if r.Type != nil {
		p.write(" " + FormatType(r.Type))
	}
	p.write(" = ")
	if pr, ok := r.Expr.(*OperatorsRule); ok {
		p.write("operators ")
		p.operatorsRule(pr, "")
	} else {
		if r.BodyBreak != nil || r.ActionBreak != nil || expressionLayout(r.Expr) {
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
			p.write(FormatTerm(r.Action))
		}
	}
	p.comments(r.CloseBreak, indentUnit, true)
	p.inline(r.CloseBreak)
}

func expressionLayout(e Expr) bool {
	switch e := e.(type) {
	case *Seq:
		for _, b := range e.Breaks {
			if b != nil {
				return true
			}
		}
		for _, it := range e.Items {
			if expressionLayout(it) {
				return true
			}
		}
	case *Choice:
		for _, b := range e.Breaks {
			if b != nil {
				return true
			}
		}
		for _, alt := range e.Alts {
			if expressionLayout(alt) {
				return true
			}
		}
	case *Attributed:
		for _, b := range e.Breaks {
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
		return expressionLayout(e.Expr)
	case *And:
		return expressionLayout(e.Expr)
	case *Not:
		return expressionLayout(e.Expr)
	case *Discard:
		return expressionLayout(e.Expr)
	}
	return false
}

func undecorate(e Expr, attrs []*Attribute) (Expr, bool) {
	for _, a := range attrs {
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
	c, ok := e.(*Choice)
	if !ok {
		p.sequence(e, indent, precChoice)
		return
	}
	for i, a := range c.Alts {
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
	switch e := e.(type) {
	case *Choice:
		p.write(formatExpr(e, outer))
	case *Seq:
		for i, it := range e.Items {
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
		p.write(formatExpr(e.Expr, precSuffix))
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write("#atomic")
	case *Stream:
		p.write(formatExpr(e.Expr, precSuffix))
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write("#stream")
	case *Recovery:
		p.write(formatExpr(e.Expr, precSuffix))
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write("#recover(skip=" + FormatExpr(e.Skip) + ")")
	case *Repeat:
		if e.PragmaBreak == nil {
			p.write(formatExpr(e, precPrefix))
			break
		}
		p.write(formatExpr(e.Expr, precSuffix))
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
		p.write(formatExpr(e.Expr, precSuffix))
		for i, a := range e.Attrs {
			if b := breakAt(e.Breaks, i); b != nil {
				p.lineBreak(b, indent, false)
			} else {
				p.write(" ")
			}
			p.write(formatAttribute(a))
		}
	default:
		p.write(formatExpr(e, precPrefix))
	}
}

func (p *printer) group(g *Group, indent string) {
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
	if action, ok := e.(*InlineAction); ok {
		p.body(action.Expr, indent)
		p.write(" => ")
		if action.DefaultAction {
			p.write("...")
		} else {
			p.write(FormatTerm(action.Action))
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
	inner := indent + indentUnit
	p.openBlock()
	if pr.Skip != nil {
		p.lineBreak(pr.SkipBreak, inner, false)
		p.write("skip " + FormatExpr(pr.Skip))
	}
	for _, o := range pr.Operands {
		p.lineBreak(o.Break, inner, false)
		p.write("operand " + FormatExpr(o.Expr))
		if o.Action != nil {
			p.write(" => " + FormatTerm(o.Action))
		} else if o.DefaultAction {
			p.write(" => ...")
		}
	}
	for _, l := range pr.Levels {
		p.lineBreak(l.Break, inner, false)
		p.header("level", l.Pos)
		p.write(" ")
		if l.Name != "" {
			p.write(l.Name + " ")
		}
		empty := noComments(l.CloseBreak)
		for _, op := range l.Operators {
			empty = empty && noComments(op.Break)
		}
		switch {
		case empty && (len(l.Operators) == 0 || l.OneLine):
			p.write("{")
			for i, op := range l.Operators {
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
				p.lineBreak(op.Break, inner+indentUnit, false)
				p.operator(op)
			}
			p.closeBlock(l.CloseBreak, inner)
		}
	}
	p.closeBlock(pr.CloseBreak, indent)
}

func formatOperator(op *OperatorsRuleOperator) string {
	s := op.Kind + " "
	if op.Assoc != "" {
		s += op.Assoc + " "
	}
	s += FormatExpr(op.Expr)
	if op.Action != nil {
		s += " => " + FormatTerm(op.Action)
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
		j, w := i, 0
		for j < len(ls) && ls[j].field && ls[j].indent == ls[i].indent {
			w = max(w, utf8.RuneCountInString(ls[j].name))
			j++
		}
		for _, l := range ls[i:j] {
			l.text = pad(l.name, w) + " " + l.text
		}
		i = max(j, i+1)
	}
	// Align the trailing comments of consecutive lines.
	for i := 0; i < len(ls); {
		j, w := i, 0
		for j < len(ls) && ls[j].comment != "" && ls[j].indent == ls[i].indent {
			w = max(w, utf8.RuneCountInString(ls[j].text))
			j++
		}
		for _, l := range ls[i:j] {
			l.text = pad(l.text, w) + " " + l.comment
		}
		i = max(j, i+1)
	}
	var b strings.Builder
	for _, l := range ls {
		if !l.blank {
			b.WriteString(strings.TrimRight(l.indent+l.text, " "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", w-utf8.RuneCountInString(s))
}

// FormatType returns t in PEGO syntax.
func FormatType(t TypeExpr) string {
	switch t := t.(type) {
	case *TypeGroup:
		return formatSourceGroup(FormatType(t.Type), t.OpenBreak, t.BodyBreak, t.CloseBreak, t.OneLine)
	case *TypeRef:
		return t.Name
	case *ListType:
		return "[]" + formatTypeTerm(t.Elem)
	case *OptionalType:
		return "*" + formatTypeTerm(t.Elem)
	case *UnionType:
		parts := make([]string, len(t.Types))
		for i, x := range t.Types {
			parts[i] = formatTypeTerm(x)
		}
		return strings.Join(parts, " | ")
	}
	return "?"
}

func formatTypeTerm(t TypeExpr) string {
	if _, ok := t.(*UnionType); ok {
		return "(" + FormatType(t) + ")"
	}
	return FormatType(t)
}

// formatSourceGroup keeps explicit delimiters and their boundary trivia.
func formatSourceGroup(body string, open, first, close *LineBreak, oneLine bool) string {
	if oneLine {
		leading, ok1 := groupInlineComments(open, true)
		opening, ok0 := groupInlineComments(open, false)
		before, ok2 := groupInlineComments(first, false)
		after, ok3 := groupInlineComments(first, true)
		closing, ok4 := groupInlineComments(close, false)
		if ok0 && ok1 && ok2 && ok3 && ok4 && (close == nil || close.Trailing == nil) {
			return "(" + leading + opening + before + body + after + closing + ")"
		}
	}
	p := &printer{}
	p.write("(")
	p.inline(open)
	p.start = true
	p.comments(open, indentUnit, false)
	p.lineBreak(first, indentUnit, false)
	p.write(strings.ReplaceAll(body, "\n", "\n"+indentUnit))
	p.comments(close, indentUnit, true)
	p.newLine("")
	p.write(")")
	p.inline(close)
	p.flush()
	return strings.TrimSuffix(p.String(), "\n")
}

func groupInlineComments(b *LineBreak, trailing bool) (string, bool) {
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
		if c.Header.IsValid() {
			continue
		}
		if !strings.HasPrefix(c.Text, "/*") || strings.ContainsAny(c.Text, "\r\n") {
			return "", false
		}
		s.WriteByte(' ')
		s.WriteString(c.Text)
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
func FormatExpr(e Expr) string { return formatExpr(e, precChoice) }

func paren(s string, inner, outer int) string {
	if inner < outer {
		return "(" + s + ")"
	}
	return s
}

func formatExpr(e Expr, outer int) string {
	switch e := e.(type) {
	case *InlineAction:
		action := "..."
		if !e.DefaultAction {
			action = FormatTerm(e.Action)
		}
		return "(" + FormatExpr(e.Expr) + " => " + action + ")"
	case *Choice:
		parts := make([]string, len(e.Alts))
		for i, a := range e.Alts {
			parts[i] = formatExpr(a, precSeq)
		}
		return paren(strings.Join(parts, " / "), precChoice, outer)
	case *Seq:
		parts := make([]string, len(e.Items))
		for i, it := range e.Items {
			parts[i] = formatExpr(it, precPrefix)
		}
		return paren(strings.Join(parts, " "), precSeq, outer)
	case *Capture:
		return paren(e.Name+":"+formatExpr(e.Expr, precPrefix), precPrefix, outer)
	case *And:
		return paren("&"+formatExpr(e.Expr, precPrefix), precPrefix, outer)
	case *Not:
		return paren("!"+formatExpr(e.Expr, precPrefix), precPrefix, outer)
	case *Atomic:
		return paren(formatExpr(e.Expr, precSuffix)+" #atomic", precSuffix, outer)
	case *Discard:
		return paren(joinMinusTokens("-", formatExpr(e.Expr, precPrefix)), precPrefix, outer)
	case *Repeat:
		s := formatExpr(e.Expr, precSuffix)
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
		return s
	case *Optional:
		return formatExpr(e.Expr, precSuffix) + "?"
	case *Stream:
		return formatExpr(e.Expr, precSuffix) + " #stream"
	case *Recovery:
		return formatExpr(e.Expr, precSuffix) + " #recover(skip=" + FormatExpr(e.Skip) + ")"
	case *Attributed:
		s := formatExpr(e.Expr, precSuffix)
		for _, a := range e.Attrs {
			s += " " + formatAttribute(a)
		}
		return s
	case *Group:
		p := &printer{}
		p.group(e, "")
		p.flush()
		return strings.TrimSuffix(p.String(), "\n")
	case *Ref:
		if e.Level != "" {
			return "level(" + e.Name + ", " + e.Level + ")"
		}
		return e.Name
	case *Literal:
		return quote(e.Value)
	case *CharClass:
		return formatCharClass(e)
	case *Any:
		return "."
	case *Cut:
		return "--"
	case *Top:
		return "_"
	case *Bottom:
		return "_|_"
	case *BeginInput:
		return "^^"
	case *EndInput:
		return "$$"
	case *BeginLine:
		return "^"
	case *EndLine:
		return "$"
	case *Predicate:
		if block, ok := e.Term.(*Block); ok {
			return FormatTerm(block)
		}
		return FormatTerm(&Block{Statements: []Term{e.Term}, Breaks: e.Breaks, CloseBreak: e.CloseBreak, OneLine: e.OneLine || len(e.Breaks) == 0 && e.CloseBreak == nil})
	case *OperatorsRule:
		p := &printer{}
		p.operatorsRule(e, "")
		p.flush()
		return strings.TrimSuffix(p.String(), "\n")
	}
	return fmt.Sprintf("<%T>", e)
}

func formatAttribute(a *Attribute) string {
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
		if value == nil && len(a.Args) > 1 {
			args = append(args, "")
			continue
		}
		args = append(args, formatAttributeValue(value))
	}
	var keys []string
	seen := map[string]bool{}
	for _, key := range a.KeywordOrder {
		if _, exists := a.KwArgs[key]; exists && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	var remaining []string
	for key := range a.KwArgs {
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	keys = append(keys, remaining...)
	for _, key := range keys {
		args = append(args, key+"="+formatAttributeValue(a.KwArgs[key]))
	}
	return s + "(" + strings.Join(args, ", ") + ")"
}

func formatAttributeValue(value any) string {
	switch value := value.(type) {
	case nil:
		return "nil"
	case int:
		return strconv.Itoa(value)
	case string:
		return quote(value)
	case bool:
		return strconv.FormatBool(value)
	case Expr:
		return FormatExpr(value)
	}
	return fmt.Sprintf("<%T>", value)
}

// quote returns s as a PEGO string literal.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
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

func formatCharClass(e *CharClass) string {
	var b strings.Builder
	b.WriteString("[")
	if e.Negated {
		b.WriteString("^")
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
		b.WriteString(esc(rg.Lo))
		if rg.Hi != rg.Lo {
			b.WriteString("-" + esc(rg.Hi))
		}
	}
	b.WriteString("]")
	return b.String()
}

var termPrec = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3,
	"+": 4, "-": 4,
	"*": 5, "/": 5, "%": 5,
}

// FormatTerm returns the value expression t in PEGO syntax.
func FormatTerm(t Term) string { return formatTerm(t, 0) }

func formatTerm(t Term, outer int) string {
	switch t := t.(type) {
	case *TermGroup:
		return formatSourceGroup(FormatTerm(t.Term), t.OpenBreak, t.BodyBreak, t.CloseBreak, t.OneLine)
	case *Block:
		p := &printer{}
		if t.OneLine && noComments(t.CloseBreak) {
			plain := true
			for _, b := range t.Breaks {
				plain = plain && noComments(b)
			}
			if plain {
				parts := make([]string, len(t.Statements))
				for i, stmt := range t.Statements {
					parts[i] = FormatTerm(stmt)
				}
				return "{" + strings.Join(parts, "; ") + "}"
			}
		}
		p.openBlock()
		for i, stmt := range t.Statements {
			p.lineBreak(breakAt(t.Breaks, i), indentUnit, false)
			p.write(FormatTerm(stmt))
		}
		p.closeBlock(t.CloseBreak, "")
		p.flush()
		return strings.TrimSuffix(p.String(), "\n")
	case *IntLit:
		if t.Value == -int(^uint(0)>>1)-1 {
			// The absolute minimum cannot be a positive source token. Use
			// representable operands and honor the surrounding precedence.
			return formatTerm(&Binary{Op: "-", L: &IntLit{Value: t.Value + 1}, R: &IntLit{Value: 1}}, outer)
		}
		return strconv.Itoa(t.Value)
	case *StringLit:
		return quote(t.Value)
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
		return formatTerm(t.X, 7) + "." + t.Name
	case *New:
		fields := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = f.Name + ": " + FormatTerm(f.Value)
		}
		return t.Type + "{" + strings.Join(fields, ", ") + "}"
	case *ListConstructor:
		items := make([]string, len(t.Items))
		for i, item := range t.Items {
			items[i] = FormatTerm(item)
		}
		return "[]" + formatTypeTerm(t.Elem) + "{" + strings.Join(items, ", ") + "}"
	case *TerminalConstructor:
		args := make([]string, len(t.Args))
		for i, arg := range t.Args {
			args[i] = FormatTerm(arg)
		}
		return t.Type + "{" + strings.Join(args, ", ") + "}"
	case *Call:
		args := make([]string, len(t.Args))
		for i, a := range t.Args {
			args[i] = FormatTerm(a)
		}
		return t.Func + "(" + strings.Join(args, ", ") + ")"
	case *Lambda:
		s := "(" + strings.Join(t.Params, ", ") + ") => " + FormatTerm(t.Body)
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
		s := formatTerm(t.L, lp) + " " + t.Op + " " + formatTerm(t.R, p+1)
		if p < outer {
			return "(" + s + ")"
		}
		return s
	case *Unary:
		return joinMinusTokens(t.Op, formatTerm(t.X, 6))
	case *Assign:
		return t.Name + " := " + FormatTerm(t.Value)
	}
	return fmt.Sprintf("<%T>", t)
}

// header keeps forbidden comments inside the header, so formatting never
// changes a semantic placement error into valid source.
func (p *printer) header(word string, pos Pos) {
	p.write(word)
	for _, comment := range p.headers[pos] {
		p.write(" " + comment.Text)
		if strings.HasPrefix(comment.Text, "//") {
			p.newLine(indentUnit)
		}
	}
}
func (p *printer) operator(op *OperatorsRuleOperator) {
	p.header(op.Kind, op.Pos)
	p.write(" ")
	if op.Assoc != "" {
		p.write(op.Assoc + " ")
	}
	p.write(FormatExpr(op.Expr))
	if op.Action != nil {
		p.write(" => " + FormatTerm(op.Action))
	} else if op.DefaultAction {
		p.write(" => ...")
	}
}
