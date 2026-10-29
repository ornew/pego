package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// PEGO_TYPED_POOL_DIR preserves this generated fixture for standalone heap and
// throughput measurements. Normal tests use an automatically removed directory.
func TestGeneratedTypedPoolRetention(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedTypedPoolRetention)
}

func testGeneratedTypedPoolRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(`type A = { Text string }
rule main A = x:large x:small #cut => $x
rule large A = t:([a]*) #atomic => A{Text: text($t)}
rule small A = t:("b") #atomic => A{Text: text($t)}
rule repeated A = t:((x:"b") #repeat(2) #cut) => A{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	name := "direct"
	t.Run(name, func(t *testing.T) {
		code, err := generateCode(t, g, gen.GenOptions{Package: "poolfixture", Start: "main", Types: true})
		if err != nil {
			t.Fatal(err)
		}
		dir := os.Getenv("PEGO_TYPED_POOL_DIR")
		if dir == "" {
			dir = t.TempDir()
		} else {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		fixture := typedPoolFixture
		for name, contents := range map[string][]byte{
			"parser.go": code, "pool_test.go": []byte(fixture), "go.mod": []byte(testGoMod("poolfixture")),
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

// TestGeneratedUntypedPoolRetention verifies that the untyped generated parser
// does not keep an oversized source after release, and that a returned short
// terminal remains valid while the parser is reset.
func TestGeneratedUntypedPoolRetention(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedUntypedPoolRetention)
}

func testGeneratedUntypedPoolRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(`type A = node[string]
rule main A = lead value:small => $value
rule lead = [^b]*
rule small A = ("b") #atomic`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(t, g, gen.GenOptions{Package: "main", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, contents := range map[string][]byte{
		"parser.go": code, "pool_test.go": []byte(untypedPoolFixture), "go.mod": []byte(testGoMod("untypedpool")),
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
	pegort "github.com/ornew/pego/runtime"
	"encoding/json"
	"strings"
	"testing"
)

