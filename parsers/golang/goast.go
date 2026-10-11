package golang

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// ToGoAST converts a file parsed from src with the Bytes position unit into the go/ast tree that
// go/parser.ParseFile returns for src, with positions in a token.File added to fset under filename.
// The line table and the //line directives of the token.File are set as go/scanner sets them. With
// ParseComments, the comments are collected into File.Comments in groups, as go/parser groups them;
// Doc and Comment fields are not set. Identifiers are not resolved (as with go/parser's
// SkipObjectResolution).
//
// f must have been parsed from src with the Bytes unit (ParseAST(src, WithUnit(Bytes))); ToGoAST panics if f
// does not fit src.
func ToGoAST(fset *token.FileSet, filename, src string, f *File, mode Mode) *ast.File {
	tf := fset.AddFile(filename, -1, len(src))
	c := &converter{src: src, base: tf.Base()}
	if f.Start != 0 || f.End != len(src) {
		panic(fmt.Sprintf("golang: ToGoAST: the file covers %d-%d of %d bytes; parse it with the Bytes unit", f.Start, f.End, len(src)))
	}
	c.lines(tf, mode)
	start := 0
	if strings.HasPrefix(src, bom) {
		start = len(bom)
	}
	af := &ast.File{
		Package:   c.pos(c.next(start)),
		Name:      c.ident(f.Name),
		FileStart: c.pos(0),
		FileEnd:   c.pos(len(src)),
		GoVersion: c.goVersion,
		Comments:  c.comments,
	}
	for _, d := range f.Decls {
		af.Decls = append(af.Decls, c.decl(d))
	}
	af.Imports = c.imports
	return af
}

// Mode selects what ToGoAST and ParseFile add to the go/ast tree.
type Mode uint

const (
	// ParseComments collects the comments into File.Comments (but not the Doc and Comment fields).
	ParseComments Mode = 1 << iota
)

// bom is the byte order mark, which go/scanner skips at the beginning of a file.
const bom = "\xef\xbb\xbf"

type converter struct {
	src       string
	base      int
	imports   []*ast.ImportSpec
	comments  []*ast.CommentGroup
	goVersion string
}

func (c *converter) pos(off int) token.Pos { return token.Pos(c.base + off) }

// next returns the offset of the first token at or after off, skipping whitespace and comments.
func (c *converter) next(off int) int {
	s := c.src
	for off < len(s) {
		switch s[off] {
		case ' ', '\t', '\r', '\n':
			off++
		case '/':
			if off+1 < len(s) && s[off+1] == '/' {
				i := strings.IndexByte(s[off:], '\n')
				if i < 0 {
					return len(s)
				}
				off += i
			} else if off+1 < len(s) && s[off+1] == '*' {
				off += 2 + strings.Index(s[off+2:], "*/") + 2
			} else {
				return off
			}
		default:
			return off
		}
	}
	return off
}

// spanOf returns the range of a value of the typed AST.
func rangeOf(n any) (int, int) {
	return n.(interface{ tspan() (int, int) }).tspan()
}

func end(n any) int {
	_, e := rangeOf(n)
	return e
}

func (c *converter) ident(id *Ident) *ast.Ident {
	if id == nil {
		return nil
	}
	return &ast.Ident{NamePos: c.pos(id.Start), Name: id.Text}
}

func (c *converter) idents(ids []*Ident) []*ast.Ident {
	if len(ids) == 0 {
		return nil
	}
	out := make([]*ast.Ident, len(ids))
	for i, id := range ids {
		out[i] = c.ident(id)
	}
	return out
}

func (c *converter) basicLit(s Span, kind token.Token, text string) *ast.BasicLit {
	if kind == token.STRING && strings.HasPrefix(text, "`") && strings.Contains(text, "\r") {
		text = strings.ReplaceAll(text, "\r", "") // as go/scanner does for raw strings
	}
	return &ast.BasicLit{ValuePos: c.pos(s.Start), ValueEnd: c.pos(s.End), Kind: kind, Value: text}
}

// --- Declarations ---

func (c *converter) decl(d Decl) ast.Decl {
	switch d := d.(type) {
	case *GenDecl:
		return c.genDecl(d)
	case *FuncDecl:
		fd := &ast.FuncDecl{
			Recv: c.fieldList(d.Recv),
			Name: c.ident(d.Name),
			Type: c.funcType(d.Type, c.pos(d.Start)),
		}
		if d.Body != nil {
			fd.Body = c.block(d.Body)
		}
		return fd
	}
	panic(fmt.Sprintf("golang: unexpected declaration %T", d))
}

