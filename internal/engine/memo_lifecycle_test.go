package engine

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const memoizedStream = `
def main = item* #stream $$
def item = word "," word "\n"
def word = @letter+
def letter = (?a-z)`

const leftRecursiveStream = `
def main = item* #stream $$
def item = word "," word "\n"
def word = word letter / letter
def letter = (?a-z)`

func TestMemoizedStreamMemoryIsBounded(t *testing.T) {
	for _, src := range []string{memoizedStream, leftRecursiveStream} {
		prog := compile(t, src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				var at [2]uint64
				count := 0
				err := prog.ParseStreamWith("main", &lineReader{line: "abc,abc\n", n: 60000}, func(*Node) error {
					count++
					if count == 20000 || count == 60000 {
						var ms runtime.MemStats
						runtime.GC()
						runtime.ReadMemStats(&ms)
						at[count/60000] = ms.HeapAlloc
					}
					return nil
				}, ParseOptions{Backend: backend, Unit: unit})
				if err != nil || count != 60000 {
					t.Fatalf("%s/%s: emitted %d, error %v", backend, unit, count, err)
				}
				if grown := int64(at[1]) - int64(at[0]); grown > 4<<20 {
					t.Errorf("%s/%s: live heap grew %d KiB", backend, unit, grown>>10)
				}
				t.Logf("stream %s/%s: live heap %d -> %d bytes", backend, unit, at[0], at[1])
			}
		}
	}
}

func TestDocumentReplacedMemoMemoryIsBounded(t *testing.T) {
	prog := compile(t, `def main = @.* $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			doc, err := prog.NewDocumentWith("main", strings.Repeat("a", 10000), ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			var at [2]uint64
			for i := 0; i <= 300; i++ {
				if err := doc.Edit(0, 1, string(rune('a'+i%2))); err != nil {
					t.Fatal(err)
				}
				if _, err := doc.Parse(); err != nil {
					t.Fatal(err)
				}
				if i == 20 || i == 300 {
					var ms runtime.MemStats
					runtime.GC()
					runtime.ReadMemStats(&ms)
					at[i/300] = ms.HeapAlloc
				}
			}
			if grown := int64(at[1]) - int64(at[0]); grown > 4<<20 {
				t.Errorf("%s/%s: live heap grew %d KiB across edits", backend, unit, grown>>10)
			}
			t.Logf("Document %s/%s: live heap %d -> %d bytes", backend, unit, at[0], at[1])
			runtime.KeepAlive(doc)
		}
	}
}

func TestMemoPruneKeepsActiveSeed(t *testing.T) {
	table := newMemoTable()
	seed := table.alloc()
	*seed = memoEntry{growing: true, node: &Node{Text: "active"}}
	table.put(memoKey{rule: 1}, seed)
	completed := table.alloc()
	*completed = memoEntry{node: &Node{Text: "retired"}}
	table.put(memoKey{rule: 2}, completed)
	table.prune(1)
	if seed.node == nil || seed.node.Text != "active" || !seed.growing {
		t.Fatal("pruning overwrote a stack-owned growth seed")
	}
	if table.len() != 0 {
		t.Fatal("pruned entries remain in the lookup table")
	}
	if next := table.alloc(); next == seed || next.node != nil || next.env != nil || next.expected != nil {
		t.Fatal("allocation reused an active seed or retained a retired result")
	}
	if !table.reset() {
		t.Fatal("small memo could not reset")
	}
	if next := table.alloc(); next.node != nil || next.next != nil {
		t.Fatal("reset left stale slab/free-list references")
	}
}

// BenchmarkMemoLifecycle measures the same workloads before and after memo
// retirement changes; input construction and grammar compilation are excluded.
func BenchmarkMemoLifecycle(b *testing.B) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, c := range []struct{ name, src, line string }{
			{"Stream", memoizedStream, "abc,abc\n"},
			{"StreamLeftRec", leftRecursiveStream, "abc,abc\n"},
		} {
			b.Run(fmt.Sprintf("%s/%s", c.name, backend), func(b *testing.B) {
				g, err := syntax.Parse(c.src)
				if err != nil {
					b.Fatal(err)
				}
				prog, err := Compile(g, Options{})
				if err != nil {
					b.Fatal(err)
				}
				// Prepare lazy VM compilation outside the timed interval.
				if _, err := prog.rule(backend, "main"); err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(c.line) * 10000))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := prog.ParseStreamWith("main", &lineReader{line: c.line, n: 10000}, func(*Node) error { return nil }, ParseOptions{Backend: backend}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
