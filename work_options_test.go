//go:build !pego_work_controls_reference

package pego

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine"
)

func TestWorkOptionsOrderAndTraceComposition(t *testing.T) {
	p, err := CompileSource(`rule main = "abc" $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := p.Parse("abc", WithContext(ctx), WithContext(nil), WithWorkLimits(WorkLimits{MaxSteps: 1}), WithWorkLimits(WorkLimits{}))
	if n == nil || err != nil {
		t.Fatalf("last option did not win: node=%v err=%v", n, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	first, second := 0, 0
	n, err = p.Parse("abc", WithContext(ctx), WithTrace(func(TraceEvent) { first++; cancel() }), WithTrace(func(TraceEvent) { second++ }))
	if n != nil || !errors.Is(err, context.Canceled) || first != 1 || second != 0 {
		t.Fatalf("callback order: node=%v err=%v first=%d second=%d", n, err, first, second)
	}
}

func TestWorkStreamPublicOptions(t *testing.T) {
	p, err := CompileSource(`rule main = "a"* #stream $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options, emits := 0, 0
	err = p.ParseStream(strings.NewReader("aaa"), func(*Node) error { emits++; return nil },
		WithContext(ctx), WithContext(nil), WithWorkLimits(WorkLimits{MaxSteps: 1}), WithWorkLimits(WorkLimits{}),
		func(o *engine.ParseOptions) { options++ })
	if err != nil || emits != 3 || options != 1 {
		t.Fatalf("error=%v emits=%d options=%d", err, emits, options)
	}
	first, second := 0, 0
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	err = p.ParseStream(strings.NewReader("aaa"), func(*Node) error { t.Fatal("emitted after trace cancellation"); return nil }, WithContext(ctx), WithTrace(func(TraceEvent) { first++; cancel() }), WithTrace(func(TraceEvent) { second++ }))
	if err != context.Canceled || first != 1 || second != 0 {
		t.Fatalf("error=%v trace=%d/%d", err, first, second)
	}
}
