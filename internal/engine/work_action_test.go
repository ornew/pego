//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func actionWorkParser(ctx context.Context, limits limits.WorkLimits, unit PositionUnit) *core.Parser {
	return &core.Parser{Input: core.Input{Unit: unit, Control: core.NewWorkControl(ctx, limits)}}
}

func actionWorkNodes(n int) []*value.Node {
	items := make([]*value.Node, n)
	for i := range items {
		items[i] = value.Ptr(value.Data{Start: int32(i), End: int32(i + 1), Terminal: true, Text: "x"})
	}
	return items
}

func TestWorkActionStringHelperParity(t *testing.T) {
	t.Parallel()
	malformed := string([]byte{'a', 0xff, 0xc3, 0x80, 'z'})
	left := strings.Repeat("prefix-", 2048) + "a" + string([]byte{0xff})
	right := strings.Repeat("prefix-", 2048) + "b" + string([]byte{0x00})

	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		p := actionWorkParser(nil, limits.WorkLimits{MaxInputWork: 1}, unit)
		wantLen := len(malformed)
		if unit == CodePoints {
			wantLen = utf8.RuneCountInString(malformed)
		}
		if got := p.ValueTextLen(malformed); got != wantLen {
			t.Errorf("%v valueTextLen(%q) = %d, want %d", unit, malformed, got, wantLen)
		}
		if got, want := p.CompareText(left, right), strings.Compare(left, right); got != want {
			t.Errorf("%v compareText(long equal prefix) = %d, want %d", unit, got, want)
		}
		if got := p.CompareText(malformed, malformed); got != 0 {
			t.Errorf("%v compareText(malformed equal strings) = %d, want 0", unit, got)
		}
		joined, err := p.ValueBinary("+", malformed, string([]byte{0xfe, 'q'}))
		if err != nil {
			t.Fatalf("%v string addition: %v", unit, err)
		}
		if got, want := []byte(joined.(string)), append([]byte(malformed), 0xfe, 'q'); string(got) != string(want) {
			t.Errorf("%v string addition bytes = %v, want %v", unit, got, want)
		}
		if got, err := p.ValueBinary("<", left, right); err != nil || got != true {
			t.Errorf("%v lexical binary comparison = %v, %v; want true", unit, got, err)
		}
		if got, err := p.ValueBinary("+", 20, 22); err != nil || got != 42 {
			t.Errorf("%v integer addition = %v, %v; want 42", unit, got, err)
		}
		if p.Control.InputUsed != 0 || p.Control.StepsUsed == 0 {
			t.Fatalf("scalar work counters: input=%d steps=%d", p.Control.InputUsed, p.Control.StepsUsed)
		}
	}
}

func TestWorkActionStringHelpersChargeLongWork(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 8192)
	p := actionWorkParser(nil, limits.WorkLimits{MaxSteps: 256}, CodePoints)
	if err := catchFatal(t, func() { p.CompareText(long+"x", long+"y") }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("long equal-prefix comparison error = %v, want ErrWorkLimit", err)
	}

	p = actionWorkParser(nil, limits.WorkLimits{MaxSteps: 128}, Bytes)
	if err := catchFatal(t, func() {
		_, _ = p.ValueBinary("+", long, long)
	}); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("long string concatenation error = %v, want ErrWorkLimit", err)
	}

	p = actionWorkParser(nil, limits.WorkLimits{MaxSteps: 128}, CodePoints)
	if err := catchFatal(t, func() { p.ValueTextLen(long) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("long rune length error = %v, want ErrWorkLimit", err)
	}
}

func TestWorkActionCollectionLoopsChargeElements(t *testing.T) {
	t.Parallel()
	const size = 2048
	items := actionWorkNodes(size)
	list := value.Ptr(value.Data{Children: items})
	identity := workActionFunc{params: 1, fn: func(x, _ any) (any, error) { return x, nil }}
	keepLeft := workActionFunc{params: 2, fn: func(x, _ any) (any, error) { return x, nil }}

	cases := []struct {
		name string
		run  func(*core.EvalCtx) error
	}{
		{"map", func(c *core.EvalCtx) error { _, err := c.Builtin("map", []any{list, identity}); return err }},
		{"foldl", func(c *core.EvalCtx) error { _, err := c.Builtin("foldl", []any{nil, list, keepLeft}); return err }},
		{"foldr", func(c *core.EvalCtx) error { _, err := c.Builtin("foldr", []any{nil, list, keepLeft}); return err }},
		{"list", func(c *core.EvalCtx) error {
			args := make([]any, len(items))
			for i, item := range items {
				args[i] = item
			}
			_, err := c.Builtin("list", args)
			return err
		}},
		{"concat", func(c *core.EvalCtx) error { _, err := c.Builtin("concat", []any{list}); return err }},
		{"newList", func(c *core.EvalCtx) error { _ = c.NewList(items); return nil }},
		{"listNode", func(c *core.EvalCtx) error { _ = c.ListNode(items); return nil }},
		{"local copy", func(c *core.EvalCtx) error { _ = c.Keep(); return nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newPollingContext(1)
			p := actionWorkParser(ctx, limits.WorkLimits{}, CodePoints)
			var locals *core.Local
			if tc.name == "local copy" {
				for i := 0; i < size; i++ {
					locals = &core.Local{Name: fmt.Sprintf("v%d", i), Val: i, Next: locals}
				}
			}
			c := &core.EvalCtx{P: p, Locals: locals}
			var returned error
			fatalErr := catchFatal(t, func() { returned = tc.run(c) })
			if returned != nil {
				t.Fatalf("operation returned ordinary error: %v", returned)
			}
			if !errors.Is(fatalErr, context.Canceled) {
				t.Fatalf("operation panic error = %v, want context.Canceled", fatalErr)
			}
		})
	}
}

