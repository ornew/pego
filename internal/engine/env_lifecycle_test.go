package engine

import (
	"fmt"
	"runtime"
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
						names := make(map[string]bool)
						n := 0
						for e := p.Env; e != nil; e = e.Next {
							names[e.Name] = true
							n++
						}
						if n != len(names) || n != 3 {
							t.Errorf("element %d: %d bindings for %d names", count, n, len(names))
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

func TestBindingSnapshots(t *testing.T) {
	t.Parallel()
	p := &core.Parser{}
	p.Bind("x", 1)
	first := p.Env
	for i := 0; i < 100; i++ {
		p.Bind("x", 1)
	}
	if p.Env != first {
		t.Fatal("equal head binding allocates state")
	}
	p.Bind("y", "two")
	second := p.Env
	p.Bind("x", 1)
	if p.Env != second {
		t.Fatal("equal non-head binding replaces state")
	}
	p.Bind("x", 3)
	if got, _ := second.Lookup("x"); got != 1 {
		t.Fatal("replacement changed saved environment")
	}
	if got, _ := p.Env.Lookup("y"); got != "two" {
		t.Fatal("replacement lost prefix")
	}
	p.Env = first
	p.Bind("y", false)
	if got, _ := p.Env.Lookup("y"); got != false {
		t.Fatal("seen name absent after rollback was not bound")
	}
	p.Env = nil
	p.Bind("z", true)
	p.Bind("w", "four")
	p.Bind("z", false)
	names := make(map[string]bool)
	for e := p.Env; e != nil; e = e.Next {
		if names[e.Name] {
			t.Fatalf("duplicate binding %s", e.Name)
		}
		names[e.Name] = true
	}
	if len(names) != 2 {
		t.Fatalf("have %d bindings", len(names))
	}
	if got, _ := first.Lookup("x"); got != 1 {
		t.Fatal("first environment changed")
	}
}

// Compare persistent environments with independent value maps through changes
// and restores, including names first seen only in abandoned branches.
func TestBindingRestoresReferenceValues(t *testing.T) {
	t.Parallel()
	type snapshot struct {
		env    *core.Env
		values map[string]any
	}
	p := &core.Parser{}
	values := map[string]any{}
	saved := []snapshot{{}}
	clone := func(m map[string]any) map[string]any {
		n := make(map[string]any, len(m))
		for k, v := range m {
			n[k] = v
		}
		return n
	}
	verify := func(e *core.Env, want map[string]any) {
		t.Helper()
		seen := make(map[string]bool)
		for ; e != nil; e = e.Next {
			if seen[e.Name] || want[e.Name] != e.Val {
				t.Fatalf("wrong binding %s=%v", e.Name, e.Val)
			}
			seen[e.Name] = true
		}
		if len(seen) != len(want) {
			t.Fatalf("%d bindings, want %d", len(seen), len(want))
		}
	}
	for i := 0; i < 500; i++ {
		if i%5 == 0 {
			s := saved[(i*13)%len(saved)]
			p.Env = s.env
			values = clone(s.values)
		}
		name := fmt.Sprintf("v%d", (i*7)%16)
		var val any
		switch i % 3 {
		case 0:
			val = i % 11
		case 1:
			val = name
		case 2:
			val = i%2 == 0
		}
		p.Bind(name, val)
		values[name] = val
		verify(p.Env, values)
		if i%7 == 0 {
			saved = append(saved, snapshot{p.Env, clone(values)})
		}
		for _, s := range saved {
			verify(s.env, s.values)
		}
	}
}

// A hint created after switching between single-name snapshots must still know
// names from the earlier snapshot when it is restored behind another binding.
func TestBindingRestoresBeforeSecondName(t *testing.T) {
	t.Parallel()
	p := &core.Parser{}
	p.Bind("x", 1)
	saved := p.Env
	p.Env = nil
	p.Bind("y", 2)
	p.Bind("z", 3)
	p.Env = saved
	p.Bind("w", 4)
	p.Bind("x", 5)
	count := 0
	for e := p.Env; e != nil; e = e.Next {
		count++
	}
	if count != 2 {
		t.Fatalf("%d bindings for two names", count)
	}
	if saved.Val != 1 {
		t.Fatal("saved binding changed")
	}
}