func (c *converter) genDecl(d *GenDecl) *ast.GenDecl {
	gd := &ast.GenDecl{TokPos: c.pos(d.Start), Tok: keyword(d.Tok)}
	if p := c.next(d.Start + len(d.Tok)); c.src[p] == '(' {
		gd.Lparen, gd.Rparen = c.pos(p), c.pos(d.End-1)
	}
	for _, s := range d.Specs {
		gd.Specs = append(gd.Specs, c.spec(s))
	}
	return gd
}

func (c *converter) spec(s Spec) ast.Spec {
	switch s := s.(type) {
	case *ImportSpec:
		is := &ast.ImportSpec{Name: c.ident(s.Name), Path: c.basicLit(s.Path.Span, token.STRING, s.Path.Text)}
		c.imports = append(c.imports, is)
		return is
	case *ValueSpec:
		return &ast.ValueSpec{Names: c.idents(s.Names), Type: c.expr(s.Type), Values: c.exprs(s.Values)}
	case *TypeSpec:
		ts := &ast.TypeSpec{Name: c.ident(s.Name), TypeParams: c.fieldList(s.TypeParams), Type: c.expr(s.Type)}
		if s.Assign != nil {
			ts.Assign = c.pos(s.Assign.Start)
		}
		return ts
	}
	panic(fmt.Sprintf("golang: unexpected specification %T", s))
}

// --- Fields ---

func (c *converter) fieldList(l *FieldList) *ast.FieldList {
	if l == nil {
		return nil
	}
	fl := &ast.FieldList{}
	// A list without parentheses (a single result type) has the range of its only field.
	if len(l.List) != 1 || l.List[0].Start != l.Start {
		fl.Opening, fl.Closing = c.pos(l.Start), c.pos(l.End-1)
	}
	if len(l.List) > 0 {
		fl.List = make([]*ast.Field, len(l.List))
		for i, f := range l.List {
			fl.List[i] = c.field(f)
		}
	}
	return fl
}

func (c *converter) field(f *Field) *ast.Field {
	af := &ast.Field{Names: c.idents(f.Names), Type: c.expr(f.Type)}
	if f.Tag != nil {
		af.Tag = c.basicLit(f.Tag.Span, token.STRING, f.Tag.Text)
	}
	return af
}

// funcType converts a function type; fn is the position of "func" (NoPos for a method of an
// interface).
func (c *converter) funcType(t *FuncType, fn token.Pos) *ast.FuncType {
	return &ast.FuncType{
		Func:       fn,
		TypeParams: c.fieldList(t.TypeParams),
		Params:     c.fieldList(t.Params),
		Results:    c.fieldList(t.Results),
	}
}

// --- Expressions ---

func (c *converter) exprs(xs []Expr) []ast.Expr {
	if len(xs) == 0 {
		return nil
	}
	out := make([]ast.Expr, len(xs))
	for i, x := range xs {
		out[i] = c.expr(x)
	}
	return out
}

