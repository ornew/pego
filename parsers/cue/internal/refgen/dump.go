package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

// result is what the reference implementation makes of one source.
type result struct {
	Name     string
	Accepted bool
	Err      string // the first error of a rejected source
	Tree     string // the canonical tree of an accepted source
}

func (r result) dump() string {
	if !r.Accepted {
		return "error: " + r.Err
	}
	return r.Tree
}

// run parses src with the reference parser (default configuration: latest language version, no
// experiments except those the file enables itself) and describes the result. Acceptance and the tree are
// those of the default mode; the comments come from a second parse that keeps them, which rejects a few inputs
// the first accepts (a comment between the name of an attribute and its parenthesis): the comments of such a
// source are not known.
func run(name string, src []byte) (r result) {
	r.Name = name
	defer func() {
		if e := recover(); e != nil {
			r.Accepted = false
			r.Err = fmt.Sprintf("panic: %v", e)
		}
	}()
	f, err := parser.ParseFile(name, src)
	if err != nil || f == nil {
		r.Err = "unknown error"
		if err != nil {
			r.Err = err.Error()
			if i := strings.IndexByte(r.Err, '\n'); i >= 0 {
				r.Err = r.Err[:i]
			}
		}
		return r
	}
	r.Accepted = true
	fc, cerr := parser.ParseFile(name, src, parser.ParseComments)
	var b strings.Builder
	d := &dumper{b: &b}
	d.file(f, fc, cerr)
	r.Tree = b.String()
	return r
}

// The canonical tree is a line of s-expressions: (kind start end children...), with byte offsets.
// Identical code in the module's tests prints the tree of the PEGO parser's result.
type dumper struct {
	b *strings.Builder
}

func (d *dumper) w(format string, args ...any) { fmt.Fprintf(d.b, format, args...) }

func off(p token.Pos) int {
	if !p.IsValid() {
		return -1
	}
	return p.Offset()
}

func (d *dumper) open(kind string, n ast.Node) {
	d.w("(%s %d %d", kind, off(n.Pos()), off(n.End()))
}

func (d *dumper) file(f, fc *ast.File, cerr error) {
	d.w("(file")
	for _, x := range f.Decls {
		d.w(" ")
		d.decl(x)
	}
	d.w(")\n")
	if cerr != nil || fc == nil {
		d.w("(comments ?)\n")
		return
	}
	// Comments, in source order.
	var cs []*ast.Comment
	seen := map[*ast.Comment]bool{}
	ast.Walk(fc, func(n ast.Node) bool {
		for _, g := range ast.Comments(n) {
			for _, c := range g.List {
				if !seen[c] {
					seen[c] = true
					cs = append(cs, c)
				}
			}
		}
		return true
	}, nil)
	sort.Slice(cs, func(i, j int) bool { return off(cs[i].Pos()) < off(cs[j].Pos()) })
	d.w("(comments")
	for _, c := range cs {
		d.w(" (%d %s)", off(c.Pos()), strconv.Quote(c.Text))
	}
	d.w(")\n")
}

