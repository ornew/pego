package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var snapshotBenchmarkResult any

type snapshotLegacyResult struct {
	version DocumentVersion
	unit    Unit
	text    string
	root    *Node
	err     error
	stats   Stats
}

// Compare complete capture and later materialization against a legacy Parse
// plus Clone of the current result. The grammar, text, and memo are warmed
// before timing; methods are run serially by go test.
func BenchmarkDocumentSnapshotCapture(b *testing.B) {
	g, err := syntax.Parse(`rule main = item* $$
rule item = [a-z]`)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{4 << 10, 64 << 10} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, back := range []Backend{Closure, Bytecode, BytecodeIterative} {
				base := ParseOptions{Unit: unit, Backend: back}
				d, err := prog.NewDocumentWith("main", strings.Repeat("a", size), base)
				if err != nil {
					b.Fatal(err)
				}
				if _, err = d.ParseWith(base); err != nil {
					b.Fatal(err)
				}
				snap, err := d.SnapshotWith(base)
				if err != nil {
					b.Fatal(err)
				}
				name := unit.String() + "/" + back.String() + "/"
				if size == 4<<10 {
					name += "4KiB"
				} else {
					name += "64KiB"
				}
				b.Run(name+"/Snapshot", func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						got, e := d.SnapshotWith(base)
						if e != nil {
							b.Fatal(e)
						}
						snapshotBenchmarkResult = got
					}
				})
				b.Run(name+"/ParseClone", func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						got, e := d.ParseWith(base)
						if e != nil {
							b.Fatal(e)
						}
						snapshotBenchmarkResult = snapshotLegacyResult{d.Version(), d.Unit(), d.Text(), got.Clone(), e, d.Stats()}
					}
				})
				b.Run(name+"/Materialize", func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						snapshotBenchmarkResult = snap.Materialize()
					}
				})
			}
		}
	}
	snapshotBenchmarkResult = nil
}
