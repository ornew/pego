package grammar

import (
	"strconv"
	"strings"
	"testing"
)

func TestFormatSourceGroupOpeningComments(t *testing.T) {
	for _, oneLine := range []bool{false, true} {
		for _, comment := range []string{"/* documentation */", "// documentation"} {
			open := &LineBreak{Comments: []*Comment{{Text: comment}}}
			for _, out := range []string{
				FormatType(&TypeGroup{Type: &TypeRef{Name: "T"}, OpenBreak: open, OneLine: oneLine}),
				FormatTerm(&TermGroup{Term: &IntLit{Value: 1}, OpenBreak: open, OneLine: oneLine}),
			} {
				if strings.Count(out, comment) != 1 || !strings.HasPrefix(out, "(") || !strings.HasSuffix(out, ")") {
					t.Fatalf("opening comment lost or duplicated: %q", out)
				}
			}
		}
	}
}

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
		{&Discard{Expr: &Cut{}}, "-#cut"},
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

// A list with a trailing comma has one element per line; without one it stays on
// one line. The comma of a one-element tuple does not count.
func TestFormatTrailingComma(t *testing.T) {
	one, two := &IntLit{Value: 1}, &IntLit{Value: 2}
	for _, tc := range []struct {
		name string
		term Term
		want string
	}{
		{"call", &Call{Func: "f", Args: []Term{one, two}}, "f(1, 2)"},
		{"call with a trailing comma", &Call{Func: "f", Args: []Term{one, two}, TrailingComma: true}, "f(\n    1,\n    2,\n)"},
		{"constructor", &New{Type: "N", Args: []Term{one}}, "N{1}"},
		{"constructor with a trailing comma", &New{Type: "N", Args: []Term{one}, TrailingComma: true}, "N{\n    1,\n}"},
		{"list constructor", &ListConstructor{Elem: &TypeRef{Name: "int"}, Items: []Term{one, two}}, "[]int{1, 2}"},
		{"list constructor with a trailing comma", &ListConstructor{Elem: &TypeRef{Name: "int"}, Items: []Term{one, two}, TrailingComma: true}, "[]int{\n    1,\n    2,\n}"},
		{"tuple", &TupleLit{Elems: []Term{one, two}}, "(1, 2)"},
		{"tuple with a trailing comma", &TupleLit{Elems: []Term{one, two}, TrailingComma: true}, "(\n    1,\n    2,\n)"},
		{"tuple of one element", &TupleLit{Elems: []Term{one}}, "(1,)"},
		{"tuple of one element with a trailing comma", &TupleLit{Elems: []Term{one}, TrailingComma: true}, "(1,)"},
		{"nested", &Call{Func: "f", Args: []Term{&TupleLit{Elems: []Term{one, two}, TrailingComma: true}}}, "f(\n    (\n        1,\n        2,\n    ),\n)"},
	} {
		if got := FormatTerm(tc.term); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	a, b := &TypeRef{Name: "A"}, &TypeRef{Name: "B"}
	for _, tc := range []struct {
		name string
		typ  TypeExpr
		want string
	}{
		{"tuple type", &TupleType{Types: []TypeExpr{a, b}}, "(A, B)"},
		{"tuple type with a trailing comma", &TupleType{Types: []TypeExpr{a, b}, TrailingComma: true}, "(\n    A,\n    B,\n)"},
		{"tuple type of one element with a trailing comma", &TupleType{Types: []TypeExpr{a}, TrailingComma: true}, "(A,)"},
		{"type arguments", &TypeApp{Name: "node", Args: []TypeExpr{a}}, "node[A]"},
		{"type arguments with a trailing comma", &TypeApp{Name: "node", Args: []TypeExpr{a}, TrailingComma: true}, "node[\n    A,\n]"},
	} {
		if got := FormatType(tc.typ); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
