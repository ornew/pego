package pego

import "github.com/ornew/pego/internal/engine"

// WalkAction controls whether a walk descends, skips descendants, or stops.
type WalkAction = engine.WalkAction

const (
	WalkContinue = engine.WalkContinue
	WalkSkip     = engine.WalkSkip
	WalkStop     = engine.WalkStop
)

// WalkVisit describes a live value, its incoming edge and nearest owning node.
// Node is non-nil only for node values. Depth counts edges from the root.
type WalkVisit = engine.WalkVisit

// WalkEdge identifies a root, child, field, or nested []any list edge.
type WalkEdge = engine.WalkEdge

// WalkEdgeKind identifies the kind of a visit's incoming edge.
type WalkEdgeKind = engine.WalkEdgeKind

const (
	WalkRoot        = engine.WalkRoot
	WalkChild       = engine.WalkChild
	WalkField       = engine.WalkField
	WalkListElement = engine.WalkListElement
)

// CycleError reports the root-to-back-edge path of a cyclic node or []any graph.
type CycleError = engine.CycleError

// Cursor selects node spans in an explicitly supplied input position unit.
// At selects a point; Range selects a half-open range. It does not infer or
// convert units and does not prune children based on a parent's span.
type Cursor = engine.Cursor

// Walk visits a live node graph in pre-order: children in slice order, then
// fields in name order, and nested []any elements in index order. Scalar and nil
// values are visits too. Shared subtrees are visited once per incoming edge;
// active-path cycles return CycleError. The graph must not be mutated concurrently.
// A callback error returns unchanged; WalkStop succeeds and WalkSkip skips descendants.
func Walk(root *Node, visit func(WalkVisit) (WalkAction, error)) error {
	return engine.Walk(root, visit)
}
