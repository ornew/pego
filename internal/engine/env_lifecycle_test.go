package engine

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

func TestEnvironmentRollback(t *testing.T) {
	t.Parallel()
	for i, c := range gentest.EnvBindingCases {
		want := `V{X: 1, Y: 2}`
		if i == 2 {
			want = `V{X: 3, Y: 4}`
		}
		if i == 3 {
			want = `V{X: 1}`
		}
		check(t, c.Src, ok(c.Inputs[0], want))
	}
}

func TestBoundStreamMemoryIsBounded(t *testing.T) {
	prog := mustCompile(t, gentest.BoundStream)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				p := core.NewStreamParser(&prog.sharedProgram, &lineReader{line: "a", n: 60000}, unit)
				count := 0
				var at [2]uint64
				p.Emit = func(*value.Node) error {
					count++
					if count == 20000 || count == 60000 {
						// The environment holds one value for each of the three declared
						// variables, however many bindings have been made.
						n := 0
						for slot := 0; slot < 3; slot++ {
							if p.Env.Get(slot, p.EnvDepth()) != nil {
								n++
							}
						}
						if n != 3 {
							t.Errorf("element %d: %d of 3 variables have a value", count, n)
						}
						var ms runtime.MemStats
						runtime.GC()
						runtime.ReadMemStats(&ms)
						at[count/60000] = ms.HeapAlloc
						t.Logf("element %d: heap %d bytes, bindings %d", count, ms.HeapAlloc, n)
					}
					return nil
				}
				if _, err := prog.Run(p, backend, "main"); err != nil || count != 60000 {
					t.Fatalf("emitted %d, error %v", count, err)
				}
				if growth := int64(at[1]) - int64(at[0]); growth > 4<<20 {
					t.Errorf("heap grew %d KiB", growth>>10)
				}
				runtime.KeepAlive(p)
			})
		}
	}
}

func BenchmarkEnvironmentBindings(b *testing.B) {
	for _, c := range []struct{ name, body string }{
		{"Nearest", `{i := 1} {i==1} -"a"`},
		{"Outer", `{i := 1} {limit==1} -"a"`},
		{"Alternating", `{i := 1} {j := 2} {limit==1} -"a"`},
		{"Changing", `{i := i+1} {j := j+1} {limit==1} -"a"`},
		{"Rollback", `-"a" ({i := i+1} {j := j+1} "x" / {i := i+1} {j := j+1}) {limit==1}`},
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, n := range []int{2000, 4000, 8000, 16000} {
				b.Run(fmt.Sprintf("%s/%s/%d", c.name, backend, n), func(b *testing.B) {
					g, err := parseTest(`var limit int
var i int
var j int
rule main = {limit := 1} {i := 0} {j := 0} (` + c.body + `)* $$`)
					if err != nil {
						b.Fatal(err)
					}
					prog, err := Compile(g, compile.Options{})
					if err != nil {
						b.Fatal(err)
					}
					if _, err := prog.rule(backend, "main"); err != nil {
						b.Fatal(err)
					}
					input := strings.Repeat("a", n)
					b.SetBytes(int64(n))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Recognize: true}); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func BenchmarkDistinctBindings(b *testing.B) {
	for _, n := range []int{16, 128, 1024} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			var src strings.Builder
			for i := 0; i < n; i++ {
				fmt.Fprintf(&src, "var v%d int\n", i)
			}
			src.WriteString("rule main = ")
			for i := 0; i < n; i++ {
				fmt.Fprintf(&src, "{v%d := 1} ", i)
			}
			src.WriteString("$$")
			g, err := parseTest(src.String())
			if err != nil {
				b.Fatal(err)
			}
			prog, err := Compile(g, compile.Options{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := prog.ParseWith("main", "", ParseOptions{Recognize: true}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// variableCounts cover a single leaf, the full leaf, and the trie of two and three levels.
var variableCounts = []int{1, 2, 3, 4, 5, 16, 17, 256, 257, 280}

func TestBindingSnapshots(t *testing.T) {
	t.Parallel()
	for _, n := range variableCounts {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			p := &core.Parser{}
			p.SetVars(memoTestVars(n))
			d := p.EnvDepth()
			first := p.Env
			last := n - 1
			for i := 0; i < 100; i++ {
				p.Bind(last, last)
				p.Bind(0, 0)
			}
			if p.Env != first {
				t.Fatal("equal binding allocates state")
			}
			p.Bind(0, "two")
			second := p.Env
			p.Bind(0, "two")
			if p.Env != second {
				t.Fatal("equal rebinding replaces state")
			}
			if n > 1 {
				p.Bind(last, "three")
				if got := second.Get(last, d); got != last {
					t.Fatal("replacement changed saved environment")
				}
				if got := p.Env.Get(0, d); got != "two" {
					t.Fatal("replacement lost an earlier binding")
				}
			}
			p.Env = first
			p.Bind(0, false)
			if got := p.Env.Get(0, d); got != false {
				t.Fatal("binding after rollback was lost")
			}
			if got := second.Get(0, d); got != "two" {
				t.Fatal("rollback binding changed another environment")
			}
			for i := 0; i < n; i++ {
				if got := first.Get(i, d); got != i {
					t.Fatalf("first environment changed at slot %d: %v", i, got)
				}
			}
		})
	}
}

// Compare persistent environments with independent value slices through changes
// and restores.
func TestBindingRestoresReferenceValues(t *testing.T) {
	t.Parallel()
	for _, n := range variableCounts {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			type snapshot struct {
				env    *core.Env
				values []any
			}
			p := &core.Parser{}
			p.SetVars(memoTestVars(n))
			d := p.EnvDepth()
			values := memoTestVars(n)
			saved := []snapshot{{p.Env, slices.Clone(values)}}
			verify := func(e *core.Env, want []any) {
				t.Helper()
				for i, w := range want {
					if got := e.Get(i, d); got != w {
						t.Fatalf("wrong binding in slot %d: %v, want %v", i, got, w)
					}
				}
			}
			for i := 0; i < 500; i++ {
				if i%5 == 0 {
					s := saved[(i*13)%len(saved)]
					p.Env = s.env
					values = slices.Clone(s.values)
				}
				slot := (i * 7) % n
				var val any
				switch i % 3 {
				case 0:
					val = i % 11
				case 1:
					val = fmt.Sprint("v", slot)
				case 2:
					val = i%2 == 0
				}
				p.Bind(slot, val)
				values[slot] = val
				verify(p.Env, values)
				if i%7 == 0 {
					saved = append(saved, snapshot{p.Env, slices.Clone(values)})
				}
				for _, s := range saved {
					verify(s.env, s.values)
				}
			}
		})
	}
}

// Two branches that continue from one saved environment do not see each other's bindings.
func TestBindingBranchesFromOneSnapshot(t *testing.T) {
	t.Parallel()
	for _, n := range variableCounts {
		p := &core.Parser{}
		p.SetVars(memoTestVars(n))
		d := p.EnvDepth()
		saved := p.Env
		p.Bind(0, "a")
		left := p.Env
		p.Env = saved
		p.Bind(n-1, "b")
		right := p.Env
		if left.Get(n-1, d) != n-1 && n > 1 || right.Get(0, d) != 0 && n > 1 {
			t.Fatalf("%d variables: a branch saw the binding of another", n)
		}
		if saved.Get(0, d) != 0 || saved.Get(n-1, d) != n-1 {
			t.Fatalf("%d variables: the saved environment changed", n)
		}
	}
}
