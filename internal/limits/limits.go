// Package limits defines the work limits of one parse invocation, the error
// that reports an exhausted limit, and the overflow-safe arithmetic used to
// account work against them. The parser, the document and the structural
// query share these definitions; the public API exposes them as aliases.
package limits

import (
	"errors"
	"fmt"
	"math"
)

// Default limits on call nesting depth. Backends that run recursively use the host stack, so
// their limit is smaller.
const (
	DefaultMaxDepth          = 100_000
	DefaultMaxDepthIterative = 10_000_000
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

// ExceedsLimit checks a reservation without computing a potentially
// overflowing used+requested sum. A zero limit is unlimited.
func ExceedsLimit(used, requested, limit uint64) bool {
	return limit != 0 && (used > limit || requested > limit-used)
}

// ReachesInterval checks whether adding requested would meet or cross interval
// without overflowing. Progress counters are kept below their interval.
func ReachesInterval(used, requested, interval uint64) bool {
	return used >= interval || requested >= interval-used
}

// SaturatingAdd returns a+b, or the largest uint64 when the sum overflows.
func SaturatingAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}
