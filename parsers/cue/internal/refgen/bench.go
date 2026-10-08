package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"cuelang.org/go/cue/parser"
)

// runBench parses src with the reference parser repeatedly and prints the time and the memory of a parse. The
// minimum of several runs of at least a second each is the figure the README uses for orientation.
func runBench(src []byte) {
	if _, err := parser.ParseFile("bench.cue", src); err != nil {
		fmt.Fprintln(os.Stderr, "refgen:", err)
		os.Exit(1)
	}
	best := time.Duration(1 << 62)
	var allocs, bytes uint64
	for run := 0; run < 5; run++ {
		var m0, m1 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m0)
		start := time.Now()
		n := 0
		for time.Since(start) < time.Second {
			if _, err := parser.ParseFile("bench.cue", src); err != nil {
				fmt.Fprintln(os.Stderr, "refgen:", err)
				os.Exit(1)
			}
			n++
		}
		d := time.Since(start) / time.Duration(n)
		runtime.ReadMemStats(&m1)
		if d < best {
			best = d
			allocs = (m1.Mallocs - m0.Mallocs) / uint64(n)
			bytes = (m1.TotalAlloc - m0.TotalAlloc) / uint64(n)
		}
	}
	fmt.Printf("parser.ParseFile: %d bytes, %.2f ms/op, %.2f MB/s, %d B/op, %d allocs/op\n",
		len(src), float64(best)/1e6, float64(len(src))/best.Seconds()/1e6, bytes, allocs)
}
