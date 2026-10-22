package engine

import (
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"sort"
	"strings"
)

// trimComments removes the comments of the generated source that explain unexported code: nobody
// reads generated code, and the comments are a few percent of its size. It keeps the file header,
// the package comment, compiler directives (`//go:`, `// +build`, `//line`, `/*line`), lint
// directives (`//nolint`), the comments of exported declarations (the API documentation,
// including their bodies), and the markers that begin with "rule " and name the grammar rule of
// the code that follows.
func trimComments(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "parser.go", src, goparser.ParseComments)
	if err != nil {
		return nil, err
	}
	type span struct{ from, to int }
	var kept []span
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	for _, d := range f.Decls {
		if !exportedDecl(d) {
			continue
		}
		from := d.Pos()
		switch d := d.(type) {
		case *goast.FuncDecl:
			if d.Doc != nil {
				from = d.Doc.Pos()
			}
		case *goast.GenDecl:
			if d.Doc != nil {
				from = d.Doc.Pos()
			}
		}
		kept = append(kept, span{offset(from), offset(d.End())})
	}
	inKept := func(off int) bool {
		for _, k := range kept {
			if off >= k.from && off < k.to {
				return true
			}
		}
		return false
	}
	var cut []span
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			off := offset(c.Pos())
			text := c.Text
			switch {
			case inKept(off),
				strings.HasPrefix(text, "// Code generated"),
				strings.HasPrefix(text, "// Package "),
				strings.HasPrefix(text, "//go:"),
				strings.HasPrefix(text, "// +build"),
				strings.HasPrefix(text, "//nolint"),
				strings.HasPrefix(text, "//line "),
				strings.HasPrefix(text, "/*line "),
				strings.HasPrefix(text, "// rule "):
				continue
			}
			cut = append(cut, span{off, off + len(text)})
		}
	}
	if len(cut) == 0 {
		return src, nil
	}
	sort.Slice(cut, func(i, j int) bool { return cut[i].from < cut[j].from })
	var out []byte
	last := 0
	for _, c := range cut {
		from, to := c.from, c.to
		// A comment alone on its line takes the line with it.
		ls := from
		for ls > last && (src[ls-1] == ' ' || src[ls-1] == '\t') {
			ls--
		}
		if (ls == 0 || src[ls-1] == '\n') && to < len(src) && src[to] == '\n' {
			from, to = ls, to+1
		}
		out = append(out, src[last:from]...)
		last = to
	}
	out = append(out, src[last:]...)
	return format.Source(out)
}

// exportedDecl reports whether d declares an exported name (for a method, of an exported type).
func exportedDecl(d goast.Decl) bool {
	switch d := d.(type) {
	case *goast.FuncDecl:
		if !d.Name.IsExported() {
			return false
		}
		if d.Recv == nil || len(d.Recv.List) == 0 {
			return true
		}
		t := d.Recv.List[0].Type
		for {
			switch x := t.(type) {
			case *goast.StarExpr:
				t = x.X
				continue
			case *goast.IndexExpr:
				t = x.X
				continue
			case *goast.IndexListExpr:
				t = x.X
				continue
			case *goast.Ident:
				return x.IsExported()
			}
			return false
		}
	case *goast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *goast.TypeSpec:
				if s.Name.IsExported() {
					return true
				}
			case *goast.ValueSpec:
				for _, n := range s.Names {
					if n.IsExported() {
						return true
					}
				}
			}
		}
	}
	return false
}
