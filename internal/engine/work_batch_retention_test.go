//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func batchRetentionParser(t *testing.T, e TraceEvent) *parser {
	t.Helper()
	if e.p == nil {
		t.Fatal("trace callback did not retain parser")
	}
	return e.p
}

func batchRetentionJSON(t *testing.T, n *value.Node) []byte {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWorkBatchTraceEventDetachesParserOnSuccessAndSyntaxFailure(t *testing.T) {
	prog := compile(t, `type Entry = {
    Key   string
    Value node[string]
    Count int
}

rule main = "!" entry "?" $$

rule entry = key:[a-z]+ #atomic ":" value:[0-9]+ #atomic => Entry{Key: text($key), Value: $value, Count: len(text($value))}`)

	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				for _, active := range []bool{false, true} {
					name := backend.String() + "/" + unit.String() + "/recognize=" + map[bool]string{false: "false", true: "true"}[recognize] + "/controls=" + map[bool]string{false: "inactive", true: "active"}[active]
					t.Run(name, func(t *testing.T) {
						if active && !workControlsEnabled {
							t.Skip("active work controls are unavailable in the reference build")
						}
						parse := func(input string, wantFailure bool) (*value.Node, error, TraceEvent) {
							var retained TraceEvent
							opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, Trace: func(e TraceEvent) {
								if e.Kind == TraceEnter && e.Rule == "main" {
									retained = e
								}
								if e.Kind != TraceExit || e.Rule != "main" {
									return
								}
								if line, col := e.LineCol(0); line != 1 || col != 1 {
									t.Fatalf("LineCol(0) in callback = %d,%d", line, col)
								}
								if wantFailure {
									if e.Matched || e.Failure() == nil || e.Text() != "" {
										t.Fatalf("failure exit matched=%t failure=%v text=%q", e.Matched, e.Failure(), e.Text())
									}
								} else if !e.Matched || e.Text() != input {
									t.Fatalf("success exit matched=%t text=%q, want %q", e.Matched, e.Text(), input)
								}
							}}
							if active {
								opts.WorkLimits = limits.WorkLimits{MaxSteps: 1 << 30}
							}
							n, err := prog.ParseWith("main", input, opts)
							return n, err, retained
						}

						good := "!alpha:123?"
						node, err, event := parse(good, false)
						if err != nil || recognize && node != nil || !recognize && node == nil {
							t.Fatalf("successful parse node=%v err=%v", node, err)
						}
						var before []byte
						if node != nil {
							if node.Type() != "tuple" || len(value.D(node).Children) != 3 || value.D(node).Children[1].Type() != "Entry" {
								t.Fatalf("unexpected result: %s", batchRetentionJSON(t, node))
							}
							entry := value.D(node).Children[1]
							key, hasKey := value.D(entry).Fields.Get("Key")
							val, hasValue := value.D(entry).Fields.Get("Value")
							count, hasCount := value.D(entry).Fields.Get("Count")
							valueNode, valueIsNode := val.(*value.Node)
							if value.D(value.D(node).Children[0]).Text != "!" || value.D(value.D(node).Children[2]).Text != "?" || !hasKey || key != "alpha" || !hasValue || !valueIsNode || valueNode == nil || value.D(valueNode).Text != "123" || !hasCount || count != 3 {
								t.Fatalf("unexpected fields/terminals: %s", batchRetentionJSON(t, node))
							}
							before = batchRetentionJSON(t, node)
						}
						if event.Kind != TraceEnter || event.Rule != "main" || event.Depth != 1 || event.Pos != 0 {
							t.Fatalf("retained event metadata changed: %+v", event)
						}
						assertTraceParserDetached(t, batchRetentionParser(t, event), false)

						// A later parse reuses scratch; the caller-owned prior Node stays intact.
						retry, retryErr, retryEvent := parse("!beta:45?", false)
						if retryErr != nil || recognize && retry != nil || !recognize && (retry == nil || value.D(retry).Children[1].Field("Key") != "beta" || value.D(retry).Children[1].Field("Count") != 2) {
							t.Fatalf("retry node=%v err=%v", retry, retryErr)
						}
						assertTraceParserDetached(t, batchRetentionParser(t, retryEvent), false)
						if node != nil && !reflect.DeepEqual(batchRetentionJSON(t, node), before) {
							t.Fatal("retry changed the earlier successful Node")
						}

						failed, failedErr, failedEvent := parse("!alpha:?", true)
						var syntaxErr *SyntaxError
						if failed != nil || !errors.As(failedErr, &syntaxErr) {
							t.Fatalf("syntax failure node=%v err=%v", failed, failedErr)
						}
						assertTraceParserDetached(t, batchRetentionParser(t, failedEvent), false)
						if node != nil && !reflect.DeepEqual(batchRetentionJSON(t, node), before) {
							t.Fatal("syntax failure changed the earlier successful Node")
						}
					})
				}
			}
		}
	}
}

