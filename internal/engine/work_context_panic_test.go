//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

type workContextPanicToken struct{ phase string }

func (*workContextPanicToken) Error() string { return "context panic token" }
func (*workContextPanicToken) RuntimeError() {}

var _ runtime.Error = (*workContextPanicToken)(nil)

type workContextErrPanic struct {
	context.Context
	done         chan struct{}
	armed        bool
	panicAt      int
	calls        int
	token        *workContextPanicToken
	control      *core.WorkControl
	stepsAtPanic uint64
	stepsSince   uint64
	inputAtPanic uint64
	inputSince   uint64
}

func newWorkContextErrPanic(token *workContextPanicToken, panicAt int) *workContextErrPanic {
	return &workContextErrPanic{Context: context.Background(), done: make(chan struct{}), token: token, panicAt: panicAt}
}

func (c *workContextErrPanic) Done() <-chan struct{} { return c.done }

func (c *workContextErrPanic) Err() error {
	if !c.armed {
		return nil
	}
	c.calls++
	if c.calls == c.panicAt {
		if c.control != nil {
			c.stepsAtPanic, c.stepsSince = c.control.StepsUsed, c.control.StepsSinceCheck
			c.inputAtPanic, c.inputSince = c.control.InputUsed, c.control.InputSinceCheck
		}
		panic(c.token)
	}
	return nil
}

type workContextDonePanic struct {
	context.Context
	token *workContextPanicToken
}

func (c *workContextDonePanic) Done() <-chan struct{} { panic(c.token) }

func workContextRecover(run func()) (got any) {
	defer func() { got = recover() }()
	run()
	return nil
}

func workContextAssertCleanup(t *testing.T, p *core.Parser) {
	t.Helper()
	if p == nil || p.Scratch != nil || p.Control != nil {
		t.Fatalf("parser not released after caller panic: %#v", p)
	}
}

func workContextNodeJSON(t *testing.T, n *value.Node) []byte {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func workContextVariants() []ParseOptions {
	var variants []ParseOptions
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				variants = append(variants, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
			}
		}
	}
	return variants
}

func workContextVariantName(opts ParseOptions) string {
	return opts.Backend.String() + "/" + opts.Unit.String() + "/recognize=" + strconv.FormatBool(opts.Recognize)
}

func TestWorkContextErrPanicIdentityBeforeExecution(t *testing.T) {
	text := strings.Repeat("a", 8192)
	prog := mustCompile(t, `rule main = "`+text+`" $$`)
	for _, base := range workContextVariants() {
		for _, phase := range []string{"admission", "later-check", "Done"} {
			t.Run(workContextVariantName(base)+"/"+phase, func(t *testing.T) {
				token := &workContextPanicToken{phase: phase}
				opts := base
				ctx := newWorkContextErrPanic(token, 1)
				ctx.armed = true
				opts.Context = ctx
				if phase == "later-check" {
					ctx.panicAt = 3
				}
				if phase == "Done" {
					opts.Context = &workContextDonePanic{Context: context.Background(), token: token}
				}
				entries := 0
				opts.Trace = func(e TraceEvent) {
					if e.Kind == TraceEnter {
						entries++
					}
				}
				var node *value.Node
				var returnedErr error
				got := workContextRecover(func() { node, returnedErr = prog.ParseWith("main", text, opts) })
				if got != token || node != nil || returnedErr != nil || entries != 0 {
					t.Fatalf("panic=%#v node=%v error=%v trace entries=%d", got, node, returnedErr, entries)
				}
				if phase != "Done" && ctx.calls != ctx.panicAt {
					t.Fatalf("Err calls=%d want %d at %s", ctx.calls, ctx.panicAt, phase)
				}
				// This fixture checks identity before execution, without assigning
				// an ordinal Context call to a specific lazy-setup component.
				// Phase predicates and direct input tests isolate those boundaries.
				retry, err := prog.ParseWith("main", text, base)
				if err != nil || base.Recognize && retry != nil || !base.Recognize && retry == nil {
					t.Fatalf("setup/preparation retry node=%v error=%v", retry, err)
				}
			})
		}
	}
}

