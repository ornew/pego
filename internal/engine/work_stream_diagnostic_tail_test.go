//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"testing"
)

func filledStreamDiagnosticTail(limit int) streamDiagnosticTail {
	tail := streamDiagnosticTail{limit: limit}
	for pos := 0; pos <= limit; pos++ {
		tail.add(&SyntaxError{Pos: pos})
	}
	return tail
}

func TestWorkStreamDiagnosticTailResultCopy(t *testing.T) {
	tail := filledStreamDiagnosticTail(300) // the ring wraps, so result must copy
	p := &parser{input: input{control: newWorkControl(nil, WorkLimits{MaxSteps: 128})}}
	err := catchFatal(t, func() { _ = tail.result(p) })
	var limit *WorkLimitError
	if !errors.As(err, &limit) || limit.Kind != StepWorkLimit || p.control.stepsUsed != 128 {
		t.Fatalf("result copy limit: err=%v used=%d", err, p.control.stepsUsed)
	}
	p.control = newWorkControl(nil, WorkLimits{MaxSteps: 300})
	got, ok := tail.result(p).(*TruncatedSyntaxErrors)
	if !ok || len(got.Errors) != 300 || got.Errors[0].Pos != 1 || got.Errors[299].Pos != 300 || p.control.stepsUsed != 300 {
		t.Fatalf("result retry: result=%v used=%d", got, p.control.stepsUsed)
	}
}

func TestWorkStreamDiagnosticTailResultChecksBeforePublication(t *testing.T) {
	tail := filledStreamDiagnosticTail(300)
	ctx := newPollingContext(3) // admission, 256 copied entries, then publication
	p := &parser{input: input{control: newWorkControl(ctx, WorkLimits{})}}
	var published error
	err := catchFatal(t, func() { published = tail.result(p) })
	if !errors.Is(err, context.Canceled) || published != nil || p.control.stepsUsed != 300 || ctx.checks != 3 {
		t.Fatalf("publication cancellation: err=%v published=%v used=%d polls=%d", err, published, p.control.stepsUsed, ctx.checks)
	}
}

func TestWorkStreamDiagnosticTailTracePreviewCancellation(t *testing.T) {
	tail := filledStreamDiagnosticTail(300)
	ctx := newPollingContext(2)
	p := &parser{input: input{control: newWorkControl(ctx, WorkLimits{})}}
	suffix := []*SyntaxError{{Pos: 301}}
	err := catchFatal(t, func() { _ = tail.preview(p, suffix) })
	if !errors.Is(err, context.Canceled) || p.control.stepsUsed != 128 || ctx.checks != 2 {
		t.Fatalf("preview cancellation: err=%v used=%d polls=%d", err, p.control.stepsUsed, ctx.checks)
	}
	// An interrupted preview leaves the collector available for the parse's
	// fatal cleanup and does not publish a partially ordered trace event.
	got := tail.result(&parser{}).(*TruncatedSyntaxErrors)
	if got.Errors[0].Pos != 1 || got.Errors[299].Pos != 300 {
		t.Fatalf("interrupted preview changed tail: %v", got.Errors)
	}
}
