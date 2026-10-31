//go:build !zp_budget_controls_reference

package apitest_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/engine"
)

func TestBudgetDocumentPublicDefaultsAndOptionsOnce(t *testing.T) {
	p, err := zp.CompileSource(`@start
rule main = inner $$

rule inner = "(" inner ")" / "a"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			constructCalls, parseCalls := 0, 0
			var order []string
			d, err := p.NewDocument("((a))", zp.WithBackend(backend), zp.WithMaxDepth(1),
				func(o *engine.ParseOptions) { constructCalls++ },
				zp.WithTrace(func(e zp.TraceEvent) {
					if e.Rule == "main" && e.Kind == zp.TraceEnter {
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
			n, err := d.Parse(zp.WithMaxDepth(20), func(o *engine.ParseOptions) { parseCalls++ },
				zp.WithTrace(func(e zp.TraceEvent) {
					if e.Rule == "main" && e.Kind == zp.TraceEnter {
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
			if err := d.Edit(0, 5, "((a))"); err != nil {
				t.Fatal(err)
			}
			if n, err := d.Parse(); n != nil || err == nil {
				t.Fatalf("default changed: node=%v error=%v", n, err)
			}
		})
	}
}

func TestBudgetDocumentPublicContextOverrideAndTraceOrder(t *testing.T) {
	p, err := zp.CompileSource(`@start
rule main = "a" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
		ctx, cancel := context.WithCancel(context.Background())
		d, err := p.NewDocument("a", zp.WithBackend(backend), zp.WithContext(ctx), zp.WithLimits(zp.Limits{Budget: 10000}))
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		if n, err := d.Parse(); n != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s defaults: node=%v error=%v", backend, n, err)
		}
		if n, err := d.Parse(zp.WithContext(nil), zp.WithLimits(zp.Limits{})); n == nil || err != nil {
			t.Fatalf("%s override: node=%v error=%v", backend, n, err)
		}
		if n, err := d.Parse(); n != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s defaults mutated: node=%v error=%v", backend, n, err)
		}
		ctx, cancel = context.WithCancel(context.Background())
		first, second := 0, 0
		_, err = d.Parse(zp.WithContext(ctx), zp.WithTrace(func(zp.TraceEvent) { first++; cancel() }), zp.WithTrace(func(zp.TraceEvent) { second++ }))
		cancel()
		if !errors.Is(err, context.Canceled) || first != 1 || second != 0 {
			t.Fatalf("%s callbacks: error=%v first=%d second=%d", backend, err, first, second)
		}
	}
}
