package engine

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const nilActionStream = `
type V struct { N int }
def main = item* #stream $$
def item = "a" -> foldl(nil, list(new V{N: 1}), (acc, v) => nil)`

var actionLifecycleCases = []genCase{
	{"nil allocating action", nilActionStream, []string{"", "a", "aaa", "ab"}},
	{"nil Pratt action", `
type V struct { N int }
def main = e $$
def e = pratt {
    operand "a" -> foldl(nil, list(new V{N: 1}), (acc, v) => nil)
    level { postfix "!" -> foldl(nil, list(new V{N: 2}), (acc, v) => nil) }
}`, []string{"a", "a!", "a!!", "a?"}},
}

func TestActionResultReleasesTracking(t *testing.T) {
	for _, c := range []struct {
		name  string
		value any
		err   error
	}{
		{"nil", nil, nil},
		{"typed nil", (*Node)(nil), nil},
		{"node", &Node{Start: 90, End: 91, fresh: true}, nil},
		{"error", nil, errors.New("action failed")},
		{"invalid result", 1, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			prefix := &Node{Text: "keep"}
			p := &parser{created: []*Node{prefix}}
			ctx := &evalCtx{p: p, cbase: 1, start: 3, end: 4}
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				n := p.actionResult(ctx, func(*evalCtx) (any, error) {
					p.created = append(p.created, &Node{Text: "temporary"})
					if n, ok := c.value.(*Node); ok && n != nil {
						p.created = append(p.created, n)
					}
					return c.value, c.err
				}, "test")
				if n != nil && (n.Start != 3 || n.End != 4 || n.fresh) {
					t.Errorf("returned node has wrong span/freshness: %+v", n)
				}
			}()
			wantPanic := c.err != nil || c.name == "invalid result"
			if (panicked != nil) != wantPanic {
				t.Errorf("panic = %v, want panic %v", panicked, wantPanic)
			}
			if len(p.created) != 1 || p.created[0] != prefix {
				t.Fatalf("tracking prefix changed: %v", p.created)
			}
			for _, n := range p.created[len(p.created):cap(p.created)] {
				if n != nil {
					t.Fatal("tracking backing array retains a discarded node")
				}
			}
		})
	}
}

func TestNilActionStreamMemoryIsBounded(t *testing.T) {
	prog := compile(t, nilActionStream)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				p := newStreamParser(prog, &lineReader{line: "a", n: 60000}, unit)
				var at [2]uint64
				count := 0
				p.emit = func(n *Node) error {
					count++
					if n != nil || len(p.created) != 0 {
						return fmt.Errorf("element %d: node %v, tracking length %d", count, n, len(p.created))
					}
					for _, n := range p.created[:cap(p.created)] {
						if n != nil {
							return fmt.Errorf("element %d: stale tracking reference", count)
						}
					}
					if count == 20000 || count == 60000 {
						var ms runtime.MemStats
						runtime.GC()
						runtime.ReadMemStats(&ms)
						at[count/60000] = ms.HeapAlloc
					}
					return nil
				}
				if _, err := prog.run(p, backend, "main"); err != nil || count != 60000 {
					t.Fatalf("emitted %d, error %v", count, err)
				}
				if growth := int64(at[1]) - int64(at[0]); growth > 4<<20 {
					t.Errorf("live heap grew %d KiB", growth>>10)
				}
				t.Logf("live heap %d -> %d bytes, tracking capacity %d", at[0], at[1], cap(p.created))
				runtime.KeepAlive(p)
			})
		}
	}
}

func BenchmarkNilActionStream(b *testing.B) {
	g, err := syntax.Parse(nilActionStream)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		b.Run(backend.String(), func(b *testing.B) {
			if _, err := prog.rule(backend, "main"); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(10000)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := prog.ParseStreamWith("main", &lineReader{line: "a", n: 10000}, func(*Node) error { return nil }, ParseOptions{Backend: backend}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
