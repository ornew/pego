//go:build !zp_budget_controls_reference

package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/limits"
)

// scanGrammar scans the rest of the input from every position of a scan from every position, so its
// cost grows with the cube of the input length: far above the default budget of a short input.
const scanGrammar = `rule main = (. &mid)* $$
rule mid = (. &.*)*`

func limitOf(t *testing.T, err error) uint64 {
	t.Helper()
	var le *limits.LimitError
	if !errors.As(err, &le) {
		t.Fatalf("want a budget limit error, got %v", err)
	}
	return le.Limit
}

// A zero budget selects max(2^20, 1024*n) for the input length n in the position unit, and Unlimited
// disables the budget.
func TestBudgetDefault(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, scanGrammar)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, c := range []struct {
			name  string
			input string
			unit  PositionUnit
			want  uint64
		}{
			{"floor", strings.Repeat("a", 300), CodePoints, 1 << 20},
			{"ascii bytes", strings.Repeat("a", 5000), Bytes, 5000 * 1024},
			{"wide bytes", strings.Repeat("é", 2000), Bytes, 4000 * 1024},
			{"wide code points", strings.Repeat("é", 2000), CodePoints, 2000 * 1024},
		} {
			t.Run(fmt.Sprintf("%s/%s", b, c.name), func(t *testing.T) {
				_, err := prog.ParseWith("main", c.input, ParseOptions{Backend: b, Unit: c.unit})
				if got := limitOf(t, err); got != c.want {
					t.Fatalf("default limit %d, want %d", got, c.want)
				}
				_, err = prog.ParseWith("main", c.input, ParseOptions{Backend: b, Unit: c.unit, Limits: limits.Limits{Budget: 1000}})
				if got := limitOf(t, err); got != 1000 {
					t.Fatalf("explicit limit %d, want 1000", got)
				}
			})
		}
		t.Run(b.String()+"/unlimited", func(t *testing.T) {
			if _, err := prog.ParseWith("main", strings.Repeat("a", 300), ParseOptions{Backend: b, Limits: limits.Limits{Budget: limits.Unlimited}}); err != nil {
				t.Fatal(err)
			}
		})
		t.Run(b.String()+"/document", func(t *testing.T) {
			d, err := prog.NewDocumentWith("main", strings.Repeat("a", 300), ParseOptions{Backend: b})
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Parse()
			if got := limitOf(t, err); got != 1<<20 {
				t.Fatalf("document default limit %d", got)
			}
			if _, err := d.ParseWith(ParseOptions{Backend: b, Limits: limits.Limits{Budget: limits.Unlimited}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
