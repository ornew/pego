package cue

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A SemanticError is an error that the syntax does not show, found after parsing: a name declared twice in a
// scope where that is not allowed, or an alias that may not be the blank identifier. Span is the node in error,
// in the position unit of the parse; ParseFile and Valid also give its line and column, 1-based and in code
// points, as SyntaxError does.
type SemanticError struct {
	Span
	Line, Col int // 0 if unknown
	Msg       string
}

func (e *SemanticError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("cue: %d:%d: %s", e.Line, e.Col, e.Msg)
	}
	return fmt.Sprintf("cue: at %d: %s", e.Start, e.Msg)
}

// Check reports the errors that cuelang.org/go/cue/parser finds besides those of the syntax: a label in square
// brackets (a pattern constraint) that does not have exactly one element, a syntax that
// the file has not enabled with an @experiment attribute (the grammar reads the syntax of all experiments: the
// postfix "..." of explicitopen, the postfix alias of aliasv2 and the else and otherwise clauses of try), an
// experiment that does not exist, an alias of the old kind in a file that has enabled aliasv2, an import path that
// is not valid (it has a character that a path cannot have, or is empty), and errors in the scopes of the file
// (astutil.Resolve): an alias or let clause declared twice in a scope, a field that has the name of
// an alias of the same scope, a field with both a label alias and a postfix alias, an alias that is the
// blank identifier, and a reference in a pattern constraint to a field of its struct. Of the errors, it
// returns the first in the order of the source, a *SemanticError.
func (f *File) Check() error {
	c := &checker{}
	c.experiments(f)
	for _, d := range f.Decls {
		if x, ok := d.(*ImportDecl); ok {
			for _, spec := range x.Specs {
				if !isValidImport(spec.Path.Text) {
					c.errf(spec.Path, "invalid import path: %s", spec.Path.Text)
				}
			}
		}
	}
	root := &scope{index: map[string]entry{}, node: f}
	c.fill(root, f.Decls)
	s := root
	for _, d := range f.Decls {
		c.walk(s, d)
	}
	if len(c.errs) == 0 {
		return nil
	}
	first := c.errs[0]
	for _, e := range c.errs[1:] {
		if e.Start < first.Start {
			first = e
		}
	}
	return first
}

// A scope is a block with its declared names, as in astutil.
type scope struct {
	outer   *scope
	node    any // the file, struct, field or clause that opens the scope
	index   map[string]entry
	inField bool
}

type entry struct {
	node, link any
	field      *Field // for a label alias of a pattern constraint
}

type checker struct {
	errs []*SemanticError
}

func (c *checker) errf(n any, format string, args ...any) {
	c.errs = append(c.errs, &SemanticError{Span: SpanOf(n), Msg: fmt.Sprintf(format, args...)})
}

func (c *checker) newScope(outer *scope, node any, decls []Decl) *scope {
	s := &scope{outer: outer, node: node, index: map[string]entry{}}
	c.fill(s, decls)
	return s
}

// fill declares the names of the declarations of a file or struct.
func (c *checker) fill(s *scope, decls []Decl) {
	for _, d := range decls {
		switch x := d.(type) {
		case *Field:
			if a, ok := x.Label.(*Alias); ok {
				if _, isList := a.Expr.(*ListLit); !isList {
					c.insert(s, a.Ident.Text, x, a, nil)
				}
				if x.Alias != nil {
					c.errf(x, "field has both label alias and postfix alias")
				}
			}
			if _, isPattern := x.Label.(*ListLit); !isPattern && x.Alias != nil {
				c.insertPostfixAliases(s, x, x.Alias.Label)
			}
			if name, ok := labelName(x.Label); ok {
				v := x.Value
				if a, ok := v.(*Alias); ok {
					v = a.Expr
				}
				c.insert(s, name, v, x, nil)
			}
		case *LetClause:
			if name := x.Ident.Text; ast_isValidIdent(name) {
				c.insert(s, name, x, x, nil)
			}
		case *ImportDecl:
			for _, spec := range x.Specs {
				if name := importIdent(spec); name != "" {
					c.insert(s, name, spec, spec, nil)
				}
			}
		}
	}
}

// importIdent returns the identifier that an import declares: its name, or the package qualifier of its path.
func importIdent(spec *ImportSpec) string {
	path, err := strconv.Unquote(spec.Path.Text)
	if err != nil {
		return ""
	}
	if spec.Name != nil {
		return spec.Name.Text
	}
	// ast.ParseImportPath: an explicit qualifier after the last colon, otherwise the last element of
	// the path without its version.
	if i := strings.LastIndexAny(path, "/:"); i >= 0 && path[i] == ':' {
		return path[i+1:]
	}
	path, _, _ = strings.Cut(path, "@")
	q := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		q = path[i+1:]
	}
	if !ast_isValidIdent(q) || strings.HasPrefix(q, "#") || q == "_" {
		return ""
	}
	return q
}

