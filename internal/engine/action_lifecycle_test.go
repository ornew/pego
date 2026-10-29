package engine

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

const nilActionStream = `type V = { N int }
rule main = item* #stream $$
rule item = "a" => foldl(nil, list(V{N: 1}), (acc, v) => nil)`

var actionLifecycleCases = []genCase{
	{"nil allocating action", nilActionStream, []string{"", "a", "aaa", "ab"}},
	{"nil OperatorsRule action", `type V = { N int }
rule main = e $$
rule e = #operators {
    operand "a" => foldl(nil, list(V{N: 1}), (acc, v) => nil)
    level { postfix "!" => foldl(nil, list(V{N: 2}), (acc, v) => nil) }
}`, []string{"a", "a!", "a!!", "a?"}},
}

func TestActionResultReleasesTracking(t *testing.T) {
	for _, c := range []struct {
		name  string
		value any
		err   error
	}{
		{"nil", nil, nil},
		{"typed nil", (*value.Node)(nil), nil},
		{"node", value.Ptr(value.Data{Start: 90, End: 91, Fresh: true}), nil},
		{"error", nil, errors.New("action failed")},
		{"boxed value", 1, nil},
		{"invalid result", workActionFunc{params: 1}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			prefix := value.Ptr(value.Data{Text: "keep"})
			p := &core.Parser{Created: []*value.Node{prefix}}
			ctx := &core.EvalCtx{P: p, Cbase: 1, Start: 3, End: 4}
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				n := p.ActionResult(ctx, func(*core.EvalCtx) (any, error) {
					p.Created = append(p.Created, value.Ptr(value.Data{Text: "temporary"}))
					if n, ok := c.value.(*value.Node); ok && n != nil {
						p.Created = append(p.Created, n)
					}
					return c.value, c.err
				}, "test")
				if n != nil && (value.D(n).Start != 3 || value.D(n).End != 4 || value.D(n).Fresh) {
					t.Errorf("returned node has wrong span/freshness: %+v", n)
				}
			}()
			wantPanic := c.err != nil || c.name == "invalid result"
			if (panicked != nil) != wantPanic {
				t.Errorf("panic = %v, want panic %v", panicked, wantPanic)
			}
			if len(p.Created) != 1 || p.Created[0] != prefix {
				t.Fatalf("tracking prefix changed: %v", p.Created)
			}
			for _, n := range p.Created[len(p.Created):cap(p.Created)] {
				if n != nil {
					t.Fatal("tracking backing array retains a discarded node")
				}
			}
		})
	}
}

func TestNilActionStreamMemoryIsBounded(t *testing.T) {
	prog := mustCompile(t, nilActionStream)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				p := core.NewStreamParser(&prog.sharedProgram, &lineReader{line: "a", n: 60000}, unit)
				var at [2]uint64
				count := 0
				p.Emit = func(n *value.Node) error {
					count++
					if n != nil || len(p.Created) != 0 {
						return fmt.Errorf("element %d: node %v, tracking length %d", count, n, len(p.Created))
					}
					for _, n := range p.Created[:cap(p.Created)] {
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
				t.Logf("live heap %d -> %d bytes, tracking capacity %d", at[0], at[1], cap(p.Created))
				runtime.KeepAlive(p)
			})
		}
	}
}

func BenchmarkNilActionStream(b *testing.B) {
	g, err := parseTest(nilActionStream)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
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
				if err := prog.ParseStreamWith("main", &lineReader{line: "a", n: 10000}, func(*value.Node) error { return nil }, ParseOptions{Backend: backend}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
