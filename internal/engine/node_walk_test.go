package engine

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ornew/pego/internal/value"
)

func TestWalkEdgeOrderAndSharedParents(t *testing.T) {
	shared := value.Ptr(value.Data{Text: "shared"})
	left := value.Ptr(value.Data{Children: []*value.Node{shared}})
	right := value.Ptr(value.Data{Fields: value.Fields{{Name: "nested", Value: []any{shared, nil, 7}}}})
	root := value.Ptr(value.Data{Children: []*value.Node{left, right, nil}, Fields: value.Fields{{Name: "z", Value: false}, {Name: "a", Value: shared}}})
	var got []string
	var parents []*value.Node
	err := value.Walk(root, func(v value.WalkVisit) (value.WalkAction, error) {
		switch v.Edge.Kind {
		case value.WalkRoot:
			got = append(got, "root")
		case value.WalkChild:
			got = append(got, "child")
		case value.WalkField:
			got = append(got, v.Edge.FieldName)
		case value.WalkListElement:
			got = append(got, "list")
		}
		if v.Node == shared {
			parents = append(parents, v.Parent)
		}
		return value.WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"root", "child", "child", "child", "nested", "list", "list", "list", "child", "a", "z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visits = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(parents, []*value.Node{left, right, root}) {
		t.Fatal("shared edge parents lost")
	}
	if value.D(root).Fields[0].Name != "z" {
		t.Fatal("walk sorted fields in place")
	}
}

func TestWalkCyclesAndControls(t *testing.T) {
	n := &value.Node{}
	value.D(n).Children = []*value.Node{n}
	var cycle *value.CycleError
	err := value.Walk(n, func(value.WalkVisit) (value.WalkAction, error) { return value.WalkContinue, nil })
	if !errors.As(err, &cycle) || len(cycle.Path) != 2 || cycle.Path[1].Kind != value.WalkChild {
		t.Fatalf("cycle = %#v, %v", cycle, err)
	}
	list := make([]any, 1)
	list[0] = list
	n = value.Ptr(value.Data{Fields: value.Fields{{Name: "x", Value: list}}})
	err = value.Walk(n, func(value.WalkVisit) (value.WalkAction, error) { return value.WalkContinue, nil })
	if !errors.As(err, &cycle) || len(cycle.Path) != 3 {
		t.Fatalf("list cycle = %v", err)
	}
	sentinel := errors.New("callback")
	if err := value.Walk(n, func(value.WalkVisit) (value.WalkAction, error) { return value.WalkContinue, sentinel }); err != sentinel {
		t.Fatal(err)
	}
	for _, action := range []value.WalkAction{value.WalkSkip, value.WalkStop} {
		count := 0
		if err := value.Walk(n, func(value.WalkVisit) (value.WalkAction, error) { count++; return action, nil }); err != nil || count != 1 {
			t.Fatalf("control %d = %d, %v", action, count, err)
		}
	}
	count := 0
	if err := value.Walk(nil, func(v value.WalkVisit) (value.WalkAction, error) {
		count++
		if v.Value != nil || v.Node != nil || v.Depth != 0 {
			t.Fatal(v)
		}
		return value.WalkContinue, nil
	}); err != nil || count != 1 {
		t.Fatal(err, count)
	}
}

func TestWalkDeep(t *testing.T) {
	n := &value.Node{}
	const depth = 20000
	for i := 0; i < depth; i++ {
		n = value.Ptr(value.Data{Fields: value.Fields{{Name: "next", Value: []any{n}}}})
	}
	count := 0
	if err := value.Walk(n, func(v value.WalkVisit) (value.WalkAction, error) {
		if v.Node != nil {
			count++
		}
		return value.WalkContinue, nil
	}); err != nil || count != depth+1 {
		t.Fatal(count, err)
	}
}

func TestCursorBoundariesAndUncontainedChildren(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		a, b, z := value.Ptr(value.Data{Start: 1, End: 3}), value.Ptr(value.Data{Start: 3, End: 4}), value.Ptr(value.Data{Start: 3, End: 3})
		root := value.Ptr(value.Data{Start: 20, End: 21, Children: []*value.Node{a, b, z, a}})
		c := Cursor{Root: root, Unit: unit}
		var got []*value.Node
		callback := func(v value.WalkVisit) (value.WalkAction, error) {
			got = append(got, v.Node)
			return value.WalkContinue, nil
		}
		if err := c.At(3, callback); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []*value.Node{b, z}) {
			t.Fatalf("point = %v", got)
		}
		got = nil
		if err := c.Range(1, 3, callback); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []*value.Node{a, a}) {
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
	if err := (Cursor{Unit: PositionUnit(99)}).At(0, func(value.WalkVisit) (value.WalkAction, error) { return value.WalkContinue, nil }); err == nil {
		t.Fatal("invalid unit accepted")
	}
}

func TestWalkSliceIdentityIgnoresCapacity(t *testing.T) {
	x := make([]any, 1, 2)
	x[0] = x[:1:1]
	root := value.Ptr(value.Data{Fields: value.Fields{{Name: "x", Value: x}}})
	visits := 0
	err := value.Walk(root, func(value.WalkVisit) (value.WalkAction, error) {
		visits++
		return value.WalkContinue, nil
	})
	var cycle *value.CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("capacity-limited slice cycle = %v, want CycleError", err)
	}
	want := []value.WalkEdge{{Kind: value.WalkRoot}, {Kind: value.WalkField, FieldName: "x"}, {Kind: value.WalkListElement, ListIndex: 0}}
	if visits != 2 || !reflect.DeepEqual(cycle.Path, want) {
		t.Fatalf("cycle detected after %d visits at %v, want 2 visits at %v", visits, cycle.Path, want)
	}
}
