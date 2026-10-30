//go:build !pego_budget_controls_reference

package apitest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/engine"
)

func TestBudgetOptionsOrderAndTraceComposition(t *testing.T) {
	p, err := pego.CompileSource(`@start
rule main = "abc" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := p.Parse("abc", pego.WithContext(ctx), pego.WithContext(nil), pego.WithLimits(pego.Limits{Budget: 1}), pego.WithLimits(pego.Limits{}))
	if n == nil || err != nil {
		t.Fatalf("last option did not win: node=%v err=%v", n, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	first, second := 0, 0
	n, err = p.Parse("abc", pego.WithContext(ctx), pego.WithTrace(func(pego.TraceEvent) { first++; cancel() }), pego.WithTrace(func(pego.TraceEvent) { second++ }))
	if n != nil || !errors.Is(err, context.Canceled) || first != 1 || second != 0 {
		t.Fatalf("callback order: node=%v err=%v first=%d second=%d", n, err, first, second)
	}
}

func TestBudgetStreamPublicOptions(t *testing.T) {
	p, err := pego.CompileSource(`@start
rule main = "a"* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options, emits := 0, 0
	err = p.ParseStream(strings.NewReader("aaa"), func(*pego.Node) error { emits++; return nil },
		pego.WithContext(ctx), pego.WithContext(nil), pego.WithLimits(pego.Limits{Budget: 1}), pego.WithLimits(pego.Limits{}),
		func(o *engine.ParseOptions) { options++ })
	if err != nil || emits != 3 || options != 1 {
		t.Fatalf("error=%v emits=%d options=%d", err, emits, options)
	}
	first, second := 0, 0
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	err = p.ParseStream(strings.NewReader("aaa"), func(*pego.Node) error { t.Fatal("emitted after trace cancellation"); return nil }, pego.WithContext(ctx), pego.WithTrace(func(pego.TraceEvent) { first++; cancel() }), pego.WithTrace(func(pego.TraceEvent) { second++ }))
	if err != context.Canceled || first != 1 || second != 0 {
		t.Fatalf("error=%v trace=%d/%d", err, first, second)
	}
}
