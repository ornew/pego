package grammar

import (
	"strconv"
	"testing"
)

func TestFormatMinimumInteger(t *testing.T) {
	max := int(^uint(0) >> 1)
	minimum := &IntLit{Value: -max - 1}
	expr := "-" + strconv.Itoa(max) + " - 1"
	for _, tc := range []struct {
		term Term
		want string
	}{
		{minimum, expr},
		{&Unary{Op: "-", X: minimum}, "-(" + expr + ")"},
		{&Binary{Op: "+", L: minimum, R: &IntLit{Value: 1}}, expr + " + 1"},
		{&Binary{Op: "-", L: &IntLit{Value: 1}, R: minimum}, "1 - (" + expr + ")"},
		{&Binary{Op: "*", L: minimum, R: &IntLit{Value: 2}}, "(" + expr + ") * 2"},
	} {
		if got := FormatTerm(tc.term); got != tc.want {
			t.Errorf("FormatTerm: got %q; want %q", got, tc.want)
		}
	}
}

func TestFormatMinusFragments(t *testing.T) {
	for _, tc := range []struct {
		expr Expr
		want string
	}{
		{&Discard{Expr: &Discard{Expr: &Literal{Value: "a"}}}, `- -"a"`},
		{&Discard{Expr: &Cut{}}, "- --"},
	} {
		if got := FormatExpr(tc.expr); got != tc.want {
			t.Errorf("FormatExpr: got %q, want %q", got, tc.want)
		}
	}
	for _, x := range []Term{
		&Unary{Op: "-", X: &IntLit{Value: 1}},
		&IntLit{Value: -1},
	} {
		if got := FormatTerm(&Unary{Op: "-", X: x}); got != "- -1" {
			t.Errorf("FormatTerm: got %q, want %q", got, "- -1")
		}
	}
}
