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

// TestGeneratedUntypedPoolRetention verifies that the untyped generated parser
// does not keep an oversized source after release, and that a returned short
// terminal remains valid while the parser is reset.
func TestGeneratedUntypedPoolRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := syntax.Parse(`
type A terminal
def main: A = lead value:small -> $value
def lead = (?^b)*
def small: A = "b"`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "main", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, contents := range map[string][]byte{
		"parser.go": code, "pool_test.go": []byte(untypedPoolFixture), "go.mod": []byte("module untypedpool\n\ngo 1.27.1\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestUntypedPoolRetention$")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated untyped pool: %v\n%s", err, out)
	}
}

const untypedPoolFixture = `package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUntypedPoolRetention(t *testing.T) {
	for _, tc := range []struct {
		name string
		unit Unit
		prefix string
		start int32
		retry string
		retryStart int32
	}{
		{"code points", CodePoints, strings.Repeat("é", (1<<20)+1), 1<<20 + 1, "ééb", 2},
		{"bytes with invalid UTF-8", Bytes, strings.Repeat("é", 1<<19) + "\xff", 1<<20 + 1, "\xffb", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.prefix + "b"
			p := &parser{maxDepth: defaultMaxDepth}
			p.memo.stride = nseen
			if tc.unit == Bytes {
				p.unit, p.bs, p.n = Bytes, input, len(input)
			} else {
				p.setSource(input)
			}
			n, ok := p.call(rules[0], 0)
			if !ok || p.pos != p.n {
				t.Fatalf("large parse failed at %d/%d", p.pos, p.n)
			}
			before, err := json.Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			if n.Type() != "A" || n.Text != "b" || n.Start != tc.start || n.End != tc.start+1 {
				t.Fatalf("large result: type=%s span=[%d,%d) text=%q", n.Type(), n.Start, n.End, n.Text)
			}

			p.release()
			if p.bs != "" || p.src != "" || p.n != 0 || p.pos != 0 || len(p.in) != 0 || len(p.offs) != 0 {
				t.Fatal("released parser still references the input")
			}
			if cap(p.in) != 0 || cap(p.offs) != 0 {
				t.Fatalf("oversized input buffers retained: in=%d offsets=%d", cap(p.in), cap(p.offs))
			}
			for i, entry := range p.memo.slots[:cap(p.memo.slots)] {
				if entry != nil {
					t.Fatalf("memo slot %d retains an entry", i)
				}
			}
			for i, chunk := range p.memo.chunks {
				for j, entry := range chunk {
					if entry.node != nil || entry.errs != nil || entry.expected != nil || entry.next != nil || entry.env != nil {
						t.Fatalf("memo chunk %d entry %d retains parse state", i, j)
					}
				}
			}
			after, err := json.Marshal(n)
			if err != nil || string(after) != string(before) {
				t.Fatalf("release changed returned node: %s / %s (error %v)", before, after, err)
			}

			if _, err := Parse("x", WithUnit(tc.unit)); err == nil {
				t.Fatal("invalid small input accepted")
			}
			got, err := Parse(tc.retry, WithUnit(tc.unit))
			if err != nil || got == nil || got.Type() != "A" || got.Text != "b" || got.Start != tc.retryStart || got.End != tc.retryStart+1 {
				t.Fatalf("small retry: node=%v error=%v", got, err)
			}
			after, err = json.Marshal(n)
			if err != nil || string(after) != string(before) {
				t.Fatalf("later parse changed returned node: %s / %s (error %v)", before, after, err)
			}
		})
	}
}
`

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
		p := &tparser{parser: &parser{maxDepth: defaultMaxDepth}}
		v, err := p.run(trules[0], strings.Repeat("a", 2<<20)+"b", parseOptions{unit: unit, maxDepth: defaultMaxDepth}, &tslabs{})
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
		if _, err := p.run(trules[0], "", parseOptions{unit: unit, maxDepth: defaultMaxDepth}, &tslabs{}); err == nil {
			t.Fatal("empty parse accepted")
		}
		p.recycle()
		checkTrail(t, p)
		if v, err := p.run(trules[0], "ab", parseOptions{unit: unit, maxDepth: defaultMaxDepth}, &tslabs{}); err != nil || v.(*A).Text != "b" {
			t.Fatalf("small followup: %v", err)
		}
		p.recycle()
		if v, err := p.run(trules[3], "bb", parseOptions{unit: unit, maxDepth: defaultMaxDepth}, &tslabs{}); err != nil || v.(*A).Text != "bb" {
			t.Fatalf("repeated capture: %v", err)
		}
		checkTrail(t, p)
		p.recycle()
	}
}

func TestTypedPoolUndoAndSaved(t *testing.T) {
	p := &tparser{parser: &parser{maxDepth: defaultMaxDepth}}
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
	p := &tparser{parser: &parser{maxDepth: defaultMaxDepth}}
	v, err := p.run(trules[0], strings.Repeat("a", n)+"b", parseOptions{unit: unit, maxDepth: defaultMaxDepth}, &tslabs{})
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
				if _, err := ParseAST(input, WithUnit(unit)); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if v, err := ParseAST(input, WithUnit(unit)); err != nil || v.Text != "b" {
						b.Fatalf("parse: %v", err)
					}
				}
			})
		}
	}
}
`