func TestWorkContextErrPanicIdentityAtTraceBoundaries(t *testing.T) {
	childProg := mustCompile(t, `rule main = child $$

rule child = "a"`)
	plainProg := mustCompile(t, `rule main = "a" $$`)
	for _, base := range workContextVariants() {
		backend := base.Backend
		t.Run(workContextVariantName(base)+"/child precheck", func(t *testing.T) {
			token := &workContextPanicToken{phase: "child trace precheck"}
			ctx := newWorkContextErrPanic(token, 2)
			var observed *core.Parser
			childCallbacks := 0
			var node *value.Node
			var returnedErr error
			got := workContextRecover(func() {
				node, returnedErr = childProg.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceEnter && e.Rule == "main" {
						observed = core.TraceParser(e)
						ctx.control, ctx.armed = core.TraceParser(e).Control, true
					}
					if e.Kind == TraceEnter && e.Rule == "child" {
						childCallbacks++
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.calls != 2 || childCallbacks != 0 {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d child callback=%d, want panic at child's pre-callback check", got, node, returnedErr, ctx.calls, childCallbacks)
			}
			workContextAssertCleanup(t, observed)
		})

		t.Run(workContextVariantName(base)+"/callback postcheck", func(t *testing.T) {
			token := &workContextPanicToken{phase: "trace postcheck"}
			ctx := newWorkContextErrPanic(token, 1)
			var observed *core.Parser
			var node *value.Node
			var returnedErr error
			got := workContextRecover(func() {
				node, returnedErr = plainProg.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceEnter && e.Rule == "main" {
						observed = core.TraceParser(e)
						ctx.control, ctx.armed = core.TraceParser(e).Control, true
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.calls != 1 {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d, want exact panic at callback postcheck", got, node, returnedErr, ctx.calls)
			}
			workContextAssertCleanup(t, observed)
		})
	}
}

func TestWorkContextErrPanicIdentityAtPublicationAndFailedReservation(t *testing.T) {
	prog := mustCompile(t, `rule main = "a" $$`)
	for _, base := range workContextVariants() {
		backend := base.Backend
		t.Run(workContextVariantName(base)+"/publication", func(t *testing.T) {
			token := &workContextPanicToken{phase: "publication"}
			ctx := newWorkContextErrPanic(token, 2)
			var observed *core.Parser
			var node *value.Node
			var returnedErr error
			got := workContextRecover(func() {
				node, returnedErr = prog.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceExit && e.Rule == "main" {
						observed = core.TraceParser(e)
						ctx.control, ctx.armed = core.TraceParser(e).Control, true
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.calls != 2 {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d, want final publication panic", got, node, returnedErr, ctx.calls)
			}
			workContextAssertCleanup(t, observed)
		})

		t.Run(workContextVariantName(base)+"/failed reservation", func(t *testing.T) {
			token := &workContextPanicToken{phase: "failed reservation"}
			ctx := newWorkContextErrPanic(token, 2)
			var observed *core.Parser
			var node *value.Node
			var returnedErr error
			got := workContextRecover(func() {
				node, returnedErr = prog.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: base.Unit, Recognize: base.Recognize, Context: ctx, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 20}, Trace: func(e TraceEvent) {
					if e.Kind == TraceEnter && e.Rule == "main" {
						observed = core.TraceParser(e)
						ctx.control = core.TraceParser(e).Control
						ctx.armed = true
						ctx.control.Limits.MaxSteps = ctx.control.StepsUsed
					}
				}})
			})
			if got != token || node != nil || returnedErr != nil || ctx.calls != 2 || ctx.control == nil || ctx.stepsAtPanic != ctx.control.StepsUsed || ctx.stepsAtPanic != ctx.control.Limits.MaxSteps {
				t.Fatalf("panic=%#v node=%v error=%v Err calls=%d control=%v, want context panic on rejected reservation", got, node, returnedErr, ctx.calls, ctx.control)
			}
			workContextAssertCleanup(t, observed)
		})
	}
}

func TestWorkContextErrPanicIdentityAtCountedWorkAndRetry(t *testing.T) {
	literal := strings.Repeat("a", 1024)
	prog := mustCompile(t, `rule main = "`+literal+`" $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize), func(t *testing.T) {
					var retained *value.Node
					var retainedJSON []byte
					if !recognize {
						var err error
						retained, err = prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || retained == nil {
							t.Fatalf("baseline parse node=%v err=%v", retained, err)
						}
						retainedJSON = workContextNodeJSON(t, retained)
					}

					token := &workContextPanicToken{phase: "counted work"}
					ctx := newWorkContextErrPanic(token, 2)
					var observed *core.Parser
					var node *value.Node
					var returnedErr error
					got := workContextRecover(func() {
						node, returnedErr = prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, Context: ctx, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 30}, Trace: func(e TraceEvent) {
							if e.Kind == TraceEnter && e.Rule == "main" {
								observed = core.TraceParser(e)
								ctx.control, ctx.armed = core.TraceParser(e).Control, true
							}
						}})
					})
					if got != token || node != nil || returnedErr != nil {
						t.Fatalf("panic=%#v node=%v error=%v, want exact panic during counted work", got, node, returnedErr)
					}
					if ctx.calls != 2 || ctx.stepsSince != core.WorkCheckStepInterval-1 || ctx.stepsAtPanic < core.WorkCheckStepInterval-1 {
						t.Fatalf("Err calls=%d steps=%d since check=%d, want callback postcheck then next counted-work poll", ctx.calls, ctx.stepsAtPanic, ctx.stepsSince)
					}
					workContextAssertCleanup(t, observed)

					if !recognize {
						if string(workContextNodeJSON(t, retained)) != string(retainedJSON) {
							t.Fatal("caller panic mutated a previously retained Node")
						}
						retry, err := prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || string(workContextNodeJSON(t, retry)) != string(retainedJSON) || string(workContextNodeJSON(t, retained)) != string(retainedJSON) {
							t.Fatalf("panic/retry changed result or retained Node: node=%v err=%v", retry, err)
						}
					} else if n, err := prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit, Recognize: true}); n != nil || err != nil {
						t.Fatalf("recognition retry node=%v err=%v", n, err)
					}
				})
			}
		}
	}
}

func TestWorkContextErrPanicIdentityLoadedASTOmittedProgram(t *testing.T) {
	literal := strings.Repeat("a", 1024)
	source := mustCompile(t, `rule main = "`+literal+`" $$`)
	// The parse below is traced, which a compiled grammar serves in document mode.
	prog := saveLoad(t, source, "main", CompiledDocument)
	if prog.Grammar != nil {
		t.Fatal("loaded fixture retained an AST")
	}

	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				retained, err := prog.ParseWith("main", literal, servedBy(CompiledDocument, ParseOptions{Backend: backend, Unit: unit}))
				if err != nil || retained == nil {
					t.Fatalf("baseline node=%v err=%v", retained, err)
				}
				before := workContextNodeJSON(t, retained)

				token := &workContextPanicToken{phase: "loaded counted work"}
				ctx := newWorkContextErrPanic(token, 2)
				var observed *core.Parser
				var node *value.Node
				var returnedErr error
				got := workContextRecover(func() {
					node, returnedErr = prog.ParseWith("main", literal, ParseOptions{Backend: backend, Unit: unit, Context: ctx, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 30}, Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							observed = core.TraceParser(e)
							ctx.control, ctx.armed = core.TraceParser(e).Control, true
						}
					}})
				})
				if got != token || node != nil || returnedErr != nil || ctx.calls != 2 {
					t.Fatalf("panic=%#v node=%v error=%v Err calls=%d, want exact loaded-VM panic", got, node, returnedErr, ctx.calls)
				}
				if ctx.stepsSince != core.WorkCheckStepInterval-1 || ctx.stepsAtPanic < core.WorkCheckStepInterval-1 {
					t.Fatalf("steps at panic=%d since check=%d, want counted-work polling boundary", ctx.stepsAtPanic, ctx.stepsSince)
				}
				workContextAssertCleanup(t, observed)

				retry, retryErr := prog.ParseWith("main", literal, servedBy(CompiledDocument, ParseOptions{Backend: backend, Unit: unit}))
				if retryErr != nil || string(workContextNodeJSON(t, retry)) != string(before) || string(workContextNodeJSON(t, retained)) != string(before) {
					t.Fatalf("loaded retry node=%v err=%v; retained=%v", retry, retryErr, retained)
				}
			})
		}
	}
}
