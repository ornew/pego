package query

import (
	"sort"

	"github.com/ornew/pego/internal/value"
)

// graphVisit is the adapter boundary for a future common graph walker. Each event contains a
// value, the carrier that immediately contains it and an owned root-relative path. Visits occur
// in preorder per edge: the elements of a tuple, a list or a node's value, then a node's captures
// that are not elements, by name, and the fields of a struct by name. Boxes are seen through. The
// walker rejects active-path cycles and charges before discovery.
type graphVisit struct {
	Value  any
	parent *value.Node
	path   []Edge
}
type visitFrame struct {
	visit graphVisit
	leave bool
}

// visitGraph calls visit for every value reachable from root, in the order graphVisit
// describes, charging the work to b.
func visitGraph(root *value.Node, b *budget, visit func(graphVisit) error) error {
	frames := []visitFrame{{visit: graphVisit{Value: value.UnboxNode(root)}}}
	nodes := map[*value.Node]bool{}
	for len(frames) > 0 {
		f := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		v := f.visit
		if f.leave {
			delete(nodes, v.Value.(*value.Node))
			continue
		}
		if err := b.charge(uint64(1 + len(v.path))); err != nil {
			return err
		}
		if _, err := valueCategory(v.Value); err != nil {
			return err
		}
		x, ok := v.Value.(*value.Node)
		if ok && x != nil {
			if nodes[x] {
				return ErrCycle
			}
			nodes[x] = true
		}
		if err := visit(v); err != nil {
			return err
		}
		if !ok || x == nil {
			continue
		}
		frames = append(frames, visitFrame{visit: v, leave: true})
		push := func(val any, e Edge) error {
			if err := b.charge(uint64(len(v.path) + 1)); err != nil {
				return err
			}
			frames = append(frames, visitFrame{visit: graphVisit{value.UnboxValue(val), x, appendPath(v.path, e)}})
			return nil
		}
		switch value.ShapeOf(x) {
		case value.ShapeStruct:
			if err := b.charge(uint64(len(value.D(x).Fields))); err != nil {
				return err
			}
			fields := append(value.Fields(nil), value.D(x).Fields...)
			sort.SliceStable(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
			for i := len(fields) - 1; i >= 0; i-- {
				if err := push(fields[i].Value, Edge{Kind: "field", Name: fields[i].Name}); err != nil {
					return err
				}
			}
		case value.ShapeTuple, value.ShapeList, value.ShapeTupleValue, value.ShapeListValue:
			if err := b.charge(uint64(len(value.D(x).Children) + len(value.D(x).Fields))); err != nil {
				return err
			}
			if value.ShapeOf(x) == value.ShapeTuple && len(value.D(x).Fields) > 0 {
				locations := value.CaptureLocations(x)
				for i := len(locations) - 1; i >= 0; i-- {
					if c := locations[i]; c.Side {
						if err := push(c.Value, Edge{Kind: "capture", Name: c.Name}); err != nil {
							return err
						}
					}
				}
			}
			for i := len(value.D(x).Children) - 1; i >= 0; i-- {
				if err := push(value.D(x).Children[i], Edge{Kind: "element", Index: i}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