func (c *converter) expr(e Expr) ast.Expr {
	switch e := e.(type) {
	case nil:
		return nil
	case *Ident:
		return c.ident(e)
	case *IntLit:
		return c.basicLit(e.Span, token.INT, e.Text)
	case *FloatLit:
		return c.basicLit(e.Span, token.FLOAT, e.Text)
	case *ImagLit:
		return c.basicLit(e.Span, token.IMAG, e.Text)
	case *CharLit:
		return c.basicLit(e.Span, token.CHAR, e.Text)
	case *StringLit:
		return c.basicLit(e.Span, token.STRING, e.Text)
	case *Ellipsis:
		return &ast.Ellipsis{Ellipsis: c.pos(e.Start), Elt: c.expr(e.Elt)}
	case *CompositeLit:
		lbrace := e.Start
		if e.Type != nil {
			lbrace = c.next(end(e.Type))
		}
		return &ast.CompositeLit{Type: c.expr(e.Type), Lbrace: c.pos(lbrace), Elts: c.exprs(e.Elts), Rbrace: c.pos(e.End - 1)}
	case *FuncLit:
		return &ast.FuncLit{Type: c.funcType(e.Type, c.pos(e.Start)), Body: c.block(e.Body)}
	case *ParenExpr:
		return &ast.ParenExpr{Lparen: c.pos(e.Start), X: c.expr(e.X), Rparen: c.pos(e.End - 1)}
	case *SelectorExpr:
		return &ast.SelectorExpr{X: c.expr(e.X), Sel: c.ident(e.Sel)}
	case *IndexExpr:
		return &ast.IndexExpr{X: c.expr(e.X), Lbrack: c.pos(c.next(end(e.X))), Index: c.expr(e.Index), Rbrack: c.pos(e.End - 1)}
	case *IndexListExpr:
		return &ast.IndexListExpr{X: c.expr(e.X), Lbrack: c.pos(c.next(end(e.X))), Indices: c.exprs(e.Indices), Rbrack: c.pos(e.End - 1)}
	case *SliceExpr:
		return &ast.SliceExpr{X: c.expr(e.X), Lbrack: c.pos(c.next(end(e.X))), Low: c.expr(e.Low), High: c.expr(e.High),
			Max: c.expr(e.Max), Slice3: e.Max != nil, Rbrack: c.pos(e.End - 1)}
	case *TypeAssertExpr:
		dot := c.next(end(e.X))
		return &ast.TypeAssertExpr{X: c.expr(e.X), Lparen: c.pos(c.next(dot + 1)), Type: c.expr(e.Type), Rparen: c.pos(e.End - 1)}
	case *CallExpr:
		call := &ast.CallExpr{Fun: c.expr(e.Fun), Lparen: c.pos(c.next(end(e.Fun))), Args: c.exprs(e.Args), Rparen: c.pos(e.End - 1)}
		if e.Ellipsis != nil {
			call.Ellipsis = c.pos(e.Ellipsis.Start)
		}
		return call
	case *StarExpr:
		return &ast.StarExpr{Star: c.pos(e.Start), X: c.expr(e.X)}
	case *UnaryExpr:
		return &ast.UnaryExpr{OpPos: c.pos(e.Start), Op: operator(e.Op), X: c.expr(e.X)}
	case *BinaryExpr:
		return &ast.BinaryExpr{X: c.expr(e.X), OpPos: c.pos(c.next(end(e.X))), Op: operator(e.Op), Y: c.expr(e.Y)}
	case *KeyValueExpr:
		return &ast.KeyValueExpr{Key: c.expr(e.Key), Colon: c.pos(c.next(end(e.Key))), Value: c.expr(e.Value)}
	case *ArrayType:
		return &ast.ArrayType{Lbrack: c.pos(e.Start), Len: c.expr(e.Len), Elt: c.expr(e.Elt)}
	case *StructType:
		return &ast.StructType{Struct: c.pos(e.Start), Fields: c.fieldList(e.Fields)}
	case *FuncType:
		return c.funcType(e, c.pos(e.Start))
	case *InterfaceType:
		it := &ast.InterfaceType{Interface: c.pos(e.Start), Methods: c.fieldList(e.Methods)}
		for _, m := range it.Methods.List {
			if ft, ok := m.Type.(*ast.FuncType); ok && len(m.Names) > 0 {
				ft.Func = token.NoPos // a method has no "func"
			}
		}
		return it
	case *MapType:
		return &ast.MapType{Map: c.pos(e.Start), Key: c.expr(e.Key), Value: c.expr(e.Value)}
	case *ChanType:
		ct := &ast.ChanType{Begin: c.pos(e.Start), Dir: ast.ChanDir(e.Dir), Value: c.expr(e.Value)}
		switch e.Dir {
		case int(ast.SEND):
			ct.Arrow = c.pos(c.next(e.Start + len("chan")))
		case int(ast.RECV):
			ct.Arrow = ct.Begin
		}
		return ct
	}
	panic(fmt.Sprintf("golang: unexpected expression %T", e))
}

// --- Statements ---

func (c *converter) block(b *BlockStmt) *ast.BlockStmt {
	return &ast.BlockStmt{Lbrace: c.pos(b.Start), List: c.stmts(b.List), Rbrace: c.pos(b.End - 1)}
}

func (c *converter) stmts(ss []Stmt) []ast.Stmt {
	if len(ss) == 0 {
		return nil
	}
	out := make([]ast.Stmt, len(ss))
	for i, s := range ss {
		out[i] = c.stmt(s)
	}
	return out
}

