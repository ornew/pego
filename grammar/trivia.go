package grammar

// Comments and line breaks are not part of a grammar's meaning, but a source
// formatter has to keep them. The parser records them in LineBreak values
// attached to the nodes that Format may start a new line with: statements,
// struct fields, the items of a pratt expression, the closing braces of
// blocks, and the optional line breaks inside a rule body. These fields are
// tagged `json:"-"`, so the JSON form of a grammar does not contain them.

// Comment is a line comment ("// ...") in PEGO source.
type Comment struct {
	Pos Pos
	// Text is the comment including the leading "//", without the line
	// break and trailing white space.
	Text string
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
	var expr func(e Expr)
	expr = func(e Expr) {
		switch e := e.(type) {
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
		case *Capture:
			expr(e.Expr)
		case *And:
			expr(e.Expr)
		case *Not:
			expr(e.Expr)
		case *Atomic:
			expr(e.Expr)
		case *Discard:
			expr(e.Expr)
		case *Pratt:
			add(e.SkipBreak)
			add(e.CloseBreak)
			for _, o := range e.Operands {
				add(o.Break)
			}
			for _, l := range e.Levels {
				add(l.Break)
				add(l.CloseBreak)
				for _, op := range l.Operators {
					add(op.Break)
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
			expr(s.Expr)
		}
	}
	add(g.EndBreak)
	return cs
}
