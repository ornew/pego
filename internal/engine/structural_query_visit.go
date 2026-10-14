package engine

import (
	"reflect"
	"sort"
)

// queryVisit is the adapter boundary for a future common graph walker. Each
// event contains value, immediate containing Node and an owned root-relative
// path. Visits occur in preorder per edge (children, sorted fields, list indexes).
// The walker rejects active-path node/list cycles and charges before discovery.
type queryVisit struct {
	value  any
	parent *Node
	path   []QueryEdge
}
type queryListIdentity struct {
	pointer uintptr
	length  int
}
type queryVisitFrame struct {
	visit queryVisit
	leave bool
}

func visitQueryGraph(root *Node, b *queryBudget, visit func(queryVisit) error) error {
	frames := []queryVisitFrame{{visit: queryVisit{value: root}}}
	nodes := map[*Node]bool{}
	lists := map[queryListIdentity]bool{}
	for len(frames) > 0 {
		f := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		v := f.visit
		var listID queryListIdentity
		if list, ok := v.value.([]any); ok {
			listID = queryListIdentity{reflect.ValueOf(list).Pointer(), len(list)}
		}
		if f.leave {
			switch x := v.value.(type) {
			case *Node:
				delete(nodes, x)
			case []any:
				delete(lists, listID)
			}
			continue
		}
		if err := b.charge(uint64(1 + len(v.path))); err != nil {
			return err
		}
		if _, err := queryCategory(v.value); err != nil {
			return err
		}
		switch x := v.value.(type) {
		case *Node:
			if x == nil {
				break
			}
			if nodes[x] {
				return ErrQueryCycle
			}
			nodes[x] = true
		case []any:
			if len(x) > 0 {
				if lists[listID] {
					return ErrQueryCycle
				}
				lists[listID] = true
			}
		}
		if err := visit(v); err != nil {
			return err
		}
		switch x := v.value.(type) {
		case *Node:
			if x == nil {
				continue
			}
			if err := b.charge(uint64(len(x.Children) + len(x.Fields))); err != nil {
				return err
			}
			frames = append(frames, queryVisitFrame{visit: v, leave: true})
			fields := append(Fields(nil), x.Fields...)
			sort.SliceStable(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
			for i := len(fields) - 1; i >= 0; i-- {
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return err
				}
				frames = append(frames, queryVisitFrame{visit: queryVisit{fields[i].Value, x, appendQueryPath(v.path, QueryEdge{Kind: "field", Name: fields[i].Name})}})
			}
			for i := len(x.Children) - 1; i >= 0; i-- {
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return err
				}
				frames = append(frames, queryVisitFrame{visit: queryVisit{x.Children[i], x, appendQueryPath(v.path, QueryEdge{Kind: "child", Index: i})}})
			}
		case []any:
			if err := b.charge(uint64(len(x))); err != nil {
				return err
			}
			frames = append(frames, queryVisitFrame{visit: v, leave: true})
			for i := len(x) - 1; i >= 0; i-- {
				if err := b.charge(uint64(len(v.path) + 1)); err != nil {
					return err
				}
				frames = append(frames, queryVisitFrame{visit: queryVisit{x[i], v.parent, appendQueryPath(v.path, QueryEdge{Kind: "list", Index: i})}})
			}
		}
	}
	return nil
}
