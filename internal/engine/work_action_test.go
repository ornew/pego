//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

type workActionFunc struct {
	params int
	fn     func(any, any) (any, error)
}

func (f workActionFunc) arity() int { return f.params }
func (f workActionFunc) apply(x, y any) (any, error) {
	return f.fn(x, y)
}

func actionWorkParser(ctx context.Context, limits WorkLimits, unit Unit) *parser {
	return &parser{input: input{unit: unit, control: newWorkControl(ctx, limits)}}
}

func actionWorkNodes(n int) []*Node {
	items := make([]*Node, n)
	for i := range items {
		items[i] = &Node{Start: int32(i), End: int32(i + 1), terminal: true, Text: "x"}
	}
	return items
}

func TestWorkActionStringHelperParity(t *testing.T) {
	malformed := string([]byte{'a', 0xff, 0xc3, 0x80, 'z'})
	left := strings.Repeat("prefix-", 2048) + "a" + string([]byte{0xff})
	right := strings.Repeat("prefix-", 2048) + "b" + string([]byte{0x00})

	for _, unit := range []Unit{Bytes, CodePoints} {
		p := actionWorkParser(nil, WorkLimits{MaxInputWork: 1}, unit)
		wantLen := len(malformed)
		if unit == CodePoints {
			wantLen = utf8.RuneCountInString(malformed)
		}
		if got := p.valueTextLen(malformed); got != wantLen {
			t.Errorf("%v valueTextLen(%q) = %d, want %d", unit, malformed, got, wantLen)
		}
		if got, want := p.compareText(left, right), strings.Compare(left, right); got != want {
			t.Errorf("%v compareText(long equal prefix) = %d, want %d", unit, got, want)
		}
		if got := p.compareText(malformed, malformed); got != 0 {
			t.Errorf("%v compareText(malformed equal strings) = %d, want 0", unit, got)
		}
		joined, err := p.valueBinary("+", malformed, string([]byte{0xfe, 'q'}))
		if err != nil {
			t.Fatalf("%v string addition: %v", unit, err)
		}
		if got, want := []byte(joined.(string)), append([]byte(malformed), 0xfe, 'q'); string(got) != string(want) {
			t.Errorf("%v string addition bytes = %v, want %v", unit, got, want)
		}
		if got, err := p.valueBinary("<", left, right); err != nil || got != true {
			t.Errorf("%v lexical binary comparison = %v, %v; want true", unit, got, err)
		}
		if got, err := p.valueBinary("+", 20, 22); err != nil || got != 42 {
			t.Errorf("%v integer addition = %v, %v; want 42", unit, got, err)
		}
		if p.control.inputUsed != 0 || p.control.stepsUsed == 0 {
			t.Fatalf("scalar work counters: input=%d steps=%d", p.control.inputUsed, p.control.stepsUsed)
		}
	}
}

func TestWorkActionStringHelpersChargeLongWork(t *testing.T) {
	long := strings.Repeat("a", 8192)
	p := actionWorkParser(nil, WorkLimits{MaxSteps: 256}, CodePoints)
	if err := catchFatal(t, func() { p.compareText(long+"x", long+"y") }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("long equal-prefix comparison error = %v, want ErrWorkLimit", err)
	}

	p = actionWorkParser(nil, WorkLimits{MaxSteps: 128}, Bytes)
	if err := catchFatal(t, func() {
		_, _ = p.valueBinary("+", long, long)
	}); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("long string concatenation error = %v, want ErrWorkLimit", err)
	}

	p = actionWorkParser(nil, WorkLimits{MaxSteps: 128}, CodePoints)
	if err := catchFatal(t, func() { p.valueTextLen(long) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("long rune length error = %v, want ErrWorkLimit", err)
	}
}

