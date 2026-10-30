//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

type streamControlRetentionSentinel struct{ data [128]byte }

//go:noinline
func streamControlRetentionTouch(s *streamControlRetentionSentinel) byte { return s.data[0] }

type streamControlRetentionContext struct {
	context.Context
	done      chan struct{}
	state     *streamControlRetentionSentinel
	armed     bool
	cancelled bool
	token     any
}

func (c *streamControlRetentionContext) Done() <-chan struct{} { return c.done }
func (c *streamControlRetentionContext) Err() error {
	if streamControlRetentionTouch(c.state) != 42 {
		panic("context sentinel missing")
	}
	if c.armed {
		panic(c.token)
	}
	if c.cancelled {
		return context.Canceled
	}
	return nil
}

type streamControlRetentionRun struct {
	event        TraceEvent
	program      *Program
	control      weak.Pointer[core.WorkControl]
	context      weak.Pointer[streamControlRetentionContext]
	reader       weak.Pointer[streamCallbackReader]
	emitState    weak.Pointer[streamControlRetentionSentinel]
	nodes        []*value.Node
	snapshots    []string
	err          error
	wantErr      error
	panic        any
	panicToken   any
	readerClosed bool
}

//go:noinline
func runStreamControlRetentionCase(t *testing.T, prog *Program, outcome string, options ParseOptions) streamControlRetentionRun {
	t.Helper()
	const input = "#records\na=1\nbc=22\n"
	stop := errors.New("caller stopped")
	contextState := &streamControlRetentionSentinel{}
	contextState.data[0] = 42
	ctx := &streamControlRetentionContext{Context: context.Background(), done: make(chan struct{}), state: contextState}
	reader := &streamCallbackReader{text: input}
	callbackState := &streamControlRetentionSentinel{}
	callbackState.data[0] = 42
	ctxRef, readerRef, callbackRef := weak.Make(ctx), weak.Make(reader), weak.Make(callbackState)
	if ctxRef.Value() == nil || readerRef.Value() == nil || callbackRef.Value() == nil {
		t.Fatal("weak-reference positive control failed")
	}
	runtime.GC()
	if ctxRef.Value() == nil || readerRef.Value() == nil || callbackRef.Value() == nil {
		t.Fatal("weak-reference positive control did not survive GC with strong locals")
	}

	if outcome == "reader-error" {
		reader.text, reader.err = "#records\na=1\n", stop
	} else if outcome == "syntax" {
		reader.text = "#records\na=1\nbad=x\n"
	}
	if outcome == "emit-panic" || outcome == "trace-panic" || outcome == "context-panic" {
		ctx.token = &streamRuntimePanicToken{phase: outcome}
	}
	panicToken := ctx.token
	var controlRef weak.Pointer[core.WorkControl]
	var event TraceEvent
	var nodes []*value.Node
	var snapshots []string
	var returnedErr error
	gotPanic := catchStreamPanic(func() {
		returnedErr = prog.ParseStreamWith("main", reader, func(n *value.Node) error {
			if streamControlRetentionTouch(callbackState) != 42 {
				panic("emit sentinel missing")
			}
			nodes = append(nodes, n)
			snapshots = append(snapshots, gentest.ResultJSON(n, nil))
			switch outcome {
			case "emit-error":
				return stop
			case "emit-panic":
				panic(ctx.token)
			case "canceled":
				ctx.cancelled = true
			case "context-panic":
				ctx.armed = true // The post-emit checkpoint must rethrow this caller panic.
			}
			return nil
		}, streamControlRetentionOptions(options, ctx, func(e TraceEvent) {
			if e.Kind == TraceEnter && e.Rule == "main" {
				event = e
				controlRef = weak.Make(core.TraceParser(e).Control)
				if controlRef.Value() == nil {
					t.Fatal("missing active controller")
				}
			}
			switch {
			case outcome == "trace-panic" && e.Kind == TraceEnter && e.Rule == "record" && e.Pos > 9:
				panic(ctx.token)
			case outcome == "input-limit" && e.Kind == TraceExit && e.Rule == "record" && e.Matched && core.TraceParser(e).Control != nil:
				if core.TraceParser(e).Control.Used == 0 {
					panic("input limit fixture did not consume input")
				}
				// The first record is emitted (its emission needs a few units
				// after this event); the next record's reservations must fail.
				core.TraceParser(e).Control.Limits.MaxWork = core.TraceParser(e).Control.Used + 4
			}
		}))
	})

	// Keep only the saved public values and weak references alive after this frame.
	runtime.KeepAlive(ctx)
	runtime.KeepAlive(reader)
	runtime.KeepAlive(callbackState)
	return streamControlRetentionRun{
		event: event, program: prog, control: controlRef, context: ctxRef, reader: readerRef,
		emitState: callbackRef, nodes: nodes, snapshots: snapshots,
		err: returnedErr, wantErr: stop, panic: gotPanic, panicToken: panicToken,
		readerClosed: reader.closed,
	}
}

