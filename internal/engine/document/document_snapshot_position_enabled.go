//go:build pego_snapshot_position_overlays

package document

const snapshotPositionOverlayEnabled = true

// SnapshotNode is a comparable read-only handle. Its zero value denotes nil.
type SnapshotNode struct {
	graph    *snapshotGraph
	ref      *snapshotRecord
	chunks   *snapshotChildChunks
	position *snapshotPositionDelta
}

func newSnapshotHandle(g *snapshotGraph, ref *snapshotRecord, id int) SnapshotNode {
	n := SnapshotNode{graph: g, ref: ref}
	if g.positions != nil {
		n.position = &g.positions[id-1]
	}
	return n
}

// Start returns the inclusive start position.
func (n SnapshotNode) Start() int32 {
	if n.position != nil {
		return n.ref.start + n.position.start
	}
	return n.record().start
}

// End returns the exclusive end position.
func (n SnapshotNode) End() int32 {
	if n.position != nil {
		return n.ref.end + n.position.end
	}
	return n.record().end
}

// Keep version-specific metadata outside the default graph layout.
type snapshotPositionState struct {
	positions []snapshotPositionDelta
}

func (g *snapshotGraph) position(i int, r *snapshotRecord) snapshotPosition {
	if g.positions != nil {
		delta := g.positions[i]
		return snapshotPosition{r.start + delta.start, r.end + delta.end, delta.gen}
	}
	return snapshotPosition{r.start, r.end, r.gen}
}

func (g *snapshotGraph) setPositions(p *parser, next *snapshotGraph) {
	g.positions = make([]snapshotPositionDelta, len(next.nodes))
	for i := range next.nodes {
		p.valueWork(3)
		delta, fits := relativeSnapshotPosition(g.refs[i], next.position(i, &next.nodes[i]))
		if !fits {
			panic("snapshot position delta invariant")
		}
		g.positions[i] = delta
	}
}