// labelName returns the name of a label and whether it is an identifier (ast.LabelName).
func labelName(l Label) (string, bool) {
	if a, ok := l.(*Alias); ok {
		l, _ = a.Expr.(Label)
	}
	switch n := l.(type) {
	case *Ident:
		if !ast_isValidIdent(n.Text) {
			return "", false
		}
		return n.Text, true
	case *Bool:
		return n.Text, true
	case *Null:
		return n.Text, true
	}
	return "", false
}

// ast_isValidIdent reports whether str is a valid identifier (ast.IsValidIdent).
func ast_isValidIdent(ident string) bool {
	if ident == "" {
		return false
	}
	ident, consumed := strings.CutPrefix(ident, "_")
	if ident == "" {
		return true
	}
	ident, consumedHash := strings.CutPrefix(ident, "#")
	if consumedHash {
		consumed = false
	}
	if !consumed {
		if r, _ := utf8.DecodeRuneInString(ident); isDigitRune(r) {
			return false
		}
	}
	for _, r := range ident {
		if isLetterRune(r) || isDigitRune(r) || r == '_' || r == '$' {
			continue
		}
		return false
	}
	return true
}

func isLetterRune(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch >= utf8.RuneSelf && unicode.IsLetter(ch)
}

func isDigitRune(ch rune) bool {
	return '0' <= ch && ch <= '9' || ch >= utf8.RuneSelf && unicode.IsDigit(ch)
}

// isLet reports whether a declaration is an alias or let clause rather than a field (scope.isLet).
func (s *scope) isLet(n, link any) bool {
	if _, ok := s.node.(*Field); ok {
		return true
	}
	if _, ok := link.(*PostfixAlias); ok {
		return true
	}
	switch n.(type) {
	case *LetClause, *TryClause, *Alias, *Field:
		return true
	}
	return false
}

// mustBeUnique reports whether the name of a declaration may be declared once in its scope.
func (s *scope) mustBeUnique(n, link any) bool {
	if _, ok := s.node.(*Field); ok {
		return true
	}
	if _, ok := link.(*PostfixAlias); ok {
		return true
	}
	switch n.(type) {
	case *ImportSpec, *LetClause, *TryClause, *Alias, *Field:
		return true
	}
	return false
}

func (s *scope) lookup(name string) (*scope, entry, bool) {
	if name == "_" {
		return nil, entry{}, false
	}
	for ; s != nil; s = s.outer {
		if e, ok := s.index[name]; ok {
			return s, e, true
		}
	}
	return nil, entry{}, false
}

func (c *checker) insert(s *scope, name string, n, link any, f *Field) {
	if name == "" {
		return
	}
	if outer, existing, ok := s.lookup(name); ok && existing.node != nil {
		if s.isLet(n, link) != outer.isLet(existing.node, existing.link) {
			c.errf(n, "cannot have both alias and field with name %q in same scope", name)
			return
		} else if s.mustBeUnique(n, link) || outer.mustBeUnique(existing.node, existing.link) {
			if outer == s {
				if _, ok := existing.node.(*ImportSpec); ok {
					return
				}
				c.errf(n, "alias %q redeclared in same scope", name)
				return
			}
		}
	}
	s.index[name] = entry{n, link, f}
}

func (c *checker) insertPostfixAliases(s *scope, x *Field, expr any) {
	a := x.Alias
	if a == nil {
		return
	}
	hasField := a.Field != nil && a.Field.Text != "_"
	if a.Label == nil {
		if !hasField {
			c.errf(a, "single postfix alias %q field cannot be the blank identifier", a.Field.Text)
		} else {
			c.insert(s, a.Field.Text, x, a, nil)
		}
		return
	}
	hasLabel := a.Label != nil && a.Label.Text != "_"
	if !hasField && !hasLabel {
		c.errf(a, "both label and field in postfix alias cannot be the blank identifier")
		return
	}
	if hasLabel {
		c.insert(s, a.Label.Text, expr, a, x)
	}
	if hasField {
		c.insert(s, a.Field.Text, x, a, nil)
	}
}

