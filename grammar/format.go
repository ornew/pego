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
		p.lineBreak(ownedLead(&g.File.Pkg.SourceInfo, g.File.Pkg.Break, false), "", false)
		p.header("package", g.File.Pkg.Pos, g.File.Pkg.HeaderComments)
		p.write(" " + g.File.Pkg.Name)
		p.inline(ownedTrailing(&g.File.Pkg.SourceInfo, g.File.Pkg.Break))
	}
	for _, s := range g.File.Decls {
		pollFormat(control)
		switch s := s.(type) {
		case *TypeDef:
			p.lineBreak(ownedLead(&s.SourceInfo, s.Break, true), "", true)
			p.typeDef(s)
			p.inline(ownedTrailing(&s.SourceInfo, s.Break))
		case *TypeAlias:
			p.lineBreak(ownedLead(&s.SourceInfo, s.Break, true), "", true)
			p.typeAlias(s)
			p.inline(ownedTrailing(&s.SourceInfo, s.Break))
		case *VarDef:
			p.lineBreak(ownedLead(&s.SourceInfo, s.Break, true), "", true)
			p.varDef(s)
			p.inline(ownedTrailing(&s.SourceInfo, s.Break))
		case *RuleDef:
			p.lineBreak(ownedLead(&s.SourceInfo, s.Break, true), "", true)
			p.ruleDef(s)
			p.inline(ownedTrailing(&s.SourceInfo, s.Break))
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
	// keepFirst keeps a blank line before the first comment after the brace
	// that opens the block: the blank line makes the comment a detached one.
	keepFirst bool
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

// closeInline is inline for the break of a closing delimiter: a block comment
// followed by a token on its line stays before that token.
func (p *printer) closeInline(b *LineBreak) {
	if c := b.inlineBlock(); c != nil {
		p.write(" " + c.Text)
		return
	}
	p.inline(b)
}

func (p *printer) newLine(indent string) {
	p.flush()
	p.cur = &line{indent: indent}
	p.lines = append(p.lines, p.cur)
	p.start = false
}

func (p *printer) blankLine() {
	if p.start && !p.keepStart && !p.keepFirst || len(p.lines) == 0 || p.lines[len(p.lines)-1].blank {
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
	if c := b.leadBlock(); c != nil && !strings.ContainsAny(c.Text, "\r\n") {
		return c
	}
	return nil
}

// leadBlock returns the trailing comment of b if it is a block comment, over
// one line or several, that a token follows on its last line.
func (b *LineBreak) leadBlock() *Comment {
	if b == nil || b.Trailing == nil || b.TrailingEnds || b.Trailing.Header.IsValid() {
		return nil
	}
	if c := b.Trailing; strings.HasPrefix(c.Text, "/*") {
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

// last returns the comment that trails the last token before b.
func (b *LineBreak) last() *Comment {
	if b == nil || b.Last == nil || b.Last.Header.IsValid() {
		return nil
	}
	return b.Last
}

// noComments reports whether b holds no comments before its node and no
// blank line, so that the node can stay on the current line.
func noComments(b *LineBreak) bool {
	return b == nil || len(b.Comments) == 0 && !b.Blank
}

// openBlock writes the brace that opens a block whose first line starts with
// the break first.
func (p *printer) openBlock(first *LineBreak) {
	p.write("{")
	p.start = true
	p.keepFirst = first != nil && len(first.Comments) > 0 && first.Comments[0].Blank
}

// closeBlock writes the comments before a closing brace and the brace.
func (p *printer) closeBlock(b *LineBreak, indent string) {
	p.comments(b, indent+indentUnit, true)
	p.newLine(indent)
	p.write("}")
	p.inline(b)
}

// fieldComments returns the comments to write between the name and the type of a field: those that
// it owns.
func fieldComments(f *Field) string {
	return inlineHeaderComments(f.HeaderComments)
}

func (p *printer) typeDef(t *TypeDef) {
	pollFormat(p.control)
	p.header("type", t.Pos, t.HeaderComments)
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
	p.header("type", t.Pos, t.HeaderComments)
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
		empty = empty && noComments(closeBreak) && !ownsComments(&fields[0].SourceInfo, fields[0].Break)
		for _, f := range fields[1:] {
			pollFormat(p.control)
			empty = empty && noComments(f.Break) && !ownsComments(&f.SourceInfo, f.Break)
		}
	case oneLine:
		lead, empty = inlineLead(closeBreak, Pos{})
	default:
		empty = noComments(closeBreak)
		for _, f := range fields {
			pollFormat(p.control)
			empty = empty && noComments(f.Break) && !ownsComments(&f.SourceInfo, f.Break)
		}
	}
	if empty && (len(fields) == 0 || oneLine) {
		parts := make([]string, len(fields))
		for i, f := range fields {
			pollFormat(p.control)
			parts[i] = f.Name + " " + fieldComments(f) + formatType(f.Type, p.control)
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
	var first *LineBreak
	if len(fields) > 0 {
		first = ownedLead(&fields[0].SourceInfo, fields[0].Break, false)
	}
	p.openBlock(first)
	for _, f := range fields {
		pollFormat(p.control)
		p.lineBreak(ownedLead(&f.SourceInfo, f.Break, false), indent+indentUnit, false)
		p.cur.field = true
		p.cur.name = f.Name
		// A type over several lines continues at the field's indentation.
		p.write(indentTypeLines(fieldComments(f)+formatType(f.Type, p.control), indent+indentUnit, p.control))
		p.inline(ownedTrailing(&f.SourceInfo, f.Break))
	}
	p.closeBlock(closeBreak, indent)
}

func (p *printer) varDef(v *VarDef) {
	pollFormat(p.control)
	p.header("var", v.Pos, v.HeaderComments)
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
	// Parsed source always has a KeywordBreak; without one, the attributes
	// write the comments they own, as a grammar decoded from JSON has them.
	owned := r.KeywordBreak == nil
	trailed := false
	for i, a := range r.Attrs {
		pollFormat(p.control)
		if i > 0 {
			b := breakAt(r.AttrBreaks, i)
			if inline := inlineOwnedComment(&a.SourceInfo); b == nil && owned && !trailed && inline != nil {
				// A lone block comment between two attributes of a line is
				// detached from both, and stays between them.
				p.write(" " + inline.Text + " ")
			} else {
				if b == nil && owned && (trailed || a.LeadingComments != "" || len(a.LeadingDetachedComments) > 0) {
					// An attribute that owns leading comments, or follows a
					// trailing comment, starts a line.
					b = ownedLeadFrom(&a.SourceInfo, false)
				}
				if b != nil {
					p.lineBreak(b, "", false)
				} else {
					p.write(" ")
				}
			}
		}
		p.write(attributeGap(a, p.control) + indentTermLines(formatAttribute(a, p.control), "", p.control))
		if owned {
			t := ownedTrailing(&a.SourceInfo, nil)
			p.inline(t)
			trailed = t != nil
		}
	}
	if len(r.Attrs) > 0 {
		b := r.KeywordBreak
		if owned && (r.KeywordComments != "" || len(r.KeywordDetachedComments) > 0) {
			b = ownedLeadFrom(&SourceInfo{LeadingComments: r.KeywordComments, LeadingDetachedComments: r.KeywordDetachedComments}, false)
		}
		p.lineBreak(b, "", false)
	}
	p.header("rule", r.Diagnostic(), r.HeaderComments)
	p.write(" " + r.Name)
	if r.Type != nil {
		p.write(" " + formatType(r.Type, p.control))
	}
	p.write(" = ")
	if pr, attributed := RuleOperators(r.Expr); pr != nil {
		p.write("operators ")
		p.operatorsRule(pr, "")
		if attributed != nil {
			for _, a := range attributed.Attrs {
				pollFormat(p.control)
				p.write(" " + attributeGap(a, p.control) + formatAttribute(a, p.control))
			}
		}
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
		if b := breakAt(c.Breaks, i); b != nil && !(emptyBreak(b) && endsWithGap(c.Alts[i-1])) {
			p.lineBreak(b, indent, false)
		} else {
			// A comment before "/" on its line stays before it: on a line of its
			// own it would belong to the alternative before.
			p.write(" ")
			p.inline(breakAt(c.Breaks, i))
		}
		if g := leadGap(a); g != nil {
			// Comments between "/" and the alternative keep their lines.
			s := indentTermLines(gapString(g, " ", p.control), indent+"  ", p.control)
			if strings.HasSuffix(s, "\n") {
				s += indent + "  "
			}
			p.write("/" + s)
		} else {
			p.write("/ ")
		}
		// Continuation lines of an alternative line up after "/ ".
		p.sequence(a, indent+"  ", precSeq)
	}
	if l := nodeLayout(c); l != nil {
		p.write(gapAfter(l.After, p.control))
	}
}

// emptyBreak reports whether b only starts a line, perhaps with a comment that
// ends it.
func emptyBreak(b *LineBreak) bool {
	return !b.Blank && len(b.Comments) == 0
}

// endsWithGap reports whether e is followed by comments written on its line
// that precede the next token.
func endsWithGap(e Expr) bool {
	if l := nodeLayout(e); l != nil && len(l.After) > 0 {
		return true
	}
	if s, ok := e.(*Seq); ok && len(s.Items) > 0 {
		return endsWithGap(s.Items[len(s.Items)-1])
	}
	return false
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
		if l := nodeLayout(e); l != nil {
			p.write(gapAfter(l.After, p.control))
		}
	default:
		p.item(e, indent)
	}
}

// item writes an item of a top-level sequence of a rule body, keeping the
// line breaks before its attributes and the comments around it.
func (p *printer) item(e Expr, indent string) {
	pollFormat(p.control)
	l := nodeLayout(e)
	if l != nil {
		p.write(gapBefore(l.Before, p.control))
	}
	p.itemNode(e, indent)
	if l != nil {
		p.write(gapAfter(l.After, p.control))
	}
}

func (p *printer) itemNode(e Expr, indent string) {
	pollFormat(p.control)
	switch e := e.(type) {
	case *Group:
		p.group(e, indent)
	case *Capture:
		p.write(e.Name + ":")
		p.operand(e.Expr, indent)
	case *And:
		p.write("&")
		p.operand(e.Expr, indent)
	case *Not:
		p.write("!")
		p.operand(e.Expr, indent)
	case *Atomic:
		p.suffixOperand(e.Expr, indent)
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write(gapBefore(operatorLayout(e), p.control) + "#atomic")
	case *Stream:
		p.suffixOperand(e.Expr, indent)
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		p.write(gapBefore(operatorLayout(e), p.control) + "#stream")
	case *Recovery:
		p.suffixOperand(e.Expr, indent)
		if e.Break != nil {
			p.lineBreak(e.Break, indent, false)
		} else {
			p.write(" ")
		}
		// Lines of the parentheses are indented, and the closing one is not.
		inner := indentTermLines(recoveryHead(e, p.control)+formatExpr(e.Skip, precChoice, p.control)+recoveryClose(e, p.control), indent+indentUnit, p.control)
		if strings.HasSuffix(inner, "\n") {
			inner += indent
		}
		p.write(gapBefore(operatorLayout(e), p.control) + "#recover" + inner + ")")
	case *Repeat:
		if e.PragmaBreak == nil && hasBreak(e.Expr) {
			p.item(e.Expr, indent)
			p.write(indentTermLines(repeatSuffix(e, p.control), indent, p.control))
			break
		}
		if e.PragmaBreak == nil {
			p.write(indentTermLines(formatExprNode(e, precPrefix, p.control), indent, p.control))
			break
		}
		p.suffixOperand(e.Expr, indent)
		p.lineBreak(e.PragmaBreak, indent, false)
		p.write(indentTermLines(formatRepeatPragma(e, p.control), indent, p.control))
	case *Optional:
		if hasBreak(e.Expr) {
			p.item(e.Expr, indent)
			p.write(indentTermLines(suffixGap(e, p.control)+"?", indent, p.control))
			break
		}
		p.write(indentTermLines(formatExprNode(e, precPrefix, p.control), indent, p.control))
	case *Discard:
		p.write("-")
		p.operand(e.Expr, indent)
	case *Attributed:
		p.suffixOperand(e.Expr, indent)
		for i, a := range e.Attrs {
			pollFormat(p.control)
			if b := breakAt(e.Breaks, i); b != nil {
				p.lineBreak(b, indent, false)
			} else {
				p.write(" ")
			}
			// The lines of an attribute with a trailing comma continue at the indentation of its line.
			p.write(attributeGap(a, p.control) + indentTermLines(formatAttribute(a, p.control), indent, p.control))
		}
	default:
		p.write(formatExprNode(e, precPrefix, p.control))
	}
}

// suffixOperand writes the operand of a pragma. A group, or an operand that
// holds line breaks, is written by this printer, so that the comments at the
// end of a line stay at the end of that line.
func (p *printer) suffixOperand(e Expr, indent string) {
	if _, group := e.(*Group); group || hasBreak(e) {
		p.item(e, indent)
		return
	}
	p.write(formatExpr(e, precSuffix, p.control))
}

// operand writes the operand of a label or a prefix operator, after the
// comments between the two.
func (p *printer) operand(e Expr, indent string) {
	if g := leadGap(e); g != nil {
		s := indentTermLines(gapString(g, " ", p.control), indent, p.control)
		if strings.HasSuffix(s, "\n") {
			s += indent
		}
		p.write(s)
	}
	p.item(e, indent)
}

func (p *printer) group(g *Group, indent string) {
	pollFormat(p.control)
	lead, plain := "", false
	if g.OneLine && (g.BodyBreak == nil || g.BodyBreak.Trailing == nil) {
		lead, plain = inlineLead(g.BodyBreak, exprPos(g.Expr))
	}
	last := g.CloseBreak.last()
	if last != nil && (!strings.HasPrefix(last.Text, "/*") || strings.ContainsAny(last.Text, "\r\n")) {
		plain = false // only a block comment over one line stays before the ")"
	}
	if plain && noComments(g.CloseBreak) && p.tryOneLine(func() {
		p.write("(" + lead)
		p.groupBody(g.Expr, indent)
		if last != nil {
			p.write(" " + last.Text)
		}
		p.write(")")
		p.closeInline(g.CloseBreak)
	}) {
		return
	}
	p.write("(")
	p.lineBreak(g.BodyBreak, indent+indentUnit, false)
	p.groupBody(g.Expr, indent+indentUnit)
	if c := g.CloseBreak.last(); c != nil {
		p.trailing = append(p.trailing, c)
	}
	p.comments(g.CloseBreak, indent+indentUnit, false)
	p.newLine(indent)
	p.write(")")
	p.closeInline(g.CloseBreak)
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
		arrow := indentTermLines(actionArrow(action, p.control), indent, p.control)
		if strings.HasSuffix(arrow, "\n=>") {
			arrow = arrow[:len(arrow)-2] + indent + "=>"
		}
		if action.DefaultAction {
			p.write(arrow + " ...")
		} else {
			p.write(joinLayout(arrow+" ", indentTermLines(formatTerm(action.Action, 0, p.control), indent, p.control)))
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
	var first *LineBreak
	switch {
	case pr.Skip != nil:
		first = ownedLead(&pr.SkipSource, pr.SkipBreak, false)
	case len(pr.Operands) > 0:
		first = ownedLead(&pr.Operands[0].SourceInfo, pr.Operands[0].Break, false)
	case len(pr.Levels) > 0:
		first = ownedLead(&pr.Levels[0].SourceInfo, pr.Levels[0].Break, false)
	}
	p.openBlock(first)
	if pr.Skip != nil {
		p.lineBreak(ownedLead(&pr.SkipSource, pr.SkipBreak, false), inner, false)
		p.write("skip " + formatExpr(pr.Skip, precChoice, p.control))
		p.inline(ownedTrailing(&pr.SkipSource, pr.SkipBreak))
	}
	for _, o := range pr.Operands {
		pollFormat(p.control)
		p.lineBreak(ownedLead(&o.SourceInfo, o.Break, false), inner, false)
		p.write("operand " + formatExpr(o.Expr, precChoice, p.control))
		if o.Action != nil {
			p.write(joinLayout(" => ", indentTermLines(formatTerm(o.Action, 0, p.control), inner, p.control)))
		} else if o.DefaultAction {
			p.write(" => ...")
		}
		p.inline(ownedTrailing(&o.SourceInfo, o.Break))
	}
	for _, l := range pr.Levels {
		pollFormat(p.control)
		p.lineBreak(ownedLead(&l.SourceInfo, l.Break, false), inner, false)
		p.header("level", l.Pos, nil)
		p.write(" ")
		if l.Name != "" {
			p.write(l.Name + " ")
		}
		empty := noComments(l.CloseBreak)
		for _, op := range l.Operators {
			pollFormat(p.control)
			empty = empty && noComments(op.Break) && !ownsComments(&op.SourceInfo, op.Break) && (op.Break == nil || op.Break.Trailing == nil)
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
			var first *LineBreak
			if len(l.Operators) > 0 {
				first = ownedLead(&l.Operators[0].SourceInfo, l.Operators[0].Break, false)
			}
			p.openBlock(first)
			for _, op := range l.Operators {
				pollFormat(p.control)
				p.lineBreak(ownedLead(&op.SourceInfo, op.Break, false), inner+indentUnit, false)
				p.operator(op, inner+indentUnit)
				p.inline(ownedTrailing(&op.SourceInfo, op.Break))
			}
			p.closeBlock(l.CloseBreak, inner)
		}
		p.inline(ownedTrailing(&l.SourceInfo, l.Break))
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
			oneLine = oneLine && (f.Break == nil || f.Break.Trailing == nil) && !ownsComments(&f.SourceInfo, f.Break)
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
	// A body over several lines is read back as a group over several lines, so
	// it gets the layout of one.
	if oneLine && !hasFormatNewline(body, control) {
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
	p.start = true
	if c := open.leadBlock(); c != nil && body != "" {
		// A block comment before the first token on the line of the delimiter
		// leads that token: it goes on a line before it, not at the end of the
		// line of the delimiter, where it would trail the delimiter.
		p.commentLine(indentUnit, c)
	} else {
		p.inline(open)
	}
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

// formatExpr returns e with the comments that stand before and after it on one
// line with its tokens.
func formatExpr(e Expr, outer int, control *metadataControl) string {
	s := formatExprNode(e, outer, control)
	if l := nodeLayout(e); l != nil {
		s = gapBefore(l.Before, control) + s + gapAfter(l.After, control)
	}
	return s
}

// gapBefore returns the comments of an inline gap with a space after each, to
// write before the token that follows the gap.
func gapBefore(cs []*Comment, control *metadataControl) string {
	var b strings.Builder
	for _, c := range cs {
		pollFormat(control)
		writeFormatString(&b, c.Text+" ", control)
	}
	return b.String()
}

// gapAfter returns the comments of an inline gap with a space before each, to
// write after the token that precedes the gap.
func gapAfter(cs []*Comment, control *metadataControl) string {
	var b strings.Builder
	for _, c := range cs {
		pollFormat(control)
		writeFormatString(&b, " "+c.Text, control)
	}
	return b.String()
}

// operatorLayout returns the comments between the operand of e and its
// operator.
func operatorLayout(e any) []*Comment {
	if l := nodeLayout(e); l != nil {
		return l.Operator
	}
	return nil
}

func formatExprNode(e Expr, outer int, control *metadataControl) string {
	pollFormat(control)
	switch e := e.(type) {
	case *InlineAction:
		action := actionArrow(e, control) + " ..."
		if !e.DefaultAction {
			action = joinLayout(actionArrow(e, control)+" ", formatTerm(e.Action, 0, control))
		}
		return finishFormat("("+formatExpr(e.Expr, precChoice, control)+action+")", control)
	case *Choice:
		parts := make([]string, len(e.Alts))
		for i, a := range e.Alts {
			pollFormat(control)
			parts[i] = formatExpr(a, precSeq, control)
			if i > 0 {
				if g := leadGap(a); g != nil {
					parts[i] = "/" + gapString(g, " ", control) + parts[i]
				} else {
					parts[i] = "/ " + parts[i]
				}
				if l := nodeLayout(a); l != nil && l.Sep != nil {
					// Comments before the "/" keep their lines.
					parts[i] = gapString(l.Sep, " ", control) + parts[i]
				} else {
					parts[i] = " " + parts[i]
				}
			}
		}
		return finishFormat(paren(joinFormatParts(parts, "", control), precChoice, outer), control)
	case *Seq:
		parts := make([]string, len(e.Items))
		for i, it := range e.Items {
			pollFormat(control)
			parts[i] = formatExpr(it, precPrefix, control)
		}
		return finishFormat(paren(joinFormatParts(parts, " ", control), precSeq, outer), control)
	case *Capture:
		return finishFormat(paren(e.Name+":"+leadSep(e.Expr, control)+formatExpr(e.Expr, precPrefix, control), precPrefix, outer), control)
	case *And:
		return finishFormat(paren("&"+leadSep(e.Expr, control)+formatExpr(e.Expr, precPrefix, control), precPrefix, outer), control)
	case *Not:
		return finishFormat(paren("!"+leadSep(e.Expr, control)+formatExpr(e.Expr, precPrefix, control), precPrefix, outer), control)
	case *Atomic:
		return finishFormat(paren(formatExpr(e.Expr, precSuffix, control)+gapAfter(operatorLayout(e), control)+" #atomic", precSuffix, outer), control)
	case *Discard:
		return finishFormat(paren(joinMinusTokens("-", leadSep(e.Expr, control)+formatExpr(e.Expr, precPrefix, control)), precPrefix, outer), control)
	case *Repeat:
		return finishFormat(formatExpr(e.Expr, precSuffix, control)+repeatSuffix(e, control), control)
	case *Optional:
		return finishFormat(formatExpr(e.Expr, precSuffix, control)+suffixGap(e, control)+"?", control)
	case *Stream:
		return finishFormat(formatExpr(e.Expr, precSuffix, control)+gapAfter(operatorLayout(e), control)+" #stream", control)
	case *Recovery:
		return finishFormat(formatExpr(e.Expr, precSuffix, control)+gapAfter(operatorLayout(e), control)+" #recover"+recoveryHead(e, control)+formatExpr(e.Skip, precChoice, control)+recoveryClose(e, control)+")", control)
	case *Attributed:
		s := formatExpr(e.Expr, precSuffix, control)
		for _, a := range e.Attrs {
			pollFormat(control)
			s += " " + attributeGap(a, control) + formatAttribute(a, control)
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
		if e.IgnoreCase {
			return finishFormat(quote(e.Value, control)+"i", control)
		}
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
		return finishFormat(formatTerm(&Block{Statements: []Term{e.Term}, OpenBreak: e.OpenBreak, Breaks: e.Breaks, CloseBreak: e.CloseBreak, OneLine: e.OneLine || len(e.Breaks) == 0 && e.CloseBreak == nil}, 0, control), control)
	case *OperatorsRule:
		p := &printer{control: control}
		p.operatorsRule(e, "")
		p.flush()
		return finishFormat(strings.TrimSuffix(p.String(), "\n"), control)
	}
	return finishFormat(fmt.Sprintf("<%T>", e), control)
}

// repeatSuffix returns what follows the operand of e: the comments before the
// operator, and the operator.
func repeatSuffix(e *Repeat, control *metadataControl) string {
	switch {
	case !repeatHasComments(e) && e.Min == 0 && e.Max < 0:
		return suffixGap(e, control) + "*"
	case !repeatHasComments(e) && e.Min == 1 && e.Max < 0:
		return suffixGap(e, control) + "+"
	}
	return gapAfter(operatorLayout(e), control) + " " + formatRepeatPragma(e, control)
}

// hasBreak reports whether the operand e of a suffix holds a line break, or a
// comment at the end of a line, that only the printer writes.
func hasBreak(e Expr) bool {
	switch e := e.(type) {
	case *Group:
		return true // its closing delimiter may be followed by a comment ending the line
	case *Atomic:
		return e.Break != nil || hasBreak(e.Expr)
	case *Stream:
		return e.Break != nil || hasBreak(e.Expr)
	case *Recovery:
		return e.Break != nil || hasBreak(e.Expr)
	case *Attributed:
		return e.Breaks != nil || hasBreak(e.Expr)
	case *Repeat:
		return e.PragmaBreak != nil || hasBreak(e.Expr)
	case *Optional:
		return hasBreak(e.Expr)
	}
	return false
}

// suffixGap returns what stands between the operand of e and its "*", "+" or
// "?": the comments in its gap, and a space after them.
func suffixGap(e Expr, control *metadataControl) string {
	if l := nodeLayout(e); l != nil && l.OpGap != nil {
		return gapString(l.OpGap, " ", control)
	}
	return gapAfter(operatorLayout(e), control) + operatorSpace(e)
}

// operatorSpace is the space between inline comments and the operator after
// them.
func operatorSpace(e Expr) string {
	if len(operatorLayout(e)) > 0 {
		return " "
	}
	return ""
}

// joinsStatement reports whether the statement s of a block stays on the line
// of the semicolon before it: it has comments there, and nothing else precedes
// it on the line.
func joinsStatement(s Term, b *LineBreak) bool {
	l := termLayout(s)
	return l != nil && len(l.Before) > 0 && l.PreviousLine == l.NextLine && noComments(b)
}

// endsStatementGap reports whether the semicolon of the block statement s
// follows comments on its line, so that the semicolon stays after them.
func endsStatementGap(s Term) bool {
	l := termLayout(s)
	return l != nil && len(l.After) > 0
}

// arrow returns the "=>" of a lambda with the comments before it and the space
// after it.
func arrow(t *Lambda, control *metadataControl) string {
	if l := nodeLayout(t); l != nil && l.OpGap != nil {
		return gapString(l.OpGap, " ", control) + "=> "
	}
	return " => "
}

// actionArrow returns the "=>" of an inline action, with the comments before it
// and the space before them.
func actionArrow(a *InlineAction, control *metadataControl) string {
	if l := nodeLayout(a); l != nil && l.OpGap != nil {
		return gapString(l.OpGap, " ", control) + "=>"
	}
	return " =>"
}

// binaryGap returns what stands between the left operand of t and its operator.
func binaryGap(t *Binary, control *metadataControl) string {
	if l := nodeLayout(t); l != nil && l.OpGap != nil {
		return gapString(l.OpGap, " ", control)
	}
	return " "
}

// recoveryClose returns the comments before the closing parenthesis of e.
func recoveryClose(e *Recovery, control *metadataControl) string {
	if l := nodeLayout(e); l != nil && l.Close != nil {
		return gapString(l.Close, "", control)
	}
	return ""
}

// leadGap returns the comments between the "/" or "=" before e and its first
// token.
func leadGap(e Expr) *Gap {
	if l := nodeLayout(e); l != nil {
		return l.Lead
	}
	return nil
}

// recoveryHead returns what stands between "#recover" and the skip expression
// of e: "(skip=" with the comments of its gaps.
func recoveryHead(e *Recovery, control *metadataControl) string {
	s := "("
	l := nodeLayout(e)
	if l != nil && l.Open != nil {
		s += gapString(l.Open, " ", control)
	}
	s += "skip"
	if l != nil && l.Eq != nil {
		s += gapString(l.Eq, " ", control)
	}
	return s + "=" + leadSep(e.Skip, control)
}

// leadSep returns what stands between the "=" after "skip" and e.
func leadSep(e Expr, control *metadataControl) string {
	if g := leadGap(e); g != nil {
		return gapString(g, " ", control)
	}
	return ""
}

// gapString returns the comments of g with the white space around them: a
// comment on the line of the token before follows it after a space, a comment
// on a later line goes on a line of its own, and what follows the last comment
// is final when the token after is on the same line. A line comment always ends
// its line.
func gapString(g *Gap, final string, control *metadataControl) string {
	var b strings.Builder
	previous := g.PreviousLine
	newline := false
	lines := func(to int) {
		b.WriteByte('\n')
		if to-previous > 1 {
			b.WriteByte('\n')
		}
	}
	for _, c := range g.Comments {
		pollFormat(control)
		if c == nil {
			continue
		}
		switch {
		case newline:
			b.WriteByte('\n')
		case c.Pos.Line > previous:
			lines(c.Pos.Line)
		default:
			b.WriteByte(' ')
		}
		writeFormatString(&b, c.Text, control)
		previous = c.End.Line
		newline = strings.HasPrefix(c.Text, "//")
	}
	switch {
	case newline:
		lines(g.NextLine)
	case g.NextLine > previous:
		lines(g.NextLine)
	default:
		b.WriteString(final)
	}
	return b.String()
}

// attributeGap returns the comments before an attribute, each followed by a
// space.
func attributeGap(a *Attribute, control *metadataControl) string {
	if a.Layout != nil {
		return gapBefore(a.Layout.Before, control)
	}
	return ""
}

// Kinds of the tokens between the parentheses of #repeat(...), in the order in
// which they can be written.
const (
	repeatMin = iota
	repeatComma
	repeatMax
)

// repeatTokens returns the tokens of the canonical spelling of the bounds of e:
// #repeat(n) for exactly n, #repeat(n,) for at least n, #repeat(,m) for at most
// m and #repeat(n, m) for the others.
func repeatTokens(e *Repeat) (tokens []string, kinds []int) {
	switch {
	case e.Min == e.Max:
		return []string{strconv.Itoa(e.Min)}, []int{repeatMin}
	case e.Max < 0:
		return []string{strconv.Itoa(e.Min), ","}, []int{repeatMin, repeatComma}
	case e.Min == 0:
		return []string{",", strconv.Itoa(e.Max)}, []int{repeatComma, repeatMax}
	}
	return []string{strconv.Itoa(e.Min), ",", strconv.Itoa(e.Max)}, []int{repeatMin, repeatComma, repeatMax}
}

// repeatGaps returns the comments of e.Spelling in the gaps of the canonical
// spelling: len(kinds)+1 gaps, one before each token and one before the closing
// parenthesis. A comment stays before the token it was before, or before the
// next token of the canonical spelling that comes after it, or else at the end.
func repeatGaps(e *Repeat, kinds []int, control *metadataControl) []RepeatGap {
	gaps := make([]RepeatGap, len(kinds)+1)
	sp := e.Spelling
	if sp == nil {
		return gaps
	}
	var written []int
	if sp.HasMin {
		written = append(written, repeatMin)
	}
	if sp.Comma {
		written = append(written, repeatComma)
	}
	if sp.HasMax {
		written = append(written, repeatMax)
	}
	for j, g := range sp.Gaps {
		pollFormat(control)
		if len(g.Comments) == 0 {
			continue
		}
		to := len(kinds)
		if j < len(written) {
			for t, k := range kinds {
				if k >= written[j] {
					to = t
					break
				}
			}
		}
		if len(gaps[to].Comments) == 0 {
			gaps[to].PrevLine = g.PrevLine
		}
		gaps[to].Comments = append(gaps[to].Comments, g.Comments...)
		gaps[to].NextLine = g.NextLine
	}
	return gaps
}

// repeatHasComments reports whether e has comments between the tokens of its
// pragma.
func repeatHasComments(e *Repeat) bool {
	if e.Spelling != nil {
		for _, g := range e.Spelling.Gaps {
			if len(g.Comments) > 0 {
				return true
			}
		}
	}
	return false
}

// formatRepeatPragma returns the pragma #repeat(...) that repeats e. Its bounds
// are written in the canonical spelling, whichever the source used, with the
// comments between its tokens where the canonical spelling has them. The comma
// is part of the syntax, so it is never a list separator and the pragma has no
// trailing comma. A comment that ends its line puts the tokens on lines.
func formatRepeatPragma(e *Repeat, control *metadataControl) string {
	pollFormat(control)
	tokens, kinds := repeatTokens(e)
	gaps := repeatGaps(e, kinds, control)
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
	if len(a.Args) == 0 && len(a.KwArgs) == 0 && (a.ArgsLayout == nil || staleListComments(a.ArgsLayout, 0, control) == nil) {
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
		list = staleListComments(list, len(args), control) // the arguments changed since the source was read
	}
	if len(args) == 0 && (list == nil || !listHasComments(list.OpenBreak, list.BodyBreak, list.CloseBreak)) {
		return s
	}
	return s + formatItemList(args, a.TrailingComma, list, control)
}

// staleListComments returns a layout that holds every comment of list after the
// opening parenthesis, in source order, for a list of n items whose items changed since
// the source was read: the comments no longer belong to any item, but Format
// does not drop a comment.
func staleListComments(list *ListLayout, n int, control *metadataControl) *ListLayout {
	var cs []*Comment
	add := func(b *LineBreak) {
		pollFormat(control)
		if b != nil {
			cs = appendMetadata(cs, b.Comments, control)
			if b.Trailing != nil {
				cs = append(cs, b.Trailing)
			}
		}
	}
	add(list.OpenBreak)
	add(list.BodyBreak)
	for _, e := range list.Elems {
		pollFormat(control)
		if e != nil {
			cs = appendMetadata(cs, e.Before, control)
			cs = appendMetadata(cs, e.After, control)
			cs = appendMetadata(cs, e.Trail, control)
		}
	}
	add(list.CloseBreak)
	if len(cs) == 0 {
		return nil
	}
	return &ListLayout{OpenBreak: &LineBreak{Comments: cs}, Elems: make([]*SourceLayout, n)}
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

// leadingComments returns the layout of the first term along the left spine of
// t that holds comments before it. An index, a call or a binary expression
// starts where its left operand does, but the comments may sit on any of them.
func leadingComments(t Term) *SourceLayout {
	for {
		if layout := termLayout(t); layout != nil && len(layout.Before) > 0 {
			return layout
		}
		switch x := t.(type) {
		case *Binary:
			t = x.L
		case *Cast:
			t = x.X
		case *Member:
			t = x.X
		default:
			return nil
		}
	}
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
			if n := len(t.Statements); n > 0 && endsStatementGap(t.Statements[n-1]) {
				plain = false // the semicolon after the comments ends the statement
			}
			for i, part := range parts {
				// A statement over several lines breaks the block too.
				plain = plain && !hasFormatNewline(part, control)
				if i > 0 {
					parts[i] = strings.TrimLeft(part, " ")
				}
			}
			if plain {
				return "{" + lead + joinFormatParts(parts, "; ", control) + "}"
			}
		}
		// A blank line before the first comment decides what it belongs to.
		if b := breakAt(t.Breaks, 0); b != nil && len(b.Comments) > 0 && b.Comments[0].Blank {
			p.keepStart = true
		}
		p.openBlock(breakAt(t.Breaks, 0))
		p.inline(t.OpenBreak)
		for i := range t.Statements {
			pollFormat(control)
			if i > 0 && joinsStatement(t.Statements[i], breakAt(t.Breaks, i)) {
				// Comments between the semicolon and the statement keep the
				// statement on their line.
				p.write("; " + indentTermLines(strings.TrimLeft(parts[i], " "), indentUnit, control))
				p.inline(breakAt(t.Breaks, i))
				continue
			}
			if i > 0 && endsStatementGap(t.Statements[i-1]) {
				p.write(";")
			}
			p.lineBreak(breakAt(t.Breaks, i), indentUnit, false)
			p.write(indentTermLines(parts[i], indentUnit, control))
		}
		if n := len(t.Statements); n > 0 && endsStatementGap(t.Statements[n-1]) {
			p.write(";")
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
		body := formatTerm(t.Body, 0, control)
		if layout := leadingComments(t.Body); layout != nil && endsLine(layout.Before[len(layout.Before)-1], layout.NextLine) {
			// Comments between "=>" and the body that end a line put the body on
			// a line of its own, one level in from the parameters.
			body = indentTermLines(body, indentUnit, control)
		}
		s := formatItemList(t.Params, t.TrailingComma, list, control) + joinLayout(arrow(t, control), body)
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
			// The index collapses to one line unless its comments need lines.
			return l + formatSourceDelimited(formatTerm(t.R, 0, control), "[", "]", t.OpenBreak, t.BodyBreak, t.CloseBreak, true, control)
		}
		p := termPrec[t.Op]
		// Operators are left-associative, so the right operand is
		// parenthesized even at the same precedence.
		lp := p
		if p == termPrec["=="] {
			lp++ // comparison operators do not chain
		}
		s := joinLayout(formatTerm(t.L, lp, control)+binaryGap(t, control)+t.Op+" ", formatTerm(t.R, p+1, control))
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
func (p *printer) header(word string, pos Pos, owned []string) {
	p.write(word)
	comments := p.headers[pos]
	if len(comments) == 0 {
		// A grammar decoded from JSON has no comment tokens; its declaration owns them.
		for _, text := range owned {
			pollFormat(p.control)
			comments = append(comments, ownedComments(text)...)
		}
	}
	for _, comment := range comments {
		pollFormat(p.control)
		p.write(" " + comment.Text)
		if strings.HasPrefix(comment.Text, "//") {
			p.newLine(indentUnit)
		}
	}
}
func (p *printer) operator(op *OperatorsRuleOperator, indent string) {
	pollFormat(p.control)
	p.header(op.Kind, op.Pos, nil)
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
	if strings.HasSuffix(separator, " ") && strings.HasPrefix(body, "\n") {
		separator = separator[:len(separator)-1] // no space at the end of a line
	}
	return separator + body
}
