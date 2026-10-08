package cue

import (
	"fmt"
	"strconv"
	"strings"
)

// dump prints a file in the canonical form of the reference results (see internal/refgen/dump.go): a line
// of s-expressions (kind start end children...) with byte offsets, then the comments.
func dump(f *File, comments []Comment) string {
	var b strings.Builder
	d := &dumper{b: &b}
	b.WriteString("(file")
	for _, x := range f.Decls {
		b.WriteString(" ")
		d.decl(x)
	}
	b.WriteString(")\n(comments")
	for _, c := range comments {
		fmt.Fprintf(&b, " (%d %s)", c.Start, strconv.Quote(c.Text))
	}
	b.WriteString(")\n")
	return b.String()
}

type dumper struct {
	b *strings.Builder
}

func (d *dumper) w(format string, args ...any) { fmt.Fprintf(d.b, format, args...) }

func (d *dumper) open(kind string, x any) { d.w("(%s %d %d", kind, SpanOf(x).Start, nodeEnd(x)) }

// nodeEnd returns the end of a node as the reference implementation computes it (ast.Node.End): from the
// last child for the nodes that end with one, with the one oddity that the end of _|_ is one byte after its
// start. The end of the other nodes is the end of the span.
func nodeEnd(x any) int {
	switch x := x.(type) {
	case *Bottom:
		return x.Start + 1
	case *String:
		// The reference drops the carriage returns of a multi-line string from its text, and so shortens it.
		return x.Start + len(refText(x.Text))
	case *Interpolation:
		return nodeEnd(x.Elts[len(x.Elts)-1])
	case *BinaryExpr:
		return nodeEnd(x.Y)
	case *UnaryExpr:
		return nodeEnd(x.X)
	case *Alias:
		return nodeEnd(x.Expr)
	case *EmbedDecl:
		return nodeEnd(x.Expr)
	case *LetClause:
		return nodeEnd(x.Expr)
	case *IfClause:
		return nodeEnd(x.Condition)
	case *ForClause:
		return nodeEnd(x.Source)
	case *TryClause:
		if x.Expr != nil {
			return nodeEnd(x.Expr)
		}
	case *Ellipsis:
		if x.Type != nil {
			return nodeEnd(x.Type)
		}
	case *Field:
		if len(x.Attrs) == 0 {
			return nodeEnd(x.Value)
		}
	case *StructLit:
		// The struct of a field chain (a: b: c) has no braces: it ends with its field.
		if len(x.Elts) == 1 {
			if f, ok := x.Elts[0].(*Field); ok && f.Start == x.Start && f.End == x.End {
				return nodeEnd(f)
			}
		}
	}
	return SpanOf(x).End
}

func (d *dumper) decl(x Decl) {
	switch x := x.(type) {
	case *Package:
		d.open("package", x)
		d.w(" ")
		d.expr(x.Name)
		d.w(")")
	case *Attribute:
		d.open("attr", x)
		d.w(" %s)", strconv.Quote(x.Text))
	case *ImportDecl:
		d.open("import", x)
		if x.Lparen != nil {
			d.w(" paren")
		}
		for _, s := range x.Specs {
			d.w(" ")
			d.open("spec", s)
			if s.Name != nil {
				d.w(" ")
				d.expr(s.Name)
			}
			d.w(" ")
			d.expr(s.Path)
			d.w(")")
		}
		d.w(")")
	case *Field:
		d.open("field", x)
		switch {
		case x.Constraint == nil:
			d.w(" -")
		default:
			d.w(" %s", x.Constraint.Text)
		}
		d.w(" ")
		d.expr(x.Label)
		if x.Alias != nil {
			d.w(" ")
			d.postfixAlias(x.Alias)
		}
		d.w(" ")
		d.expr(x.Value)
		for _, a := range x.Attrs {
			d.w(" ")
			d.decl(a)
		}
		d.w(")")
	case *EmbedDecl:
		d.open("embed", x)
		d.w(" ")
		d.expr(x.Expr)
		d.w(")")
	case *LetClause:
		d.clause(x)
	case *Ellipsis:
		d.open("ellipsis", x)
		if x.Type != nil {
			d.w(" ")
			d.expr(x.Type)
		}
		d.w(")")
	case *Comprehension:
		d.expr(x)
	default:
		d.w("(?decl %T)", x)
	}
}

func (d *dumper) postfixAlias(a *PostfixAlias) {
	d.open("postfixalias", a)
	if a.Label != nil {
		d.w(" ")
		d.expr(a.Label)
	}
	d.w(" ")
	d.expr(a.Field)
	d.w(")")
}

