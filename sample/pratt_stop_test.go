package sample

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

// Filtered checks can outlive the next stop-check call while a caller's continuation runs. Their
// backing arrays must stay independent, and different masks must not grow the static stop cache.
func TestPrattStopCheckOwnershipAndCacheBound(t *testing.T) {
	const levels = 70
	var src strings.Builder
	src.WriteString("def main = e $$\ndef e = pratt { operand \"a\" ")
	for i := range levels {
		fmt.Fprintf(&src, "level { infix none \"<%d>\" } ", i)
	}
	src.WriteByte('}')
	ast, err := pego.ParseGrammar(src.String())
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	ri := in.rules["e"]
	g := newGen(in, &config{})
	open := make([]bool, levels)
	open[levels-1] = true
	first := g.prattStopCheck(ri, 0, open)
	m := matcher{in: in}
	check := func(allowLast bool) {
		t.Helper()
		st, _ := m.runPrattStop(first, []byte("<69>a"), 0, true)
		if st != failed {
			t.Fatalf("previous check changed after a new mask: %v", st)
		}
		st = failed
		if c := g.prattStopCheck(ri, 0, open); c != nil {
			st, _ = m.runPrattStop(c, []byte("<69>a"), 0, true)
		}
		want := failed
		if allowLast {
			want = matched
		}
		if st != want {
			t.Fatalf("current mask: status %v, want %v", st, want)
		}
	}
	for n := range 256 {
		clear(open)
		for i := range levels - 1 {
			open[i] = (n >> (i % 8) & 1) != 0
		}
		check(true)
		if len(ri.stops) != 1 {
			t.Fatalf("static cache grew with masks: %d", len(ri.stops))
		}
	}
	for i := range open {
		open[i] = true
	}
	if c := g.prattStopCheck(ri, 0, open); c != nil {
		t.Fatal("fully closed none levels still forbid a caller continuation")
	}
	check(false)
}
