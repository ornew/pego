package engine

import "testing"

func TestDocumentChangesDirectoryEqualityHasBoundedScratch(t *testing.T) {
	root := &Node{kind: kindOf("Root", "main"), Children: make([]*Node, 4096)}
	for i := range root.Children {
		root.Children[i] = &Node{kind: kindOf("Leaf", "item"), Start: int32(i), End: int32(i + 1), Text: "a", terminal: true}
	}
	// Keep aliases and a cycle in the wide graph, not only a flat tree.
	root.Fields = Fields{{"self", root}, {"alias", root.Children[0]}}
	a, b := changeGraphSnapshot(t, root), changeGraphSnapshot(t, root.Clone())
	equal := true
	allocations := testing.AllocsPerRun(10, func() { equal = equal && equalChangeSnapshots(a, b) })
	if !equal {
		t.Fatal("equal wide cyclic graph differs")
	}
	// Comparison needs no scratch proportional to width or number of nodes.
	// Capture/setup allocations are deliberately outside this observation.
	if allocations != 0 {
		t.Fatalf("comparison allocated scratch: %.1f allocs/run", allocations)
	}
}

func TestDocumentChangesDirectoryEqualityChecksIDEdges(t *testing.T) {
	leaf := func() *Node { return &Node{kind: kindOf("Leaf", "item"), Text: "x", terminal: true} }
	first, second := leaf(), leaf()
	root := &Node{Children: []*Node{first, second, first}, Fields: Fields{{"link", first}, {"cycle", nil}}}
	root.Fields[1].Value = root
	a := changeGraphSnapshot(t, root)
	changed := root.Clone()
	changed.Children[2] = changed.Children[1]
	b := changeGraphSnapshot(t, changed)
	if a.graph.count() != b.graph.count() {
		t.Fatal("fixture must preserve directory size")
	}
	if equalChangeSnapshots(a, b) {
		t.Fatal("equal-sized graphs lost child alias topology")
	}
	changed = root.Clone()
	changed.Fields[0].Value = changed.Children[1]
	b = changeGraphSnapshot(t, changed)
	if a.graph.count() != b.graph.count() {
		t.Fatal("fixture must preserve directory size")
	}
	if equalChangeSnapshots(a, b) {
		t.Fatal("equal-sized graphs lost node-valued field identity")
	}
}

func TestDocumentChangesDirectoryEqualitySupportsChunkedChildStorage(t *testing.T) {
	root := &Node{Children: make([]*Node, snapshotChildChunkSize+1)}
	for i := range root.Children {
		root.Children[i] = &Node{Text: "x", terminal: true}
	}
	a := changeGraphSnapshot(t, root)
	// Build an equivalent immutable pointer directory with chunked root children.
	// Record identity/storage layout differs; edge IDs and observable output agree.
	b := &Snapshot{graph: snapshotGraph{root: a.graph.root, refs: make([]*snapshotRecord, a.graph.count())}}
	for i := range b.graph.refs {
		b.graph.refs[i] = a.graph.at(i)
	}
	chunked := *a.graph.at(0)
	ids := chunked.children
	chunked.children = nil
	chunked.childChunked = true
	b.graph.refs[0] = &chunked
	b.graph.childChunks = map[*snapshotRecord]*snapshotChildChunks{&chunked: {parts: [][]int{ids[:snapshotChildChunkSize], ids[snapshotChildChunkSize:]}, length: len(ids)}}
	if !equalChangeSnapshots(a, b) {
		t.Fatal("equivalent child storage differs")
	}
}