// walk visits a node in scope s, as scope.Before of astutil does.
func (c *checker) walk(s *scope, n any) {
	switch x := n.(type) {
	case nil:
	case *StructLit:
		inner := c.newScope(s, x, x.Elts)
		for _, e := range x.Elts {
			c.walk(inner, e)
		}
	case *Comprehension:
		inner := c.scopeClauses(s, x.Clauses)
		c.walk(inner, x.Value)
		if x.Fallback != nil {
			c.walk(s, x.Fallback.Body)
		}
	case *Field:
		c.walkField(s, x)
	case *LetClause:
		name := x.Ident.Text
		saved := s.index[name]
		delete(s.index, name)
		s.inField = true
		c.walk(s, x.Expr)
		s.inField = false
		s.index[name] = saved
	case *Alias:
		name := x.Ident.Text
		saved := s.index[name]
		delete(s.index, name)
		c.walk(s, x.Expr)
		s.index[name] = saved
	case *ImportDecl:
	case *SelectorExpr:
		c.walk(s, x.X)
	default:
		for _, ch := range children(n) {
			c.walk(s, ch)
		}
	}
}

func (c *checker) walkField(s *scope, x *Field) {
	var n any = x.Label
	alias, hasAlias := x.Label.(*Alias)
	if hasAlias {
		n = alias.Expr
	}
	switch label := n.(type) {
	case *ParenExpr:
		c.walk(s, label)
	case *Interpolation:
		c.walk(s, label)
	case *ListLit:
		if len(label.Elts) != 1 {
			break
		}
		s = c.newScope(s, x, nil)
		if hasAlias {
			if name, ok := labelName(alias.Ident); ok && name != "" {
				c.insert(s, name, x, alias, nil)
			}
		}
		expr := label.Elts[0]
		if a, ok := expr.(*Alias); ok {
			if x.Alias != nil {
				c.errf(x, "pattern constraint has both label alias and postfix alias")
			}
			expr = a.Expr
			c.insert(s, a.Ident.Text, a.Expr, a, x)
		} else {
			c.insertPostfixAliases(s, x, expr)
		}
		inspect(expr, func(n any) {
			if id, ok := n.(*Ident); ok {
				for sc := s; sc != nil && !sc.inField; sc = sc.outer {
					if _, ok := sc.index[id.Text]; ok {
						c.errf(id, "reference %q in label expression refers to field against which it would be matched", id.Text)
					}
				}
			}
		})
		c.walk(s, expr)
	}
	if x.Value != nil {
		v := x.Value
		if a, ok := v.(*Alias); ok {
			s = c.newScope(s, x, nil)
			c.insert(s, a.Ident.Text, a, x, nil)
			v = a.Expr
		}
		s.inField = true
		c.walk(s, v)
		s.inField = false
	}
}

func (c *checker) scopeClauses(s *scope, clauses []Clause) *scope {
	for _, cl := range clauses {
		switch x := cl.(type) {
		case *ForClause:
			c.walk(s, x.Source)
			s = &scope{outer: s, node: x, index: map[string]entry{}}
			if x.Key != nil {
				c.insert(s, x.Key.Text, x.Key, x, nil)
			}
			c.insert(s, x.Value.Text, x.Value, x, nil)
		case *LetClause:
			c.walk(s, x.Expr)
			s = &scope{outer: s, node: x, index: map[string]entry{}}
			c.insert(s, x.Ident.Text, x.Ident, x, nil)
		case *TryClause:
			if x.Ident != nil {
				c.walk(s, x.Expr)
				s = &scope{outer: s, node: x, index: map[string]entry{}}
				c.insert(s, x.Ident.Text, x.Ident, x, nil)
			}
		case *IfClause:
			c.walk(s, x.Condition)
		}
	}
	return s
}

// children returns the child nodes of a node, as ast.Walk visits them.
func children(n any) []any {
	var out []any
	add := func(xs ...any) {
		for _, x := range xs {
			if !isNil(x) {
				out = append(out, x)
			}
		}
	}
	switch x := n.(type) {
	case *Field:
		add(x.Label)
		if x.Alias != nil {
			add(x.Alias)
		}
		add(x.Value)
		for _, a := range x.Attrs {
			add(a)
		}
	case *StructLit:
		for _, e := range x.Elts {
			add(e)
		}
	case *Interpolation:
		for _, e := range x.Elts {
			add(e)
		}
	case *ListLit:
		for _, e := range x.Elts {
			add(e)
		}
	case *Ellipsis:
		add(x.Type)
	case *ParenExpr:
		add(x.X)
	case *SelectorExpr:
		add(x.X, x.Sel)
	case *IndexExpr:
		add(x.X, x.Index)
	case *SliceExpr:
		add(x.X, x.Low, x.High)
	case *CallExpr:
		add(x.Fun)
		for _, a := range x.Args {
			add(a)
		}
	case *UnaryExpr:
		add(x.X)
	case *BinaryExpr:
		add(x.X, x.Y)
	case *PostfixExpr:
		add(x.X)
	case *ImportSpec:
		if x.Name != nil {
			add(x.Name)
		}
		add(x.Path)
	case *ImportDecl:
		for _, s := range x.Specs {
			add(s)
		}
	case *EmbedDecl:
		add(x.Expr)
		if x.Alias != nil {
			add(x.Alias)
		}
	case *LetClause:
		add(x.Ident, x.Expr)
	case *TryClause:
		if x.Ident != nil {
			add(x.Ident, x.Expr)
		}
	case *Alias:
		add(x.Ident, x.Expr)
	case *PostfixAlias:
		if x.Label != nil {
			add(x.Label)
		}
		add(x.Field)
	case *Comprehension:
		for _, cl := range x.Clauses {
			add(cl)
		}
		add(x.Value)
		if x.Fallback != nil {
			add(x.Fallback)
		}
	case *Package:
		add(x.Name)
	case *ForClause:
		if x.Key != nil {
			add(x.Key)
		}
		add(x.Value, x.Source)
	case *IfClause:
		add(x.Condition)
	case *FallbackClause:
		add(x.Body)
	}
	return out
}

