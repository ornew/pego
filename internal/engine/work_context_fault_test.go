//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWorkContextAttributionPreservesVMFaults(t *testing.T) {
	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			prog := compile(t, `rule main = "z" $$`)
			if _, err := prog.rule(backend, "main"); err != nil {
				t.Fatal(err)
			}
			vm := prog.vmFor(backend)
			ip := -1
			for i, in := range vm.m.Code {
				if in.Op == OpStr {
					ip = i
					break
				}
			}
			if ip < 0 {
				t.Fatal("fixture did not emit a literal instruction")
			}
			// Corrupt the prepared VM, bypassing the saved-module validator, to
			// exercise its execution recovery rather than a caller's panic.
			original := vm.m.Code[ip]
			vm.m.Code[ip].A = int32(len(vm.runes))
			var observed *parser
			n, err := prog.ParseWith("main", "z", ParseOptions{
				Backend: backend, WorkLimits: WorkLimits{MaxSteps: 1 << 20},
				Trace: func(e TraceEvent) { observed = e.p },
			})
			if n != nil || err == nil || !strings.HasPrefix(err.Error(), "invalid bytecode: ") {
				t.Fatalf("VM fault node=%v error=%v", n, err)
			}
			if observed == nil || !observed.aborted || observed.scratch != nil || observed.control != nil {
				t.Fatalf("VM fault cleanup parser=%v", observed)
			}
			vm.m.Code[ip] = original
			if n, err := prog.ParseWith("main", "z", ParseOptions{Backend: backend}); n == nil || err != nil {
				t.Fatalf("restored VM retry node=%v error=%v", n, err)
			}
		})
	}
}

type workContextAccessorContext struct {
	context.Context
	done chan struct{}
	fn   func() error
}

func (c *workContextAccessorContext) Done() <-chan struct{} { return c.done }
func (c *workContextAccessorContext) Err() error {
	if c.fn != nil {
		return c.fn()
	}
	return nil
}

func TestWorkContextAccessorInterruptionRemainsFatal(t *testing.T) {
	text := strings.Repeat("a", 1024)
	prog := compile(t, `rule main = "`+text+`" $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{Bytes, CodePoints} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				ctx := &workContextAccessorContext{Context: context.Background(), done: make(chan struct{})}
				var observed *parser
				accessorEntered := false
				n, err := prog.ParseWith("main", text, ParseOptions{
					Backend: backend, Unit: unit, Context: ctx,
					Trace: func(e TraceEvent) {
						if e.Kind != TraceEnter || e.Rule != "main" {
							return
						}
						observed = e.p
						e.p.control.limits.MaxSteps = e.p.control.stepsUsed + 1
						ctx.fn = func() error {
							// The accessor's own reservation may poll the context.
							ctx.fn = nil
							accessorEntered = true
							_, _ = e.LineCol(len(text))
							return nil
						}
						// Keep the accessor within the trace callback's lifetime.
						e.p.control.check()
					},
				})
				var limit *WorkLimitError
				if n != nil || !accessorEntered || !errors.As(err, &limit) || limit.Kind != StepWorkLimit {
					t.Fatalf("context accessor node=%v entered=%v error=%v", n, accessorEntered, err)
				}
				if observed == nil || observed.scratch != nil || observed.control != nil {
					t.Fatalf("context accessor cleanup parser=%v", observed)
				}
				if n, err := prog.ParseWith("main", text, ParseOptions{Backend: backend, Unit: unit}); n == nil || err != nil {
					t.Fatalf("context accessor retry node=%v error=%v", n, err)
				}
			})
		}
	}
}

func TestWorkContextNestedAccessorPanicIdentity(t *testing.T) {
	text := strings.Repeat("a", 1024)
	prog := compile(t, `rule main = "`+text+`" $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{Bytes, CodePoints} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				token := &struct{ label string }{"nested context panic"}
				ctx := &workContextAccessorContext{Context: context.Background(), done: make(chan struct{})}
				var observed *parser
				var got any
				accessorEntered, nestedPoll := false, false
				func() {
					defer func() { got = recover() }()
					_, _ = prog.ParseWith("main", text, ParseOptions{
						Backend: backend, Unit: unit, Context: ctx,
						Trace: func(e TraceEvent) {
							if e.Kind != TraceEnter || e.Rule != "main" {
								return
							}
							observed = e.p
							ctx.fn = func() error {
								if accessorEntered {
									nestedPoll = true
									panic(token)
								}
								accessorEntered = true
								_, _ = e.LineCol(len(text))
								return nil
							}
							// The accessor remains inside the trace callback.
							e.p.control.check()
						},
					})
				}()
				if got != token || !accessorEntered || !nestedPoll {
					t.Fatalf("nested context panic=%v accessor=%v poll=%v", got, accessorEntered, nestedPoll)
				}
				if observed == nil || observed.scratch != nil || observed.control != nil {
					t.Fatalf("nested context panic cleanup parser=%v", observed)
				}
				if n, err := prog.ParseWith("main", text, ParseOptions{Backend: backend, Unit: unit}); n == nil || err != nil {
					t.Fatalf("nested context panic retry node=%v error=%v", n, err)
				}
			})
		}
	}
}
