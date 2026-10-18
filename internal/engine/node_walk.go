package engine

import "fmt"

// WalkAction controls descent after a visit.
type WalkAction uint8

const (
	WalkContinue WalkAction = iota
	WalkSkip
	WalkStop
)

// WalkEdgeKind identifies the incoming edge of a value.
type WalkEdgeKind uint8

const (
	WalkRoot WalkEdgeKind = iota
	WalkChild
	WalkField
	WalkListElement
)

// WalkEdge identifies an incoming edge. Only its Kind's index or name is meaningful.
type WalkEdge struct {
	Kind       WalkEdgeKind
	ChildIndex int
	FieldName  string
	ListIndex  int
}

// WalkVisit describes a live value and its nearest owning node. Depth counts edges.
// Node is populated only for non-nil *Node values; Value retains typed nil leaves.
type WalkVisit struct {
	Value  any
	Node   *Node
	Parent *Node
	Depth  int
	Edge   WalkEdge
}

// CycleError describes an active-path back-edge, including the root and failing edge.
type CycleError struct{ Path []WalkEdge }

func (e *CycleError) Error() string {
	return fmt.Sprintf("pego: tree cycle at depth %d", len(e.Path)-1)
}

type walkTask struct {
	visit    WalkVisit
	exit     bool
	identity any
}

// Walk visits the root, then children in slice order, then fields in sorted name order.
// Nested []any values are visited in index order. Other containers are leaves. Shared
// nodes and lists are visited per edge; only cycles on the active path are rejected.
// Callback errors return unchanged; Stop succeeds; Skip omits the value's descendants.
// The graph must not be mutated concurrently. Walk does not mutate or retain it.
func Walk(root *Node, visit func(WalkVisit) (WalkAction, error)) error {
	if visit == nil {
		return fmt.Errorf("pego: nil walk callback")
	}
	var value any = root
	if root == nil {
		value = nil
	}
	stack := []walkTask{{visit: WalkVisit{Value: value, Node: root, Edge: WalkEdge{Kind: WalkRoot}}}}
	active := make(map[any]bool)
	var path []WalkEdge
	for len(stack) > 0 {
		t := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if t.exit {
			delete(active, t.identity)
			path = path[:len(path)-1]
			continue
		}
		v := t.visit
		var identity any
		switch x := v.Value.(type) {
		case *Node:
			if x != nil {
				identity = x
				v.Node = x
			}
		case []any:
			if len(x) > 0 {
				identity = sliceIdentity(x)
			}
		}
		if identity != nil && active[identity] {
			p := append([]WalkEdge(nil), path...)
			return &CycleError{Path: append(p, v.Edge)}
		}
		action, err := visit(v)
		if err != nil {
			return err
		}
		switch action {
		case WalkStop:
			return nil
		case WalkSkip:
			continue
		case WalkContinue:
		default:
			return fmt.Errorf("pego: invalid walk action %d", action)
		}
		if identity == nil {
			continue
		}
		active[identity] = true
		path = append(path, v.Edge)
		stack = append(stack, walkTask{exit: true, identity: identity})
		push := func(value any, parent *Node, edge WalkEdge) {
			stack = append(stack, walkTask{visit: WalkVisit{Value: value, Parent: parent, Depth: v.Depth + 1, Edge: edge}})
		}
		switch x := v.Value.(type) {
		case *Node:
			fields := x.fieldList().sorted()
			for i := len(fields) - 1; i >= 0; i-- {
				push(fields[i].Value, x, WalkEdge{Kind: WalkField, FieldName: fields[i].Name})
			}
			for i := len(x.children) - 1; i >= 0; i-- {
				push(x.children[i], x, WalkEdge{Kind: WalkChild, ChildIndex: i})
			}
		case []any:
			for i := len(x) - 1; i >= 0; i-- {
				push(x[i], v.Parent, WalkEdge{Kind: WalkListElement, ListIndex: i})
			}
		}
	}
	return nil
}
