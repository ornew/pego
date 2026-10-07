package grammar

import (
	"fmt"
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
	p := &printer{start: true}
	if g.Package != "" {
		p.lineBreak(g.PackageBreak, "", false)
		p.write("package " + g.Package)
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
}

func (p *printer) write(s string) {
	if p.cur == nil {
		p.newLine("")
	}
	p.cur.text += s
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
	if b != nil && b.Trailing != nil {
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
	p.write("type " + t.Name + " ")
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
			p.write("{ " + strings.Join(parts, ", ") + " }")
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
	p.write("def " + r.Name)
	if r.Type != nil {
		p.write(": " + FormatType(r.Type))
	}
	p.write(" =")
	body := indentUnit
	pratt := ""
	if r.BodyBreak != nil {
		p.lineBreak(r.BodyBreak, body, false)
		pratt = body
	} else {
		p.write(" ")
	}
	if pr, ok := r.Expr.(*Pratt); ok {
		p.pratt(pr, pratt)
	} else {
		p.body(r.Expr, body)
	}
	if r.Action != nil {
		if r.ActionBreak != nil {
			p.lineBreak(r.ActionBreak, body, false)
		} else {
			p.write(" ")
		}
		p.write("-> " + FormatTerm(r.Action))
	}
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
		p.write("@")
		p.item(e.Expr, indent)
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

func breakAt(bs []*LineBreak, i int) *LineBreak {
	if i < len(bs) {
		return bs[i]
	}
	return nil
}

func (p *printer) pratt(pr *Pratt, indent string) {
	inner := indent + indentUnit
	p.write("pratt ")
	p.openBlock()
	if pr.Skip != nil {
		p.lineBreak(pr.SkipBreak, inner, false)
		p.write("skip " + FormatExpr(pr.Skip))
	}
	for _, o := range pr.Operands {
		p.lineBreak(o.Break, inner, false)
		p.write("operand " + FormatExpr(o.Expr))
		if o.Action != nil {
			p.write(" -> " + FormatTerm(o.Action))
		}
	}
	for _, l := range pr.Levels {
		p.lineBreak(l.Break, inner, false)
		p.write("level ")
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
			for _, op := range l.Operators {
				p.write(" " + formatOperator(op))
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
				p.write(formatOperator(op))
			}
			p.closeBlock(l.CloseBreak, inner)
		}
	}
	p.closeBlock(pr.CloseBreak, indent)
}

func formatOperator(op *PrattOperator) string {
	s := op.Kind + " "
	if op.Assoc != "" {
		s += op.Assoc + " "
	}
	s += FormatExpr(op.Expr)
	if op.Action != nil {
		s += " -> " + FormatTerm(op.Action)
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
		return paren("@"+formatExpr(e.Expr, precPrefix), precPrefix, outer)
	case *Discard:
		return paren("-"+formatExpr(e.Expr, precPrefix), precPrefix, outer)
	case *Repeat:
		s := formatExpr(e.Expr, precSuffix)
		switch {
		case e.Min == 0 && e.Max < 0:
			s += "*"
		case e.Min == 1 && e.Max < 0:
			s += "+"
		case e.Max < 0:
			s += fmt.Sprintf("{%d,}", e.Min)
		case e.Min == e.Max:
			s += fmt.Sprintf("{%d}", e.Min)
		default:
			s += fmt.Sprintf("{%d,%d}", e.Min, e.Max)
		}
		return s
	case *Optional:
		return formatExpr(e.Expr, precSuffix) + "?"
	case *Attributed:
		s := formatExpr(e.Expr, precSuffix)
		for _, a := range e.Attrs {
			s += " " + formatAttribute(a)
		}
		return s
	case *Ref:
		if e.Level != "" {
			return e.Name + "(" + e.Level + ")"
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
		return "[" + FormatTerm(e.Term) + "]"
	case *Pratt:
		p := &printer{}
		p.pratt(e, "")
		p.flush()
		return strings.TrimSuffix(p.String(), "\n")
	}
	return fmt.Sprintf("<%T>", e)
}

func formatAttribute(a *Attribute) string {
	s := "#" + a.Name
	if len(a.Args) > 0 {
		args := make([]string, len(a.Args))
		for i, arg := range a.Args {
			args[i] = arg.Name + "=" + FormatExpr(arg.Value)
		}
		s += "(" + strings.Join(args, ", ") + ")"
	}
	return s
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
	b.WriteString("(?")
	if e.Negated {
		b.WriteString("^")
	}
	esc := func(r rune) string {
		switch r {
		case '-', '(', ')', '?', '^', '\\':
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
	b.WriteString(")")
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
	case *IntLit:
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
		return "new " + t.Type + "{" + strings.Join(fields, ", ") + "}"
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
		return t.Op + formatTerm(t.X, 6)
	case *Assign:
		return t.Name + " = " + FormatTerm(t.Value)
	}
	return fmt.Sprintf("<%T>", t)
}
