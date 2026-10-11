package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// WorkLimits bounds cumulative engine work for one parse invocation. A zero
// limit leaves that counter unlimited.
type WorkLimits struct {
	MaxInputWork uint64
	MaxSteps     uint64
}

// ErrWorkLimit is matched by errors.Is for any WorkLimitError.
var ErrWorkLimit = errors.New("parse work limit exceeded")

// WorkLimitKind identifies the counter that exhausted its limit.
type WorkLimitKind uint8

const (
	InputWorkLimit WorkLimitKind = iota
	StepWorkLimit
)

// WorkLimitError reports a reservation that would exceed one work limit.
// Used is the counter value before the rejected reservation; Requested is the
// complete atomic reservation that could not be made.
type WorkLimitError struct {
	Kind      WorkLimitKind
	Limit     uint64
	Used      uint64
	Requested uint64
}

func (e *WorkLimitError) Error() string {
	if e == nil {
		return ErrWorkLimit.Error()
	}
	name := "input work"
	if e.Kind == StepWorkLimit {
		name = "steps"
	}
	return fmt.Sprintf("parse %s limit exceeded: limit %d, used %d, requested %d", name, e.Limit, e.Used, e.Requested)
}

// Is reports whether target is the work-limit sentinel.
func (e *WorkLimitError) Is(target error) bool { return e != nil && target == ErrWorkLimit }

const (
	workCheckStepInterval  = uint64(256)
	workCheckInputInterval = uint64(4096)
)

// workControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
type workControl struct {
	ctx    context.Context
	limits WorkLimits

	inputUsed uint64
	stepsUsed uint64

	inputSinceCheck uint64
	stepsSinceCheck uint64
}

// newWorkControl returns nil when neither cancellation nor a finite budget is
// active. A nil context is equivalent to context.Background.
func newWorkControl(ctx context.Context, limits WorkLimits) *workControl {
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

	inputExceeded := exceedsLimit(c.inputUsed, inputWork, c.limits.MaxInputWork)
	stepsExceeded := exceedsLimit(c.stepsUsed, steps, c.limits.MaxSteps)
	poll := reachesInterval(c.inputSinceCheck, inputWork, workCheckInputInterval) ||
		reachesInterval(c.stepsSinceCheck, steps, workCheckStepInterval)

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
		panic(fatal{err: &WorkLimitError{
			Kind: InputWorkLimit, Limit: c.limits.MaxInputWork,
			Used: c.inputUsed, Requested: inputWork,
		}})
	}
	if stepsExceeded {
		panic(fatal{err: &WorkLimitError{
			Kind: StepWorkLimit, Limit: c.limits.MaxSteps,
			Used: c.stepsUsed, Requested: steps,
		}})
	}

	c.inputUsed = saturatingAdd(c.inputUsed, inputWork)
	c.stepsUsed = saturatingAdd(c.stepsUsed, steps)
	if poll {
		c.inputSinceCheck = 0
		c.stepsSinceCheck = 0
		return
	}
	c.inputSinceCheck = saturatingAdd(c.inputSinceCheck, inputWork)
	c.stepsSinceCheck = saturatingAdd(c.stepsSinceCheck, steps)
}

// exceedsLimit checks a reservation without computing a potentially
// overflowing used+requested sum. A zero limit is unlimited.
func exceedsLimit(used, requested, limit uint64) bool {
	return limit != 0 && (used > limit || requested > limit-used)
}

// reachesInterval checks whether adding requested would meet or cross interval
// without overflowing. Progress counters are kept below their interval.
func reachesInterval(used, requested, interval uint64) bool {
	return used >= interval || requested >= interval-used
}

func saturatingAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}
