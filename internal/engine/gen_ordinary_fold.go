package engine

import (
	goast "go/ast"
	"go/token"
)

// foldOrdinaryControl simplifies the conditions that rewriteOrdinaryControl left constant. A read
// of the control became a typed nil, so its comparison with nil is known, and the boolean
// operators around it fold to the operand that remains. Without this, each class test of the
// nil-control branch kept the dead clause for a running control.
func foldOrdinaryControl(root goast.Node) {
	goast.Inspect(root, func(n goast.Node) bool {
		switch x := n.(type) {
		case *goast.IfStmt:
			x.Cond = foldOrdinaryExpr(x.Cond)
		case *goast.ReturnStmt:
			for i := range x.Results {
				x.Results[i] = foldOrdinaryExpr(x.Results[i])
			}
		case *goast.AssignStmt:
			for i := range x.Rhs {
				x.Rhs[i] = foldOrdinaryExpr(x.Rhs[i])
			}
		}
		return true
	})
}

func isTypedNilControl(e goast.Expr) bool {
	call, ok := e.(*goast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if id, ok := call.Args[0].(*goast.Ident); !ok || id.Name != "nil" {
		return false
	}
	paren, ok := call.Fun.(*goast.ParenExpr)
	if !ok {
		return false
	}
	star, ok := paren.X.(*goast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*goast.Ident)
	return ok && id.Name == "WorkControl"
}

func boolConstant(e goast.Expr) (value, known bool) {
	for {
		p, ok := e.(*goast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	if id, ok := e.(*goast.Ident); ok && (id.Name == "true" || id.Name == "false") {
		return id.Name == "true", true
	}
	return false, false
}

func boolIdent(v bool) goast.Expr {
	if v {
		return goast.NewIdent("true")
	}
	return goast.NewIdent("false")
}

func foldOrdinaryExpr(e goast.Expr) goast.Expr {
	switch x := e.(type) {
	case *goast.ParenExpr:
		x.X = foldOrdinaryExpr(x.X)
		if _, ok := x.X.(*goast.ParenExpr); ok {
			return x.X
		}
		if _, ok := x.X.(*goast.Ident); ok {
			return x.X
		}
		return x
	case *goast.BinaryExpr:
		x.X, x.Y = foldOrdinaryExpr(x.X), foldOrdinaryExpr(x.Y)
		if (x.Op == token.EQL || x.Op == token.NEQ) && isTypedNilControl(x.X) {
			if id, ok := x.Y.(*goast.Ident); ok && id.Name == "nil" {
				return boolIdent(x.Op == token.EQL)
			}
		}
		if x.Op != token.LAND && x.Op != token.LOR {
			return x
		}
		decides := x.Op == token.LOR // the constant that decides the operator
		if v, ok := boolConstant(x.X); ok {
			if v == decides {
				return boolIdent(decides)
			}
			return x.Y
		}
		if v, ok := boolConstant(x.Y); ok {
			if v == decides {
				// Dropping the left operand is safe only when it has no effects.
				if pureCondition(x.X) {
					return boolIdent(decides)
				}
				return x
			}
			return x.X
		}
	case *goast.UnaryExpr:
		x.X = foldOrdinaryExpr(x.X)
		if v, ok := boolConstant(x.X); ok && x.Op == token.NOT {
			return boolIdent(!v)
		}
	}
	return e
}

// pureCondition reports whether e reads only names and constants, so that dropping it cannot
// change what a program does.
func pureCondition(e goast.Expr) bool {
	switch x := e.(type) {
	case *goast.Ident, *goast.SelectorExpr, *goast.BasicLit:
		return true
	case *goast.ParenExpr:
		return pureCondition(x.X)
	case *goast.UnaryExpr:
		return x.Op == token.NOT && pureCondition(x.X)
	case *goast.BinaryExpr:
		return pureCondition(x.X) && pureCondition(x.Y)
	}
	return false
}
