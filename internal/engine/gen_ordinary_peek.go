package engine

import (
	goast "go/ast"
	"go/token"
)

// compactOrdinaryPeeks replaces the common-case read of the next character that the generator
// writes out,
//
//	if p.Pos < len(p.In) { ch, size, ok = p.In[p.Pos], 1, true } else { ch, size, ok = p.PeekOrdinary() }
//
// (after the charges are removed) by ch, size, ok = p.NextOrdinary(), which the compiler inlines to
// the same code.
func compactOrdinaryPeeks(root goast.Node) {
	goast.Inspect(root, func(n goast.Node) bool {
		var list *[]goast.Stmt
		switch x := n.(type) {
		case *goast.BlockStmt:
			list = &x.List
		case *goast.CaseClause:
			list = &x.Body
		case *goast.CommClause:
			list = &x.Body
		}
		if list == nil {
			return true
		}
		for i, s := range *list {
			if a := compactPeek(s); a != nil {
				(*list)[i] = a
			}
		}
		return true
	})
}

func compactPeek(s goast.Stmt) goast.Stmt {
	ifs, ok := s.(*goast.IfStmt)
	if !ok || ifs.Init != nil || len(ifs.Body.List) != 1 {
		return nil
	}
	els, ok := ifs.Else.(*goast.BlockStmt)
	if !ok || len(els.List) != 1 || !endsWithPosCheck(ifs.Cond) {
		return nil
	}
	fast, ok1 := ifs.Body.List[0].(*goast.AssignStmt)
	slow, ok2 := els.List[0].(*goast.AssignStmt)
	if !ok1 || !ok2 || fast.Tok != token.ASSIGN || slow.Tok != token.ASSIGN || len(fast.Lhs) != 3 || len(slow.Lhs) != 3 || len(fast.Rhs) != 3 || len(slow.Rhs) != 1 {
		return nil
	}
	for i := range fast.Lhs {
		a, ok1 := fast.Lhs[i].(*goast.Ident)
		b, ok2 := slow.Lhs[i].(*goast.Ident)
		if !ok1 || !ok2 || a.Name != b.Name {
			return nil
		}
	}
	idx, ok := fast.Rhs[0].(*goast.IndexExpr)
	if !ok || !isParserField(idx.X, "In") || !isParserField(idx.Index, "Pos") {
		return nil
	}
	if lit, ok := fast.Rhs[1].(*goast.BasicLit); !ok || lit.Kind != token.INT || lit.Value != "1" {
		return nil
	}
	if id, ok := fast.Rhs[2].(*goast.Ident); !ok || id.Name != "true" {
		return nil
	}
	call, ok := slow.Rhs[0].(*goast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil
	}
	sel, ok := call.Fun.(*goast.SelectorExpr)
	if !ok || !ordinaryReceiver(sel.X) || sel.Sel.Name != "PeekOrdinary" {
		return nil
	}
	return &goast.AssignStmt{Lhs: slow.Lhs, Tok: token.ASSIGN, Rhs: []goast.Expr{&goast.CallExpr{Fun: &goast.SelectorExpr{X: sel.X, Sel: goast.NewIdent("NextOrdinary")}}}}
}

func isParserField(e goast.Expr, name string) bool {
	sel, ok := e.(*goast.SelectorExpr)
	return ok && sel.Sel.Name == name && ordinaryReceiver(sel.X)
}

// endsWithPosCheck reports whether cond is p.Pos < len(p.In), alone or after the test
// p.Document == nil, which holds in the ordinary branch.
func endsWithPosCheck(cond goast.Expr) bool {
	b, ok := cond.(*goast.BinaryExpr)
	if !ok {
		return false
	}
	if b.Op == token.LAND {
		l, ok := b.X.(*goast.BinaryExpr)
		if !ok || l.Op != token.EQL || !isParserField(l.X, "Document") {
			return false
		}
		if id, ok := l.Y.(*goast.Ident); !ok || id.Name != "nil" {
			return false
		}
		return endsWithPosCheck(b.Y)
	}
	if b.Op != token.LSS || !isParserField(b.X, "Pos") {
		return false
	}
	call, ok := b.Y.(*goast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	fn, ok := call.Fun.(*goast.Ident)
	return ok && fn.Name == "len" && isParserField(call.Args[0], "In")
}
