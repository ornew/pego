package sample

import (
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestAnalysisLengthSaturation(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name         string
		a, b         int
		sum, product int
	}{
		{"empty", 0, maxInt, inf, 0},
		{"large nullable repeat", maxInt, 0, inf, 0},
		{"small", 2, 3, 5, 6},
		{"exact limit", inf / 2, 2, inf/2 + 2, inf},
		{"near limit", inf - 1, 1, inf, inf - 1},
		{"two infinite", inf, inf, inf, inf},
		{"large minimum", maxInt, 1, inf, inf},
		{"large product", maxInt, maxInt, inf, inf},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &info{rules: map[string]*ruleInfo{
				"a": {length: tc.a}, "b": {length: tc.b},
			}}
			a, b := &grammar.Ref{Name: "a"}, &grammar.Ref{Name: "b"}
			if got := in.length(&grammar.Seq{Items: []grammar.Expr{a, b}}); got != tc.sum {
				t.Errorf("sum(%d, %d) = %d; want %d", tc.a, tc.b, got, tc.sum)
			}
			if got := in.length(&grammar.Seq{Items: []grammar.Expr{a, b, &grammar.Literal{Value: ""}}}); got != tc.sum {
				t.Errorf("sum(%d, %d, 0) = %d; want %d", tc.a, tc.b, got, tc.sum)
			}
			if got := in.length(&grammar.Repeat{Expr: b, Min: tc.a, Max: tc.a}); got != tc.product {
				t.Errorf("product(%d, %d) = %d; want %d", tc.a, tc.b, got, tc.product)
			}
		})
	}
}
