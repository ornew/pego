package engine

import "sort"

// queryVisit is the adapter boundary for a future common graph walker. Each event contains a
// value, the carrier that immediately contains it and an owned root-relative path. Visits occur
// in preorder per edge: the elements of a tuple, a list or a node's value, then a node's captures
// that are not elements, by name, and the fields of a struct by name. Boxes are seen through. The
// walker rejects active-path cycles and charges before discovery.
type queryVisit struct {
	value  any
	parent *Node
	path   []QueryEdge
}
type queryVisitFrame struct {
	visit queryVisit
	leave bool
}

func visitQueryGraph(root *Node, b *queryBudget, visit func(queryVisit) error) error {
	frames := []queryVisitFrame{{visit: queryVisit{value: unboxNode(root)}}}
	nodes := map[*Node]bool{}
	for len(frames) > 0 {
		f := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		v := f.visit
		if f.leave {
			delete(nodes, v.value.(*Node))
			continue
		}
		if err := b.charge(uint64(1 + len(v.path))); err != nil {
			return err
		}
		if _, err := queryCategory(v.value); err != nil {
			return err
		}
		x, ok := v.value.(*Node)
		if ok && x != nil {
			if nodes[x] {
				return ErrQueryCycle
			}
			nodes[x] = true
		}
		if err := visit(v); err != nil {
			return err
		}
		if !ok || x == nil {
			continue
		}
		frames = append(frames, queryVisitFrame{visit: v, leave: true})
		push := func(value any, e QueryEdge) error {
			if err := b.charge(uint64(len(v.path) + 1)); err != nil {
				return err
			}
			frames = append(frames, queryVisitFrame{visit: queryVisit{unboxValue(value), x, appendQueryPath(v.path, e)}})
			return nil
		}
		switch x.shape() {
		case shapeStruct:
			if err := b.charge(uint64(len(x.fields))); err != nil {
				return err
			}
			fields := append(Fields(nil), x.fields...)
			sort.SliceStable(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
			for i := len(fields) - 1; i >= 0; i-- {
				if err := push(fields[i].Value, QueryEdge{Kind: "field", Name: fields[i].Name}); err != nil {
					return err
				}
			}
		case shapeTuple, shapeList, shapeTupleValue, shapeListValue:
			if err := b.charge(uint64(len(x.children) + len(x.fields))); err != nil {
				return err
			}
			if x.shape() == shapeTuple && len(x.fields) > 0 {
				locations := captureLocations(x)
				for i := len(locations) - 1; i >= 0; i-- {
					if c := locations[i]; c.side {
						if err := push(c.value, QueryEdge{Kind: "capture", Name: c.name}); err != nil {
							return err
						}
					}
				}
			}
			for i := len(x.children) - 1; i >= 0; i-- {
				if err := push(x.children[i], QueryEdge{Kind: "element", Index: i}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