func TestWorkBatchTraceCallbackPanicDetachesParserAndPreservesRetry(t *testing.T) {
	text := strings.Repeat("a", 512)
	prog := compile(t, `rule main = "a"* $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				token := &struct{ marker string }{"trace panic"}
				var observed *parser
				var node *value.Node
				var err error
				got := workContextRecover(func() {
					node, err = prog.ParseWith("main", text, ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) {
						if e.Kind == TraceEnter && e.Rule == "main" {
							observed = e.p
							panic(token)
						}
					}})
				})
				if got != token || node != nil || err != nil {
					t.Fatalf("panic=%#v node=%v err=%v, want callback panic identity", got, node, err)
				}
				assertTraceParserDetached(t, observed, true)
				if n, err := prog.ParseWith("main", text, ParseOptions{Backend: backend, Unit: unit}); n == nil || err != nil {
					t.Fatalf("retry node=%v err=%v", n, err)
				}
			})
		}
	}
}

func TestWorkBatchTraceInterruptionDetachesParser(t *testing.T) {
	prog := compile(t, `rule main = "a"* $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				for _, cause := range []string{"cancel", "steps", "context-panic", "publication-panic"} {
					t.Run(fmt.Sprintf("%s/%s/recognize=%t/%s", backend, unit, recognize, cause), func(t *testing.T) {
						held, err := prog.ParseWith("main", "aaa", ParseOptions{Backend: backend, Unit: unit})
						if held == nil || err != nil {
							t.Fatalf("prior Node=%v err=%v", held, err)
						}
						before := batchRetentionJSON(t, held)
						token := &workContextPanicToken{phase: cause}
						var observed *parser
						var panicCtx *workContextErrPanic
						var cancel context.CancelFunc
						opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
						switch cause {
						case "cancel":
							opts.Context, cancel = context.WithCancel(context.Background())
							defer cancel()
						case "steps":
							opts.WorkLimits = limits.WorkLimits{MaxSteps: 1 << 30}
						default:
							at := 1
							if cause == "publication-panic" {
								at = 2
							}
							panicCtx = newWorkContextErrPanic(token, at)
							opts.Context = panicCtx
						}
						opts.Trace = func(e TraceEvent) {
							if e.Rule != "main" {
								return
							}
							if e.Kind == TraceEnter {
								observed = e.p
								switch cause {
								case "cancel":
									cancel()
								case "steps":
									e.p.control.limits.MaxSteps = e.p.control.stepsUsed
								case "context-panic":
									panicCtx.armed = true
								}
							} else if cause == "publication-panic" {
								panicCtx.armed = true
							}
						}
						var node *value.Node
						var returnedErr error
						got := workContextRecover(func() { node, returnedErr = prog.ParseWith("main", strings.Repeat("a", 512), opts) })
						if node != nil {
							t.Fatal("interrupted parse published a Node")
						}
						switch cause {
						case "cancel":
							if got != nil || !errors.Is(returnedErr, context.Canceled) {
								t.Fatalf("panic=%v err=%v", got, returnedErr)
							}
						case "steps":
							var limit *limits.WorkLimitError
							if got != nil || !errors.As(returnedErr, &limit) || limit.Kind != limits.StepWorkLimit {
								t.Fatalf("panic=%v err=%v", got, returnedErr)
							}
						default:
							if got != token || returnedErr != nil || panicCtx.calls != panicCtx.panicAt {
								t.Fatalf("panic=%v err=%v context calls=%d", got, returnedErr, panicCtx.calls)
							}
						}
						// Publication checks follow a completed run; they do not mark that run aborted.
						assertTraceParserDetached(t, observed, cause != "publication-panic")
						if !reflect.DeepEqual(batchRetentionJSON(t, held), before) {
							t.Fatal("interruption changed prior Node")
						}
						retry, retryErr := prog.ParseWith("main", "aaaa", ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
						if retryErr != nil || recognize && retry != nil || !recognize && retry == nil {
							t.Fatalf("retry Node=%v err=%v", retry, retryErr)
						}
						if !reflect.DeepEqual(batchRetentionJSON(t, held), before) {
							t.Fatal("retry changed prior Node")
						}
					})
				}
			}
		}
	}
}

