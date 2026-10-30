//go:build !pego_budget_controls_reference

package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
)

// Alternate inactive and active invocations after a resumable run has been
// cached, so graph selection must preserve repetition-site identities and
// control admission even when the Document reuses an earlier result.
func TestBudgetDocumentAlternatesDispatchGraphs(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`rule main = item+ $$

rule item = "a"`,
		`rule main = expr $$

rule expr = #operators {
    operand "a"
    level {
        infix left "+"
    }
}`,
	} {
		prog := mustCompile(t, source)
		text := strings.Repeat("a", 80)
		if strings.Contains(source, "#operators {") {
			text = strings.TrimSuffix(strings.Repeat("a+", 40), "+")
		}
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			d, err := prog.NewDocumentWith("main", text, ParseOptions{Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 6; i++ {
				opts := ParseOptions{Unit: unit}
				if i%2 == 1 {
					opts.Limits = limits.Limits{Budget: 1000000}
				}
				got, err := d.ParseWith(opts)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := prog.ParseWith("main", d.Text(), opts)
				if err != nil || gentest.ResultJSON(got, nil) != gentest.ResultJSON(fresh, err) {
					t.Fatalf("iteration %d: cached/fresh mismatch: %v", i, err)
				}
				// A cached tree must not bypass admission on the other dispatch graph.
				blocked, err := d.ParseWith(ParseOptions{Unit: unit, Limits: limits.Limits{Budget: 1}})
				if blocked != nil || !errors.Is(err, limits.ErrLimit) {
					t.Fatalf("iteration %d: cached result bypassed budget: %v", i, err)
				}
				if err := d.Edit(0, 1, "a"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// Rule calls consume budget even when they match no input, and the zero-width terms of one rule
// consume none beyond the call: between two charges only the straight-line code of a rule body
// runs, whose length the grammar bounds. Recognition discards CST construction so input and value
// spend cannot mask a missing call charge on the controlled graph.
func TestBudgetClosureDispatchMetersZeroWidthExpressions(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "rule a = ^\n"+"rule main = "+strings.Repeat("a ", 64)+"$$")
	straight := mustCompile(t, "rule main = "+strings.Repeat("^ ", 64)+"$$")
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		opts := ParseOptions{Unit: unit, Recognize: true, Limits: limits.Limits{Budget: 16}}
		n, err := prog.ParseWith("main", "", opts)
		if n != nil || !errors.Is(err, limits.ErrLimit) {
			t.Fatalf("zero-width calls bypassed budget: node=%v error=%v", n, err)
		}
		if _, err := straight.ParseWith("main", "", opts); err != nil {
			t.Fatalf("straight-line zero-width terms were charged: %v", err)
		}
		opts.Limits = limits.Limits{}
		if _, err := prog.ParseWith("main", "", opts); err != nil {
			t.Fatal(err)
		}
	}
}
