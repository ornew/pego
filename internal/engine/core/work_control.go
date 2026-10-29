package core

import "github.com/ornew/pego/internal/limits"
import "context"

const (
	WorkCheckStepInterval  = uint64(256)
	WorkCheckInputInterval = uint64(4096)
)

// WorkControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
type WorkControl struct {
	Ctx    context.Context
	Limits limits.WorkLimits

	InputUsed uint64
	StepsUsed uint64

	InputSinceCheck uint64
	StepsSinceCheck uint64
}

// NewWorkControl returns nil when neither cancellation nor a finite budget is
// active. A nil context is equivalent to context.Background.
func NewWorkControl(ctx context.Context, limits limits.WorkLimits) *WorkControl {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Done() == nil && limits.MaxInputWork == 0 && limits.MaxSteps == 0 {
		return nil
	}
	return &WorkControl{Ctx: ctx, Limits: limits}
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
func (c *WorkControl) Check() {
	if c == nil || c.Ctx == nil {
		return
	}
	if err := contextErr(c.Ctx); err != nil {
		panic(Fatal{Err: err})
	}
	c.InputSinceCheck = 0
	c.StepsSinceCheck = 0
}

// Charge atomically reserves input-work and step units. Cancellation wins when
// it is observed at a poll or when either reservation would fail; otherwise
// input-work exhaustion takes priority over step exhaustion. A failed
// reservation leaves both counters unchanged.
func (c *WorkControl) Charge(inputWork, steps uint64) {
	if c == nil {
		return
	}

	inputExceeded := limits.ExceedsLimit(c.InputUsed, inputWork, c.Limits.MaxInputWork)
	stepsExceeded := limits.ExceedsLimit(c.StepsUsed, steps, c.Limits.MaxSteps)
	poll := limits.ReachesInterval(c.InputSinceCheck, inputWork, WorkCheckInputInterval) ||
		limits.ReachesInterval(c.StepsSinceCheck, steps, WorkCheckStepInterval)

	// Check on each poll boundary and on a failed reservation so an already
	// canceled invocation wins over a simultaneously exhausted budget.
	if poll || inputExceeded || stepsExceeded {
		if c.Ctx != nil {
			if err := contextErr(c.Ctx); err != nil {
				panic(Fatal{Err: err})
			}
		}
	}
	if inputExceeded {
		panic(Fatal{Err: &limits.WorkLimitError{
			Kind: limits.InputWorkLimit, Limit: c.Limits.MaxInputWork,
			Used: c.InputUsed, Requested: inputWork,
		}})
	}
	if stepsExceeded {
		panic(Fatal{Err: &limits.WorkLimitError{
			Kind: limits.StepWorkLimit, Limit: c.Limits.MaxSteps,
			Used: c.StepsUsed, Requested: steps,
		}})
	}

	c.InputUsed = limits.SaturatingAdd(c.InputUsed, inputWork)
	c.StepsUsed = limits.SaturatingAdd(c.StepsUsed, steps)
	if poll {
		c.InputSinceCheck = 0
		c.StepsSinceCheck = 0
		return
	}
	c.InputSinceCheck = limits.SaturatingAdd(c.InputSinceCheck, inputWork)
	c.StepsSinceCheck = limits.SaturatingAdd(c.StepsSinceCheck, steps)
}
