package engine

import (
	"testing"
)

var benchmarkNodeJSONBytes []byte

func BenchmarkNodeJSON(b *testing.B) {
	for _, workload := range []struct {
		name string
		root *Node
	}{
		{name: "shallow", root: benchmarkShallowJSONNode()},
		{name: "deep", root: benchmarkDeepJSONNode(512)},
	} {
		b.Run(workload.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data, err := workload.root.MarshalJSON()
				if err != nil {
					b.Fatal(err)
				}
				benchmarkNodeJSONBytes = data
			}
		})
	}
}

func benchmarkShallowJSONNode() *Node {
	children := make([]*Node, 8)
	for i := range children {
		children[i] = &Node{
			kind:  kindOf("Token", "item"),
			Start: int32(i * 3),
			End:   int32(i*3 + 2),
			Text:  "item<&>",
		}
	}
	return &Node{
		kind:     kindOf("Record", "entry"),
		Start:    0,
		End:      24,
		Children: children,
		Fields: Fields{
			{Name: "name", Value: "example"},
			{Name: "count", Value: int64(len(children))},
			{Name: "items", Value: []any{"one", int32(2), true}},
		},
	}
}

func benchmarkDeepJSONNode(depth int) *Node {
	root := &Node{kind: kindOf("Tree", "branch")}
	node := root
	for i := 1; i < depth; i++ {
		child := &Node{kind: kindOf("Tree", "branch"), Start: int32(i), End: int32(i + 1)}
		node.Children = []*Node{child}
		node = child
	}
	node.kind = kindOf("Token", "leaf")
	node.Text = "end"
	return root
}
