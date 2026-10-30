package core

import (
	"context"
	"errors"
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

	// Adaptive is set for a stream whose budget is the default: the budget follows the furthest
	// position the parser has demanded from the stream (Demand).
	Adaptive bool
}

// ResolveLimits returns the limits a parse runs under for an input of n units of its position
// unit: a zero Budget selects the default (limits.DefaultBudget), and Unlimited disables the
// budget, which the result leaves zero. A stream does not know n: its default starts at the floor
// and grows with the furthest position the parser demands (BudgetControl.Demand), which adaptive reports. The reference
// build has no controls to charge, so no default applies there.
func ResolveLimits(l limits.Limits, n uint64, stream bool) (resolved limits.Limits, adaptive bool) {
	if l.MaxInput == limits.Unlimited {
		l.MaxInput = 0 // the default of MaxInput is Unlimited
	}
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

// Demand raises the default budget of a stream to that of n units, where n is the number of
// units up to the furthest position the parser has asked the stream for. The number depends on the
// input and the grammar only, not on how the reader chunks the input. It does nothing for any
// other budget, and on a nil receiver; the budget never shrinks.
func (c *BudgetControl) Demand(n int) {
	if c == nil || !c.Adaptive || n <= 0 {
		return
	}
	if b := limits.DefaultBudget(uint64(n)); b > c.Limits.Budget {
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

// ErrNoControls is returned in the reference build when a context or a finite limit is requested.
var ErrNoControls = errors.New("budget controls are unavailable in the reference build")

// CheckInput rejects an input of s that is longer than the resolved MaxInput of l, in the position
// unit. The check is O(1) unless the byte length passes the limit in CodePoints, where the exact
// count decides: the byte length bounds the count from above.
func CheckInput(l limits.Limits, s string, unit PositionUnit) error {
	if l.MaxInput == 0 {
		return nil
	}
	if !BudgetControlsEnabled {
		return ErrNoControls
	}
	n := uint64(len(s))
	if n > l.MaxInput && unit == CodePoints {
		n = uint64(utf8.RuneCountInString(s))
	}
	return CheckInputUnits(l, n)
}

// CheckInputUnits rejects an input of n units that is longer than the resolved MaxInput of l.
func CheckInputUnits(l limits.Limits, n uint64) error {
	if l.MaxInput == 0 {
		return nil
	}
	if !BudgetControlsEnabled {
		return ErrNoControls
	}
	if n > l.MaxInput {
		return &limits.LimitError{Kind: limits.LimitInput, Limit: l.MaxInput, Requested: n}
	}
	return nil
}
