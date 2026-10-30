//go:build !pego_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

const budgetClassRangeCount = 2048

func budgetClassRanges(n int) []grammar.CharRange {
	ranges := make([]grammar.CharRange, n)
	for i := range ranges {
		// Keep a one-code-point gap between ranges so normalization cannot merge them.
		r := rune(0x10000 + 2*i)
		ranges[i] = grammar.CharRange{Lo: r, Hi: r}
	}
	return ranges
}

func budgetClassNode(t *testing.T, p *Program, input string, opts ParseOptions) *value.Node {
	t.Helper()
	n, err := p.ParseWith("main", input, opts)
	if err != nil || n == nil {
		t.Fatalf("parse %q: node=%v err=%v", input, n, err)
	}
	return n
}

func budgetClassBudgetOptions(backend Backend, unit PositionUnit, allowance uint64, observed **core.Parser) ParseOptions {
	return ParseOptions{
		Backend: backend,
		Unit:    unit,
		Limits:  limits.Limits{Budget: 1 << 30},
		Trace: func(e TraceEvent) {
			if e.Kind == TraceEnter && e.Rule == "main" {
				if observed != nil {
					*observed = core.TraceParser(e)
				}
				core.TraceParser(e).Control.Limits.Budget = core.TraceParser(e).Control.Used + allowance
			}
		},
	}
}

func requireClassBudgetLimit(t *testing.T, p *Program, input string, backend Backend, unit PositionUnit) *core.Parser {
	t.Helper()
	var observed *core.Parser
	n, err := p.ParseWith("main", input, budgetClassBudgetOptions(backend, unit, 32, &observed))
	if n != nil || !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("%v/%v input %q: node=%v err type=%T, want fatal budget limit error", backend, unit, input, n, err)
	}
	var limit *limits.LimitError
	if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget {
		t.Fatalf("%v/%v input %q: error=%T %v, want spend LimitError", backend, unit, input, err, err)
	}
	if limit.Requested != 1 || limit.Used != limit.Limit {
		t.Fatalf("range reservation=%+v, want one rejected step at the limit", limit)
	}
	return observed
}

func TestBudgetClassLargeNonASCIIRangesAreCharged(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
	late := string(ranges[len(ranges)-1].Lo)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	positive := classMembershipProgram(t, &grammar.CharClass{Ranges: ranges})
	negative := classMembershipProgram(t, &grammar.CharClass{Ranges: ranges, Negated: true})

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				if n := budgetClassNode(t, positive, late, ParseOptions{Backend: backend, Unit: unit}); value.D(n).Text != late {
					t.Fatalf("late class hit text=%q, want %q", value.D(n).Text, late)
				}
				if n := budgetClassNode(t, negative, gap, ParseOptions{Backend: backend, Unit: unit}); value.D(n).Text != gap {
					t.Fatalf("negated class text=%q, want %q", value.D(n).Text, gap)
				}
				if n, err := positive.ParseWith("main", gap, ParseOptions{Backend: backend, Unit: unit}); n != nil || err == nil || errors.Is(err, limits.ErrLimit) {
					t.Fatalf("unlimited positive miss: node=%v err=%v", n, err)
				}

				// The entry trace leaves room for rule/dispatch and one input probe.
				// A class with many ranges must exhaust the remainder in membership.
				requireClassBudgetLimit(t, positive, late, backend, unit)
				requireClassBudgetLimit(t, positive, gap, backend, unit)
				requireClassBudgetLimit(t, negative, gap, backend, unit)
			})
		}
	}
}

