package engine

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/value"
)

var snapshotBenchmarkResult any
var snapshotBenchmarkPosition int64

type snapshotLegacyResult struct {
	version DocumentVersion
	unit    PositionUnit
	text    string
	root    *value.Node
	err     error
	stats   Stats
}

// Compare complete capture and later materialization against a legacy Parse
// plus Clone of the current result. The grammar, text, and memo are warmed
// before timing; methods are run serially by go test.
func BenchmarkDocumentSnapshotCapture(b *testing.B) {
	g, err := parseTest(`rule main = item* $$
rule item = [a-z]`)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{4 << 10, 64 << 10} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
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

// Prefix insert/delete forces shifted spans. Keep the previous snapshot live
// through capture so both exact-record fallback and relative sharing are tested.
func BenchmarkDocumentSnapshotPrefixEdit(b *testing.B) {
	g, err := parseTest(`rule main = item* $$
rule item = [a-z]`)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{4 << 10, 64 << 10} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			d, err := prog.NewDocument("main", strings.Repeat("a", size))
			if err != nil {
				b.Fatal(err)
			}
			previous, err := d.SnapshotWith(ParseOptions{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					err = d.Edit(0, 0, "b")
				} else {
					err = d.Edit(0, 1, "")
				}
				if err != nil {
					b.Fatal(err)
				}
				s, err := d.SnapshotWith(ParseOptions{})
				if err != nil {
					b.Fatal(err)
				}
				runtime.KeepAlive(previous)
				previous = s
			}
			snapshotBenchmarkResult = previous
		})
	}
	snapshotBenchmarkResult = nil
}

// A same-length middle edit keeps nearly all repetition child IDs stable while
// changing the containing record's generation. Keep each previous snapshot
// alive so sharing is eligible on every capture.
func BenchmarkDocumentSnapshotMiddleEdit(b *testing.B) {
	g, err := parseTest(`rule main = item* $$
rule item = [a-z]`)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{4 << 10, 64 << 10} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			d, err := prog.NewDocument("main", strings.Repeat("a", size))
			if err != nil {
				b.Fatal(err)
			}
			previous, err := d.SnapshotWith(ParseOptions{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				letter := "b"
				if i%2 != 0 {
					letter = "a"
				}
				if err := d.Edit(size/2, size/2+1, letter); err != nil {
					b.Fatal(err)
				}
				current, err := d.SnapshotWith(ParseOptions{})
				if err != nil {
					b.Fatal(err)
				}
				runtime.KeepAlive(previous)
				previous = current
			}
			snapshotBenchmarkResult = previous
		})
	}
}

func BenchmarkDocumentSnapshotMiddleEditRead(b *testing.B) {
	benchmarkDocumentSnapshotEditRead(b, false)
}

func BenchmarkDocumentSnapshotPrefixEditRead(b *testing.B) {
	benchmarkDocumentSnapshotEditRead(b, true)
}

func benchmarkDocumentSnapshotEditRead(b *testing.B, prefix bool) {
	g, err := parseTest(`rule main = item* $$
rule item = [a-z]`)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{4 << 10, 64 << 10} {
		d, err := prog.NewDocument("main", strings.Repeat("a", size))
		if err != nil {
			b.Fatal(err)
		}
		first, err := d.SnapshotWith(ParseOptions{})
		if err != nil {
			b.Fatal(err)
		}
		start, end, childCount := size/2, size/2+1, size
		if prefix {
			start, end, childCount = 0, 0, size+1
		}
		if err := d.Edit(start, end, "b"); err != nil {
			b.Fatal(err)
		}
		s, err := d.SnapshotWith(ParseOptions{})
		if err != nil {
			b.Fatal(err)
		}
		runtime.KeepAlive(first)
		var wide SnapshotNode
		for i := 0; i < s.graph.count(); i++ {
			n := snapshotHandle(&s.graph, i+1)
			if n.ChildCount() == childCount {
				wide = n
				break
			}
		}
		if !wide.Valid() {
			b.Fatal("wide repetition missing")
		}
		b.Run(fmt.Sprintf("%dKiB/Materialize", size>>10), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				snapshotBenchmarkResult = s.Materialize()
			}
		})
		b.Run(fmt.Sprintf("%dKiB/IndexedChildren", size>>10), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var sum int64
				for j, count := 0, wide.ChildCount(); j < count; j++ {
					sum += int64(wide.Child(j).End())
				}
				snapshotBenchmarkPosition = sum
			}
		})
	}
}