func streamControlRetentionOptions(o ParseOptions, ctx context.Context, trace func(TraceEvent)) ParseOptions {
	o.Context = ctx
	o.Limits = limits.Limits{MaxWork: 1 << 30}
	o.Trace = trace
	return o
}

func TestWorkStreamTraceRetentionWithActiveControls(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	workStreamVariants(t, func(t *testing.T, options ParseOptions) {
		for _, outcome := range []string{
			"success", "syntax", "input-limit", "canceled", "reader-error", "emit-error",
			"trace-panic", "emit-panic", "context-panic",
		} {
			t.Run(outcome, func(t *testing.T) {
				run := runStreamControlRetentionCase(t, prog, outcome, options)
				switch outcome {
				case "success":
					if run.panic != nil || run.err != nil || len(run.nodes) != 2 {
						t.Fatalf("panic=%v err=%v emitted=%d", run.panic, run.err, len(run.nodes))
					}
				case "syntax":
					var syntaxErr *SyntaxError
					if run.panic != nil || !errors.As(run.err, &syntaxErr) || len(run.nodes) != 1 {
						t.Fatalf("panic=%v err=%v emitted=%d", run.panic, run.err, len(run.nodes))
					}
				case "canceled":
					if run.panic != nil || run.err != context.Canceled || len(run.nodes) != 1 {
						t.Fatalf("panic=%v err=%v emitted=%d", run.panic, run.err, len(run.nodes))
					}
				case "input-limit":
					if run.panic != nil || !errors.Is(run.err, limits.ErrLimit) || len(run.nodes) != 1 {
						t.Fatalf("panic=%v err=%v emitted=%d", run.panic, run.err, len(run.nodes))
					}
				case "reader-error", "emit-error":
					if run.panic != nil || !errors.Is(run.err, run.wantErr) || len(run.nodes) != 1 {
						t.Fatalf("panic=%v err=%v emitted=%d", run.panic, run.err, len(run.nodes))
					}
				case "trace-panic", "emit-panic", "context-panic":
					if run.panic != run.panicToken || run.err != nil || len(run.nodes) != 1 {
						t.Fatalf("panic=%v err=%v emitted=%d", run.panic, run.err, len(run.nodes))
					}
				}
				if run.event.Kind != TraceEnter || run.event.Rule != "main" || run.event.Depth != 1 || run.event.Pos != 0 {
					t.Fatalf("saved trace metadata changed: %+v", run.event)
				}
				if len(run.nodes) != len(run.snapshots) || len(run.nodes) == 0 {
					t.Fatalf("emitted nodes/snapshots = %d/%d", len(run.nodes), len(run.snapshots))
				}
				for i, n := range run.nodes {
					if got := gentest.ResultJSON(n, nil); got != run.snapshots[i] {
						t.Fatalf("emitted node %d changed before GC", i)
					}
				}
				assertTraceParserDetached(t, core.TraceParser(run.event), false) // Stream executions do not retain the Document abort flag.
				if run.readerClosed {
					t.Fatal("parser closed caller-owned reader")
				}

				for range 5 {
					runtime.GC()
				}
				if run.control.Value() != nil {
					t.Error("saved event retains invocation controller")
				}
				if run.context.Value() != nil {
					t.Error("saved trace event retains the active work context")
				}
				if run.reader.Value() != nil {
					t.Error("saved trace event retains the caller reader")
				}
				if run.emitState.Value() != nil {
					t.Error("saved trace event retains the emit callback capture")
				}
				if run.event.Kind != TraceEnter || run.event.Rule != "main" || run.event.Depth != 1 || run.event.Pos != 0 {
					t.Fatal("GC changed saved trace metadata")
				}
				for i, n := range run.nodes {
					if got := gentest.ResultJSON(n, nil); got != run.snapshots[i] {
						t.Fatalf("GC changed emitted node %d", i)
					}
				}
				retryCount := 0
				if err := run.program.ParseStreamWith("main", strings.NewReader("#records\nxy=3\n"), func(*value.Node) error {
					retryCount++
					return nil
				}, ParseOptions{}); err != nil || retryCount != 1 {
					t.Fatalf("program retry err=%v emitted=%d", err, retryCount)
				}
				for i, n := range run.nodes {
					if got := gentest.ResultJSON(n, nil); got != run.snapshots[i] {
						t.Fatalf("later parse changed emitted node %d", i)
					}
				}
				// Retain both public owners through collection; neither should own stream callbacks.
				runtime.KeepAlive(run.event)
				runtime.KeepAlive(run.program)
			})
		}
	})
}
