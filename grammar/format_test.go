package grammar

import "testing"

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
