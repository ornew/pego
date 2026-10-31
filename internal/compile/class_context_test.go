package compile

import (
	"context"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

func TestFirstTerminalContextFacts(t *testing.T) {
	t.Parallel()
	leaf := &grammar.Literal{Value: "a"}
	a := &Rule{Rec: true, Def: &grammar.RuleDef{Name: "a", Expr: &grammar.Ref{Name: "b"}}}
	b := &Rule{Rec: true, Def: &grammar.RuleDef{Name: "b", Expr: &grammar.Capture{Name: "x", Expr: &grammar.Atomic{Expr: &grammar.Discard{Expr: &grammar.Seq{Items: []grammar.Expr{leaf}}}}}}}
	p := &Program{ByName: map[string]*Rule{"a": a, "b": b}}
	for _, control := range []*Control{nil, {Ctx: context.Background()}} {
		got, depth := FirstTerminalControlled(p, &grammar.Ref{Name: "a"}, 0, map[string]bool{}, control)
		if got != leaf || depth != 2 {
			t.Fatalf("terminal=%v depth=%d", got, depth)
		}
		for _, expr := range []grammar.Expr{&grammar.Literal{}, &grammar.Ref{Name: "missing"}, &grammar.Optional{Expr: leaf}} {
			if got, depth := FirstTerminalControlled(p, expr, 0, map[string]bool{}, control); got != nil || depth != 0 {
				t.Fatal("guard crossed an unsupported boundary")
			}
		}
	}
	// Only calls of recursive rules count a level.
	a.Rec, b.Rec = false, false
	for _, control := range []*Control{nil, {Ctx: context.Background()}} {
		if got, depth := FirstTerminalControlled(p, &grammar.Ref{Name: "a"}, 0, map[string]bool{}, control); got != leaf || depth != 0 {
			t.Fatalf("terminal=%v depth=%d without recursive rules", got, depth)
		}
	}
	a.Rec, b.Rec = true, true
	b.Def.Expr = &grammar.Ref{Name: "a"}
	if got, depth := FirstTerminalControlled(p, &grammar.Ref{Name: "a"}, 0, map[string]bool{}, &Control{Ctx: context.Background()}); got != nil || depth != 0 {
		t.Fatal("cyclic guard did not stop")
	}
}

func TestClassBitmapPollingBoundaries(t *testing.T) {
	t.Parallel()
	for _, rs := range [][]grammar.CharRange{
		contextClassRanges(),
		{{Lo: 0, Hi: 31}, {Lo: 'a', Hi: 'z'}, {Lo: 127, Hi: 127}},
	} {
		for _, start := range []uint32{0, PollStride - 1, ^uint32(0) - 1} {
			for _, neg := range []bool{false, true} {
				// Count a reference scan one visit at a time, including early matches.
				var visits uint64
				var want [2]uint64
				for r := rune(0); r < 128; r++ {
					matched := false
					for _, rg := range rs {
						visits++
						if rg.Lo <= r && r <= rg.Hi {
							matched = true
							break
						}
					}
					if matched != neg {
						want[r>>6] |= 1 << (r & 63)
					}
				}
				ctx := compileTestContext(t, 1<<20)
				c := &Control{Ctx: ctx, Steps: start}
				got := ClassASCII(rs, neg, c)
				checks := (uint64(start%PollStride) + visits) / PollStride
				if got != want || c.Steps != start+uint32(visits) || (1<<20)-ctx.left != int(checks) {
					t.Fatalf("start=%d neg=%t steps=%d checks=%d wantchecks=%d", start, neg, c.Steps, (1<<20)-ctx.left, checks)
				}
			}
		}
	}
}
