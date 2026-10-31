package document

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/value"
)

func snapshotVariants(t *testing.T, run func(*testing.T, core.ParseOptions)) {
	t.Helper()
	for _, backend := range []core.Backend{core.Closure, core.Bytecode, core.BytecodeIterative} {
		for _, unit := range []core.PositionUnit{core.CodePoints, core.Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) { run(t, core.ParseOptions{Backend: backend, Unit: unit}) })
		}
	}
}

func TestSnapshotGraphIdentityAndOwnership(t *testing.T) {
	leaf := value.Ptr(value.Data{Kind: value.KindOf("Leaf", "leaf"), Start: 2, End: 3, Text: "x", Terminal: true, Children: []*value.Node{}, Fields: value.Fields{}})
	root := value.Ptr(value.Data{Kind: value.SeqKind, Start: 1, End: 4, Children: []*value.Node{leaf, nil, leaf}})
	value.D(root).Fields = value.Fields{{Name: "self", Value: root}, {Name: "leaf", Value: leaf}, {Name: "typedNil", Value: (*value.Node)(nil)}, {Name: "nil", Value: nil}, {Name: "int", Value: 42}, {Name: "text", Value: "hi"}, {Name: "bool", Value: true}}
	g, err := freezeSnapshotGraph(&core.Parser{}, root)
	if err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{graph: g}
	n := s.Root()
	if !n.Valid() || n.Type() != value.TypeTuple || n.Start() != 1 || n.End() != 4 || n.ChildCount() != 3 {
		t.Fatal("metadata")
	}
	if n.Child(0) != n.Child(2) || n.Child(1).Valid() || n.Child(0).Rule() != "leaf" || !n.Child(0).IsTerminal() {
		t.Fatal("identity")
	}
	if self, _ := n.Field("self"); self != n {
		t.Fatal("cycle")
	}
	if v, ok := n.Field("typedNil"); !ok || v != (SnapshotNode{}) {
		t.Fatal("typed nil")
	}
	if v, ok := n.Field("nil"); !ok || v != nil {
		t.Fatal("nil")
	}
	if _, ok := n.Field("absent"); ok {
		t.Fatal("absent")
	}
	fs := n.Fields()
	fs[0].Name = "changed"
	fs[0].Value = nil
	if v, ok := n.Field("self"); !ok || v != n {
		t.Fatal("field slice alias")
	}
	value.D(root).Start = 99
	value.D(root).Children[0] = nil
	value.D(root).Fields[0].Value = nil
	value.D(leaf).Text = "changed"
	if n.Start() != 1 || n.Child(0).Text() != "x" {
		t.Fatal("legacy alias")
	}
	a, b := s.Materialize(), s.Materialize()
	if a == b || value.D(a).Children[0] == value.D(b).Children[0] || value.D(a).Children[0] != value.D(a).Children[2] || value.D(a).Children[1] != nil || value.D(a).Fields[0].Value != a {
		t.Fatal("materialized graph")
	}
	if value.D(value.D(a).Children[0]).Children == nil || value.D(value.D(a).Children[0]).Fields == nil {
		t.Fatal("allocated empty lost")
	}
	if v, ok := value.D(a).Fields[2].Value.(*value.Node); !ok || v != nil {
		t.Fatal("materialized typed nil")
	}
	value.D(value.D(a).Children[0]).Text = "mutated"
	if n.Child(0).Text() != "x" || value.D(value.D(b).Children[0]).Text != "x" {
		t.Fatal("materialized alias")
	}
	var z SnapshotNode
	if z.Valid() || z.Type() != "" || z.Rule() != "" || z.Start() != 0 || z.End() != 0 || z.Text() != "" || z.IsTerminal() || z.ChildCount() != 0 || z.Fields() != nil {
		t.Fatal("zero metadata")
	}
	for _, handle := range []SnapshotNode{z, n} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("bad child index did not panic")
				}
			}()
			handle.Child(handle.ChildCount())
		}()
	}
}

func TestSnapshotGraphDeepAndUnsupported(t *testing.T) {
	root := &value.Node{}
	at := root
	for i := 0; i < 20000; i++ {
		next := &value.Node{}
		value.D(at).Children = []*value.Node{next}
		at = next
	}
	value.D(at).Fields = value.Fields{{Name: "back", Value: root}}
	g, err := freezeSnapshotGraph(&core.Parser{}, root)
	if err != nil || len(g.nodes) != 20001 {
		t.Fatal("deep capture", err)
	}
	s := &Snapshot{graph: g}
	at = s.Materialize()
	for i := 0; i < 20000; i++ {
		at = value.D(at).Children[0]
	}
	if value.D(value.D(at).Fields[0].Value.(*value.Node)).Children[0] == nil {
		t.Fatal("deep cycle")
	}
	type namedInt int
	for _, val := range []any{namedInt(1), []int(nil), map[string]int(nil), (*int)(nil), (chan int)(nil), (func())(nil)} {
		child := value.Ptr(value.Data{Fields: value.Fields{{Name: "bad\"name", Value: val}}})
		root := value.Ptr(value.Data{Children: []*value.Node{child}, Fields: value.Fields{{Name: "other", Value: val}}})
		_, err := freezeSnapshotGraph(&core.Parser{}, root)
		if err == nil || err.ValueType != reflect.TypeOf(val) || err.Path != `root.Children[0].Fields[0]("bad\"name")` {
			t.Fatalf("rejection %v", err)
		}
	}
}