func (c *converter) stmt(s Stmt) ast.Stmt {
	switch s := s.(type) {
	case nil:
		return nil
	case *DeclStmt:
		return &ast.DeclStmt{Decl: c.genDecl(s.Decl)}
	case *EmptyStmt:
		return &ast.EmptyStmt{Semicolon: c.pos(s.Start), Implicit: s.Start == s.End}
	case *LabeledStmt:
		return &ast.LabeledStmt{Label: c.ident(s.Label), Colon: c.pos(c.next(s.Label.End)), Stmt: c.stmt(s.Stmt)}
	case *ExprStmt:
		return &ast.ExprStmt{X: c.expr(s.X)}
	case *SendStmt:
		return &ast.SendStmt{Chan: c.expr(s.Chan), Arrow: c.pos(c.next(end(s.Chan))), Value: c.expr(s.Value)}
	case *IncDecStmt:
		return &ast.IncDecStmt{X: c.expr(s.X), TokPos: c.pos(c.next(end(s.X))), Tok: operator(s.Tok)}
	case *AssignStmt:
		return c.assign(s)
	case *GoStmt:
		return &ast.GoStmt{Go: c.pos(s.Start), Call: c.expr(s.Call).(*ast.CallExpr)}
	case *DeferStmt:
		return &ast.DeferStmt{Defer: c.pos(s.Start), Call: c.expr(s.Call).(*ast.CallExpr)}
	case *ReturnStmt:
		return &ast.ReturnStmt{Return: c.pos(s.Start), Results: c.exprs(s.Results)}
	case *BranchStmt:
		return &ast.BranchStmt{TokPos: c.pos(s.Start), Tok: keyword(s.Tok), Label: c.ident(s.Label)}
	case *BlockStmt:
		return c.block(s)
	case *IfStmt:
		is := &ast.IfStmt{If: c.pos(s.Start), Init: c.optStmt(s.Init), Cond: c.expr(s.Cond), Body: c.block(s.Body)}
		if s.Else != nil {
			is.Else = c.stmt(s.Else)
		}
		return is
	case *CaseClause:
		cc := &ast.CaseClause{Case: c.pos(s.Start), List: c.exprs(s.List), Body: c.stmts(s.Body)}
		if len(s.List) > 0 {
			cc.Colon = c.pos(c.next(end(s.List[len(s.List)-1])))
		} else {
			cc.Colon = c.pos(c.next(s.Start + len("default")))
		}
		return cc
	case *SwitchStmt:
		return &ast.SwitchStmt{Switch: c.pos(s.Start), Init: c.optStmt(s.Init), Tag: c.optExpr(s.Tag), Body: c.block(s.Body)}
	case *TypeSwitchStmt:
		return &ast.TypeSwitchStmt{Switch: c.pos(s.Start), Init: c.optStmt(s.Init), Assign: c.stmt(s.Assign), Body: c.block(s.Body)}
	case *CommClause:
		cc := &ast.CommClause{Case: c.pos(s.Start), Body: c.stmts(s.Body)}
		if s.Comm != nil {
			cc.Comm = c.stmt(s.Comm)
			cc.Colon = c.pos(c.next(end(s.Comm)))
		} else {
			cc.Colon = c.pos(c.next(s.Start + len("default")))
		}
		return cc
	case *SelectStmt:
		return &ast.SelectStmt{Select: c.pos(s.Start), Body: c.block(s.Body)}
	case *ForStmt:
		return &ast.ForStmt{For: c.pos(s.Start), Init: c.optStmt(s.Init), Cond: c.optExpr(s.Cond), Post: c.optStmt(s.Post), Body: c.block(s.Body)}
	case *RangeStmt:
		rs := &ast.RangeStmt{For: c.pos(s.Start), Key: c.optExpr(s.Key), Value: c.optExpr(s.Value), X: c.expr(s.X), Body: c.block(s.Body)}
		rangePos := c.next(s.Start + len("for"))
		if s.Tok != "" {
			last := s.Key
			if s.Value != nil {
				last = s.Value
			}
			tok := c.next(end(last))
			rs.TokPos, rs.Tok = c.pos(tok), operator(s.Tok)
			rangePos = c.next(tok + len(s.Tok))
		}
		rs.Range = c.pos(rangePos)
		return rs
	}
	panic(fmt.Sprintf("golang: unexpected statement %T", s))
}

func (c *converter) assign(s *AssignStmt) *ast.AssignStmt {
	return &ast.AssignStmt{Lhs: c.exprs(s.Lhs), TokPos: c.pos(c.next(end(s.Lhs[len(s.Lhs)-1]))), Tok: operator(s.Tok), Rhs: c.exprs(s.Rhs)}
}

