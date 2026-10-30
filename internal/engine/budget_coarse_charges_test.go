//go:build !pego_budget_controls_reference

package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

// leastBudget returns the smallest budget under which the parse succeeds.
func leastBudget(t *testing.T, prog *Program, input string, o ParseOptions) uint64 {
	t.Helper()
	fits := func(b uint64) bool {
		o.Limits = limits.Limits{Budget: b}
		_, err := prog.ParseWith("main", input, o)
		return err == nil
	}
	hi := uint64(1)
	for !fits(hi) {
		if hi > 1<<30 {
			t.Fatalf("input %q does not parse", input)
		}
		hi *= 2
	}
	lo := hi / 2 // fails, or 0
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if fits(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// Each repetition iteration (a scan charges one per character), rule call, Operators Rule level
// and value built charges the budget on every backend, and the input a terminal reads charges
// nothing: one more element costs exactly the units below, so a lost charge changes the step.
func TestBudgetCoarseChargesPerIteration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src     string
		input         func(k int) string
		parse, recogn [3]uint64 // closure, bytecode, iterative
	}{
		{"repeat", `rule main = ("a" "b")* $$`, func(k int) string { return strings.Repeat("ab", k) },
			[3]uint64{3, 4, 4}, [3]uint64{1, 1, 1}},
		{"scan", `rule main = [a-z]* $$`, func(k int) string { return strings.Repeat("a", k) },
			[3]uint64{3, 2, 2}, [3]uint64{1, 1, 1}},
		{"call", "rule main = item* $$\nrule item = \"a\"", func(k int) string { return strings.Repeat("a", k) },
			[3]uint64{4, 3, 3}, [3]uint64{2, 2, 2}},
		{"capture call", "rule main = item* $$\nrule item = x:\"a\" {len(text($x)) == 1}", func(k int) string { return strings.Repeat("a", k) },
			[3]uint64{8, 8, 8}, [3]uint64{4, 4, 4}},
		{"projected", "type L = { N int }\nrule main = xs:(-\",\" r:x)* $$ => L{N: len(map($xs, (e) => e.$r))}\nrule x = [a-z] #atomic",
			func(k int) string { return strings.Repeat(",a", k) }, [3]uint64{7, 8, 8}, [3]uint64{2, 2, 2}},
		{"prefix", "rule main = expr $$\nrule expr = #operators {\n    operand \"a\"\n    level {\n        infix left \"+\"\n    }\n    level {\n        prefix \"-\"\n    }\n}",
			func(k int) string { return strings.Repeat("-", k) + "a" }, [3]uint64{2, 2, 2}, [3]uint64{2, 2, 2}},
		{"infix", "rule main = expr $$\nrule expr = #operators {\n    operand \"a\"\n    level {\n        infix left \"+\"\n    }\n}",
			func(k int) string { return "a" + strings.Repeat("+a", k) }, [3]uint64{3, 3, 3}, [3]uint64{3, 3, 3}},
	}
	for _, c := range cases {
		prog := mustCompile(t, c.src)
		for bi, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				o := ParseOptions{Backend: backend, Recognize: recognize}
				want := c.parse[bi]
				if recognize {
					want = c.recogn[bi]
				}
				for k := 8; k <= 10; k++ {
					if step := leastBudget(t, prog, c.input(k+1), o) - leastBudget(t, prog, c.input(k), o); step != want {
						t.Errorf("%s %v recognize=%t: element %d costs %d, want %d", c.name, backend, recognize, k+1, step, want)
					}
				}
			}
		}
	}
}
