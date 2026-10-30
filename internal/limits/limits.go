// Package limits defines the limits of one parse invocation, the error
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

// Limits bounds one parse invocation. A zero limit leaves that limit
// unlimited.
//
// MaxWork bounds the cumulative work of the parse: every input unit
// inspected, every rule call and loop iteration, and every unit of value
// construction adds to one counter, so a bound on it bounds scanning,
// recursion and value building alike.
type Limits struct {
	MaxWork uint64
}

// ErrLimit is matched by errors.Is for any LimitError.
var ErrLimit = errors.New("parse limit exceeded")

// LimitKind identifies the limit that was exhausted.
type LimitKind uint8

const (
	LimitWork LimitKind = iota
)

// LimitError reports a reservation that would exceed a limit. Used is the
// counter value before the rejected reservation; Requested is the complete
// atomic reservation that could not be made.
type LimitError struct {
	Kind      LimitKind
	Limit     uint64
	Used      uint64
	Requested uint64
}

func (e *LimitError) Error() string {
	if e == nil {
		return ErrLimit.Error()
	}
	return fmt.Sprintf("parse work limit exceeded: limit %d, used %d, requested %d", e.Limit, e.Used, e.Requested)
}

// Is reports whether target is the limit sentinel.
func (e *LimitError) Is(target error) bool { return e != nil && target == ErrLimit }

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
