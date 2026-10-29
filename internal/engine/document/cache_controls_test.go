package document_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/document"

	. "github.com/ornew/pego/internal/engine"
)

func TestCacheControlsResetPreservesDocument(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				prog := mustCompile(t, `rule main = item* $$
rule item = [a-z]+ "\n"`)
				options := ParseOptions{Backend: backend, Unit: unit, MaxDepth: 12345}
				text := strings.Repeat("word\n", 40)
				d, err := prog.NewDocumentWith("main", text, options)
				if err != nil {
					t.Fatal(err)
				}
				d.ResetHistory()
				if got := d.CacheStats(); got != (CacheStats{}) {
					t.Fatalf("initial reset: %+v", got)
				}
				old, err := d.Parse()
				if err != nil {
					t.Fatal(err)
				}
				oldSnapshot, err := d.SnapshotWith(options)
				if err != nil {
					t.Fatal(err)
				}
				if document.SnapshotSharingEnabled && !d.HasPreviousSnapshot() {
					t.Fatal("snapshot sharing candidate was not recorded")
				}
				if s := d.CacheStats(); s.LiveMemoEntries == 0 || s.EstimatedBytes == 0 {
					t.Fatalf("empty cache: %+v", s)
				}
				first := d.CacheStats()
				if second := d.CacheStats(); second != first {
					t.Fatalf("inspection changed stats: %+v != %+v", second, first)
				}
				before := dump(t, old, nil)
				parseStats := d.Stats()
				d.ResetHistory()
				if got := d.CacheStats(); got != (CacheStats{}) {
					t.Fatalf("reset: %+v", got)
				}
				if d.HasPreviousSnapshot() || oldSnapshot.Text() != text || !oldSnapshot.Root().Valid() {
					t.Fatal("reset retained snapshot sharing candidate or changed old snapshot")
				}
				if d.Text() != text || !reflect.DeepEqual(d.ParseDefaults(), options) || d.Stats() != parseStats {
					t.Fatal("reset changed text, defaults or last stats")
				}
				if err := d.Edit(0, 0, "new\n"); err != nil {
					t.Fatal(err)
				}
				n, err := d.Parse()
				fresh, freshErr := prog.ParseWith("main", d.Text(), options)
				if dump(t, n, err) != dump(t, fresh, freshErr) {
					t.Fatal("reset parse differs from fresh")
				}
				if dump(t, old, nil) != before {
					t.Fatal("old tree moved after reset")
				}
			})
		}
	}
}
