package engine

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// Report live memory after the parser has gone, with either one early row
// or all rows retained. Input and compiled-program storage precede the
// baseline GC and are held through the final GC in both variants.
func BenchmarkStreamTextRetainedHeap(b *testing.B) {
	g, err := syntax.Parse(`def main = row* #stream $$
def row = key:@(?a-z)+ "=" value:@(?0-9)+ "\n"`)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	if err := prog.ParseStream("main", strings.NewReader("warm=1\n"), func(*Node) error { return nil }); err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{5000, 50000} {
		text := strings.Repeat("abcdefghij=12345\n", size)
		for _, all := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/all=%t", size, all), func(b *testing.B) {
				var total int64
				for b.Loop() {
					runtime.GC()
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					var rows []*Node
					err := prog.ParseStream("main", strings.NewReader(text), func(n *Node) error {
						if all || len(rows) == 0 {
							rows = append(rows, n)
						}
						return nil
					})
					if err != nil {
						b.Fatal(err)
					}
					runtime.GC()
					runtime.ReadMemStats(&after)
					total += int64(after.HeapAlloc) - int64(before.HeapAlloc)
					runtime.KeepAlive(rows)
					runtime.KeepAlive(text)
				}
				b.ReportMetric(float64(total)/float64(b.N), "retained-B")
			})
		}
	}
}
