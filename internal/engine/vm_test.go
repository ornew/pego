package engine

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
)

// TestBackendsMatchOnCorpus checks that the bytecode VM returns the same results as the closure
// engine on all the test grammars and examples.
func TestBackendsMatchOnCorpus(t *testing.T) {
	for _, c := range genCorpus(t) {
		t.Run(c.name, func(t *testing.T) {
			for _, o := range []Options{{}, {DisableMemo: true}} {
				prog := compile(t, c.src, o)
				for _, in := range c.inputs {
					checkBackends(t, prog, "main", in)
				}
			}
		})
	}
}

// TestBackendsMatchOnStream checks that stream parsing also yields the same elements and errors.
func TestBackendsMatchOnStream(t *testing.T) {
	prog := compile(t, records)
	inputs := []string{"#records\na=1\nbc=22\n", "#records\na=1\nb\n", "#rec"}
	for _, in := range inputs {
		for _, u := range []Unit{CodePoints, Bytes} {
			var out [3]string
			for i, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
				var got []string
				err := prog.ParseStreamWith("main", strings.NewReader(in), func(n *Node) error {
					got = append(got, fmt.Sprintf("%s [%d,%d)", n, n.Start, n.End))
					return nil
				}, ParseOptions{Unit: u, Backend: b})
				out[i] = fmt.Sprint(strings.Join(got, "\n"), " ", err)
			}
			if out[0] != out[1] || out[0] != out[2] {
				t.Errorf("input %q (unit %v)\n closure   %s\n bytecode  %s\n iterative %s", in, u, out[0], out[1], out[2])
			}
		}
	}
}

// TestIterativeDeepNesting checks that the iterative model uses hardly any host stack even on
// deeply nested input.
func TestIterativeDeepNesting(t *testing.T) {
	prog := compile(t, `
def main = e $$
def e = "(" e ")" / x
def x = pratt {
    operand "x"
    level { infix right "^" }
    level { prefix "-" }
}`)
	const depth = 100000
	inputs := []string{
		strings.Repeat("(", depth) + "x" + strings.Repeat(")", depth),
		strings.Repeat("x^", depth) + "x",
		strings.Repeat("-", depth) + "x",
	}
	prev := debug.SetMaxStack(1 << 20) // too small for the recursive model
	defer debug.SetMaxStack(prev)
	for _, in := range inputs {
		n, err := prog.ParseWith("main", in, ParseOptions{Backend: BytecodeIterative})
		if err != nil || n.End != len(in) {
			t.Errorf("input %.10q...: %v", in, err)
		}
		if _, err := prog.ParseWith("main", in[:len(in)-1], ParseOptions{Backend: BytecodeIterative}); err == nil && in[len(in)-1] == ')' {
			t.Errorf("input %.10q...: expected an error", in)
		}
	}
}
