package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"weak"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/starttest"
)

func contextClassRanges() []grammar.CharRange {
	rs := make([]grammar.CharRange, 4096)
	for i := range rs {
		rs[i] = grammar.CharRange{Lo: rune(256 + i*2), Hi: rune(256 + i*2)}
	}
	return rs
}

func TestClassConstructionContextCancellation(t *testing.T) {
	cl := &grammar.CharClass{Ranges: contextClassRanges()}
	before := append([]grammar.CharRange(nil), cl.Ranges...)
	var deep grammar.Expr = cl
	for range 1024 {
		deep = &grammar.Atomic{Expr: deep}
	}
	for _, phase := range []struct {
		name string
		run  func(*compile.Control)
	}{
		{"descriptor", func(c *compile.Control) { compile.CharClassStringControlled(cl, c) }},
		{"copy", func(c *compile.Control) { compile.CopyClassRanges(cl.Ranges, c) }},
		{"bitmap", func(c *compile.Control) { compile.ClassASCII(cl.Ranges, false, c) }},
		{"accept-construction", func(c *compile.Control) { classAcceptControlled(cl, c) }},
		{"matcher", func(c *compile.Control) {
			newClosureWalker(&compile.Compiler{Control: c}, false).ExprForm(cl, compile.NewScope(), true)
		}},
		{"choice-guard", func(c *compile.Control) { (&closureBuilder{control: c}).guard(compile.First{Term: cl}) }},
		{"scan-repeat", func(c *compile.Control) {
			(&closureBuilder{control: c}).ScanRepeat(&grammar.Repeat{Expr: cl, Max: -1}, 0)
		}},
		{"guard-extraction", func(c *compile.Control) {
			compile.FirstTerminalControlled(&compile.Program{}, deep, 0, map[string]bool{}, c)
		}},
	} {
		t.Run(phase.name, func(t *testing.T) {
			c := &compile.Control{Ctx: compileTestContext(t, 1)}
			defer func() {
				failure := recover()
				stop, ok := failure.(compile.Cancellation)
				if !ok || !errors.Is(stop.Err, context.Canceled) {
					t.Fatalf("class phase did not cancel internally: %v", failure)
				}
				if c.Steps != compile.PollStride {
					t.Fatalf("poll outside class traversal: %d", c.Steps)
				}
				if !reflect.DeepEqual(before, cl.Ranges) {
					t.Fatal("cancellation mutated ranges")
				}
			}()
			phase.run(c)
		})
	}
}

func TestClassConstructionContextFacts(t *testing.T) {
	cases := [][]grammar.CharRange{nil, {{Lo: 'a', Hi: 'z'}}, {{Lo: 'a', Hi: 'z'}, {Lo: 'β', Hi: 'β'}}, {{Lo: 'a', Hi: 'f'}, {Lo: '0', Hi: '9'}, {Lo: 'β', Hi: 'β'}, {Lo: '😀', Hi: '😀'}}}
	for i, rs := range cases {
		for _, neg := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", i, neg), func(t *testing.T) {
				cl := &grammar.CharClass{Ranges: rs, Negated: neg}
				c := &compile.Control{Ctx: context.Background()}
				if got := compile.CharClassStringControlled(cl, c); got != compile.CharClassString(cl) {
					t.Fatalf("descriptor %s", got)
				}
				ordinary, controlled := classAccept(cl), classAcceptControlled(cl, c)
				runes := []rune{'β', '😀', 0x10ffff}
				for r := rune(0); r < 256; r++ {
					runes = append(runes, r)
				}
				for _, r := range runes {
					for _, active := range []bool{false, true} {
						p, q := &parser{}, &parser{}
						if active {
							p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
							q.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 1 << 30})
						}
						expected := false
						for _, rg := range rs {
							expected = expected || rg.Lo <= r && r <= rg.Hi
						}
						expected = expected != neg
						if ordinary(p, r) != expected || controlled(q, r) != expected {
							t.Fatalf("membership rune %U active=%t", r, active)
						}
						if active && p.control.stepsUsed != q.control.stepsUsed {
							t.Fatal("runtime work charge changed")
						}
					}
				}
			})
		}
	}
	cl := &grammar.CharClass{Ranges: []grammar.CharRange{{Lo: '-', Hi: '-'}, {Lo: '[', Hi: '['}, {Lo: ']', Hi: ']'}, {Lo: '^', Hi: '^'}, {Lo: '\\', Hi: '\\'}}}
	if got := compile.CharClassStringControlled(cl, &compile.Control{Ctx: context.Background()}); got != `[\-\[\]\^\\]` {
		t.Fatalf("escaped descriptor %q", got)
	}
	rs := contextClassRanges()
	cl = &grammar.CharClass{Ranges: rs}
	accept := classAcceptControlled(cl, &compile.Control{Ctx: context.Background()})
	rs[0] = grammar.CharRange{Lo: 'a', Hi: 'a'}
	if !accept(&parser{}, 256) || accept(&parser{}, 'a') {
		t.Fatal("matcher aliases source range slice")
	}
}