func (c *converter) optStmt(s Stmt) ast.Stmt { return c.stmt(s) }

func (c *converter) optExpr(e Expr) ast.Expr { return c.expr(e) }

// --- Tokens ---

var operators = func() map[string]token.Token {
	m := map[string]token.Token{}
	for t := token.ADD; t <= token.TILDE; t++ {
		if t.IsOperator() {
			m[t.String()] = t
		}
	}
	return m
}()

func operator(s string) token.Token {
	if t, ok := operators[s]; ok {
		return t
	}
	panic("golang: unknown operator " + strconv.Quote(s))
}

func keyword(s string) token.Token {
	if t := token.Lookup(s); t.IsKeyword() {
		return t
	}
	panic("golang: unknown keyword " + strconv.Quote(s))
}

// --- Comments and line directives ---

// lines sets the line table of tf, applies the //line directives of the source and, with
// ParseComments, collects the comments; it also finds the Go version of the //go:build lines before
// the package clause.
func (c *converter) lines(tf *token.File, mode Mode) {
	src := c.src
	tf.SetLinesForContent([]byte(src))
	dir, _ := filepath.Split(tf.Name())
	line := func(off int) int { return tf.PositionFor(tf.Pos(off), false).Line }
	g := commentGrouper{collect: mode&ParseComments != 0}
	top := true         // before the first token (go/parser's top)
	insertSemi := false // a newline is a semicolon (go/scanner's insertSemi)
	lineStart := 0
	i := 0
	if strings.HasPrefix(src, bom) {
		i = len(bom)
	}
	for i < len(src) {
		ch := src[i]
		switch {
		case ch == '\n':
			if insertSemi {
				g.token(line(i))
				insertSemi = false
			}
			i++
			lineStart = i
			continue
		case ch == ' ' || ch == '\t' || ch == '\r':
			i++
			continue
		case ch == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*'):
			j := commentEnd(src, i)
			text := src[i:j]
			next := j
			if text[1] == '/' && next < len(src) {
				next++ // the position after a //-comment is after its newline
			}
			lit := text
			if text[1] == '/' && strings.HasSuffix(lit, "\r") {
				lit = lit[:len(lit)-1]
			}
			if (text[1] == '*' || i == lineStart) && strings.HasPrefix(lit[2:], "line ") {
				updateLineInfo(tf, dir, next, i, lit)
			}
			if strings.Contains(lit, "\r") {
				lit = stripCR(lit, text[1] == '*')
			}
			if top && strings.HasPrefix(lit, "//go:build") {
				if x, err := constraint.Parse(lit); err == nil {
					c.goVersion = constraint.GoVersion(x)
				}
			}
			start := line(i)
			g.comment(&ast.Comment{Slash: c.pos(i), Text: lit}, start, start+strings.Count(text, "\n"))
			// A newline in a /*-comment after a token that ends a statement is a semicolon, after
			// the comment.
			if nl := strings.IndexByte(text, '\n'); nl >= 0 && insertSemi {
				g.token(line(i + nl))
				insertSemi = false
			}
			i = j
			continue
		}
		top = false
		j := tokenEnd(src, i)
		g.token(line(i))
		insertSemi = endsStatement(src[i:j])
		i = j
	}
	c.comments = g.finish()
}

// endsStatement reports whether a newline after the token tok is a semicolon.
func endsStatement(tok string) bool {
	switch tok {
	case ")", "]", "}", "++", "--", "break", "continue", "fallthrough", "return":
		return true
	}
	switch ch := tok[0]; {
	case ch == '"' || ch == '\'' || ch == '`' || ch == '.' && len(tok) > 1:
		return true
	case isWordByte(ch):
		return !token.IsKeyword(tok)
	}
	return false
}

// commentEnd returns the end of the comment at i.
func commentEnd(src string, i int) int {
	if src[i+1] == '/' {
		if k := strings.IndexByte(src[i:], '\n'); k >= 0 {
			return i + k
		}
		return len(src)
	}
	return i + 2 + strings.Index(src[i+2:], "*/") + 2
}

