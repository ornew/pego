//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/ornew/pego/grammar"
)

const workClassRangeCount = 2048

func workClassRanges(n int) []grammar.CharRange {
	ranges := make([]grammar.CharRange, n)
	for i := range ranges {
		// Keep a one-code-point gap between ranges so normalization cannot merge them.
		r := rune(0x10000 + 2*i)
		ranges[i] = grammar.CharRange{Lo: r, Hi: r}
	}
	return ranges
}

func workClassNode(t *testing.T, p *Program, input string, opts ParseOptions) *Node {
	t.Helper()
	n, err := p.ParseWith("main", input, opts)
	if err != nil || n == nil {
		t.Fatalf("parse %q: node=%v err=%v", input, n, err)
	}
	return n
}

func workClassBudgetOptions(backend Backend, unit PositionUnit, allowance uint64, observed **parser) ParseOptions {
	return ParseOptions{
		Backend:    backend,
		Unit:       unit,
		WorkLimits: WorkLimits{MaxSteps: 1 << 30},
		Trace: func(e TraceEvent) {
			if e.Kind == TraceEnter && e.Rule == "main" {
				if observed != nil {
					*observed = e.p
				}
				e.p.control.limits.MaxSteps = e.p.control.stepsUsed + allowance
			}
		},
	}
}

func requireClassWorkLimit(t *testing.T, p *Program, input string, backend Backend, unit PositionUnit) *parser {
	t.Helper()
	var observed *parser
	n, err := p.ParseWith("main", input, workClassBudgetOptions(backend, unit, 32, &observed))
	if n != nil || !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("%v/%v input %q: node=%v err type=%T, want fatal work-limit error", backend, unit, input, n, err)
	}
	var limit *WorkLimitError
	if !errors.As(err, &limit) || limit.Kind != StepWorkLimit {
		t.Fatalf("%v/%v input %q: error=%T %v, want step WorkLimitError", backend, unit, input, err, err)
	}
	if limit.Requested != 1 || limit.Used != limit.Limit {
		t.Fatalf("range reservation=%+v, want one rejected step at the limit", limit)
	}
	return observed
}

func TestWorkClassLargeNonASCIIRangesAreCharged(t *testing.T) {
	ranges := workClassRanges(workClassRangeCount)
	late := string(ranges[len(ranges)-1].Lo)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	positive := classMembershipProgram(t, &grammar.CharClass{Ranges: ranges})
	negative := classMembershipProgram(t, &grammar.CharClass{Ranges: ranges, Negated: true})

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				if n := workClassNode(t, positive, late, ParseOptions{Backend: backend, Unit: unit}); n.text != late {
					t.Fatalf("late class hit text=%q, want %q", n.text, late)
				}
				if n := workClassNode(t, negative, gap, ParseOptions{Backend: backend, Unit: unit}); n.text != gap {
					t.Fatalf("negated class text=%q, want %q", n.text, gap)
				}
				if n, err := positive.ParseWith("main", gap, ParseOptions{Backend: backend, Unit: unit}); n != nil || err == nil || errors.Is(err, ErrWorkLimit) {
					t.Fatalf("unlimited positive miss: node=%v err=%v", n, err)
				}

				// The entry trace leaves room for rule/dispatch and one input probe.
				// A class with many ranges must exhaust the remainder in membership.
				requireClassWorkLimit(t, positive, late, backend, unit)
				requireClassWorkLimit(t, positive, gap, backend, unit)
				requireClassWorkLimit(t, negative, gap, backend, unit)
			})
		}
	}
}

func TestWorkClassRangeCancellationAndPoolRetry(t *testing.T) {
	ranges := workClassRanges(workClassRangeCount)
	input := string(ranges[len(ranges)-1].Lo)
	p := classMembershipProgram(t, &grammar.CharClass{Ranges: ranges})

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				var observed *parser
				var control *workControl
				var startSteps uint64
				exits := 0
				opts := ParseOptions{
					Backend:    backend,
					Unit:       unit,
					WorkLimits: WorkLimits{MaxSteps: 1 << 30},
					Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							observed, control = e.p, e.p.control
							startSteps = control.stepsUsed
							// Trace emit polls once after this callback; cancel at the next
							// 256-step checkpoint, inside the deliberately long range scan.
							control.ctx = newPollingContext(2)
						} else if e.Kind == TraceExit {
							exits++
						}
					},
				}
				n, err := p.ParseWith("main", input, opts)
				if n != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("node=%v err=%v, want cancellation during class membership", n, err)
				}
				if observed == nil || control == nil || control.stepsUsed-startSteps < 128 {
					t.Fatalf("cancellation occurred before a substantial range scan: parser observed=%v steps=%v", observed != nil, control)
				}
				if exits != 0 {
					t.Fatalf("trace exits after fatal interruption=%d, want 0", exits)
				}
				if observed.scratch != nil || observed.control != nil {
					t.Fatalf("parser retained work state after interruption: scratch=%v control=%v", observed.scratch, observed.control)
				}
				if n := workClassNode(t, p, input, ParseOptions{Backend: backend, Unit: unit}); n.text != input {
					t.Fatalf("retry text=%q, want %q", n.text, input)
				}
			})
		}
	}
}

