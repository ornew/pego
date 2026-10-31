package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/limits"
)

// manyVariablesGrammar declares n variables whose names share a long prefix. The start rule
// rebinds each of them, reads one of them in a memoized rule and consumes one character.
func manyVariablesGrammar(n int) string {
	var src strings.Builder
	name := func(i int) string { return fmt.Sprintf("%s%03d", strings.Repeat("v", 28), i) }
	for i := 0; i < n; i++ {
		fmt.Fprintf(&src, "var %s int = 1\n", name(i))
	}
	src.WriteString("rule main = ")
	for round := 0; round < 1; round++ {
		for i := 0; i < n; i++ {
			fmt.Fprintf(&src, "{%[1]s := %[1]s + 1} ", name(i))
		}
	}
	fmt.Fprintf(&src, "item $$\nrule item = {%s == 2} \"a\"\n", name(n-1))
	return src.String()
}

func TestManyVariables(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, manyVariablesGrammar(280))
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := prog.ParseWith("main", "a", ParseOptions{Backend: backend, Limits: limits.Limits{Budget: 1 << 40}})
		if err != nil || n == nil {
			t.Fatalf("%s: %v", backend, err)
		}
		if _, err := prog.ParseWith("main", "b", ParseOptions{Backend: backend, Limits: limits.Limits{Budget: 1 << 40}}); err == nil {
			t.Fatalf("%s: parsed the wrong input", backend)
		}
	}
}

// minBudget returns the least budget with which the parse succeeds.
func minBudget(t testing.TB, prog *Program, backend Backend, input string) uint64 {
	t.Helper()
	lo, hi := uint64(1), uint64(1)
	for {
		if _, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Limits: limits.Limits{Budget: hi}}); err == nil {
			break
		}
		lo, hi = hi, hi*2
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if _, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Limits: limits.Limits{Budget: mid}}); err == nil {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

func TestManyVariablesBudget(t *testing.T) {
	prog := mustCompile(t, manyVariablesGrammar(280))
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Logf("%s: %d budget units", backend, minBudget(t, prog, backend, "a"))
	}
}

func BenchmarkManyVariables(b *testing.B) {
	for _, n := range []int{4, 9, 12, 16, 17, 32, 64, 280} {
		g, err := parseTest(manyVariablesGrammar(n))
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, compile.Options{})
		if err != nil {
			b.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, controlled := range []bool{false, true} {
				name := fmt.Sprintf("%d/%s/plain", n, backend)
				opts := ParseOptions{Backend: backend}
				if controlled {
					name = fmt.Sprintf("%d/%s/controlled", n, backend)
					opts.Limits = limits.Limits{Budget: 1 << 40}
				}
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						if _, err := prog.ParseWith("main", "a", opts); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
