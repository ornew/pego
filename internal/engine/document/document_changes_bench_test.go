package document_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/document"

	"github.com/ornew/zeroparse/internal/compile"
	. "github.com/ornew/zeroparse/internal/engine"
)

// BenchmarkDocumentChangesSnapshotEdit compares identical same-length middle
// edits and Snapshot captures with notifications disabled/enabled. Both cases
// retain one previous snapshot so immutable-sharing eligibility is equivalent.
// Event draining is timed: the enabled case includes bounded pull delivery.
// ASCII text makes byte and code-point coordinates identical; this benchmark
// does not model shifted spans, Unicode edits, errors, or slow consumers.
func BenchmarkDocumentChangesSnapshotEdit(b *testing.B) {
	prog, err := Compile(mustParse(`rule main = item* $$
rule item = [a-z]`), compile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{4 << 10, 64 << 10} {
		textA := strings.Repeat("a", size)
		middle := size / 2
		textB := textA[:middle] + "b" + textA[middle+1:]
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, enabled := range []bool{false, true} {
					mode := "disabled"
					if enabled {
						mode = "enabled"
					}
					b.Run(fmt.Sprintf("%dKiB/%s/%s/%s", size>>10, backend, unit, mode), func(b *testing.B) {
						b.StopTimer()
						options := ParseOptions{Backend: backend, Unit: unit}
						doc, err := prog.NewDocumentWith("main", textA, options)
						if err != nil {
							b.Fatal(err)
						}
						if enabled {
							if err := doc.EnableChanges(2); err != nil {
								b.Fatal(err)
							}
						}
						previous, err := doc.SnapshotWith(options)
						if err != nil {
							b.Fatal(err)
						}
						doc.DrainChanges()
						// Warm both stable text states and the comparison baseline before timing.
						for _, letter := range []string{"b", "a"} {
							if err := doc.Edit(middle, middle+1, letter); err != nil {
								b.Fatal(err)
							}
							current, err := doc.SnapshotWith(options)
							if err != nil {
								b.Fatal(err)
							}
							runtime.KeepAlive(previous)
							previous = current
							doc.DrainChanges()
						}
						base := doc.Version()
						var events []DocumentChange
						b.ReportAllocs()
						b.ResetTimer()
						b.StartTimer()
						for i := 0; i < b.N; i++ {
							letter := "b"
							if i%2 != 0 {
								letter = "a"
							}
							if err := doc.Edit(middle, middle+1, letter); err != nil {
								b.Fatal(err)
							}
							current, err := doc.SnapshotWith(options)
							if err != nil {
								b.Fatal(err)
							}
							runtime.KeepAlive(previous)
							previous = current
							events = doc.DrainChanges()
						}
						b.StopTimer()
						wantText := textA
						if b.N%2 != 0 {
							wantText = textB
						}
						if doc.Text() != wantText || previous.Text() != wantText || previous.Version() != doc.Version() || doc.Version().Sequence() != base.Sequence()+uint64(b.N) {
							b.Fatal("unexpected final text or version")
						}
						if enabled && b.N > 0 {
							if len(events) != 2 || events[0].Kind != ChangeEdit || len(events[0].Edits) != 1 || events[0].Edits[0].Start != middle || events[0].Edits[0].End != middle+1 || events[1].Kind != ChangeOutcome || events[1].Output != OutputChanged || events[1].To != doc.Version() {
								b.Fatal("unexpected notification events", events)
							}
						} else if len(events) != 0 {
							b.Fatal("notifications emitted while disabled")
						}
						fresh, err := prog.NewDocumentWith("main", wantText, options)
						if err != nil {
							b.Fatal(err)
						}
						expected, err := fresh.SnapshotWith(options)
						if err != nil || !document.EqualChangeSnapshots(previous, expected) {
							b.Fatal("snapshot differs from full recomputation", err)
						}
						runtime.KeepAlive(previous)
					})
				}
			}
		}
	}
}