func TestClassConstructionCompileContextParity(t *testing.T) {
	for _, shape := range []string{"single", "choice", "scan"} {
		cl := &grammar.CharClass{Ranges: []grammar.CharRange{{Lo: 'a', Hi: 'f'}, {Lo: '0', Hi: '9'}, {Lo: 'β', Hi: 'β'}}}
		var e grammar.Expr = cl
		if shape == "choice" {
			e = &grammar.Choice{Alts: []grammar.Expr{cl, &grammar.Literal{Value: "z"}}}
		}
		if shape == "scan" {
			e = &grammar.Discard{Expr: &grammar.Repeat{Expr: cl, Min: 1, Max: -1}}
		}
		g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: e}}}}
		starttest.MarkAll(g)
		for _, compiledRecognition := range []bool{false, true} {
			a, err := Compile(g, compile.Options{Recognize: compiledRecognition})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			b, err := CompileContext(ctx, g, compile.Options{Recognize: compiledRecognition})
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range []PositionUnit{Bytes, CodePoints} {
				for _, recognize := range []bool{false, true} {
					for _, active := range []bool{false, true} {
						for _, s := range []string{"a", "β", "z", "?", "", "aβ1"} {
							// Active work controls and Recognize lazily compile separate graphs.
							// Their disabled cases exercise the controlled compiler's own matchers,
							// including its directly compiled recognition-only graph.
							opts := ParseOptions{Unit: unit, Recognize: recognize}
							if active {
								opts.WorkLimits = limits.WorkLimits{MaxSteps: 1 << 30, MaxInputWork: 1 << 30}
							}
							x, xe := a.ParseWith("main", s, opts)
							y, ye := b.ParseWith("main", s, opts)
							xj, xerr := json.Marshal(x)
							yj, yerr := json.Marshal(y)
							if xerr != nil || yerr != nil {
								t.Fatalf("node JSON: %v/%v", xerr, yerr)
							}
							if string(xj) != string(yj) || fmt.Sprint(xe) != fmt.Sprint(ye) {
								t.Fatalf("%s/compiledRecognition=%t/%v/recognize=%t/active=%t/%q mismatch: %s/%v vs %s/%v", shape, compiledRecognition, unit, recognize, active, s, xj, xe, yj, ye)
							}
						}
					}
				}
			}
		}
	}
}

type classCompilationContext struct {
	context.Context
	done chan struct{}
	pad  [128]byte
}

func (c *classCompilationContext) Done() <-chan struct{} { return c.done }

