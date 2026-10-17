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
	return g.allComments(nil)
}

func (g *Grammar) allComments(control *metadataControl) []*Comment {
	var cs []*Comment
	add := func(b *LineBreak) {
		control.poll()
		if b == nil {
			return
		}
		cs = appendMetadata(cs, b.Comments, control)
		if b.Trailing != nil {
			cs = append(cs, b.Trailing)
		}
	}
	addAll := func(bs []*LineBreak) {
		for _, b := range bs {
			control.poll()
			add(b)
		}
	}
	var term func(Term)
	var typ func(TypeExpr)
	term = func(t Term) {
		control.poll()
		if n, ok := t.(SourceNode); ok && n.Source().Layout != nil {
			cs = appendMetadata(cs, n.Source().Layout.Before, control)
		}
		switch t := t.(type) {
		case *TermGroup:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			term(t.Term)
		case *Block:
			addAll(t.Breaks)
			add(t.CloseBreak)
			for _, stmt := range t.Statements {
				term(stmt)
			}
		case *New:
			for _, field := range t.Fields {
				control.poll()
				if field.Layout != nil {
					cs = appendMetadata(cs, field.Layout.Before, control)
				}
				term(field.Value)
			}
		case *ListConstructor:
			typ(t.Elem)
			for _, item := range t.Items {
				term(item)
			}
		case *TerminalConstructor:
			for _, arg := range t.Args {
				term(arg)
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
		case *Cast:
			term(t.X)
			typ(t.Type)
		case *Assign:
			term(t.Value)
		case *Member:
			term(t.X)
		}
	}
	typ = func(t TypeExpr) {
		control.poll()
		switch t := t.(type) {
		case *TypeGroup:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			typ(t.Type)
		case *ListType:
			typ(t.Elem)
		case *OptionalType:
			typ(t.Elem)
		case *UnionType:
			for _, t := range t.Types {
				typ(t)
			}
		case *IntersectionType:
			for _, t := range t.Types {
				typ(t)
			}
		case *TupleType:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, t := range t.Types {
				typ(t)
			}
		case *TypeApp:
			for _, t := range t.Args {
				typ(t)
			}
		}
	}
	var expr func(e Expr)
	expr = func(e Expr) {
		control.poll()
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
				control.poll()
				for _, arg := range attr.expressionArgs(control) {
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
				control.poll()
				add(o.Break)
				expr(o.Expr)
				term(o.Action)
			}
			for _, l := range e.Levels {
				control.poll()
				add(l.Break)
				add(l.CloseBreak)
				for _, op := range l.Operators {
					control.poll()
					add(op.Break)
					expr(op.Expr)
					term(op.Action)
				}
			}
		}
	}
	add(g.PackageBreak)
	for _, s := range g.Statements {
		control.poll()
		switch s := s.(type) {
		case *TypeDef:
			add(s.Break)
			if st, ok := s.Spec.(*StructSpec); ok {
				add(st.CloseBreak)
				for _, f := range st.Fields {
					add(f.Break)
					typ(f.Type)
				}
			}
			if alias, ok := s.Spec.(*AliasSpec); ok {
				typ(alias.Type)
			}
		case *RuleDef:
			typ(s.Type)
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
