package grammar_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// Embedded AST nodes promote their interface methods even in other packages.
type wrappedExpr struct {
	*grammar.Top
	Data []int
	Pos  int
}
type wrappedStatement struct {
	*grammar.RuleDef
	Data []int
}
type wrappedType struct {
	*grammar.TypeRef
	Data []int
}
type wrappedTypeDecl struct {
	*grammar.TypeDef
	Data []int
}
type wrappedTerm struct {
	*grammar.NilLit
	Data []int
}

func TestValidateUnsupportedEmbeddedNodes(t *testing.T) {
	for _, g := range []*grammar.Grammar{
		{File: grammar.File{Decls: []grammar.Decl{wrappedStatement{}}}},
		{File: grammar.File{Decls: []grammar.Decl{wrappedTypeDecl{}}}},
		{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "p", Type: wrappedType{}, Expr: &grammar.Top{}}}}},
		{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "p", Expr: wrappedExpr{}}}}},
		{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "p", Expr: &wrappedExpr{}}}}},
		{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "p", Expr: &grammar.Top{}, Action: wrappedTerm{}}}}},
	} {
		var e *grammar.ValidationError
		if err := grammar.Validate(g); !errors.As(err, &e) || !strings.Contains(e.Msg, "unsupported") {
			t.Fatalf("got %v; want unsupported-node validation error", err)
		}
	}
}
