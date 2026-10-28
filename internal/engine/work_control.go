package engine

import "github.com/ornew/pego/internal/limits"
import "context"

const (
	workCheckStepInterval  = uint64(256)
	workCheckInputInterval = uint64(4096)
)

// workControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
type workControl struct {
	ctx    context.Context
	limits limits.WorkLimits

	inputUsed uint64
	stepsUsed uint64

	inputSinceCheck uint64
	stepsSinceCheck uint64
}

// newWorkControl returns nil when neither cancellation nor a finite budget is
// active. A nil context is equivalent to context.Background.
func newWorkControl(ctx context.Context, limits limits.WorkLimits) *workControl {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Done() == nil && limits.MaxInputWork == 0 && limits.MaxSteps == 0 {
		return nil
	}
	return &workControl{ctx: ctx, limits: limits}
}

// contextPanic attributes a panic to caller-owned context code rather than VM
// execution. Parse boundaries re-raise its original value after cleanup.
type contextPanic struct{ value any }

func contextErr(ctx context.Context) error {
	defer func() {
		if x := recover(); x != nil {
			switch x.(type) {
			case fatal, contextPanic:
				panic(x) // preserve engine interruption and an already attributed panic
			default:
				panic(contextPanic{value: x})
			}
		}
	}()
	return ctx.Err()
}

// check observes cancellation at an explicit engine checkpoint. It is safe on
// a nil receiver.
func (c *workControl) check() {
	if c == nil || c.ctx == nil {
		return
	}
	if err := contextErr(c.ctx); err != nil {
		panic(fatal{err: err})
	}
	c.inputSinceCheck = 0
	c.stepsSinceCheck = 0
}

// charge atomically reserves input-work and step units. Cancellation wins when
// it is observed at a poll or when either reservation would fail; otherwise
// input-work exhaustion takes priority over step exhaustion. A failed
// reservation leaves both counters unchanged.
func (c *workControl) charge(inputWork, steps uint64) {
	if c == nil {
		return
	}

	inputExceeded := limits.ExceedsLimit(c.inputUsed, inputWork, c.limits.MaxInputWork)
	stepsExceeded := limits.ExceedsLimit(c.stepsUsed, steps, c.limits.MaxSteps)
	poll := limits.ReachesInterval(c.inputSinceCheck, inputWork, workCheckInputInterval) ||
		limits.ReachesInterval(c.stepsSinceCheck, steps, workCheckStepInterval)

	// Check on each poll boundary and on a failed reservation so an already
	// canceled invocation wins over a simultaneously exhausted budget.
	if poll || inputExceeded || stepsExceeded {
		if c.ctx != nil {
			if err := contextErr(c.ctx); err != nil {
				panic(fatal{err: err})
			}
		}
	}
	if inputExceeded {
		panic(fatal{err: &limits.WorkLimitError{
			Kind: limits.InputWorkLimit, Limit: c.limits.MaxInputWork,
			Used: c.inputUsed, Requested: inputWork,
		}})
	}
	if stepsExceeded {
		panic(fatal{err: &limits.WorkLimitError{
			Kind: limits.StepWorkLimit, Limit: c.limits.MaxSteps,
			Used: c.stepsUsed, Requested: steps,
		}})
	}

	c.inputUsed = limits.SaturatingAdd(c.inputUsed, inputWork)
	c.stepsUsed = limits.SaturatingAdd(c.stepsUsed, steps)
	if poll {
		c.inputSinceCheck = 0
		c.stepsSinceCheck = 0
		return
	}
	c.inputSinceCheck = limits.SaturatingAdd(c.inputSinceCheck, inputWork)
	c.stepsSinceCheck = limits.SaturatingAdd(c.stepsSinceCheck, steps)
}
