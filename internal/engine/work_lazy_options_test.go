//go:build !pego_work_controls_reference

package engine

import "testing"

func TestWorkLazyClosureSavedFlagsAndOptions(t *testing.T) {
	original := compile(t, `rule main = first:item rest:(-"," value:item)* $$ => concat(list($first), map($rest, (e) => e.$value))
rule item = [a-z]+`)
	flags := make([]ruleFlags, len(original.rules))
	for i, r := range original.rules {
		flags[i] = ruleFlags{memo: r.memo, leader: r.leader, positional: r.positional, twin: r.twinOK}
	}
	for _, savedFlags := range [][]ruleFlags{nil, flags} {
		for _, noProjections := range []bool{false, true} {
			p, err := build(original.Grammar, Options{NoTypeCheck: true, noProjections: noProjections}, savedFlags)
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range []Unit{CodePoints, Bytes} {
				o := ParseOptions{Unit: unit}
				plain, plainErr := p.ParseWith("main", "abc,def", o)
				o.WorkLimits = WorkLimits{MaxSteps: 1000000, MaxInputWork: 1000000}
				active, activeErr := p.ParseWith("main", "abc,def", o)
				if resultJSON(plain, plainErr) != resultJSON(active, activeErr) || activeErr != nil {
					t.Fatalf("options changed output: %v", activeErr)
				}
				o.Recognize = true
				if n, err := p.ParseWith("main", "abc,def", o); n != nil || err != nil {
					t.Fatalf("recognition options: %v", err)
				}
			}
			if p.workProgram == nil || p.workProgram.noProjections != noProjections {
				t.Fatal("projection policy changed")
			}
		}
	}
}