func TestUntypedPoolRetention(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		unit PositionUnit
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
			p := &pegort.Parser{MaxDepth: pegoGrammar.X_maxDepth}
			p.X_setGrammar(pegoGrammar)
			if tc.unit == Bytes {
				p.X_unit, p.X_bs, p.X_n = Bytes, input, len(input)
			} else {
				p.X_setSource(input)
			}
			n, ok := p.Call(rules[0], 0)
			if !ok || p.Pos != p.X_n {
				t.Fatalf("large parse failed at %d/%d", p.Pos, p.X_n)
			}
			before, err := json.Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			if n.Type() != "A" || n.RawText != "b" || n.RawStart != tc.start || n.RawEnd != tc.start+1 {
				t.Fatalf("large result: type=%s span=[%d,%d) text=%q", n.Type(), n.RawStart, n.RawEnd, n.RawText)
			}

			p.X_release()
			if p.X_bs != "" || p.X_src != "" || p.X_n != 0 || p.Pos != 0 || len(p.In) != 0 || len(p.X_offs) != 0 {
				t.Fatal("released parser still references the input")
			}
			if cap(p.In) != 0 || cap(p.X_offs) != 0 {
				t.Fatalf("oversized input buffers retained: in=%d offsets=%d", cap(p.In), cap(p.X_offs))
			}
			for i, entry := range p.X_memo.X_slots[:cap(p.X_memo.X_slots)] {
				if entry != nil {
					t.Fatalf("memo slot %d retains an entry", i)
				}
			}
			for i, chunk := range p.X_memo.X_chunks {
				for j, entry := range chunk {
					if entry.X_node != nil || entry.X_errs != nil || entry.X_expected != nil || entry.X_next != nil || entry.X_env != nil {
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
			if err != nil || got == nil || got.Type() != "A" || got.RawText != "b" || got.RawStart != tc.retryStart || got.RawEnd != tc.retryStart+1 {
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
	pegort "github.com/ornew/pego/runtime"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

const generalCapture = false

func checkTrail(t *testing.T, p *pegort.TParser) {
	t.Helper()
	for i, u := range p.Trail[:cap(p.Trail)] {
		if u.X_f != nil || u.X_old != nil || u.X_slot != 0 {
			t.Errorf("trail slot %d retains discarded undo", i)
		}
	}
}

func TestTypedPoolRules(t *testing.T) {
	t.Parallel()
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		p := &pegort.TParser{Parser: &pegort.Parser{MaxDepth: pegoGrammar.X_maxDepth}}
		v, err := p.X_run(trules[0], strings.Repeat("a", 2<<20)+"b", pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{})
		if err != nil || v.(*A).Text != "b" {
			t.Fatalf("large parse: %v", err)
		}
		if len(p.Trail) != 0 || (generalCapture && cap(p.Trail) == 0) || (!generalCapture && cap(p.Trail) != 0) {
			t.Fatal("capture rule did not use and discard the expected undo trail")
		}
		checkTrail(t, p)
		p.X_recycle()
		checkTrail(t, p)
		// Recycling scratch must leave the caller's returned terminal unchanged.
		if v.(*A).Text != "b" {
			t.Fatal("returned value changed")
		}
		if _, err := p.X_run(trules[0], "", pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{}); err == nil {
			t.Fatal("empty parse accepted")
		}
		p.X_recycle()
		checkTrail(t, p)
		if v, err := p.X_run(trules[0], "ab", pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{}); err != nil || v.(*A).Text != "b" {
			t.Fatalf("small followup: %v", err)
		}
		p.X_recycle()
		if v, err := p.X_run(trules[3], "bb", pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{}); err != nil || v.(*A).Text != "bb" {
			t.Fatalf("repeated capture: %v", err)
		}
		checkTrail(t, p)
		p.X_recycle()
	}
}

func TestTypedPoolUndoAndSaved(t *testing.T) {
	t.Parallel()
	p := &pegort.TParser{Parser: &pegort.Parser{MaxDepth: pegoGrammar.X_maxDepth}}
	f := &pegort.TCaptureFrame{Vals: []any{"before"}}
	p.Frame = f
	m := p.Mark()
	p.SetCapture(0, "outer")
	inner := p.Mark()
	p.SetCapture(0, "inner")
	p.Reset(inner)
	if f.Vals[0] != "outer" || len(p.Trail) != 1 || p.Trail[0].X_old != "before" {
		t.Fatal("reset damaged live undo")
	}
	for _, u := range p.Trail[len(p.Trail):cap(p.Trail)] {
		if u.X_f != nil || u.X_old != nil {
			t.Error("reset retained undo tail")
		}
	}
	p.Reset(m)
	if f.Vals[0] != "before" {
		t.Fatal("reset lost original capture")
	}
	checkTrail(t, p)
	// InvokePlain still creates trails for captures in repeated element frames.
	p.SetCapture(0, "outer")
	r := &pegort.TRule{Rule: &pegort.Rule{Novalue: true}, Body: func(p *pegort.TParser, _ int) (any, bool) {
		prev := p.Frame
		p.Frame = p.NewFrame(1)
		p.SetCapture(0, "changed")
		p.Frame = prev
		return nil, true
	}}
	p.InvokePlain(r, 0)
	if len(p.Trail) != 1 || p.Trail[0].X_old != "before" || f.Vals[0] != "outer" {
		t.Fatal("call damaged parent undo")
	}
	for _, u := range p.Trail[len(p.Trail):cap(p.Trail)] {
		if u.X_f != nil || u.X_old != nil {
			t.Error("call retained undo tail")
		}
	}
	p.Reset(m)
	checkTrail(t, p)
	// OperatorsRule's saved-capture scratch can shrink after a wider candidate.
	p.X_saved = []any{&A{Text: strings.Repeat("a", 1024)}, &A{Text: "tail"}}
	p.X_saved = append(p.X_saved[:0], nil)
	p.X_recycle()
	for _, v := range p.X_saved[:cap(p.X_saved)] {
		if v != nil {
			t.Error("saved capture tail survived recycling")
		}
	}
}

// The parser is kept explicitly, so GC cannot hide references by emptying
// sync.Pool. Returned terminals are dropped before measuring scratch retention.
func retainedTypedParser(unit PositionUnit, n int) *pegort.TParser {
	p := &pegort.TParser{Parser: &pegort.Parser{MaxDepth: pegoGrammar.X_maxDepth}}
	v, err := p.X_run(trules[0], strings.Repeat("a", n)+"b", pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{})
	if err != nil || v.(*A).Text != "b" {
		panic(err)
	}
	p.X_recycle()
	return p
}

func TestTypedPoolHeap(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("diagnostic retained-heap measurement")
	}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		p := retainedTypedParser(unit, 2<<20)
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("unit=%v retained_heap_delta=%d bytes trail_capacity=%d", unit, int64(after.HeapAlloc)-int64(before.HeapAlloc), cap(p.Trail))
		runtime.KeepAlive(p)
	}
}

func BenchmarkTypedCapturePool(b *testing.B) {
	for _, n := range []int{64, 4096} {
		input := strings.Repeat("a", n) + "b"
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
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
