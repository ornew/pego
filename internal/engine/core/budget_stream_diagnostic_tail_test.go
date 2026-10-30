//go:build !pego_budget_controls_reference

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func filledStreamDiagnosticTail(limit int) StreamDiagnosticTail {
	tail := StreamDiagnosticTail{Limit: limit}
	for pos := 0; pos <= limit; pos++ {
		tail.Add(&SyntaxError{Pos: pos})
	}
	return tail
}

func TestBudgetStreamDiagnosticTailResultCopy(t *testing.T) {
	tail := filledStreamDiagnosticTail(300) // the ring wraps, so result must copy
	p := &Parser{Input: Input{Control: NewBudgetControl(nil, limits.Limits{Budget: 128})}}
	err := catchFatal(t, func() { _ = tail.Result(p) })
	var limit *limits.LimitError
	if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget || p.Control.Used != 128 {
		t.Fatalf("result copy limit: err=%v used=%d", err, p.Control.Used)
	}
	p.Control = NewBudgetControl(nil, limits.Limits{Budget: 300})
	got, ok := tail.Result(p).(*TruncatedSyntaxErrors)
	if !ok || len(got.Errors) != 300 || got.Errors[0].Pos != 1 || got.Errors[299].Pos != 300 || p.Control.Used != 300 {
		t.Fatalf("result retry: result=%v used=%d", got, p.Control.Used)
	}
}

func TestBudgetStreamDiagnosticTailResultChecksBeforePublication(t *testing.T) {
	tail := filledStreamDiagnosticTail(300)
	ctx := newPollingContext(3) // admission, 256 copied entries, then publication
	p := &Parser{Input: Input{Control: NewBudgetControl(ctx, limits.Limits{})}}
	var published error
	err := catchFatal(t, func() { published = tail.Result(p) })
	if !errors.Is(err, context.Canceled) || published != nil || p.Control.Used != 300 || ctx.checks != 3 {
		t.Fatalf("publication cancellation: err=%v published=%v used=%d polls=%d", err, published, p.Control.Used, ctx.checks)
	}
}

func TestBudgetStreamDiagnosticTailTracePreviewCancellation(t *testing.T) {
	tail := filledStreamDiagnosticTail(300)
	ctx := newPollingContext(2)
	p := &Parser{Input: Input{Control: NewBudgetControl(ctx, limits.Limits{})}}
	suffix := []*SyntaxError{{Pos: 301}}
	err := catchFatal(t, func() { _ = tail.Preview(p, suffix) })
	if !errors.Is(err, context.Canceled) || p.Control.Used != 128 || ctx.checks != 2 {
		t.Fatalf("preview cancellation: err=%v used=%d polls=%d", err, p.Control.Used, ctx.checks)
	}
	// An interrupted preview leaves the collector available for the parse's
	// fatal cleanup and does not publish a partially ordered trace event.
	got := tail.Result(&Parser{}).(*TruncatedSyntaxErrors)
	if got.Errors[0].Pos != 1 || got.Errors[299].Pos != 300 {
		t.Fatalf("interrupted preview changed tail: %v", got.Errors)
	}
}
