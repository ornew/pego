package runtime

import (
	"errors"
	"reflect"
	"testing"
)

func TestWalkEdgeOrderAndSharedParents(t *testing.T) {
	shared := &Node{RawText: "shared"}
	left := &Node{RawChildren: []*Node{shared}}
	right := &Node{RawFields: Fields{{"nested", []any{shared, nil, 7}}}}
	root := &Node{RawChildren: []*Node{left, right, nil}, RawFields: Fields{{"z", false}, {"a", shared}}}
	var got []string
	var parents []*Node
	err := Walk(root, func(v WalkVisit) (WalkAction, error) {
		switch v.Edge.Kind {
		case WalkRoot:
			got = append(got, "root")
		case WalkChild:
			got = append(got, "child")
		case WalkField:
			got = append(got, v.Edge.FieldName)
		case WalkListElement:
			got = append(got, "list")
		}
		if v.Node == shared {
			parents = append(parents, v.Parent)
		}
		return WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"root", "child", "child", "child", "nested", "list", "list", "list", "child", "a", "z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visits = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(parents, []*Node{left, right, root}) {
		t.Fatal("shared edge parents lost")
	}
	if root.RawFields[0].Name != "z" {
		t.Fatal("walk sorted fields in place")
	}
}

func TestWalkCyclesAndControls(t *testing.T) {
	n := &Node{}
	n.RawChildren = []*Node{n}
	var cycle *CycleError
	err := Walk(n, func(WalkVisit) (WalkAction, error) { return WalkContinue, nil })
	if !errors.As(err, &cycle) || len(cycle.Path) != 2 || cycle.Path[1].Kind != WalkChild {
		t.Fatalf("cycle = %#v, %v", cycle, err)
	}
	list := make([]any, 1)
	list[0] = list
	n = &Node{RawFields: Fields{{"x", list}}}
	err = Walk(n, func(WalkVisit) (WalkAction, error) { return WalkContinue, nil })
	if !errors.As(err, &cycle) || len(cycle.Path) != 3 {
		t.Fatalf("list cycle = %v", err)
	}
	sentinel := errors.New("callback")
	if err := Walk(n, func(WalkVisit) (WalkAction, error) { return WalkContinue, sentinel }); err != sentinel {
		t.Fatal(err)
	}
	for _, action := range []WalkAction{WalkSkip, WalkStop} {
		count := 0
		if err := Walk(n, func(WalkVisit) (WalkAction, error) { count++; return action, nil }); err != nil || count != 1 {
			t.Fatalf("control %d = %d, %v", action, count, err)
		}
	}
	count := 0
	if err := Walk(nil, func(v WalkVisit) (WalkAction, error) {
		count++
		if v.Value != nil || v.Node != nil || v.Depth != 0 {
			t.Fatal(v)
		}
		return WalkContinue, nil
	}); err != nil || count != 1 {
		t.Fatal(err, count)
	}
}

func TestWalkDeep(t *testing.T) {
	n := &Node{}
	const depth = 20000
	for i := 0; i < depth; i++ {
		n = &Node{RawFields: Fields{{"next", []any{n}}}}
	}
	count := 0
	if err := Walk(n, func(v WalkVisit) (WalkAction, error) {
		if v.Node != nil {
			count++
		}
		return WalkContinue, nil
	}); err != nil || count != depth+1 {
		t.Fatal(count, err)
	}
}

func TestCursorBoundariesAndUncontainedChildren(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		a, b, z := &Node{RawStart: 1, RawEnd: 3}, &Node{RawStart: 3, RawEnd: 4}, &Node{RawStart: 3, RawEnd: 3}
		root := &Node{RawStart: 20, RawEnd: 21, RawChildren: []*Node{a, b, z, a}}
		c := Cursor{Root: root, Unit: unit}
		var got []*Node
		callback := func(v WalkVisit) (WalkAction, error) { got = append(got, v.Node); return WalkContinue, nil }
		if err := c.At(3, callback); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []*Node{b, z}) {
			t.Fatalf("point = %v", got)
		}
		got = nil
		if err := c.Range(1, 3, callback); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []*Node{a, a}) {
			t.Fatal("range must preserve shared edges and exclude upper bound")
		}
		got = nil
		if err := c.Range(3, 3, callback); err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
		if err := c.Range(4, 3, callback); err == nil {
			t.Fatal("reversed range accepted")
		}
		if err := c.At(-1, callback); err == nil {
			t.Fatal("negative position accepted")
		}
	}
	if err := (Cursor{Unit: PositionUnit(99)}).At(0, func(WalkVisit) (WalkAction, error) { return WalkContinue, nil }); err == nil {
		t.Fatal("invalid unit accepted")
	}
}

func TestWalkSliceIdentityIgnoresCapacity(t *testing.T) {
	x := make([]any, 1, 2)
	x[0] = x[:1:1]
	root := &Node{RawFields: Fields{{Name: "x", Value: x}}}
	visits := 0
	err := Walk(root, func(WalkVisit) (WalkAction, error) {
		visits++
		return WalkContinue, nil
	})
	var cycle *CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("capacity-limited slice cycle = %v, want CycleError", err)
	}
	want := []WalkEdge{{Kind: WalkRoot}, {Kind: WalkField, FieldName: "x"}, {Kind: WalkListElement, ListIndex: 0}}
	if visits != 2 || !reflect.DeepEqual(cycle.Path, want) {
		t.Fatalf("cycle detected after %d visits at %v, want 2 visits at %v", visits, cycle.Path, want)
	}
}

// A capture that labels an element of a node with source information is visited as that element
// only; a capture that is not an element is visited as a field.
func TestWalkVisitsCapturedElementsOnce(t *testing.T) {
	elem := &Node{RawText: "e"}
	beside := &Node{RawText: "b"}
	root := &Node{labeled: true, RawChildren: []*Node{elem}, RawFields: Fields{{"x", elem}, {"s", beside}}}
	visits := map[*Node]int{}
	err := Walk(root, func(v WalkVisit) (WalkAction, error) {
		visits[v.Node]++
		return WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if visits[elem] != 1 || visits[beside] != 1 || visits[root] != 1 {
		t.Fatalf("visits = elem %d, beside %d, root %d, want 1 each", visits[elem], visits[beside], visits[root])
	}
}
