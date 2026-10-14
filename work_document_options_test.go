//go:build !pego_work_controls_reference

package pego

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ornew/pego/internal/engine"
)

func TestWorkDocumentPublicDefaultsAndOptionsOnce(t *testing.T) {
	p, err := CompileSource(`rule main = inner $$

rule inner = "a"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			constructCalls, parseCalls := 0, 0
			var order []string
			d, err := p.NewDocument("a", WithBackend(backend), WithMaxDepth(1),
				func(o *engine.ParseOptions) { constructCalls++ },
				WithTrace(func(e TraceEvent) {
					if e.Rule == "main" && e.Kind == TraceEnter {
						order = append(order, "default")
					}
				}))
			if err != nil || constructCalls != 1 {
				t.Fatalf("construction=%v options=%d", err, constructCalls)
			}
			if n, err := d.Parse(); n != nil || err == nil {
				t.Fatalf("default depth: node=%v error=%v", n, err)
			}
			order = nil
			n, err := d.Parse(WithMaxDepth(20), func(o *engine.ParseOptions) { parseCalls++ },
				WithTrace(func(e TraceEvent) {
					if e.Rule == "main" && e.Kind == TraceEnter {
						order = append(order, "invocation")
					}
				}))
			if n == nil || err != nil || parseCalls != 1 || constructCalls != 1 {
				t.Fatalf("override: node=%v error=%v counts=%d/%d", n, err, constructCalls, parseCalls)
			}
			if !reflect.DeepEqual(order, []string{"default", "invocation"}) {
				t.Fatalf("trace composed twice or out of order: %v", order)
			}
			// Override depth must not replace defaults; clear successful memo so
			// the next invocation genuinely evaluates the nested rule again.
			if err := d.Edit(0, 1, "a"); err != nil {
				t.Fatal(err)
			}
			if n, err := d.Parse(); n != nil || err == nil {
				t.Fatalf("default changed: node=%v error=%v", n, err)
			}
		})
	}
}

func TestWorkDocumentPublicContextOverrideAndTraceOrder(t *testing.T) {
	p, err := CompileSource(`rule main = "a" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		ctx, cancel := context.WithCancel(context.Background())
		d, err := p.NewDocument("a", WithBackend(backend), WithContext(ctx), WithWorkLimits(WorkLimits{MaxInputWork: 10000, MaxSteps: 10000}))
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		if n, err := d.Parse(); n != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s defaults: node=%v error=%v", backend, n, err)
		}
		if n, err := d.Parse(WithContext(nil), WithWorkLimits(WorkLimits{})); n == nil || err != nil {
			t.Fatalf("%s override: node=%v error=%v", backend, n, err)
		}
		if n, err := d.Parse(); n != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s defaults mutated: node=%v error=%v", backend, n, err)
		}
		ctx, cancel = context.WithCancel(context.Background())
		first, second := 0, 0
		_, err = d.Parse(WithContext(ctx), WithTrace(func(TraceEvent) { first++; cancel() }), WithTrace(func(TraceEvent) { second++ }))
		cancel()
		if !errors.Is(err, context.Canceled) || first != 1 || second != 0 {
			t.Fatalf("%s callbacks: error=%v first=%d second=%d", backend, err, first, second)
		}
	}
}
