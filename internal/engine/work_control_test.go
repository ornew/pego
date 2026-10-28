package engine

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ornew/pego/internal/limits"
)

func catchFatal(t *testing.T, f func()) error {
	t.Helper()
	var got error
	func() {
		defer func() {
			if v := recover(); v != nil {
				fatal, ok := v.(fatal)
				if !ok {
					t.Fatalf("panic = %T(%v), want fatal", v, v)
				}
				got = fatal.err
			}
		}()
		f()
	}()
	return got
}

func TestNewWorkControlDisabled(t *testing.T) {
	var absent *workControl
	absent.check()
	absent.charge(1, 1)

	for name, ctx := range map[string]context.Context{
		"nil":        nil,
		"background": context.Background(),
		"no done":    context.WithoutCancel(context.WithValue(context.Background(), struct{}{}, "v")),
	} {
		t.Run(name, func(t *testing.T) {
			if got := newWorkControl(ctx, limits.WorkLimits{}); got != nil {
				t.Fatalf("newWorkControl = %#v, want nil", got)
			}
		})
	}
	if got := newWorkControl(nil, limits.WorkLimits{MaxSteps: 1}); got == nil {
		t.Fatal("finite step limit returned nil control")
	}
}

func TestWorkControlExactLimitsAndOverflow(t *testing.T) {
	c := newWorkControl(nil, limits.WorkLimits{MaxInputWork: 5, MaxSteps: 3})
	c.charge(5, 3)
	if c.inputUsed != 5 || c.stepsUsed != 3 {
		t.Fatalf("counters = (%d,%d), want (5,3)", c.inputUsed, c.stepsUsed)
	}
	if err := catchFatal(t, func() { c.charge(1, 0) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("input N+1 error = %v, want ErrWorkLimit", err)
	}
	var limitErr *limits.WorkLimitError
	if !errors.As(catchFatal(t, func() { c.charge(0, 1) }), &limitErr) {
		t.Fatal("step N+1 error is not a WorkLimitError")
	}
	if limitErr.Kind != limits.StepWorkLimit || limitErr.Limit != 3 || limitErr.Used != 3 || limitErr.Requested != 1 {
		t.Fatalf("step error = %#v", limitErr)
	}

	c = newWorkControl(nil, limits.WorkLimits{MaxInputWork: math.MaxUint64})
	c.charge(math.MaxUint64-1, 0)
	if err := catchFatal(t, func() { c.charge(2, 0) }); err == nil {
		t.Fatal("overflowing reservation unexpectedly succeeded")
	}
	if c.inputUsed != math.MaxUint64-1 {
		t.Fatalf("failed reservation changed input counter to %d", c.inputUsed)
	}
}

func TestWorkControlAtomicReservationAndPriority(t *testing.T) {
	c := newWorkControl(nil, limits.WorkLimits{MaxInputWork: 4, MaxSteps: 2})
	c.charge(2, 1)
	var got *limits.WorkLimitError
	if !errors.As(catchFatal(t, func() { c.charge(3, 2) }), &got) {
		t.Fatal("combined reservation did not fail with WorkLimitError")
	}
	if got.Kind != limits.InputWorkLimit || got.Used != 2 || got.Requested != 3 {
		t.Fatalf("error = %#v, want input limit before step limit", got)
	}
	if c.inputUsed != 2 || c.stepsUsed != 1 {
		t.Fatalf("failed combined reservation changed counters: (%d,%d)", c.inputUsed, c.stepsUsed)
	}
}

func TestWorkControlUnlimitedCountersSaturate(t *testing.T) {
	c := newWorkControl(newPollingContext(0), limits.WorkLimits{})
	c.charge(math.MaxUint64-2, math.MaxUint64-3)
	c.charge(10, 11)
	if c.inputUsed != math.MaxUint64 || c.stepsUsed != math.MaxUint64 {
		t.Fatalf("saturated counters = (%d,%d), want MaxUint64", c.inputUsed, c.stepsUsed)
	}
	if c.inputSinceCheck != 10 || c.stepsSinceCheck != 11 {
		t.Fatalf("poll counters after following small reservation = (%d,%d), want (10,11)", c.inputSinceCheck, c.stepsSinceCheck)
	}
}

func TestWorkControlCancellationAndCause(t *testing.T) {
	cause := errors.New("caller cause")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	c := newWorkControl(ctx, limits.WorkLimits{MaxInputWork: 1})
	got := catchFatal(t, func() { c.charge(2, 0) })
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", got)
	}
	if errors.Is(got, cause) {
		t.Fatalf("error = %v, unexpectedly replaced context.Err with cause", got)
	}
	if context.Cause(ctx) != cause {
		t.Fatalf("context cause = %v, want caller cause", context.Cause(ctx))
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	deadline := newWorkControl(deadlineCtx, limits.WorkLimits{MaxSteps: 1})
	if err := catchFatal(t, deadline.check); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v, want context.DeadlineExceeded", err)
	}
}

type pollingContext struct {
	context.Context
	done   chan struct{}
	checks int
	at     int
}

func newPollingContext(at int) *pollingContext {
	return &pollingContext{Context: context.Background(), done: make(chan struct{}), at: at}
}

func (c *pollingContext) Done() <-chan struct{} { return c.done }
func (c *pollingContext) Err() error {
	c.checks++
	if c.at > 0 && c.checks >= c.at {
		return context.Canceled
	}
	return nil
}

func TestWorkControlPollIntervals(t *testing.T) {
	for _, tc := range []struct {
		name string
		work func(*workControl, uint64)
		n    uint64
	}{
		{"steps", func(c *workControl, n uint64) { c.charge(0, n) }, workCheckStepInterval},
		{"input", func(c *workControl, n uint64) { c.charge(n, 0) }, workCheckInputInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newPollingContext(1)
			c := newWorkControl(ctx, limits.WorkLimits{})
			tc.work(c, tc.n-1)
			if ctx.checks != 0 {
				t.Fatalf("checks before interval = %d, want 0", ctx.checks)
			}
			if err := catchFatal(t, func() { tc.work(c, 1) }); !errors.Is(err, context.Canceled) {
				t.Fatalf("interval poll error = %v, want context.Canceled", err)
			}
			if ctx.checks != 1 {
				t.Fatalf("checks at interval = %d, want 1", ctx.checks)
			}
		})
	}
}

func TestWorkControlExplicitCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := newWorkControl(ctx, limits.WorkLimits{})
	c.charge(1, 1)
	cancel()
	if err := catchFatal(t, c.check); !errors.Is(err, context.Canceled) {
		t.Fatalf("check error = %v, want context.Canceled", err)
	}
}