//go:noinline
func classProgramWithoutContext(t *testing.T) (*Program, weak.Pointer[classCompilationContext]) {
	ctx := &classCompilationContext{Context: context.Background(), done: make(chan struct{})}
	ref := weak.Make(ctx)
	cl := &grammar.CharClass{Ranges: []grammar.CharRange{{Lo: 'a', Hi: 'a'}, {Lo: 'b', Hi: 'b'}, {Lo: 'β', Hi: 'β'}}}
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Choice{Alts: []grammar.Expr{&grammar.Seq{Items: []grammar.Expr{cl, &grammar.EndInput{}}}, &grammar.Discard{Expr: &grammar.Repeat{Expr: cl, Min: 1, Max: -1}}}}}}}}
	starttest.MarkAll(g)
	p, err := CompileContext(ctx, g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if ref.Value() != ctx {
		t.Fatal("weak context positive control failed")
	}
	runtime.KeepAlive(ctx)
	return p, ref
}

func TestClassConstructionProgramReleasesCompileContext(t *testing.T) {
	p, ref := classProgramWithoutContext(t)
	released := false
	for range 5 {
		runtime.GC()
		if ref.Value() == nil {
			released = true
			break
		}
	}
	if !released {
		t.Fatal("compiled class closures retain compile context")
	}
	for _, s := range []string{"a", "β", "bbβ"} {
		if _, err := p.Parse("main", s); err != nil {
			t.Fatal(err)
		}
	}
	runtime.KeepAlive(p)
}

func TestFirstTerminalContextFacts(t *testing.T) {
	leaf := &grammar.Literal{Value: "a"}
	a := &compile.Rule{Def: &grammar.RuleDef{Name: "a", Expr: &grammar.Ref{Name: "b"}}}
	b := &compile.Rule{Def: &grammar.RuleDef{Name: "b", Expr: &grammar.Capture{Name: "x", Expr: &grammar.Atomic{Expr: &grammar.Discard{Expr: &grammar.Seq{Items: []grammar.Expr{leaf}}}}}}}
	p := &compile.Program{ByName: map[string]*compile.Rule{"a": a, "b": b}}
	for _, control := range []*compile.Control{nil, {Ctx: context.Background()}} {
		got, depth := compile.FirstTerminalControlled(p, &grammar.Ref{Name: "a"}, 0, map[string]bool{}, control)
		if got != leaf || depth != 2 {
			t.Fatalf("terminal=%v depth=%d", got, depth)
		}
		for _, expr := range []grammar.Expr{&grammar.Literal{}, &grammar.Ref{Name: "missing"}, &grammar.Optional{Expr: leaf}} {
			if got, depth := compile.FirstTerminalControlled(p, expr, 0, map[string]bool{}, control); got != nil || depth != 0 {
				t.Fatal("guard crossed an unsupported boundary")
			}
		}
	}
	b.Def.Expr = &grammar.Ref{Name: "a"}
	if got, depth := compile.FirstTerminalControlled(p, &grammar.Ref{Name: "a"}, 0, map[string]bool{}, &compile.Control{Ctx: context.Background()}); got != nil || depth != 0 {
		t.Fatal("cyclic guard did not stop")
	}
}

func TestClassBitmapPollingBoundaries(t *testing.T) {
	for _, rs := range [][]grammar.CharRange{
		contextClassRanges(),
		{{Lo: 0, Hi: 31}, {Lo: 'a', Hi: 'z'}, {Lo: 127, Hi: 127}},
	} {
		for _, start := range []uint32{0, compile.PollStride - 1, ^uint32(0) - 1} {
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
				c := &compile.Control{Ctx: ctx, Steps: start}
				got := compile.ClassASCII(rs, neg, c)
				checks := (uint64(start%compile.PollStride) + visits) / compile.PollStride
				if got != want || c.Steps != start+uint32(visits) || (1<<20)-ctx.left != int(checks) {
					t.Fatalf("start=%d neg=%t steps=%d checks=%d wantchecks=%d", start, neg, c.Steps, (1<<20)-ctx.left, checks)
				}
			}
		}
	}
}
