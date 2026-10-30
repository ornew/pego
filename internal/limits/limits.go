// Package limits defines the limits of one parse invocation, the error
// that reports an exhausted limit, and the overflow-safe arithmetic used to
// spend budget against them. The parser, the document and the structural
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

// Unlimited is the value of a limit that disables it. Every field of Limits is a uint64 and
// takes it, so one constant serves them all.
const Unlimited = math.MaxUint64

// The default budget of a parse is DefaultBudgetPerUnit for each unit of input, and at least
// DefaultBudgetFloor.
const (
	DefaultBudgetFloor   = 1 << 20
	DefaultBudgetPerUnit = 1024
)

// DefaultBudget returns the budget of a parse whose Budget is zero, for an input of n units of the
// position unit of the parse.
func DefaultBudget(n uint64) uint64 {
	if n > math.MaxUint64/DefaultBudgetPerUnit {
		return math.MaxUint64 - 1
	}
	return max(DefaultBudgetFloor, n*DefaultBudgetPerUnit)
}

// Limits bounds one parse invocation. A zero limit selects the default of that limit, and
// Unlimited disables it.
//
// Budget bounds the cumulative spend of the parse: every input unit
// inspected, every rule call and loop iteration, and every unit of value
// construction adds to one counter, so a bound on it bounds scanning,
// recursion and value building alike.
//
// MaxInput bounds the length of the input in the position unit of the parse. The default is
// Unlimited: the default budget already grows with the input. A parse of an input that is longer
// fails before any work, with a LimitError of kind LimitInput; a stream fails when the parser
// reaches a unit beyond the limit.
type Limits struct {
	Budget   uint64
	MaxInput uint64
}

// ErrLimit is matched by errors.Is for any LimitError.
var ErrLimit = errors.New("parse limit exceeded")

// LimitKind identifies the limit that was exhausted.
type LimitKind uint8

const (
	// LimitBudget is the kind of an exhausted Budget.
	LimitBudget LimitKind = iota
	// LimitInput is the kind of an input longer than MaxInput.
	LimitInput
)

// LimitError reports a reservation that would exceed a limit. Used is the
// counter value before the rejected reservation; Requested is the complete
// atomic reservation that could not be made. For LimitInput, Limit is MaxInput, Used is zero and
// Requested is the length of the input in the position unit; for a stream it is the number of
// units the parser had asked for, or the length of the stream if that is shorter.
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
	if e.Kind == LimitInput {
		return fmt.Sprintf("parse input limit exceeded: limit %d, length %d", e.Limit, e.Requested)
	}
	return fmt.Sprintf("parse budget limit exceeded: limit %d, used %d, requested %d", e.Limit, e.Used, e.Requested)
}

// Is reports whether target is the limit sentinel.
func (e *LimitError) Is(target error) bool { return e != nil && target == ErrLimit }

// ExceedsLimit checks a reservation without computing a potentially
// overflowing used+requested sum. The limit is already resolved: zero, which
// Unlimited resolves to, sets no bound.
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
