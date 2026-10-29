package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/internal/compile"
)

// BenchmarkNodeListConstruction compares explicit element types with the
// existing list function after source compilation, with identical results.
func BenchmarkNodeListConstruction(b *testing.B) {
	for _, workload := range []struct{ name, input, body, typed, function string }{
		{"Empty", "", `_`, `[]Word{}`, `list()`},
		{"Three", "xxx", `a:word b:word c:word`, `[]Word{$a, $b, $c}`, `list($a, $b, $c)`},
		{"Nested", "xxx", `a:word b:word c:word`, `[][]Word{[]Word{$a}, []Word{$b, $c}}`, `list(list($a), list($b, $c))`},
	} {
		b.Run(workload.name, func(b *testing.B) {
			var want string
			for _, variant := range []struct{ name, action string }{{"Function", workload.function}, {"Typed", workload.typed}} {
				g, err := parseTest("type Word = { Text string }\nrule word Word = t:(\"x\") #atomic => Word{Text: text($t)}\nrule main = " + workload.body + " => " + variant.action)
				if err != nil {
					b.Fatal(err)
				}
				p, err := Compile(g, compile.Options{})
				if err != nil {
					b.Fatal(err)
				}
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					n, err := p.ParseWith("main", workload.input, ParseOptions{Backend: backend})
					if err != nil {
						b.Fatal(err)
					}
					got := resultJSON(n, nil)
					if want == "" {
						want = got
					} else if got != want {
						b.Fatalf("unequal benchmark results: %s / %s", got, want)
					}
					b.Run(fmt.Sprint(backend)+"/"+variant.name, func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							if _, err := p.ParseWith("main", workload.input, ParseOptions{Backend: backend}); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
		})
	}
}
