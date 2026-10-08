package engine

import (
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
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
			// Projected repetitions (project.go) give what the repetitions as written give.
			prog, plain := compile(t, c.src), compile(t, c.src, Options{noProjections: true})
			for _, in := range c.inputs {
				for _, u := range []Unit{CodePoints, Bytes} {
					n1, err1 := prog.ParseWith("main", in, ParseOptions{Unit: u})
					n2, err2 := plain.ParseWith("main", in, ParseOptions{Unit: u})
					if a, b := resultJSON(n1, err1), resultJSON(n2, err2); a != b {
						t.Errorf("input %q (%v): projected repetitions differ\n projected %s\n plain     %s", in, u, a, b)
					}
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
		if err != nil || int(n.End) != len(in) {
			t.Errorf("input %.10q...: %v", in, err)
		}
		if _, err := prog.ParseWith("main", in[:len(in)-1], ParseOptions{Backend: BytecodeIterative}); err == nil && in[len(in)-1] == ')' {
			t.Errorf("input %.10q...: expected an error", in)
		}
	}
}

// TestConcurrentParses runs parses with every backend and in recognition mode concurrently on
// one Program (whose backends are prepared lazily), and checks that each gets the result of a
// parse on its own. Run with -race to check that per-parse state is not shared.
func TestConcurrentParses(t *testing.T) {
	prog := compile(t, `
type Pair struct { Key Match, Value Node }
type Op struct { L Node, R Node }
type Node = Op | Match
def main = ps:pair* -> concat(map($ps, (p) => $p), list())
def pair: Pair = k:@(?a-z)+ "=" v:expr ";" [text($k) != "bad"] -> new Pair{Key: $k, Value: $v}
def expr: Node = l:@(?0-9)+ rest:("+" r:@(?0-9)+)* -> foldl($l, $rest, (acc, x) => new Op{L: $acc, R: $x.r})`)
	inputs := []string{"a=1;b=2+3;", "x=1+2+3;y=4;z=5;", "a=1;bad=2;", "q=12+"}
	want := map[string]string{}
	for _, in := range inputs {
		want[in] = result(prog, in)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 100)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				in := inputs[(g+i)%len(inputs)]
				for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
					if got := resultWith(prog, in, ParseOptions{Backend: b}); got != want[in] {
						errs <- fmt.Sprintf("%v %q: got %s, want %s", b, in, got, want[in])
						return
					}
					_, err := prog.ParseWith("main", in, ParseOptions{Backend: b, Recognize: true})
					if (err == nil) != !strings.HasPrefix(want[in], "error: ") {
						errs <- fmt.Sprintf("%v %q: recognition gave %v", b, in, err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