type batchRetentionContext struct {
	context.Context
	done    chan struct{}
	payload [128]byte
}

func (c *batchRetentionContext) Done() <-chan struct{} { return c.done }
func (c *batchRetentionContext) Err() error            { return nil }

//go:noinline
func batchRetentionCaptureEvent(prog *Program, text string, backend Backend, unit PositionUnit, active bool) (TraceEvent, weak.Pointer[batchRetentionContext], error) {
	ctx := &batchRetentionContext{Context: context.Background(), done: make(chan struct{})}
	for i := range ctx.payload {
		ctx.payload[i] = byte(i)
	}
	weakCtx := weak.Make(ctx)
	runtime.GC()
	if weakCtx.Value() != ctx {
		runtime.KeepAlive(ctx)
		return TraceEvent{}, weakCtx, errors.New("weak context positive control was not live with a strong local root")
	}
	runtime.KeepAlive(ctx)

	var retained TraceEvent
	opts := ParseOptions{Backend: backend, Unit: unit, Recognize: true, Trace: func(e TraceEvent) {
		if e.Kind == TraceEnter && e.Rule == "main" {
			retained = e
			if !batchRetentionContextLive(ctx) {
				panic("trace context state missing")
			}
		}
	}}
	if active {
		opts.Context = ctx
	}
	n, err := prog.ParseWith("main", text, opts)
	if n != nil {
		return retained, weakCtx, errors.New("recognition returned a Node")
	}
	runtime.KeepAlive(ctx)
	return retained, weakCtx, err
}

//go:noinline
func batchRetentionContextLive(ctx *batchRetentionContext) bool {
	return ctx.done != nil && ctx.payload[1] == 1
}

func TestWorkBatchTraceEventReleasesCapturedContextAfterReturn(t *testing.T) {
	text := strings.Repeat("a", 32*1024)
	prog := compile(t, `rule main = "a"* $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, active := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/active=%t", backend, unit, active), func(t *testing.T) {
					event, ref, err := batchRetentionCaptureEvent(prog, text, backend, unit, active)
					if err != nil || event.p == nil {
						t.Fatalf("event parser=%p err=%v", event.p, err)
					}
					released := false
					for range 5 {
						runtime.GC()
						if ref.Value() == nil {
							released = true
							break
						}
					}
					runtime.KeepAlive(event)
					runtime.KeepAlive(prog)
					if !released {
						t.Fatal("stored event still retains its trace-captured context after bounded GC")
					}
					assertTraceParserDetached(t, event.p, false)
				})
			}
		}
	}
}
