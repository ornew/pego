//go:build !zp_budget_controls_reference

package apitest_test

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

type publicContextPanicToken struct{ label string }

func (*publicContextPanicToken) Error() string { return "public context panic token" }
func (*publicContextPanicToken) RuntimeError() {}

var _ runtime.Error = (*publicContextPanicToken)(nil)

type publicContextErrPanic struct {
	context.Context
	done  chan struct{}
	armed bool
	calls int
	token *publicContextPanicToken
}

func (c *publicContextErrPanic) Done() <-chan struct{} { return c.done }

func (c *publicContextErrPanic) Err() error {
	if !c.armed {
		return nil
	}
	c.calls++
	if c.calls == 2 { // The first call is the post-TraceEnter check; the second is spent budget.
		panic(c.token)
	}
	return nil
}

func TestContextErrRuntimePanicIdentityThroughPublicOptions(t *testing.T) {
	literal := strings.Repeat("a", 1024)
	p, err := compileTestSource(`rule main = "`+literal+`" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}

	for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		for _, unit := range []zp.PositionUnit{zp.Bytes, zp.CodePoints} {
			for _, recognize := range []bool{false, true} {
				t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize), func(t *testing.T) {
					retained, err := p.Parse(literal, zp.WithBackend(backend), zp.WithUnit(unit))
					if retained == nil || err != nil {
						t.Fatalf("baseline node=%v error=%v", retained, err)
					}
					snapshot := retained.String()
					token := &publicContextPanicToken{label: "public context panic"}
					ctx := &publicContextErrPanic{Context: context.Background(), done: make(chan struct{}), token: token}
					traceEntries := 0
					opts := []zp.ParseOption{
						zp.WithBackend(backend),
						zp.WithUnit(unit),
						zp.WithContext(ctx),
						zp.WithTrace(func(e zp.TraceEvent) {
							if e.Kind == zp.TraceEnter && e.Rule == "main" {
								traceEntries++
								ctx.armed = true
							}
						}),
					}
					if recognize {
						opts = append(opts, zp.RecognizeOnly())
					}

					var node *zp.Node
					var returnedErr error
					var got any
					func() {
						defer func() { got = recover() }()
						node, returnedErr = p.Parse(literal, opts...)
					}()
					if got != token || node != nil || returnedErr != nil || ctx.calls != 2 || traceEntries != 1 {
						t.Fatalf("panic=%#v node=%v error=%v Err calls=%d trace entries=%d", got, node, returnedErr, ctx.calls, traceEntries)
					}

					retryOpts := []zp.ParseOption{zp.WithBackend(backend), zp.WithUnit(unit)}
					if recognize {
						retryOpts = append(retryOpts, zp.RecognizeOnly())
					}
					retry, retryErr := p.Parse(literal, retryOpts...)
					if retained.String() != snapshot {
						t.Fatal("panic/retry mutated retained Node")
					}
					if retryErr != nil || recognize && retry != nil || !recognize && (retry == nil || retry.String() != snapshot) {
						t.Fatalf("retry node=%v err=%v recognize=%v", retry, retryErr, recognize)
					}
				})
			}
		}
	}
}
