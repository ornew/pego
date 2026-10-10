package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_TYPED_POOL_DIR preserves this generated fixture for standalone heap and
// throughput measurements. Normal tests use an automatically removed directory.
func TestGeneratedTypedPoolRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := syntax.Parse(`
type A terminal
def main: A = x:large x:small -- -> $x
def large: A = (?a)*
def small: A = "b"
def repeated: A = (x:"b"){2} --`)
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []bool{false, true} {
		name := "direct"
		if reference {
			name = "general"
		}
		t.Run(name, func(t *testing.T) {
			code, err := Generate(g, GenOptions{Package: "poolfixture", Start: "main", Types: true, disableTypedCuts: reference})
			if err != nil {
				t.Fatal(err)
			}
			dir := os.Getenv("PEGO_TYPED_POOL_DIR")
			if dir == "" {
				dir = t.TempDir()
			} else {
				if reference {
					dir = filepath.Join(dir, name)
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			fixture := typedPoolFixture
			if reference {
				fixture = strings.Replace(fixture, "const generalCapture = false", "const generalCapture = true", 1)
			}
			for name, contents := range map[string][]byte{
				"parser.go": code, "pool_test.go": []byte(fixture), "go.mod": []byte("module poolfixture\n\ngo 1.27.1\n"),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", "-run", "^TestTypedPool")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated typed pool: %v\n%s", err, out)
			}
		})
	}
}

const typedPoolFixture = `package poolfixture

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

const generalCapture = false

func checkTrail(t *testing.T, p *tparser) {
	t.Helper()
	for i, u := range p.trail[:cap(p.trail)] {
		if u.f != nil || u.old != nil || u.slot != 0 {
			t.Errorf("trail slot %d retains discarded undo", i)
		}
	}
}

func TestTypedPoolRules(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		p := &tparser{parser: &parser{}}
		v, err := p.run(trules[0], strings.Repeat("a", 2<<20)+"b", []Unit{unit}, &tslabs{})
		if err != nil || v.(*A).Text != "b" {
			t.Fatalf("large parse: %v", err)
		}
		if len(p.trail) != 0 || (generalCapture && cap(p.trail) == 0) || (!generalCapture && cap(p.trail) != 0) {
			t.Fatal("capture rule did not use and discard the expected undo trail")
		}
		checkTrail(t, p)
		p.recycle()
		checkTrail(t, p)
		// Recycling scratch must leave the caller's returned terminal unchanged.
		if v.(*A).Text != "b" {
			t.Fatal("returned value changed")
		}
		if _, err := p.run(trules[0], "", []Unit{unit}, &tslabs{}); err == nil {
			t.Fatal("empty parse accepted")
		}
		p.recycle()
		checkTrail(t, p)
		if v, err := p.run(trules[0], "ab", []Unit{unit}, &tslabs{}); err != nil || v.(*A).Text != "b" {
			t.Fatalf("small followup: %v", err)
		}
		p.recycle()
		if v, err := p.run(trules[3], "bb", []Unit{unit}, &tslabs{}); err != nil || v.(*A).Text != "bb" {
			t.Fatalf("repeated capture: %v", err)
		}
		checkTrail(t, p)
		p.recycle()
	}
}

func TestTypedPoolUndoAndSaved(t *testing.T) {
	p := &tparser{parser: &parser{}}
	f := &tframe{vals: []any{"before"}}
	p.frame = f
	m := p.mark()
	p.setCapture(0, "outer")
	inner := p.mark()
	p.setCapture(0, "inner")
	p.reset(inner)
	if f.vals[0] != "outer" || len(p.trail) != 1 || p.trail[0].old != "before" {
		t.Fatal("reset damaged live undo")
	}
	for _, u := range p.trail[len(p.trail):cap(p.trail)] {
		if u.f != nil || u.old != nil {
			t.Error("reset retained undo tail")
		}
	}
	p.reset(m)
	if f.vals[0] != "before" {
		t.Fatal("reset lost original capture")
	}
	checkTrail(t, p)
	// invokePlain still creates trails for captures in repeated element frames.
	p.setCapture(0, "outer")
	r := &trule{rule: &rule{novalue: true}, body: func(p *tparser, _ int) (any, bool) {
		prev := p.frame
		p.frame = p.newFrame(1)
		p.setCapture(0, "changed")
		p.frame = prev
		return nil, true
	}}
	p.invokePlain(r, 0)
	if len(p.trail) != 1 || p.trail[0].old != "before" || f.vals[0] != "outer" {
		t.Fatal("call damaged parent undo")
	}
	for _, u := range p.trail[len(p.trail):cap(p.trail)] {
		if u.f != nil || u.old != nil {
			t.Error("call retained undo tail")
		}
	}
	p.reset(m)
	checkTrail(t, p)
	// Pratt's saved-capture scratch can shrink after a wider candidate.
	p.saved = []any{&A{Text: strings.Repeat("a", 1024)}, &A{Text: "tail"}}
	p.saved = append(p.saved[:0], nil)
	p.recycle()
	for _, v := range p.saved[:cap(p.saved)] {
		if v != nil {
			t.Error("saved capture tail survived recycling")
		}
	}
}

// The parser is kept explicitly, so GC cannot hide references by emptying
// sync.Pool. Returned terminals are dropped before measuring scratch retention.
func retainedTypedParser(unit Unit, n int) *tparser {
	p := &tparser{parser: &parser{}}
	v, err := p.run(trules[0], strings.Repeat("a", n)+"b", []Unit{unit}, &tslabs{})
	if err != nil || v.(*A).Text != "b" {
		panic(err)
	}
	p.recycle()
	return p
}

func TestTypedPoolHeap(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("diagnostic retained-heap measurement")
	}
	for _, unit := range []Unit{CodePoints, Bytes} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		p := retainedTypedParser(unit, 2<<20)
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("unit=%v retained_heap_delta=%d bytes trail_capacity=%d", unit, int64(after.HeapAlloc)-int64(before.HeapAlloc), cap(p.trail))
		runtime.KeepAlive(p)
	}
}

func BenchmarkTypedCapturePool(b *testing.B) {
	for _, n := range []int{64, 4096} {
		input := strings.Repeat("a", n) + "b"
		for _, unit := range []Unit{CodePoints, Bytes} {
			b.Run(fmt.Sprintf("%d/%v", n, unit), func(b *testing.B) {
				if _, err := ParseAST(input, unit); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if v, err := ParseAST(input, unit); err != nil || v.Text != "b" {
						b.Fatalf("parse: %v", err)
					}
				}
			})
		}
	}
}
`
