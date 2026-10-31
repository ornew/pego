package main

import (
	"runtime"
	"testing"

	"github.com/ornew/zeroparse"
)

// BenchmarkCompileCorpus complements post-GC retained heap with compilation
// time and cumulative allocations on the identical source grammars.
func BenchmarkCompileCorpus(b *testing.B) {
	for _, name := range []string{"json", "xml", "arithmetic", "minilang"} {
		b.Run(name, func(b *testing.B) {
			source, _, err := corpus("../..", name)
			if err != nil {
				b.Fatal(err)
			}
			for range 4 {
				if _, err := zeroparse.CompileSource(source, "main"); err != nil {
					b.Fatal(err)
				}
			}
			runtime.GC()
			b.ReportAllocs()
			for b.Loop() {
				p, err := zeroparse.CompileSource(source, "main")
				if err != nil {
					b.Fatal(err)
				}
				runtime.KeepAlive(p)
			}
		})
	}
}
