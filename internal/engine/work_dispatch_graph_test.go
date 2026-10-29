//go:build !pego_work_controls_reference

package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

// Alternate inactive and active invocations after a resumable run has been
// cached, so graph selection must preserve repetition-site identities and
// control admission even when the Document reuses an earlier result.
func TestWorkDocumentAlternatesDispatchGraphs(t *testing.T) {
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
					opts.WorkLimits = limits.WorkLimits{MaxSteps: 1000000, MaxInputWork: 1000000}
				}
				got, err := d.ParseWith(opts)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := prog.ParseWith("main", d.Text(), opts)
				if err != nil || resultJSON(got, nil) != resultJSON(fresh, err) {
					t.Fatalf("iteration %d: cached/fresh mismatch: %v", i, err)
				}
				// A cached tree must not bypass admission on the other dispatch graph.
				blocked, err := d.ParseWith(ParseOptions{Unit: unit, WorkLimits: limits.WorkLimits{MaxSteps: 1}})
				if blocked != nil || !errors.Is(err, limits.ErrWorkLimit) {
					t.Fatalf("iteration %d: cached result bypassed budget: %v", i, err)
				}
				if err := d.Edit(0, 1, "a"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// Zero-width matching still consumes expression-dispatch work. Recognition
// discards CST construction so input and value work cannot mask a missing
// dispatch charge on the controlled graph.
func TestWorkClosureDispatchMetersZeroWidthExpressions(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "rule main = "+strings.Repeat("^ ", 64)+"$$")
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		opts := ParseOptions{Unit: unit, Recognize: true, WorkLimits: limits.WorkLimits{MaxSteps: 16}}
		n, err := prog.ParseWith("main", "", opts)
		if n != nil || !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("zero-width dispatch bypassed budget: node=%v error=%v", n, err)
		}
		opts.WorkLimits = limits.WorkLimits{}
		if _, err := prog.ParseWith("main", "", opts); err != nil {
			t.Fatal(err)
		}
	}
}
