package engine

import (
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"sort"
	"strings"
)

// pruneTypedEntries removes what only an entry of a typed rule other than a start rule could
// reach, from the source of a generated parser.
//
// The runtime calls the Entry of a typed rule only to run a start rule (TParse and
// TParseResolved); every other rule is called through the functions that name it. So the
// statements trules[N].Entry = f of the rules that no function outside init names by index (the
// start rules, which ParseAST and ParseRuleAST name) are dropped, and then the unexported
// functions and package variables that nothing reachable names are dropped too. A function whose
// local variable shares its name counts as naming it, which keeps more than needed, never less.
//
// The generator emits one statement per line and decimal indexes, which is all the matching of
// the entry statements and of the indexes relies on.
func pruneTypedEntries(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "", src, goparser.ParseComments)
	if err != nil {
		return nil, err
	}
	type span struct{ from, to int }
	off := func(p token.Pos) int { return fset.Position(p).Offset }

	// prunable declarations, by name
	type decl struct {
		node    goast.Node
		span    span
		reached bool
	}
	decls := map[string]*decl{}
	var roots []goast.Node
	var inits []*goast.FuncDecl
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *goast.FuncDecl:
			switch {
			case d.Recv == nil && d.Name.Name == "init":
				inits = append(inits, d)
			case d.Recv == nil && !d.Name.IsExported() && d.Name.Name != "main" && d.Name.Name != "_":
				from := d.Pos()
				if d.Doc != nil {
					from = d.Doc.Pos()
				}
				decls[d.Name.Name] = &decl{node: d, span: span{off(from), off(d.End())}}
			default:
				roots = append(roots, d)
			}
		case *goast.GenDecl:
			if d.Tok == token.VAR && len(d.Specs) == 1 {
				vs := d.Specs[0].(*goast.ValueSpec)
				if len(vs.Names) == 1 && !vs.Names[0].IsExported() && vs.Names[0].Name != "_" {
					from := d.Pos()
					if d.Doc != nil {
						from = d.Doc.Pos()
					}
					decls[vs.Names[0].Name] = &decl{node: d, span: span{off(from), off(d.End())}}
					continue
				}
			}
			roots = append(roots, d)
		}
	}

	// the start rules: indexes trules[N] named outside init
	starts := map[string]bool{}
	for _, r := range roots {
		indexed := map[*goast.Ident]bool{}
		goast.Inspect(r, func(n goast.Node) bool {
			switch n := n.(type) {
			case *goast.ValueSpec:
				for _, id := range n.Names {
					indexed[id] = true // the declaration of the table
				}
			case *goast.IndexExpr:
				if id, ok := n.X.(*goast.Ident); ok && id.Name == "trules" {
					if bl, ok := n.Index.(*goast.BasicLit); ok {
						starts[bl.Value] = true
						indexed[id] = true
					}
				}
			}
			return true
		})
		goast.Inspect(r, func(n goast.Node) bool {
			if id, ok := n.(*goast.Ident); ok && id.Name == "trules" && !indexed[id] {
				starts["*"] = true
			}
			return true
		})
	}
	// Any other use of the table (a range, an argument, a variable index) keeps every entry.
	if starts["*"] {
		return src, nil
	}

	// the statements of init that stay, and the Entry statements that go
	var cuts []span
	for _, in := range inits {
		for _, st := range in.Body.List {
			if as, ok := st.(*goast.AssignStmt); ok && len(as.Lhs) == 1 {
				if se, ok := as.Lhs[0].(*goast.SelectorExpr); ok && se.Sel.Name == "Entry" {
					if ie, ok := se.X.(*goast.IndexExpr); ok {
						if id, ok := ie.X.(*goast.Ident); ok && id.Name == "trules" {
							if bl, ok := ie.Index.(*goast.BasicLit); ok && !starts[bl.Value] {
								cuts = append(cuts, span{off(st.Pos()), off(st.End())})
								continue
							}
						}
					}
				}
			}
			roots = append(roots, st)
		}
	}

	var visit func(n goast.Node)
	visit = func(n goast.Node) {
		goast.Inspect(n, func(x goast.Node) bool {
			id, ok := x.(*goast.Ident)
			if !ok {
				return true
			}
			if d := decls[id.Name]; d != nil && !d.reached {
				d.reached = true
				visit(d.node)
			}
			return true
		})
	}
	for _, r := range roots {
		visit(r)
	}
	for _, d := range decls {
		if !d.reached {
			cuts = append(cuts, d.span)
		}
	}
	if len(cuts) == 0 {
		return src, nil
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].from < cuts[j].from })
	var out strings.Builder
	out.Grow(len(src))
	at := 0
	for _, c := range cuts {
		out.Write(src[at:c.from])
		at = c.to
	}
	out.Write(src[at:])
	return format.Source([]byte(out.String()))
}
