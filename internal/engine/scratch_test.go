package engine

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
)

// TestScratchReuse checks that parses that reuse the buffers of earlier ones (newPooledParser)
// return what parses with fresh buffers return, across inputs of different lengths, units,
// backends, recognition, and with invalid UTF-8, also from concurrent goroutines. The grammar
// calls key twice at a position, so that the memo is used.
func TestScratchReuse(t *testing.T) {
	const src = `
def main = (line / blank)* $$
def line = ^ (k:key " = " v:val / k:key " : " v:val) "\n"
def key = @(?a-z_)+
def val = @(?^\n)+
def blank = "\n"`
	pooled := compile(t, src)
	rng := rand.New(rand.NewSource(9))
	inputs := []string{""}
	for _, n := range []int{1, 200, 3, 1000, 0, 50, 5000, 2} {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "k%c%s v\xe2\x82\xac%d\n", 'a'+rng.Intn(26), [2]string{" =", " :"}[rng.Intn(2)], i)
			if rng.Intn(40) == 0 {
				b.WriteString("bad line\n") // a syntax error
			}
		}
		inputs = append(inputs, b.String())
	}
	inputs = append(inputs, "ka = \xff\xfe\n", "ka = é\n")
	var opts []ParseOptions
	for _, u := range []Unit{CodePoints, Bytes} {
		for _, be := range []Backend{Closure, Bytecode, BytecodeIterative} {
			opts = append(opts, ParseOptions{Unit: u, Backend: be}, ParseOptions{Unit: u, Backend: be, Recognize: true})
		}
	}
	check := func(t *testing.T, in string, o ParseOptions) {
		fresh := compile(t, src) // a program whose pool is empty
		n, err := pooled.ParseWith("main", in, o)
		fn, ferr := fresh.ParseWith("main", in, o)
		if g, w := dump(t, n, err), dump(t, fn, ferr); g != w {
			t.Errorf("%+v, input of %d bytes:\n got  %.300s\n want %.300s", o, len(in), g, w)
		}
	}
	for round := 0; round < 3; round++ {
		for _, in := range inputs {
			for _, o := range opts {
				check(t, in, o)
			}
		}
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				check(t, inputs[(g+i)%len(inputs)], opts[(g*7+i)%len(opts)])
			}
		}(g)
	}
	wg.Wait()
}
