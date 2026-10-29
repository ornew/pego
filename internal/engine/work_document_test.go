//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/vm"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func workDocumentVariants(t *testing.T, run func(*testing.T, ParseOptions)) {
	t.Helper()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", b, u), func(t *testing.T) { run(t, ParseOptions{Backend: b, Unit: u}) })
		}
	}
}

func workDocumentAssertAborted(t *testing.T, d *Document) {
	t.Helper()
	if d.memo.Len() != 0 || d.runs != nil || d.kids != nil || d.machine != nil || d.in.Control != nil {
		t.Fatal("interruption retained reusable state or invocation control")
	}
}

func workDocumentAssertPreserved(t *testing.T, d *Document, before Document) {
	t.Helper()
	if d.memo != before.memo || fmt.Sprintf("%p", d.runs) != fmt.Sprintf("%p", before.runs) ||
		fmt.Sprintf("%p", d.kids) != fmt.Sprintf("%p", before.kids) ||
		fmt.Sprintf("%p", documentVMValues(d)) != fmt.Sprintf("%p", documentVMValues(&before)) || !reflect.DeepEqual(*d, before) {
		t.Fatal("pre-execution interruption changed Document state or retained storage")
	}
}

func workDocumentAssertRetry(t *testing.T, d *Document) {
	t.Helper()
	o := d.ParseDefaults()
	o.Context, o.WorkLimits, o.Trace = nil, limits.WorkLimits{}, nil
	if got, want := resultJSON(d.ParseWith(o)), resultJSON(d.runner.(*Program).ParseWith(d.start, d.Text(), o)); got != want {
		t.Fatalf("retry %s; fresh %s", got, want)
	}
}

func TestWorkDocumentConstruction(t *testing.T) {
	prog := mustCompile(t, `rule main = .* $$`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for _, phase := range []string{"canceled", "input", "steps"} {
			o := base
			switch phase {
			case "canceled":
				o.Context = ctx
			case "input":
				o.WorkLimits.MaxInputWork = 1
			case "steps":
				if core.DocumentPieceInput && base.Unit == Bytes {
					continue
				}
				o.WorkLimits.MaxSteps = 1
			}
			text := "é💡abc"
			if phase == "steps" {
				text = strings.Repeat("a", 4097)
			}
			d, err := prog.NewDocumentWith("main", text, o)
			if d != nil || err == nil {
				t.Fatalf("%s: document=%v error=%v", phase, d, err)
			}
			if phase == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if phase != "canceled" && !errors.Is(err, limits.ErrWorkLimit) {
				t.Fatal(err)
			}
		}
		o := base
		o.WorkLimits = limits.WorkLimits{MaxInputWork: 100000, MaxSteps: 100000}
		d, err := prog.NewDocumentWith("main", "é💡abc", o)
		if err != nil || d == nil {
			t.Fatalf("fresh construction: %v", err)
		}
		if d.in.Control != nil {
			t.Fatal("construction control retained")
		}
		var last *core.WorkControl
		o.Trace = func(e TraceEvent) {
			if e.Kind == TraceEnter && e.Rule == "main" {
				if core.TraceParser(e).Control == nil || core.TraceParser(e).Control == last || core.TraceParser(e).Control.InputUsed != 0 {
					t.Fatal("parse inherited construction or previous counters")
				}
				last = core.TraceParser(e).Control
			}
		}
		for i := 0; i < 2; i++ {
			if n, err := d.ParseWith(o); n == nil || err != nil {
				t.Fatalf("parse %d: %v", i, err)
			}
		}
	})
}

func TestWorkDocumentAdmittedAbortAndRetry(t *testing.T) {
	prog := mustCompile(t, `rule main = item+ $$

rule item = "a"`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		for _, phase := range []string{"setup", "steps", "enter", "exit"} {
			t.Run(phase, func(t *testing.T) {
				d, err := prog.NewDocumentWith("main", "aaa", base)
				if err != nil {
					t.Fatal(err)
				}
				before, err := d.Parse()
				if err != nil {
					t.Fatal(err)
				}
				clone := before.Clone()
				oldMemo := d.memo
				state := *d
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				o := base
				o.Context = ctx
				var saved *core.Parser
				if phase == "setup" {
					cancel()
				}
				if phase == "steps" {
					o.WorkLimits.MaxSteps = 1
				}
				if phase == "enter" || phase == "exit" {
					o.Trace = func(e TraceEvent) {
						saved = core.TraceParser(e)
						if e.Rule == "main" && ((phase == "enter" && e.Kind == TraceEnter) || (phase == "exit" && e.Kind == TraceExit)) {
							cancel()
						}
					}
				}
				n, err := d.ParseWith(o)
				want := error(context.Canceled)
				if phase == "steps" {
					want = limits.ErrWorkLimit
				}
				if n != nil || !errors.Is(err, want) {
					t.Fatalf("node=%v error=%v", n, err)
				}
				if phase == "setup" {
					workDocumentAssertPreserved(t, d, state)
				} else {
					workDocumentAssertAborted(t, d)
					if d.memo == oldMemo {
						t.Fatal("execution abort did not replace memo")
					}
				}
				if d.Text() != "aaa" {
					t.Fatal("interruption changed text")
				}
				if saved != nil && (saved.Control != nil || saved.Tr != nil || saved.Prog != nil) {
					t.Fatal("saved trace retained parser references")
				}
				workDocumentAssertRetry(t, d)
				if got, want := resultJSON(clone, nil), resultJSON(before.Clone(), nil); got != want {
					t.Fatalf("historical clone changed: %s != %s", got, want)
				}
			})
		}
	})
}