func isNil(x any) bool {
	return x == nil
}

// inspect calls f for every node of the tree of n, children first.
func inspect(n any, f func(any)) {
	for _, ch := range children(n) {
		inspect(ch, f)
	}
	f(n)
}

// SpanOf returns the span of a node of the syntax tree (a pointer to one of the types of the AST). It panics for
// a value that is not a node.
func SpanOf(x any) Span {
	switch x := x.(type) {
	case *File:
		return x.Span
	case *Alias:
		return x.Span
	case *Attribute:
		return x.Span
	case *BinaryExpr:
		return x.Span
	case *Bool:
		return x.Span
	case *Bottom:
		return x.Span
	case *CallExpr:
		return x.Span
	case *Comprehension:
		return x.Span
	case *Ellipsis:
		return x.Span
	case *EmbedDecl:
		return x.Span
	case *FallbackClause:
		return x.Span
	case *Field:
		return x.Span
	case *Float:
		return x.Span
	case *ForClause:
		return x.Span
	case *Ident:
		return x.Span
	case *IfClause:
		return x.Span
	case *ImportDecl:
		return x.Span
	case *ImportSpec:
		return x.Span
	case *IndexExpr:
		return x.Span
	case *Int:
		return x.Span
	case *Interpolation:
		return x.Span
	case *LetClause:
		return x.Span
	case *ListLit:
		return x.Span
	case *Null:
		return x.Span
	case *Package:
		return x.Span
	case *ParenExpr:
		return x.Span
	case *PostfixAlias:
		return x.Span
	case *PostfixExpr:
		return x.Span
	case *SelectorExpr:
		return x.Span
	case *SliceExpr:
		return x.Span
	case *String:
		return x.Span
	case *StructLit:
		return x.Span
	case *TryClause:
		return x.Span
	case *UnaryExpr:
		return x.Span
	}
	panic(fmt.Sprintf("cue: SpanOf %T", x))
}

// experiments checks the @experiment attributes at the start of the file, and the use of the syntax that they enable.
func (c *checker) experiments(f *File) {
	var try, aliasV2, explicitOpen bool
	for _, d := range f.Decls {
		a, ok := d.(*Attribute)
		if !ok {
			break
		}
		name, body := a.Split()
		if name != "experiment" || body == "" {
			continue
		}
		for _, elem := range strings.Split(body, ",") {
			switch strings.TrimSpace(elem) {
			case "testing", "accepted_", "structcmp", "shortcircuit":
			case "try":
				try = true
			case "aliasv2":
				aliasV2 = true
			case "explicitopen":
				explicitOpen = true
			default:
				c.errf(a, "unknown experiment %q", strings.TrimSpace(elem))
			}
		}
	}
	for _, d := range f.Decls {
		inspect(d, func(n any) {
			switch x := n.(type) {
			case *PostfixExpr:
				if x.Op.Text == "..." && !explicitOpen {
					c.errf(x, "postfix ... operator requires @experiment(explicitopen)")
				}
			case *Field:
				if l, ok := x.Label.(*ListLit); ok && len(l.Elts) != 1 {
					c.errf(l, "square bracket must have exactly one element")
				}
				if x.Alias != nil && !aliasV2 {
					c.errf(x.Alias, "postfix alias syntax requires @experiment(aliasv2)")
				}
			case *EmbedDecl:
				if x.Alias != nil && !aliasV2 {
					c.errf(x.Alias, "postfix alias syntax requires @experiment(aliasv2)")
				}
			case *Alias:
				if aliasV2 {
					c.errf(x, "old-style alias syntax (=) is not allowed with @experiment(aliasv2); use postfix syntax (~X or ~(K,V))")
				}
			case *FallbackClause:
				if !try {
					c.errf(x, "else requires @experiment(try)")
				}
			}
		})
	}
}
