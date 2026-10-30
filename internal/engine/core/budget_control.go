package core

import (
	"context"
	"unicode/utf8"

	"github.com/ornew/pego/internal/limits"
)

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

	// Adaptive is set for a stream whose budget is the default: the budget follows the units read
	// (GrowDefault), which are Read.
	Adaptive bool
	Read     uint64
}

// ResolveLimits returns the limits a parse runs under for an input of n units of its position
// unit: a zero Budget selects the default (limits.DefaultBudget), and Unlimited disables the
// budget, which the result leaves zero. A stream does not know n: its default starts at the floor
// and grows with the input (BudgetControl.GrowDefault), which adaptive reports. The reference
// build has no controls to charge, so no default applies there.
func ResolveLimits(l limits.Limits, n uint64, stream bool) (resolved limits.Limits, adaptive bool) {
	switch l.Budget {
	case limits.Unlimited:
		l.Budget = 0
	case 0:
		switch {
		case !BudgetControlsEnabled:
		case stream:
			l.Budget, adaptive = limits.DefaultBudgetFloor, true
		default:
			l.Budget = limits.DefaultBudget(n)
		}
	}
	return l, adaptive
}

// GrowDefault raises the default budget of a stream for k more units read. It does nothing for
// any other budget, and on a nil receiver.
func (c *BudgetControl) GrowDefault(k int) {
	if c == nil || !c.Adaptive || k <= 0 {
		return
	}
	c.Read += uint64(k)
	if b := limits.DefaultBudget(c.Read); b > c.Limits.Budget {
		c.Limits.Budget = b
	}
}

// NewBudgetControl returns nil when neither cancellation nor a finite budget is
// active. A nil context is equivalent to context.Background. A context that can never be
// canceled (Done is nil) is never polled.
func NewBudgetControl(ctx context.Context, limits limits.Limits) *BudgetControl {
	if ctx != nil && ctx.Done() == nil {
		ctx = nil
	}
	if ctx == nil && limits.Budget == 0 {
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

// InputUnits returns the length of s in units of the position unit. Below the size at which the
// default budget leaves its floor the byte length stands in for the count of code points, which it
// bounds from above.
func InputUnits(s string, unit PositionUnit) uint64 {
	if unit == Bytes || len(s) <= limits.DefaultBudgetFloor/limits.DefaultBudgetPerUnit {
		return uint64(len(s))
	}
	return uint64(utf8.RuneCountInString(s))
}
