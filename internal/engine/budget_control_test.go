package engine

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func catchFatal(t *testing.T, f func()) error {
	t.Helper()
	var got error
	func() {
		defer func() {
			if v := recover(); v != nil {
				fatal, ok := v.(core.Fatal)
				if !ok {
					t.Fatalf("panic = %T(%v), want fatal", v, v)
				}
				got = fatal.Err
			}
		}()
		f()
	}()
	return got
}

func TestNewBudgetControlDisabled(t *testing.T) {
	t.Parallel()
	var absent *core.BudgetControl
	absent.Check()
	absent.Charge(1)

	for name, ctx := range map[string]context.Context{
		"nil":        nil,
		"background": context.Background(),
		"no done":    context.WithoutCancel(context.WithValue(context.Background(), struct{}{}, "v")),
	} {
		t.Run(name, func(t *testing.T) {
			if got := core.NewBudgetControl(ctx, limits.Limits{}); got != nil {
				t.Fatalf("newBudgetControl = %#v, want nil", got)
			}
		})
	}
	if got := core.NewBudgetControl(nil, limits.Limits{Budget: 1}); got == nil {
		t.Fatal("finite budget limit returned nil control")
	}
}

func TestBudgetControlExactLimitsAndOverflow(t *testing.T) {
	t.Parallel()
	c := core.NewBudgetControl(nil, limits.Limits{Budget: 8})
	c.Charge(8)
	if c.Used != 8 {
		t.Fatalf("counter = %d, want 8", c.Used)
	}
	var limitErr *limits.LimitError
	err := catchFatal(t, func() { c.Charge(1) })
	if !errors.Is(err, limits.ErrLimit) || !errors.As(err, &limitErr) {
		t.Fatalf("limit+1 error = %v, want a LimitError matching ErrLimit", err)
	}
	if limitErr.Kind != limits.LimitBudget || limitErr.Limit != 8 || limitErr.Used != 8 || limitErr.Requested != 1 {
		t.Fatalf("error = %#v", limitErr)
	}

	c = core.NewBudgetControl(nil, limits.Limits{Budget: math.MaxUint64})
	c.Charge(math.MaxUint64 - 1)
	if err := catchFatal(t, func() { c.Charge(2) }); err == nil {
		t.Fatal("overflowing reservation unexpectedly succeeded")
	}
	if c.Used != math.MaxUint64-1 {
		t.Fatalf("failed reservation changed the counter to %d", c.Used)
	}
}

func TestBudgetControlFailedReservationLeavesCounter(t *testing.T) {
	t.Parallel()
	c := core.NewBudgetControl(nil, limits.Limits{Budget: 6})
	c.Charge(2)
	var got *limits.LimitError
	if !errors.As(catchFatal(t, func() { c.Charge(5) }), &got) {
		t.Fatal("reservation over the limit did not fail with LimitError")
	}
	if got.Kind != limits.LimitBudget || got.Used != 2 || got.Requested != 5 {
		t.Fatalf("error = %#v", got)
	}
	if c.Used != 2 {
		t.Fatalf("failed reservation changed the counter to %d", c.Used)
	}
}

func TestBudgetControlUnlimitedCounterSaturates(t *testing.T) {
	t.Parallel()
	c := core.NewBudgetControl(newPollingContext(0), limits.Limits{})
	c.Charge(math.MaxUint64 - 2)
	c.Charge(10)
	if c.Used != math.MaxUint64 {
		t.Fatalf("saturated counter = %d, want MaxUint64", c.Used)
	}
	if c.SinceCheck != 10 {
		t.Fatalf("poll counter after following small reservation = %d, want 10", c.SinceCheck)
	}
}

func TestBudgetControlCancellationAndCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("caller cause")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	c := core.NewBudgetControl(ctx, limits.Limits{Budget: 1})
	got := catchFatal(t, func() { c.Charge(2) })
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
	deadline := core.NewBudgetControl(deadlineCtx, limits.Limits{Budget: 1})
	if err := catchFatal(t, deadline.Check); !errors.Is(err, context.DeadlineExceeded) {
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

func TestBudgetControlPollInterval(t *testing.T) {
	t.Parallel()
	ctx := newPollingContext(1)
	c := core.NewBudgetControl(ctx, limits.Limits{})
	c.Charge(core.BudgetCheckInterval - 1)
	if ctx.checks != 0 {
		t.Fatalf("checks before interval = %d, want 0", ctx.checks)
	}
	if err := catchFatal(t, func() { c.Charge(1) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("interval poll error = %v, want context.Canceled", err)
	}
	if ctx.checks != 1 {
		t.Fatalf("checks at interval = %d, want 1", ctx.checks)
	}
}

func TestBudgetControlExplicitCheck(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	c := core.NewBudgetControl(ctx, limits.Limits{})
	c.Charge(1)
	cancel()
	if err := catchFatal(t, c.Check); !errors.Is(err, context.Canceled) {
		t.Fatalf("check error = %v, want context.Canceled", err)
	}
}