// tokenEnd returns the end of the token that starts at i, in source that go/parser accepts. Only
// literals need care (they may contain what looks like a comment); other tokens are returned one
// character or name at a time.
func tokenEnd(src string, i int) int {
	switch ch := src[i]; {
	case ch == '"' || ch == '\'':
		for j := i + 1; j < len(src); j++ {
			switch src[j] {
			case '\\':
				j++
			case ch:
				return j + 1
			}
		}
		return len(src)
	case ch == '`':
		return i + 1 + strings.IndexByte(src[i+1:], '`') + 1
	case isWordByte(ch):
		j := i + 1
		for j < len(src) && (isWordByte(src[j]) || src[j] == '.' && isDigitByte(src[i]) ||
			(src[j] == '+' || src[j] == '-') && isDigitByte(src[i]) && strings.ContainsRune("eEpP", rune(src[j-1]))) {
			j++
		}
		return j
	case ch == '.' && i+1 < len(src) && isDigitByte(src[i+1]):
		return tokenEnd(src, i+1)
	case ch == '+' || ch == '-':
		if i+1 < len(src) && src[i+1] == ch {
			return i + 2 // ++ and -- end statements
		}
	}
	return i + 1
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }

// stripCR removes carriage returns as go/scanner does in comments.
func stripCR(b string, comment bool) string {
	c := make([]byte, 0, len(b))
	for j := 0; j < len(b); j++ {
		ch := b[j]
		if ch != '\r' || comment && len(c) > len("/*") && c[len(c)-1] == '*' && j+1 < len(b) && b[j+1] == '/' {
			c = append(c, ch)
		}
	}
	return string(c)
}

// updateLineInfo applies the line directive text at offset offs as go/scanner does.
func updateLineInfo(tf *token.File, dir string, next, offs int, text string) {
	if text[1] == '*' {
		text = text[:len(text)-2]
	}
	text = text[7:]
	offs += 7
	i, n, ok := trailingDigits(text)
	if i == 0 || !ok {
		return
	}
	const maxLineCol = 1 << 30
	var line, col int
	i2, n2, ok2 := trailingDigits(text[:i-1])
	if ok2 {
		i, i2 = i2, i
		line, col = n2, n
		if col == 0 || col > maxLineCol {
			return
		}
		text = text[:i2-1]
	} else {
		line = n
	}
	if line == 0 || line > maxLineCol {
		return
	}
	filename := text[:i-1]
	if filename == "" && ok2 {
		filename = tf.Position(tf.Pos(offs)).Filename
	} else if filename != "" {
		filename = filepath.Clean(filename)
		if !filepath.IsAbs(filename) {
			filename = filepath.Join(dir, filename)
		}
	}
	tf.AddLineColumnInfo(next, filename, line, col)
}

func trailingDigits(text string) (int, int, bool) {
	i := strings.LastIndexByte(text, ':')
	if i < 0 {
		return 0, 0, false
	}
	n, err := strconv.ParseUint(text[i+1:], 10, 0)
	return i + 1, int(n), err == nil
}

// commentGrouper groups comments as go/parser does (parser.next and consumeCommentGroup): the
// comments between two tokens (automatic semicolons included) are split where a comment starts more
// than one line after the end of the previous one; the comments that start on the line of the
// preceding token form a group of their own, which ends where a comment starts on a later line.
type commentGrouper struct {
	collect  bool
	groups   []*ast.CommentGroup
	run      []lineComment // the comments since the last token
	prevLine int           // the line where the last token starts (0 before the first)
}

type lineComment struct {
	c             *ast.Comment
	line, endLine int
}

func (g *commentGrouper) comment(c *ast.Comment, line, endLine int) {
	if g.collect {
		g.run = append(g.run, lineComment{c, line, endLine})
	}
}

// token records a token (or an automatic semicolon) that starts on line.
func (g *commentGrouper) token(line int) {
	g.flush()
	g.prevLine = line
}

func (g *commentGrouper) flush() {
	run := g.run
	g.run = nil
	i := 0
	if len(run) > 0 && run[0].line == g.prevLine {
		i = g.group(run, 0, 0)
	}
	for i < len(run) {
		i = g.group(run, i, 1)
	}
}

// group adds the group of comments that starts at run[i], continued while a comment starts at most
// n lines after the end of the previous one, and returns where it ends.
func (g *commentGrouper) group(run []lineComment, i, n int) int {
	j, end := i+1, run[i].endLine
	for j < len(run) && run[j].line <= end+n {
		end = run[j].endLine
		j++
	}
	list := make([]*ast.Comment, j-i)
	for k := range list {
		list[k] = run[i+k].c
	}
	g.groups = append(g.groups, &ast.CommentGroup{List: list})
	return j
}

func (g *commentGrouper) finish() []*ast.CommentGroup {
	g.flush()
	return g.groups
}
