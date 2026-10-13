package grammar

// Comments and line breaks are not part of a grammar's meaning, but a source
// formatter has to keep them. The parser records them in LineBreak values
// attached to the nodes that Format may start a new line with: statements,
// struct fields, the items of an Operators Rule, the closing braces of
// blocks, and the optional line breaks inside a rule body. These fields are
// tagged `json:"-"`, so the JSON form of a grammar does not contain them.

// Comment is a line comment (// ...) or nested block comment (/* ... */).
type Comment struct {
	// Header identifies a statement header that illegally contains this comment.
	Header Pos
	Pos    Pos
	// End is the source position just after the comment.
	End Pos
	// Text includes delimiters. Line comments exclude their final line
	// break and trailing white space; block comments preserve their contents.
	Text string
	// Content is marker-free documentation text, including original line
	// endings and whitespace. It is independent of formatter spelling.
	Content string
	// Blank reports whether a blank line precedes the comment.
	Blank bool
}

// LineBreak describes the start of a line in PEGO source: the comments
// before it and the comment at its end.
type LineBreak struct {
	// Comments are the comments on their own lines before the node, in
	// source order. Comments that Format cannot keep in place, because it
	// joins the lines they end, are added to the nearest preceding
	// LineBreak.
	Comments []*Comment
	// Blank reports whether a blank line directly precedes the node.
	Blank bool
	// Trailing is the comment at the end of the line that starts with the
	// node, if any.
	Trailing *Comment
}

// AllComments returns every comment kept in g, in no particular order.
func (g *Grammar) AllComments() []*Comment {
	var cs []*Comment
	add := func(b *LineBreak) {
		if b == nil {
			return
		}
		cs = append(cs, b.Comments...)
		if b.Trailing != nil {
			cs = append(cs, b.Trailing)
		}
	}
	addAll := func(bs []*LineBreak) {
		for _, b := range bs {
			add(b)
		}
	}
	var term func(Term)
	term = func(t Term) {
		switch t := t.(type) {
		case *Block:
			addAll(t.Breaks)
			add(t.CloseBreak)
			for _, stmt := range t.Statements {
				term(stmt)
			}
		case *New:
			for _, field := range t.Fields {
				term(field.Value)
			}
		case *Call:
			for _, arg := range t.Args {
				term(arg)
			}
		case *Lambda:
			term(t.Body)
		case *Binary:
			term(t.L)
			term(t.R)
		case *Unary:
			term(t.X)
		case *Assign:
			term(t.Value)
		case *Member:
			term(t.X)
		}
	}
	var expr func(e Expr)
	expr = func(e Expr) {
		switch e := e.(type) {
		case *Group:
			add(e.BodyBreak)
			add(e.CloseBreak)
			expr(e.Expr)
		case *Choice:
			addAll(e.Breaks)
			for _, a := range e.Alts {
				expr(a)
			}
		case *Seq:
			addAll(e.Breaks)
			for _, it := range e.Items {
				expr(it)
			}
		case *Attributed:
			addAll(e.Breaks)
			expr(e.Expr)
			for _, attr := range e.Attrs {
				for _, arg := range attr.ExpressionArgs() {
					expr(arg)
				}
			}
		case *InlineAction:
			expr(e.Expr)
			term(e.Action)
		case *Predicate:
			if _, block := e.Term.(*Block); !block {
				addAll(e.Breaks)
				add(e.CloseBreak)
			}
			term(e.Term)
		case *Repeat:
			add(e.PragmaBreak)
			expr(e.Expr)
		case *Optional:
			expr(e.Expr)
		case *Stream:
			add(e.Break)
			expr(e.Expr)
		case *Recovery:
			add(e.Break)
			expr(e.Expr)
			expr(e.Skip)
		case *Capture:
			expr(e.Expr)
		case *And:
			expr(e.Expr)
		case *Not:
			expr(e.Expr)
		case *Atomic:
			add(e.Break)
			expr(e.Expr)
		case *Discard:
			expr(e.Expr)
		case *OperatorsRule:
			add(e.SkipBreak)
			add(e.CloseBreak)
			expr(e.Skip)
			for _, o := range e.Operands {
				add(o.Break)
				expr(o.Expr)
				term(o.Action)
			}
			for _, l := range e.Levels {
				add(l.Break)
				add(l.CloseBreak)
				for _, op := range l.Operators {
					add(op.Break)
					expr(op.Expr)
					term(op.Action)
				}
			}
		}
	}
	add(g.PackageBreak)
	for _, s := range g.Statements {
		switch s := s.(type) {
		case *TypeDef:
			add(s.Break)
			if st, ok := s.Spec.(*StructSpec); ok {
				add(st.CloseBreak)
				for _, f := range st.Fields {
					add(f.Break)
				}
			}
		case *RuleDef:
			add(s.Break)
			add(s.BodyBreak)
			add(s.ActionBreak)
			add(s.CloseBreak)
			expr(s.Expr)
			term(s.Action)
		}
	}
	add(g.EndBreak)
	return cs
}