func TestWorkDocumentCallerPanicAtBoundaries(t *testing.T) {
	prog := mustCompile(t, `rule main = "a" $$`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		last := 3
		if prog.backend(base.Backend) == Closure {
			last += 2 // Checks around lazy closure preparation precede execution.
		}
		for at := 0; at <= last; at++ {
			t.Run(fmt.Sprint(at), func(t *testing.T) {
				d, err := prog.NewDocumentWith("main", "a", base)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := d.Parse(); err != nil {
					t.Fatal(err)
				}
				state := *d
				token := &workContextPanicToken{phase: fmt.Sprint(at)}
				o := base
				ctx := newWorkContextErrPanic(token, at)
				ctx.armed = true
				o.Context = ctx
				if at == 0 {
					o.Context = &workContextDonePanic{Context: context.Background(), token: token}
				}
				var n *value.Node
				got := workContextRecover(func() { n, err = d.ParseWith(o) })
				if got != token || n != nil {
					t.Fatalf("panic=%v node=%v", got, n)
				}
				if at > 0 && ctx.calls != at {
					t.Fatalf("checkpoint calls=%d want %d", ctx.calls, at)
				}
				if at < last {
					workDocumentAssertPreserved(t, d, state)
				} else {
					workDocumentAssertAborted(t, d)
				}
				workDocumentAssertRetry(t, d)
			})
		}
	})
}

func TestWorkDocumentModeRejectionPreservesState(t *testing.T) {
	prog := mustCompile(t, `rule main = "a" $$`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		d, err := prog.NewDocumentWith("main", "a", base)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Parse(); err != nil {
			t.Fatal(err)
		}
		memo, stats := d.memo, d.Stats()
		for _, mode := range []string{"unit", "backend", "recognize"} {
			o := base
			o.Context = &workContextDonePanic{Context: context.Background(), token: &workContextPanicToken{phase: "must not consult"}}
			switch mode {
			case "unit":
				if o.Unit == Bytes {
					o.Unit = CodePoints
				} else {
					o.Unit = Bytes
				}
			case "backend":
				if o.Backend == Closure {
					o.Backend = Bytecode
				} else {
					o.Backend = Closure
				}
			case "recognize":
				o.Recognize = true
			}
			if n, err := d.ParseWith(o); n != nil || err == nil {
				t.Fatalf("%s: node=%v error=%v", mode, n, err)
			}
			if d.memo != memo || d.Stats() != stats {
				t.Fatal("mode rejection changed state")
			}
		}
		if base.Backend == Closure {
			o := base
			o.Backend = Default
			if n, err := d.ParseWith(o); n == nil || err != nil {
				t.Fatalf("same effective backend: %v", err)
			}
		}
	})
}

func TestWorkDocumentConstructorCallerPanicIdentity(t *testing.T) {
	prog := mustCompile(t, `rule main = .* $$`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		for _, at := range []int{0, 1, 2} {
			token := &workContextPanicToken{phase: fmt.Sprint(at)}
			o := base
			ctx := newWorkContextErrPanic(token, at)
			ctx.armed = true
			o.Context = ctx
			if at == 0 {
				o.Context = &workContextDonePanic{Context: context.Background(), token: token}
			}
			var d *Document
			var err error
			got := workContextRecover(func() { d, err = prog.NewDocumentWith("main", "é💡abc", o) })
			if got != token || d != nil || err != nil {
				t.Fatalf("checkpoint %d: panic=%v document=%v error=%v", at, got, d, err)
			}
			if at > 0 && ctx.calls != at {
				t.Fatalf("calls=%d want %d", ctx.calls, at)
			}
		}
	})
}

type workDocumentInactiveContext struct{ context.Context }

func (workDocumentInactiveContext) Err() error { panic("inactive context must not be polled") }

func TestWorkDocumentInactiveContext(t *testing.T) {
	prog := mustCompile(t, `rule main = "a" $$`)
	workDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		o.Context = workDocumentInactiveContext{Context: context.Background()}
		d, err := prog.NewDocumentWith("main", "a", o)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if n, err := d.Parse(); n == nil || err != nil {
				t.Fatalf("inactive parse: node=%v error=%v", n, err)
			}
		}
	})
}

func TestWorkDocumentPreservesVMFaultAttribution(t *testing.T) {
	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			prog := mustCompile(t, `rule main = "a" $$`)
			d, err := prog.NewDocumentWith("main", "a", ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			corruptVMLiteral(t, prog)
			n, err := d.Parse()
			if n != nil || err == nil || !strings.HasPrefix(err.Error(), "invalid bytecode:") {
				t.Fatalf("%s/%s: node=%v error=%v", backend, unit, n, err)
			}
			workDocumentAssertAborted(t, d)
		}
	}
}

func TestWorkDocumentPreRunPreservesOversizedValues(t *testing.T) {
	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			prog := mustCompile(t, `rule main = "a" $$`)
			d, err := prog.NewDocumentWith("main", "a", ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			// A large parse followed by a short replacement can leave this
			// cleared storage for the next parse's capacity policy to trim.
			d.machine = vm.NewMachine(make([]any, 0, 2048))
			before := *d
			o := d.ParseDefaults()
			o.Context = newPollingContext(2) // admission succeeds; pre-run cancels
			n, err := d.ParseWith(o)
			if n != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("node=%v error=%v", n, err)
			}
			workDocumentAssertPreserved(t, d, before)
			for i, v := range documentVMValues(d)[:cap(documentVMValues(d))] {
				if v != nil {
					t.Fatalf("scratch slot %d changed", i)
				}
			}
			workDocumentAssertRetry(t, d)
		}
	}
}
