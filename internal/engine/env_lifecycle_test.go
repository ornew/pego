package engine

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const assignedStream = `
def main = [limit=1] ([i=1] [j=2] [i=3] -"a")* #stream [limit==1] $$`

var envBindingCases = []genCase{
	{"binding choice rollback", `
type V struct { X int, Y int }
def main = [x=1] [y=2] ("a" [x=3] [y=4] "x" / "a" [x==1] [y==2] "b") $$ -> new V{X:x, Y:y}`, []string{"ab", "ax", "a", ""}},
	{"binding rule scope", `
type V struct { X int, Y int }
def main = [x=1] [y=2] inner [x==1] [y==2] "b" $$ -> new V{X:x, Y:y}
def inner = [x=3] [y=4] [x==3] [y==4] "a"`, []string{"ab", "ax", ""}},
	{"binding positive lookahead effects", `
type V struct { X int, Y int }
def main = [x=1] [y=2] &([x=3] [y=4] "a") [x==3] [y==4] "a" $$ -> new V{X:x, Y:y}`, []string{"a", "aa", ""}},
	{"binding absent after rollback", `
type V struct { X int }
def main = [x=1] ([y=2] "b" / [y=3] [y==3] "a") $$ -> new V{X:x}`, []string{"a", "b", ""}},
	{"binding negative lookahead rollback", `
type V struct { X int, Y int }
def main = [x=1] [y=2] !([x=3] [y=4] "b") [x==1] [y==2] "a" $$ -> new V{X:x, Y:y}`, []string{"a", "b", ""}},
}

func TestEnvironmentRollback(t *testing.T) {
	for i, c := range envBindingCases {
		want := `(V X=1 Y=2)`
		if i == 2 {
			want = `(V X=3 Y=4)`
		}
		if i == 3 {
			want = `(V X=1)`
		}
		check(t, c.src, ok(c.inputs[0], want))
	}
}

func TestAssignedStreamMemoryIsBounded(t *testing.T) {
	prog := compile(t, assignedStream)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				p := newStreamParser(prog, &lineReader{line: "a", n: 60000}, unit)
				count := 0
				var at [2]uint64
				p.emit = func(*Node) error {
					count++
					if count == 20000 || count == 60000 {
						names := make(map[string]bool)
						n := 0
						for e := p.env; e != nil; e = e.next {
							names[e.name] = true
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
				if _, err := prog.run(p, backend, "main"); err != nil || count != 60000 {
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
		{"Nearest", `[i=1] [i==1] -"a"`},
		{"Outer", `[i=1] [limit==1] -"a"`},
		{"Alternating", `[i=1] [j=2] [limit==1] -"a"`},
		{"Changing", `[i=i+1] [j=j+1] [limit==1] -"a"`},
		{"Rollback", `-"a" ([i=i+1] [j=j+1] "x" / [i=i+1] [j=j+1]) [limit==1]`},
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, n := range []int{2000, 4000, 8000, 16000} {
				b.Run(fmt.Sprintf("%s/%s/%d", c.name, backend, n), func(b *testing.B) {
					g, err := syntax.Parse(`def main = [limit=1] [i=0] [j=0] (` + c.body + `)* $$`)
					if err != nil {
						b.Fatal(err)
					}
					prog, err := Compile(g, Options{})
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
			src.WriteString("def main = ")
			for i := 0; i < n; i++ {
				fmt.Fprintf(&src, "[v%d=1] ", i)
			}
			src.WriteString("$$")
			g, err := syntax.Parse(src.String())
			if err != nil {
				b.Fatal(err)
			}
			prog, err := Compile(g, Options{})
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
	p := &parser{}
	p.bind("x", 1)
	first := p.env
	for i := 0; i < 100; i++ {
		p.bind("x", 1)
	}
	if p.env != first {
		t.Fatal("equal head assignment allocates state")
	}
	p.bind("y", "two")
	second := p.env
	p.bind("x", 1)
	if p.env != second {
		t.Fatal("equal non-head assignment replaces state")
	}
	p.bind("x", 3)
	if got, _ := second.lookup("x"); got != 1 {
		t.Fatal("replacement changed saved environment")
	}
	if got, _ := p.env.lookup("y"); got != "two" {
		t.Fatal("replacement lost prefix")
	}
	p.env = first
	p.bind("y", false)
	if got, _ := p.env.lookup("y"); got != false {
		t.Fatal("seen name absent after rollback was not bound")
	}
	p.env = nil
	p.bind("z", true)
	p.bind("w", "four")
	p.bind("z", false)
	names := make(map[string]bool)
	for e := p.env; e != nil; e = e.next {
		if names[e.name] {
			t.Fatalf("duplicate binding %s", e.name)
		}
		names[e.name] = true
	}
	if len(names) != 2 {
		t.Fatalf("have %d bindings", len(names))
	}
	if got, _ := first.lookup("x"); got != 1 {
		t.Fatal("first environment changed")
	}
}

// Compare persistent environments with independent value maps through changes
// and restores, including names first seen only in abandoned branches.
func TestBindingRestoresReferenceValues(t *testing.T) {
	type snapshot struct {
		env    *env
		values map[string]any
	}
	p := &parser{}
	values := map[string]any{}
	saved := []snapshot{{}}
	clone := func(m map[string]any) map[string]any {
		n := make(map[string]any, len(m))
		for k, v := range m {
			n[k] = v
		}
		return n
	}
	verify := func(e *env, want map[string]any) {
		t.Helper()
		seen := make(map[string]bool)
		for ; e != nil; e = e.next {
			if seen[e.name] || want[e.name] != e.val {
				t.Fatalf("wrong binding %s=%v", e.name, e.val)
			}
			seen[e.name] = true
		}
		if len(seen) != len(want) {
			t.Fatalf("%d bindings, want %d", len(seen), len(want))
		}
	}
	for i := 0; i < 500; i++ {
		if i%5 == 0 {
			s := saved[(i*13)%len(saved)]
			p.env = s.env
			values = clone(s.values)
		}
		name := fmt.Sprintf("v%d", (i*7)%16)
		var value any
		switch i % 3 {
		case 0:
			value = i % 11
		case 1:
			value = name
		case 2:
			value = i%2 == 0
		}
		p.bind(name, value)
		values[name] = value
		verify(p.env, values)
		if i%7 == 0 {
			saved = append(saved, snapshot{p.env, clone(values)})
		}
		for _, s := range saved {
			verify(s.env, s.values)
		}
	}
}

// A hint created after switching between single-name snapshots must still know
// names from the earlier snapshot when it is restored behind another binding.
func TestBindingRestoresBeforeSecondName(t *testing.T) {
	p := &parser{}
	p.bind("x", 1)
	saved := p.env
	p.env = nil
	p.bind("y", 2)
	p.bind("z", 3)
	p.env = saved
	p.bind("w", 4)
	p.bind("x", 5)
	count := 0
	for e := p.env; e != nil; e = e.next {
		count++
	}
	if count != 2 {
		t.Fatalf("%d bindings for two names", count)
	}
	if saved.val != 1 {
		t.Fatal("saved binding changed")
	}
}