func TestWorkClassInterruptionBypassesChoiceAndRecovery(t *testing.T) {
	ranges := workClassRanges(workClassRangeCount)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	recoverClass := &grammar.Attributed{
		Expr: &grammar.CharClass{Ranges: ranges},
		Attrs: []*grammar.Attribute{{
			Name:   "recover",
			Pragma: true,
			KwArgs: map[string]any{"skip": &grammar.Repeat{Expr: &grammar.Any{}, Min: 0, Max: -1}},
		}},
	}
	p := classMembershipProgram(t, &grammar.Choice{Alts: []grammar.Expr{
		recoverClass,
		&grammar.Literal{Value: gap},
	}})

	// Without an active control, the class miss is recovered and the grammar
	// completes. The controlled attempt must terminate before recovery/choice.
	n, err := p.ParseWith("main", gap, ParseOptions{})
	if n == nil || errors.Is(err, ErrWorkLimit) {
		t.Fatalf("uncontrolled recovery baseline: node=%v err=%v", n, err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if observed := requireClassWorkLimit(t, p, gap, backend, CodePoints); observed == nil {
			t.Fatalf("%v: trace did not observe start rule", backend)
		}
	}
}

func TestWorkClassFastPathParity(t *testing.T) {
	cases := []struct {
		name  string
		class *grammar.CharClass
		input string
	}{
		{"single-range", &grammar.CharClass{Ranges: []grammar.CharRange{{Lo: 'a', Hi: 'z'}}}, "m"},
		{"two-ranges", &grammar.CharClass{Ranges: []grammar.CharRange{{Lo: 'a', Hi: 'z'}, {Lo: 0x10000, Hi: 0x10000}}}, string(rune(0x10000))},
		{"ascii-bitmap-and-slow-unicode", &grammar.CharClass{Ranges: []grammar.CharRange{{Lo: 'a', Hi: 'z'}, {Lo: 0x10000, Hi: 0x10000}, {Lo: 0x10002, Hi: 0x10002}}}, "m"},
		{"negated-ascii-bitmap", &grammar.CharClass{Negated: true, Ranges: []grammar.CharRange{{Lo: 'a', Hi: 'z'}, {Lo: 0x10000, Hi: 0x10000}, {Lo: 0x10002, Hi: 0x10002}}}, "!"},
	}
	for _, tc := range cases {
		p := classMembershipProgram(t, tc.class)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				t.Run(tc.name+"/"+backend.String()+"/"+unit.String(), func(t *testing.T) {
					base, baseErr := p.ParseWith("main", tc.input, ParseOptions{Backend: backend, Unit: unit})
					controlled, controlledErr := p.ParseWith("main", tc.input, ParseOptions{
						Backend: backend, Unit: unit,
						WorkLimits: WorkLimits{MaxInputWork: 10000, MaxSteps: 10000},
					})
					if !reflect.DeepEqual(controlled, base) || !reflect.DeepEqual(controlledErr, baseErr) {
						t.Fatalf("controlled result differs: got %#v/%#v, want %#v/%#v", controlled, controlledErr, base, baseErr)
					}
				})
			}
		}
	}
}

// Optimized guards can skip the expression matcher; value-free scans bypass
// per-element expression dispatch. Their membership work must still be bounded.
func TestWorkClassOptimizedRoutes(t *testing.T) {
	ranges := workClassRanges(workClassRangeCount)
	late := string(ranges[len(ranges)-1].Lo)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	class := &grammar.CharClass{Ranges: ranges}
	cases := []struct {
		name  string
		expr  grammar.Expr
		input string
		op    Op
	}{
		{"guard-hit", &grammar.Choice{Alts: []grammar.Expr{class, &grammar.Literal{Value: gap}}}, late, OpGuard},
		{"guard-miss-fallback", &grammar.Choice{Alts: []grammar.Expr{class, &grammar.Literal{Value: gap}}}, gap, OpGuard},
		{"scan", &grammar.Atomic{Expr: &grammar.Repeat{Expr: class, Min: 1, Max: -1}}, late, OpScan},
		{"negated-scan", &grammar.Atomic{Expr: &grammar.Repeat{Expr: &grammar.CharClass{Ranges: ranges, Negated: true}, Min: 1, Max: -1}}, gap, OpScan},
	}
	if strconv.IntSize == 64 {
		// Construct a source-sized bound without a constant-to-int overflow on 32bit.
		wide := uint64(1) << 32
		cases = append(cases, struct {
			name  string
			expr  grammar.Expr
			input string
			op    Op
		}{
			"wide-scan", &grammar.Atomic{Expr: &grammar.Repeat{Expr: class, Min: 1, Max: int(wide)}}, late, OpScanWide,
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := classMembershipProgram(t, tc.expr)
			found := false
			for _, in := range p.Module().Code {
				if in.Op == tc.op {
					found = true
				}
			}
			if !found {
				t.Fatalf("optimized instruction %v was not emitted", tc.op)
			}
			// Exercise the same VM paths after loading the module; the checks below trace
			// the parse, which a compiled grammar serves in document mode.
			loaded := saveLoad(t, p, "main", CompiledDocument)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, program := range []*Program{p, loaded} {
						if program == loaded && backend == Closure {
							continue
						}
						workClassNode(t, program, tc.input, servedBy(program.mode, ParseOptions{Backend: backend, Unit: unit}))
						requireClassWorkLimit(t, program, tc.input, backend, unit)
						var c *workControl
						var start uint64
						n, err := program.ParseWith("main", tc.input, ParseOptions{
							Backend: backend, Unit: unit, WorkLimits: WorkLimits{MaxSteps: 1 << 30},
							Trace: func(e TraceEvent) {
								if e.Kind == TraceEnter && e.Rule == "main" {
									c = e.p.control
									start = c.stepsUsed
									c.ctx = newPollingContext(2)
								}
							},
						})
						if n != nil || !errors.Is(err, context.Canceled) || c == nil || c.stepsUsed-start < 128 {
							t.Fatalf("%v/%v: cancellation did not interrupt optimized membership: node=%v error type=%T", backend, unit, n, err)
						}
						workClassNode(t, program, tc.input, servedBy(program.mode, ParseOptions{Backend: backend, Unit: unit}))
					}
				}
			}
		})
	}
}