func TestWorkActionBatchRoutesAndPoolRetry(t *testing.T) {
	t.Parallel()
	routes := []struct {
		name   string
		action string
	}{
		{"map", `map($parts, (x) => x)`},
		{"foldl", `foldl(nil, $parts, (acc, x) => acc)`},
		{"foldr", `foldr(nil, $parts, (acc, x) => acc)`},
		{"concat", `concat($parts)`},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			prog := mustCompile(t, `rule main = parts:items => `+route.action+`
rule items = [a-z] #atomic*`)
			input := strings.Repeat("a", 1024)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{Bytes, CodePoints} {
					t.Run(fmt.Sprintf("%v/%v", backend, unit), func(t *testing.T) {
						armed := false
						n, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) {
							if !armed && e.Kind == TraceExit && e.Rule == "items" {
								armed = true
								core.TraceParser(e).Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 128})
							}
						}})
						if !armed || n != nil || !errors.Is(err, limits.ErrWorkLimit) {
							t.Fatalf("controlled parse: armed=%t node=%v err=%v", armed, n, err)
						}
						n, err = prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit})
						if err != nil {
							t.Fatalf("pooled retry: %v", err)
						}
					})
				}
			}
		})
	}
}

func TestWorkActionBatchCancellationStopsPublication(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = parts:items => map($parts, (x) => x)

rule items = [a-z] #atomic*`)
	input := strings.Repeat("a", 1024)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			t.Run(fmt.Sprintf("%v/%v", backend, unit), func(t *testing.T) {
				armed := false
				var attempt *core.WorkControl
				n, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) {
					if !armed && e.Kind == TraceExit && e.Rule == "items" {
						armed = true
						attempt = core.NewWorkControl(newPollingContext(2), limits.WorkLimits{})
						core.TraceParser(e).Control = attempt
					}
				}})
				if !armed || n != nil || !errors.Is(err, context.Canceled) || attempt.StepsUsed < 128 {
					t.Fatalf("controlled parse: armed=%t node=%v err=%v, want cancellation and no result", armed, n, err)
				}
				n, err = prog.ParseWith("main", "abc", ParseOptions{Backend: backend, Unit: unit})
				if err != nil || n == nil {
					t.Fatalf("pooled retry: node=%v err=%v", n, err)
				}
			})
		}
	}
}

func TestWorkActionShortCircuitAndRecoveryPropagation(t *testing.T) {
	t.Parallel()
	shortCircuit := mustCompile(t, `rule main = words:items {true || text($words) == text($words)} $$

rule items = [a-z]* #atomic`)
	recovery := mustCompile(t, `rule main = (words:items {text($words) == text($words)} "!") #recover(skip=[^;]* ";") $$

rule items = [a-z]* #atomic`)
	input := strings.Repeat("a", 1024) + "!"

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			t.Run(fmt.Sprintf("%v/%v", backend, unit), func(t *testing.T) {
				armContext := func(e TraceEvent, armed *bool) {
					if !*armed && e.Kind == TraceExit && e.Rule == "items" {
						*armed = true
						core.TraceParser(e).Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 128})
					}
				}
				armed := false
				n, err := shortCircuit.ParseWith("main", strings.TrimSuffix(input, "!"), ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) { armContext(e, &armed) }})
				if !armed || err != nil || n == nil {
					t.Fatalf("short-circuit parse: armed=%t node=%v err=%v", armed, n, err)
				}

				armed = false
				n, err = recovery.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) {
					if !armed && e.Kind == TraceExit && e.Rule == "items" {
						armed = true
						core.TraceParser(e).Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 256})
					}
				}})
				if !armed || n != nil || !errors.Is(err, limits.ErrWorkLimit) {
					t.Fatalf("recovery swallowed action interruption: armed=%t node=%v err=%v", armed, n, err)
				}
				n, err = recovery.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit})
				if err != nil || n == nil {
					t.Fatalf("recovery retry: node=%v err=%v", n, err)
				}
			})
		}
	}
}
