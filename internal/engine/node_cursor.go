package engine

import (
	"fmt"

	"github.com/ornew/pego/internal/value"
)

// Cursor filters edge-oriented node visits using spans in Unit. Unit must match
// the parse that populated Root's positions; it is never inferred or converted.
// Parent spans do not prune traversal. Scalar and list values have no spans.
type Cursor struct {
	Root *value.Node
	Unit PositionUnit
}

func (c Cursor) validate(start, end int32) error {
	if c.Unit != CodePoints && c.Unit != Bytes {
		return fmt.Errorf("pego: invalid cursor unit %d", c.Unit)
	}
	if start < 0 || end < start {
		return fmt.Errorf("pego: invalid cursor range [%d,%d)", start, end)
	}
	return nil
}

// At visits nodes containing position, including zero-width nodes at position.
func (c Cursor) At(position int32, visit func(value.WalkVisit) (value.WalkAction, error)) error {
	if err := c.validate(position, position); err != nil {
		return err
	}
	return c.filter(visit, func(n *value.Node) bool {
		return value.D(n).Start == value.D(n).End && value.D(n).Start == position || value.D(n).Start <= position && position < value.D(n).End
	})
}

// Range visits nodes overlapping [start,end). An empty range matches no nodes.
// Zero-width nodes match when start <= node.Start < end.
func (c Cursor) Range(start, end int32, visit func(value.WalkVisit) (value.WalkAction, error)) error {
	if err := c.validate(start, end); err != nil {
		return err
	}
	return c.filter(visit, func(n *value.Node) bool {
		if start == end {
			return false
		}
		if value.D(n).Start == value.D(n).End {
			return start <= value.D(n).Start && value.D(n).Start < end
		}
		return value.D(n).Start < end && start < value.D(n).End
	})
}

func (c Cursor) filter(visit func(value.WalkVisit) (value.WalkAction, error), matches func(*value.Node) bool) error {
	if visit == nil {
		return fmt.Errorf("pego: nil cursor callback")
	}
	return value.Walk(c.Root, func(v value.WalkVisit) (value.WalkAction, error) {
		if v.Node != nil && matches(v.Node) {
			return visit(v)
		}
		return value.WalkContinue, nil
	})
}
