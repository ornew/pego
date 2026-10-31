package document_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	. "github.com/ornew/zeroparse/internal/engine"
)

func benchmarkDocumentSnapshotEditRead(b *testing.B, prefix bool) {
	g, err := parseTest(`rule main = item* $$
rule item = [a-z]`)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
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
		for i := 0; i < s.Graph().Count(); i++ {
			n := s.Graph().Handle(i + 1)
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

func BenchmarkDocumentSnapshotMiddleEditRead(b *testing.B) {
	benchmarkDocumentSnapshotEditRead(b, false)
}

func BenchmarkDocumentSnapshotPrefixEditRead(b *testing.B) {
	benchmarkDocumentSnapshotEditRead(b, true)
}