func TestWorkActionCollectionLoopsChargeElements(t *testing.T) {
	const size = 2048
	items := actionWorkNodes(size)
	list := &Node{Children: items}
	identity := workActionFunc{params: 1, fn: func(x, _ any) (any, error) { return x, nil }}
	keepLeft := workActionFunc{params: 2, fn: func(x, _ any) (any, error) { return x, nil }}

	cases := []struct {
		name string
		run  func(*evalCtx) error
	}{
		{"map", func(c *evalCtx) error { _, err := c.builtin("map", []any{list, identity}); return err }},
		{"foldl", func(c *evalCtx) error { _, err := c.builtin("foldl", []any{nil, list, keepLeft}); return err }},
		{"foldr", func(c *evalCtx) error { _, err := c.builtin("foldr", []any{nil, list, keepLeft}); return err }},
		{"list", func(c *evalCtx) error {
			args := make([]any, len(items))
			for i, item := range items {
				args[i] = item
			}
			_, err := c.builtin("list", args)
			return err
		}},
		{"concat", func(c *evalCtx) error { _, err := c.builtin("concat", []any{list}); return err }},
		{"newList", func(c *evalCtx) error { _ = c.newList(items); return nil }},
		{"listNode", func(c *evalCtx) error { _ = c.listNode(items); return nil }},
		{"local copy", func(c *evalCtx) error { _ = c.keep(); return nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newPollingContext(1)
			p := actionWorkParser(ctx, WorkLimits{}, CodePoints)
			var locals *local
			if tc.name == "local copy" {
				for i := 0; i < size; i++ {
					locals = &local{name: fmt.Sprintf("v%d", i), val: i, next: locals}
				}
			}
			c := &evalCtx{p: p, locals: locals}
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
	routes := []struct {
		name   string
		action string
	}{
		{"map", `map($parts, (x) => $x)`},
		{"foldl", `foldl(nil, $parts, (acc, x) => $acc)`},
		{"foldr", `foldr(nil, $parts, (acc, x) => $acc)`},
		{"concat", `concat($parts)`},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			prog := compile(t, `rule main = parts:items => `+route.action+`
rule items = [a-z] #atomic*`)
			input := strings.Repeat("a", 1024)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []Unit{Bytes, CodePoints} {
					t.Run(fmt.Sprintf("%v/%v", backend, unit), func(t *testing.T) {
						armed := false
						n, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) {
							if !armed && e.Kind == TraceExit && e.Rule == "items" {
								armed = true
								e.p.control = newWorkControl(nil, WorkLimits{MaxSteps: 128})
							}
						}})
						if !armed || n != nil || !errors.Is(err, ErrWorkLimit) {
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
	prog := compile(t, `rule main = parts:items => map($parts, (x) => $x)

rule items = [a-z] #atomic*`)
	input := strings.Repeat("a", 1024)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{Bytes, CodePoints} {
			t.Run(fmt.Sprintf("%v/%v", backend, unit), func(t *testing.T) {
				armed := false
				var attempt *workControl
				n, err := prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) {
					if !armed && e.Kind == TraceExit && e.Rule == "items" {
						armed = true
						attempt = newWorkControl(newPollingContext(2), WorkLimits{})
						e.p.control = attempt
					}
				}})
				if !armed || n != nil || !errors.Is(err, context.Canceled) || attempt.stepsUsed < 128 {
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
	shortCircuit := compile(t, `rule main = words:items {true || text($words) == text($words)} $$

rule items = [a-z]* #atomic`)
	recovery := compile(t, `rule main = (words:items {text($words) == text($words)} "!") #recover(skip=[^;]* ";") $$

rule items = [a-z]* #atomic`)
	input := strings.Repeat("a", 1024) + "!"

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{Bytes, CodePoints} {
			t.Run(fmt.Sprintf("%v/%v", backend, unit), func(t *testing.T) {
				armContext := func(e TraceEvent, armed *bool) {
					if !*armed && e.Kind == TraceExit && e.Rule == "items" {
						*armed = true
						e.p.control = newWorkControl(nil, WorkLimits{MaxSteps: 128})
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
						e.p.control = newWorkControl(nil, WorkLimits{MaxSteps: 256})
					}
				}})
				if !armed || n != nil || !errors.Is(err, ErrWorkLimit) {
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