func (d *dumper) decl(x ast.Decl) {
	switch x := x.(type) {
	case *ast.Package:
		d.open("package", x)
		d.w(" ")
		d.expr(x.Name)
		d.w(")")
	case *ast.Attribute:
		d.attr(x)
	case *ast.ImportDecl:
		d.open("import", x)
		if x.Lparen.IsValid() {
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
	case *ast.Field:
		d.open("field", x)
		switch x.Constraint {
		case token.OPTION:
			d.w(" ?")
		case token.NOT:
			d.w(" !")
		default:
			d.w(" -")
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
			d.attr(a)
		}
		d.w(")")
	case *ast.EmbedDecl:
		d.open("embed", x)
		d.w(" ")
		d.expr(x.Expr)
		d.w(")")
	case *ast.LetClause:
		d.clause(x)
	case *ast.Ellipsis:
		d.open("ellipsis", x)
		if x.Type != nil {
			d.w(" ")
			d.expr(x.Type)
		}
		d.w(")")
	case *ast.Comprehension:
		d.expr(x)
	case *ast.CommentGroup:
		// Not produced by the parser.
	case *ast.BadDecl:
		d.w("(bad)")
	default:
		d.w("(?decl %T)", x)
	}
}

func (d *dumper) attr(a *ast.Attribute) {
	d.open("attr", a)
	d.w(" %s)", strconv.Quote(a.Text))
}

func (d *dumper) postfixAlias(a *ast.PostfixAlias) {
	d.w("(postfixalias %d %d", off(a.Pos()), off(a.End()))
	if a.Label != nil {
		d.w(" ")
		d.expr(a.Label)
	}
	d.w(" ")
	d.expr(a.Field)
	d.w(")")
}

func (d *dumper) clause(x ast.Clause) {
	switch x := x.(type) {
	case *ast.ForClause:
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
	case *ast.IfClause:
		d.open("if", x)
		d.w(" ")
		d.expr(x.Condition)
		d.w(")")
	case *ast.LetClause:
		d.open("let", x)
		d.w(" ")
		d.expr(x.Ident)
		d.w(" ")
		d.expr(x.Expr)
		d.w(")")
	case *ast.TryClause:
		d.open("try", x)
		if x.Ident != nil {
			d.w(" ")
			d.expr(x.Ident)
			d.w(" ")
			d.expr(x.Expr)
		}
		d.w(")")
	case *ast.FallbackClause:
		d.open("fallback", x)
		d.w(" ")
		d.expr(x.Body)
		d.w(")")
	default:
		d.w("(?clause %T)", x)
	}
}

func (d *dumper) opt(x ast.Expr) {
	if x == nil {
		d.w(" _")
		return
	}
	d.w(" ")
	d.expr(x)
}

func (d *dumper) expr(x ast.Node) {
	switch x := x.(type) {
	case *ast.Ident:
		d.open("id", x)
		d.w(" %s)", strconv.Quote(x.Name))
	case *ast.BasicLit:
		d.w("(lit %d %d %s %s)", off(x.Pos()), off(x.End()), x.Kind, strconv.Quote(x.Value))
	case *ast.BottomLit:
		d.open("bottom", x)
		d.w(")")
	case *ast.Interpolation:
		d.open("interp", x)
		for _, e := range x.Elts {
			d.w(" ")
			d.expr(e)
		}
		d.w(")")
	case *ast.StructLit:
		d.open("struct", x)
		for _, e := range x.Elts {
			d.w(" ")
			d.decl(e)
		}
		d.w(")")
	case *ast.ListLit:
		d.open("list", x)
		for _, e := range x.Elts {
			d.w(" ")
			d.expr(e)
		}
		d.w(")")
	case *ast.Ellipsis:
		d.decl(x)
	case *ast.ParenExpr:
		d.open("paren", x)
		d.w(" ")
		d.expr(x.X)
		d.w(")")
	case *ast.SelectorExpr:
		d.open("sel", x)
		d.w(" ")
		d.expr(x.X)
		d.w(" ")
		d.expr(x.Sel)
		d.w(")")
	case *ast.IndexExpr:
		d.open("index", x)
		d.w(" ")
		d.expr(x.X)
		d.w(" ")
		d.expr(x.Index)
		d.w(")")
	case *ast.SliceExpr:
		d.open("slice", x)
		d.w(" ")
		d.expr(x.X)
		d.opt(x.Low)
		d.opt(x.High)
		d.w(")")
	case *ast.CallExpr:
		d.open("call", x)
		d.w(" ")
		d.expr(x.Fun)
		for _, a := range x.Args {
			d.w(" ")
			d.expr(a)
		}
		d.w(")")
	case *ast.UnaryExpr:
		d.open("unary", x)
		d.w(" %s ", strconv.Quote(x.Op.String()))
		d.expr(x.X)
		d.w(")")
	case *ast.BinaryExpr:
		d.open("binary", x)
		d.w(" %s ", strconv.Quote(x.Op.String()))
		d.expr(x.X)
		d.w(" ")
		d.expr(x.Y)
		d.w(")")
	case *ast.PostfixExpr:
		d.open("postfix", x)
		d.w(" %s ", strconv.Quote(x.Op.String()))
		d.expr(x.X)
		d.w(")")
	case *ast.Alias:
		d.open("alias", x)
		d.w(" ")
		d.expr(x.Ident)
		d.w(" ")
		d.expr(x.Expr)
		d.w(")")
	case *ast.Comprehension:
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
			d.clause(x.Fallback)
		}
		d.w(")")
	case *ast.BadExpr:
		d.w("(bad)")
	default:
		d.w("(?expr %T)", x)
	}
}
