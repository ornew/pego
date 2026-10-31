//go:build !zp_budget_controls_reference

package engine

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/limits"
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

// Each repetition, scan, rule call and Operators Rule level charges the budget on every backend:
// one more element costs exactly the units below, so a lost charge changes the step.
func TestBudgetCoarseChargesPerIteration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src     string
		input         func(k int) string
		parse, recogn [3]uint64 // closure, bytecode, iterative
	}{
		{"repeat", `rule main = ("a" "b")* $$`, func(k int) string { return strings.Repeat("ab", k) },
			[3]uint64{9, 10, 10}, [3]uint64{7, 7, 7}},
		{"scan", `rule main = [a-z]* $$`, func(k int) string { return strings.Repeat("a", k) },
			[3]uint64{6, 5, 5}, [3]uint64{4, 4, 4}},
		{"call", "rule main = item* $$\nrule item = \"a\"", func(k int) string { return strings.Repeat("a", k) },
			[3]uint64{7, 6, 6}, [3]uint64{5, 5, 5}},
		{"capture call", "rule main = item* $$\nrule item = x:\"a\" {len(text($x)) == 1}", func(k int) string { return strings.Repeat("a", k) },
			[3]uint64{11, 11, 11}, [3]uint64{7, 7, 7}},
		{"projected", "type L = { N int }\nrule main = xs:(-\",\" r:x)* $$ => L{N: len(map($xs, (e) => e.$r))}\nrule x = [a-z] #atomic",
			func(k int) string { return strings.Repeat(",a", k) }, [3]uint64{13, 14, 14}, [3]uint64{8, 8, 8}},
		{"prefix", "rule main = expr $$\nrule expr = #operators {\n    operand \"a\"\n    level {\n        infix left \"+\"\n    }\n    level {\n        prefix \"-\"\n    }\n}",
			func(k int) string { return strings.Repeat("-", k) + "a" }, [3]uint64{7, 7, 7}, [3]uint64{7, 7, 7}},
		{"infix", "rule main = expr $$\nrule expr = #operators {\n    operand \"a\"\n    level {\n        infix left \"+\"\n    }\n}",
			func(k int) string { return "a" + strings.Repeat("+a", k) }, [3]uint64{10, 10, 10}, [3]uint64{10, 10, 10}},
	}
	for _, c := range cases {
		prog := mustCompile(t, c.src)
		for bi, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				o := ParseOptions{Expectations: true, Backend: backend, Recognize: recognize}
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
