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
	// Run lists the comments that this one stands for when several comments end
	// the same line, with Text and Content holding all of them: the formatter
	// writes them together, as they were. AllComments returns them separately.
	Run []*Comment
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
	// node, if any. When several comments end the line, it stands for all of
	// them (see Comment.Run).
	Trailing *Comment
	// TrailingEnds reports whether a line break follows Trailing in the source
	// before the next token, rather than that token following it on the same
	// line. A block comment that ends its line cannot move to the line of the
	// token after it without changing which node owns it.
	TrailingEnds bool
	// Last is the comment on the line of the last token before the closing
	// parenthesis of a group, which trails that token: Trailing, of the break
	// before a ")", is the comment after the ")".
	Last *Comment
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
		if b.Last != nil {
			cs = append(cs, b.Last)
		}
	}
	addAll := func(bs []*LineBreak) {
		for _, b := range bs {
			control.poll()
			add(b)
		}
	}
	addLayout := func(l *SourceLayout) {
		if l != nil {
			cs = appendMetadata(cs, l.Before, control)
			cs = appendMetadata(cs, l.After, control)
			cs = appendMetadata(cs, l.Trail, control)
			cs = appendMetadata(cs, l.Operator, control)
			for _, g := range []*Gap{l.Open, l.Eq, l.Close, l.OpGap, l.Sep, l.Lead} {
				if g != nil {
					cs = appendMetadata(cs, g.Comments, control)
				}
			}
		}
	}
	addList := func(l *ListLayout) {
		if l == nil {
			return
		}
		add(l.OpenBreak)
		add(l.BodyBreak)
		add(l.CloseBreak)
		for _, e := range l.Elems {
			control.poll()
			addLayout(e)
		}
	}
	var term func(Term)
	var typ func(TypeExpr)
	term = func(t Term) {
		control.poll()
		if n, ok := t.(SourceNode); ok {
			addLayout(n.Source().Layout)
		}
		switch t := t.(type) {
		case *TermGroup:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			term(t.Term)
		case *Block:
			add(t.OpenBreak)
			addAll(t.Breaks)
			add(t.CloseBreak)
			for _, stmt := range t.Statements {
				term(stmt)
			}
		case *New:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, field := range t.Fields {
				control.poll()
				if field.Layout != nil {
					cs = appendMetadata(cs, field.Layout.Before, control)
					cs = appendMetadata(cs, field.Layout.After, control)
					cs = appendMetadata(cs, field.Layout.Trail, control)
				}
				term(field.Value)
			}
			for _, x := range t.Args {
				term(x)
			}
		case *TupleLit:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, x := range t.Elems {
				term(x)
			}
		case *ListConstructor:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			typ(t.Elem)
			for _, item := range t.Items {
				term(item)
			}
		case *Call:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, arg := range t.Args {
				term(arg)
			}
		case *Lambda:
			addList(t.ParamsLayout)
			term(t.Body)
		case *Binary:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			term(t.L)
			term(t.R)
		case *Unary:
			term(t.X)
		case *Cast:
			term(t.X)
			typ(t.Type)
		case *Bind:
			term(t.Value)
		case *Member:
			term(t.X)
		}
	}
	typ = func(t TypeExpr) {
		control.poll()
		if n, ok := t.(SourceNode); ok {
			addLayout(n.Source().Layout)
		}
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
		case *JoinType:
			for _, t := range t.Types {
				typ(t)
			}
		case *MeetType:
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
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, t := range t.Args {
				typ(t)
			}
		case *StructType:
			add(t.CloseBreak)
			for _, f := range t.Fields {
				add(f.Break)
				typ(f.Type)
			}
		}
	}
	var expr func(e Expr)
	expr = func(e Expr) {
		control.poll()
		if n, ok := e.(SourceNode); ok {
			addLayout(n.Source().Layout)
		}
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
				addLayout(attr.Layout)
				addList(attr.ArgsLayout)
				for _, arg := range attr.expressionArgs(control) {
					expr(arg)
				}
			}
		case *InlineAction:
			expr(e.Expr)
			term(e.Action)
		case *Predicate:
			if _, block := e.Term.(*Block); !block {
				add(e.OpenBreak)
				addAll(e.Breaks)
				add(e.CloseBreak)
			}
			term(e.Term)
		case *Repeat:
			add(e.PragmaBreak)
			if e.Spelling != nil {
				for _, g := range e.Spelling.Gaps {
					control.poll()
					cs = appendMetadata(cs, g.Comments, control)
				}
			}
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
	add(g.File.Pkg.Break)
	for _, s := range g.File.Decls {
		control.poll()
		switch s := s.(type) {
		case *TypeDef:
			add(s.Break)
			if s.Type != nil {
				typ(s.Type)
			}
		case *TypeAlias:
			add(s.Break)
			typ(s.Type)
		case *VarDef:
			add(s.Break)
			if s.Type != nil {
				typ(s.Type)
			}
			term(s.Value)
		case *RuleDef:
			for _, attr := range s.Attrs {
				control.poll()
				addLayout(attr.Layout)
				addList(attr.ArgsLayout)
				for _, arg := range attr.expressionArgs(control) {
					expr(arg)
				}
			}
			addAll(s.AttrBreaks)
			add(s.KeywordBreak)
			typ(s.Type)
			add(s.Break)
			add(s.BodyBreak)
			add(s.ActionBreak)
			add(s.CloseBreak)
			expr(s.Expr)
			term(s.Action)
		}
	}
	add(g.File.EndBreak)
	return expandRuns(cs)
}

// expandRuns replaces each comment that stands for a run of comments by them.
func expandRuns(cs []*Comment) []*Comment {
	for i, c := range cs {
		if c != nil && len(c.Run) > 0 {
			out := append([]*Comment(nil), cs[:i]...)
			for _, c := range cs[i:] {
				if c != nil && len(c.Run) > 0 {
					out = append(out, c.Run...)
				} else {
					out = append(out, c)
				}
			}
			return out
		}
	}
	return cs
}