func TestBudgetClassRangeCancellationAndPoolRetry(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
	input := string(ranges[len(ranges)-1].Lo)
	p := classMembershipProgram(t, &grammar.CharClass{Ranges: ranges})

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				var observed *core.Parser
				var control *core.BudgetControl
				var startSteps uint64
				exits := 0
				opts := ParseOptions{
					Backend: backend,
					Unit:    unit,
					Limits:  limits.Limits{Budget: 1 << 30},
					Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							observed, control = core.TraceParser(e), core.TraceParser(e).Control
							startSteps = control.Used
							// Trace emit polls once after this callback; cancel at the next
							// 256-step checkpoint, inside the deliberately long range scan.
							control.Ctx = newPollingContext(2)
						} else if e.Kind == TraceExit {
							exits++
						}
					},
				}
				n, err := p.ParseWith("main", input, opts)
				if n != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("node=%v err=%v, want cancellation during class membership", n, err)
				}
				if observed == nil || control == nil || control.Used-startSteps < 128 {
					t.Fatalf("cancellation occurred before a substantial range scan: parser observed=%v steps=%v", observed != nil, control)
				}
				if exits != 0 {
					t.Fatalf("trace exits after fatal interruption=%d, want 0", exits)
				}
				if observed.Scratch != nil || observed.Control != nil {
					t.Fatalf("parser retained spend state after interruption: scratch=%v control=%v", observed.Scratch, observed.Control)
				}
				if n := budgetClassNode(t, p, input, ParseOptions{Backend: backend, Unit: unit}); value.D(n).Text != input {
					t.Fatalf("retry text=%q, want %q", value.D(n).Text, input)
				}
			})
		}
	}
}

func TestBudgetClassInterruptionBypassesChoiceAndRecovery(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
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
	if n == nil || errors.Is(err, limits.ErrLimit) {
		t.Fatalf("uncontrolled recovery baseline: node=%v err=%v", n, err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if observed := requireClassBudgetLimit(t, p, gap, backend, CodePoints); observed == nil {
			t.Fatalf("%v: trace did not observe start rule", backend)
		}
	}
}

func TestBudgetClassFastPathParity(t *testing.T) {
	t.Parallel()
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
						Limits: limits.Limits{Budget: 10000},
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
// per-element expression dispatch. Their membership spend must still be bounded.
func TestBudgetClassOptimizedRoutes(t *testing.T) {
	t.Parallel()
	ranges := budgetClassRanges(budgetClassRangeCount)
	late := string(ranges[len(ranges)-1].Lo)
	gap := string(ranges[len(ranges)-1].Lo + 1)
	class := &grammar.CharClass{Ranges: ranges}
	cases := []struct {
		name  string
		expr  grammar.Expr
		input string
		op    bytecode.Op
	}{
		{"guard-hit", &grammar.Choice{Alts: []grammar.Expr{class, &grammar.Literal{Value: gap}}}, late, bytecode.OpGuard},
		{"guard-miss-fallback", &grammar.Choice{Alts: []grammar.Expr{class, &grammar.Literal{Value: gap}}}, gap, bytecode.OpGuard},
		{"scan", &grammar.Atomic{Expr: &grammar.Repeat{Expr: class, Min: 1, Max: -1}}, late, bytecode.OpScan},
		{"negated-scan", &grammar.Atomic{Expr: &grammar.Repeat{Expr: &grammar.CharClass{Ranges: ranges, Negated: true}, Min: 1, Max: -1}}, gap, bytecode.OpScan},
	}
	if strconv.IntSize == 64 {
		// Construct a source-sized bound without a constant-to-int overflow on 32bit.
		wide := uint64(1) << 32
		cases = append(cases, struct {
			name  string
			expr  grammar.Expr
			input string
			op    bytecode.Op
		}{
			"wide-scan", &grammar.Atomic{Expr: &grammar.Repeat{Expr: class, Min: 1, Max: int(wide)}}, late, bytecode.OpScanWide,
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
						budgetClassNode(t, program, tc.input, servedBy(program.mode, ParseOptions{Backend: backend, Unit: unit}))
						requireClassBudgetLimit(t, program, tc.input, backend, unit)
						var c *core.BudgetControl
						var start uint64
						n, err := program.ParseWith("main", tc.input, ParseOptions{
							Backend: backend, Unit: unit, Limits: limits.Limits{Budget: 1 << 30},
							Trace: func(e TraceEvent) {
								if e.Kind == TraceEnter && e.Rule == "main" {
									c = core.TraceParser(e).Control
									start = c.Used
									c.Ctx = newPollingContext(2)
								}
							},
						})
						if n != nil || !errors.Is(err, context.Canceled) || c == nil || c.Used-start < 128 {
							t.Fatalf("%v/%v: cancellation did not interrupt optimized membership: node=%v error type=%T", backend, unit, n, err)
						}
						budgetClassNode(t, program, tc.input, servedBy(program.mode, ParseOptions{Backend: backend, Unit: unit}))
					}
				}
			}
		})
	}
}
