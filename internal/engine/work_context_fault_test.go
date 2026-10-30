//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func TestWorkContextAttributionPreservesVMFaults(t *testing.T) {
	t.Parallel()
	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			prog := mustCompile(t, `rule main = "z" $$`)
			if _, err := prog.rule(backend, "main"); err != nil {
				t.Fatal(err)
			}
			// Corrupt the prepared VM, bypassing the saved-module validator, to
			// exercise its execution recovery rather than a caller's panic.
			restore := corruptVMLiteral(t, prog)
			var observed *core.Parser
			n, err := prog.ParseWith("main", "z", ParseOptions{
				Backend: backend, Limits: limits.Limits{MaxWork: 1 << 20},
				Trace: func(e TraceEvent) { observed = core.TraceParser(e) },
			})
			if n != nil || err == nil || !strings.HasPrefix(err.Error(), "invalid bytecode: ") {
				t.Fatalf("VM fault node=%v error=%v", n, err)
			}
			if observed == nil || !observed.Aborted || observed.Scratch != nil || observed.Control != nil {
				t.Fatalf("VM fault cleanup parser=%v", observed)
			}
			restore()
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
	t.Parallel()
	text := strings.Repeat("a", 1024)
	prog := mustCompile(t, `rule main = "`+text+`" $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				ctx := &workContextAccessorContext{Context: context.Background(), done: make(chan struct{})}
				var observed *core.Parser
				accessorEntered := false
				n, err := prog.ParseWith("main", text, ParseOptions{
					Backend: backend, Unit: unit, Context: ctx,
					Trace: func(e TraceEvent) {
						if e.Kind != TraceEnter || e.Rule != "main" {
							return
						}
						observed = core.TraceParser(e)
						core.TraceParser(e).Control.Limits.MaxWork = core.TraceParser(e).Control.Used + 1
						ctx.fn = func() error {
							// The accessor's own reservation may poll the context.
							ctx.fn = nil
							accessorEntered = true
							_, _ = e.LineCol(len(text))
							return nil
						}
						// Keep the accessor within the trace callback's lifetime.
						core.TraceParser(e).Control.Check()
					},
				})
				var limit *limits.LimitError
				if n != nil || !accessorEntered || !errors.As(err, &limit) || limit.Kind != limits.LimitWork {
					t.Fatalf("context accessor node=%v entered=%v error=%v", n, accessorEntered, err)
				}
				if observed == nil || observed.Scratch != nil || observed.Control != nil {
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
	t.Parallel()
	text := strings.Repeat("a", 1024)
	prog := mustCompile(t, `rule main = "`+text+`" $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				token := &struct{ label string }{"nested context panic"}
				ctx := &workContextAccessorContext{Context: context.Background(), done: make(chan struct{})}
				var observed *core.Parser
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
							observed = core.TraceParser(e)
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
							core.TraceParser(e).Control.Check()
						},
					})
				}()
				if got != token || !accessorEntered || !nestedPoll {
					t.Fatalf("nested context panic=%v accessor=%v poll=%v", got, accessorEntered, nestedPoll)
				}
				if observed == nil || observed.Scratch != nil || observed.Control != nil {
					t.Fatalf("nested context panic cleanup parser=%v", observed)
				}
				if n, err := prog.ParseWith("main", text, ParseOptions{Backend: backend, Unit: unit}); n == nil || err != nil {
					t.Fatalf("nested context panic retry node=%v error=%v", n, err)
				}
			})
		}
	}
}