func (d *dumper) clause(x Clause) {
	switch x := x.(type) {
	case *ForClause:
		d.open("for", x)
		if x.Key != nil {
			d.w(" ")
			d.expr(x.Key)
		} else {
			d.w(" _")
		}
		d.w(" ")
		d.expr(x.Value)
		d.w(" ")
		d.expr(x.Source)
		d.w(")")
	case *IfClause:
		d.open("if", x)
		d.w(" ")
		d.expr(x.Condition)
		d.w(")")
	case *LetClause:
		d.open("let", x)
		d.w(" ")
		d.expr(x.Ident)
		d.w(" ")
		d.expr(x.Expr)
		d.w(")")
	case *TryClause:
		d.open("try", x)
		if x.Ident != nil {
			d.w(" ")
			d.expr(x.Ident)
			d.w(" ")
			d.expr(x.Expr)
		}
		d.w(")")
	default:
		d.w("(?clause %T)", x)
	}
}

func (d *dumper) opt(x Expr) {
	if x == nil {
		d.w(" _")
		return
	}
	d.w(" ")
	d.expr(x)
}

// expr prints an expression, a label or a selector (all of them are values of the typed AST that are not
// declarations or clauses).
func (d *dumper) expr(x any) {
	switch x := x.(type) {
	case *Ident:
		d.open("id", x)
		d.w(" %s)", strconv.Quote(x.Text))
	case *Int:
		d.w("(lit %d %d INT %s)", x.Start, x.End, strconv.Quote(x.Text))
	case *Float:
		d.w("(lit %d %d FLOAT %s)", x.Start, x.End, strconv.Quote(x.Text))
	case *String:
		d.w("(lit %d %d STRING %s)", x.Start, nodeEnd(x), strconv.Quote(refText(x.Text)))
	case *Bool:
		d.w("(lit %d %d %s %s)", x.Start, x.End, x.Text, strconv.Quote(x.Text))
	case *Null:
		d.w("(lit %d %d null %s)", x.Start, x.End, strconv.Quote(x.Text))
	case *Bottom:
		d.open("bottom", x)
		d.w(")")
	case *Interpolation:
		d.open("interp", x)
		for _, e := range x.Elts {
			d.w(" ")
			d.expr(e)
		}
		d.w(")")
	case *StructLit:
		d.open("struct", x)
		for _, e := range x.Elts {
			d.w(" ")
			d.decl(e)
		}
		d.w(")")
	case *ListLit:
		d.open("list", x)
		for _, e := range x.Elts {
			d.w(" ")
			d.expr(e)
		}
		d.w(")")
	case *Ellipsis:
		d.decl(x)
	case *ParenExpr:
		d.open("paren", x)
		d.w(" ")
		d.expr(x.X)
		d.w(")")
	case *SelectorExpr:
		d.open("sel", x)
		d.w(" ")
		d.expr(x.X)
		d.w(" ")
		d.expr(x.Sel)
		d.w(")")
	case *IndexExpr:
		d.open("index", x)
		d.w(" ")
		d.expr(x.X)
		d.w(" ")
		d.expr(x.Index)
		d.w(")")
	case *SliceExpr:
		d.open("slice", x)
		d.w(" ")
		d.expr(x.X)
		d.opt(x.Low)
		d.opt(x.High)
		d.w(")")
	case *CallExpr:
		d.open("call", x)
		d.w(" ")
		d.expr(x.Fun)
		for _, a := range x.Args {
			d.w(" ")
			d.expr(a)
		}
		d.w(")")
	case *UnaryExpr:
		d.open("unary", x)
		d.w(" %s ", strconv.Quote(x.Op.Text))
		d.expr(x.X)
		d.w(")")
	case *BinaryExpr:
		d.open("binary", x)
		d.w(" %s ", strconv.Quote(x.Op.Text))
		d.expr(x.X)
		d.w(" ")
		d.expr(x.Y)
		d.w(")")
	case *PostfixExpr:
		d.open("postfix", x)
		d.w(" %s ", strconv.Quote(x.Op.Text))
		d.expr(x.X)
		d.w(")")
	case *Alias:
		d.open("alias", x)
		d.w(" ")
		d.expr(x.Ident)
		d.w(" ")
		d.expr(x.Expr)
		d.w(")")
	case *Comprehension:
		d.open("comp", x)
		d.w(" (clauses")
		for _, c := range x.Clauses {
			d.w(" ")
			d.clause(c)
		}
		d.w(") ")
		d.expr(x.Value)
		if x.Fallback != nil {
			d.w(" ")
			d.open("fallback", x.Fallback)
			d.w(" ")
			d.expr(x.Fallback.Body)
			d.w(")")
		}
		d.w(")")
	default:
		d.w("(?expr %T)", x)
	}
}

// refText returns the text of a literal as the reference implementation has it. The scanner of a multi-line string
// removes all carriage returns from its text if the content has one (the line break after the opening quotes is not
// content, and each fragment of an interpolation is scanned on its own).
func refText(t string) string {
	if !strings.Contains(t, "\n") {
		return t
	}
	body := t
	if t[0] != ')' {
		body = t[strings.Index(t, "\n")+1:]
	}
	if strings.Contains(body, "\r") {
		return strings.ReplaceAll(t, "\r", "")
	}
	return t
}
