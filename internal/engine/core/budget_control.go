package core

import "github.com/ornew/pego/internal/limits"
import "context"

// BudgetCheckInterval is the spend between two observations of cancellation.
const BudgetCheckInterval = uint64(256)

// BudgetControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
type BudgetControl struct {
	Ctx    context.Context
	Limits limits.Limits

	// Used is the spend reserved so far; SinceCheck is the part of it since the
	// last observation of cancellation.
	Used       uint64
	SinceCheck uint64
}

// NewBudgetControl returns nil when neither cancellation nor a finite budget is
// active. A nil context is equivalent to context.Background.
func NewBudgetControl(ctx context.Context, limits limits.Limits) *BudgetControl {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Done() == nil && limits.Budget == 0 {
		return nil
	}
	return &BudgetControl{Ctx: ctx, Limits: limits}
}

// ContextPanic attributes a panic to caller-owned context code rather than VM
// execution. Parse boundaries re-raise its original value after cleanup.
type ContextPanic struct{ Value any }

func contextErr(ctx context.Context) error {
	defer func() {
		if x := recover(); x != nil {
			switch x.(type) {
			case Fatal, ContextPanic:
				panic(x) // preserve engine interruption and an already attributed panic
			default:
				panic(ContextPanic{Value: x})
			}
		}
	}()
	return ctx.Err()
}

// Check observes cancellation at an explicit engine checkpoint. It is safe on
// a nil receiver.
func (c *BudgetControl) Check() {
	if c == nil || c.Ctx == nil {
		return
	}
	if err := contextErr(c.Ctx); err != nil {
		panic(Fatal{Err: err})
	}
	c.SinceCheck = 0
}

// Charge atomically reserves n budget units. Cancellation wins when it is
// observed at a poll or when the reservation would fail. A failed reservation
// leaves the counter unchanged. It is safe on a nil receiver.
func (c *BudgetControl) Charge(n uint64) {
	if c == nil {
		return
	}

	exceeded := limits.ExceedsLimit(c.Used, n, c.Limits.Budget)
	poll := limits.ReachesInterval(c.SinceCheck, n, BudgetCheckInterval)

	// Check on each poll boundary and on a failed reservation so an already
	// canceled invocation wins over a simultaneously exhausted budget.
	if poll || exceeded {
		if c.Ctx != nil {
			if err := contextErr(c.Ctx); err != nil {
				panic(Fatal{Err: err})
			}
		}
	}
	if exceeded {
		panic(Fatal{Err: &limits.LimitError{
			Kind: limits.LimitBudget, Limit: c.Limits.Budget,
			Used: c.Used, Requested: n,
		}})
	}

	c.Used = limits.SaturatingAdd(c.Used, n)
	if poll {
		c.SinceCheck = 0
		return
	}
	c.SinceCheck = limits.SaturatingAdd(c.SinceCheck, n)
}
