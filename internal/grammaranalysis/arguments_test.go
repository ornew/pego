package grammaranalysis

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

func TestExpressionArgsPollingOrderAndOwnership(t *testing.T) {
	x, y, z := &grammar.Literal{Value: "x"}, &grammar.Literal{Value: "y"}, &grammar.Literal{Value: "z"}
	a := &grammar.Attribute{Args: []any{1, x, nil}, KwArgs: map[string]any{"z": z, "a": y, "scalar": true}}
	for _, poll := range []func(){nil, func() {}} {
		got := ExpressionArgsWithPoll(a, poll)
		if !reflect.DeepEqual(got, []grammar.Expr{x, y, z}) || !reflect.DeepEqual(got, a.ExpressionArgs()) {
			t.Fatalf("argument order changed: %v", got)
		}
		got[0] = nil
		if a.ExpressionArgs()[0] != x {
			t.Fatal("argument collection exposed slice storage")
		}
	}
}

func TestExpressionArgsPollingStopsDuringBudget(t *testing.T) {
	for _, keywords := range []bool{false, true} {
		t.Run(fmt.Sprint(keywords), func(t *testing.T) {
			a := &grammar.Attribute{KwArgs: map[string]any{}}
			for i := range 1024 {
				if keywords {
					a.KwArgs[fmt.Sprint(i)] = i
				} else {
					a.Args = append(a.Args, i)
				}
			}
			stopAt := 17
			if keywords {
				stopAt = 1280
			} // After key collection, during sorting.
			marker := &struct{ n int }{17}
			calls := 0
			defer func() {
				if got := recover(); got != marker || calls != stopAt {
					t.Fatalf("poll panic changed: %v at %d", got, calls)
				}
			}()
			ExpressionArgsWithPoll(a, func() {
				calls++
				if calls == stopAt {
					panic(marker)
				}
			})
		})
	}
}
