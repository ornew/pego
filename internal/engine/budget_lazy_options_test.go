//go:build !zp_budget_controls_reference

package engine

import (
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/limits"

	"testing"

	"github.com/ornew/zeroparse/internal/compile"
)

func TestBudgetLazyClosureSavedFlagsAndOptions(t *testing.T) {
	t.Parallel()
	original := mustCompile(t, `rule main = first:item rest:(-"," value:item)* $$ => concat(list($first), map($rest, (e) => e.$value))
rule item = [a-z]+`)
	flags := make([]compile.RuleFlags, len(original.RuleList))
	for i, r := range original.RuleList {
		flags[i] = compile.RuleFlags{Memo: r.Memo, Leader: r.Leader, Positional: r.Positional, Twin: r.TwinOK}
	}
	for _, savedFlags := range [][]compile.RuleFlags{nil, flags} {
		for _, noProjections := range []bool{false, true} {
			p, err := build(original.Grammar, compile.Options{NoProjections: noProjections}, savedFlags)
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				o := ParseOptions{Unit: unit}
				plain, plainErr := p.ParseWith("main", "abc,def", o)
				o.Limits = limits.Limits{Budget: 1000000}
				active, activeErr := p.ParseWith("main", "abc,def", o)
				if gentest.ResultJSON(plain, plainErr) != gentest.ResultJSON(active, activeErr) || activeErr != nil {
					t.Fatalf("options changed output: %v", activeErr)
				}
				o.Recognize = true
				if n, err := p.ParseWith("main", "abc,def", o); n != nil || err != nil {
					t.Fatalf("recognition options: %v", err)
				}
			}
			if p.budgetProgram == nil || p.budgetProgram.NoProjections != noProjections {
				t.Fatal("projection policy changed")
			}
		}
	}
}
