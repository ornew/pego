//go:build !pego_work_controls_reference

package pego

import (
	"context"
	"errors"
	"testing"
)

func TestWorkOptionsOrderAndTraceComposition(t *testing.T) {
	p, err := CompileSource(`def main = "abc" $$`, "main")
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
