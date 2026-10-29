package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unsafe"
	"weak"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

// Snapshot owns an immutable parse outcome for one document revision. Readers
// may use it concurrently with later edits of its single-owner Document.
type Snapshot struct {
	version    DocumentVersion
	unit       PositionUnit
	text       string
	graph      snapshotGraph
	parseError error
	stats      Stats
}

type snapshotGraph struct {
	snapshotPositionState
	nodes       []snapshotRecord
	refs        []*snapshotRecord                        // candidate mode; immutable records may come from older graphs
	childChunks map[*snapshotRecord]*snapshotChildChunks // only records with shared child runs
	root        int
}

type snapshotPosition struct {
	start, end int32
	gen        uint32
}

type snapshotPositionDelta struct {
	start, end int32
	gen        uint32
}

func relativeSnapshotPosition(r *snapshotRecord, pos snapshotPosition) (snapshotPositionDelta, bool) {
	start, end := int64(pos.start)-int64(r.start), int64(pos.end)-int64(r.end)
	if start < -(1<<31) || start > (1<<31)-1 || end < -(1<<31) || end > (1<<31)-1 {
		return snapshotPositionDelta{}, false
	}
	return snapshotPositionDelta{int32(start), int32(end), pos.gen}, true
}

func (g *snapshotGraph) count() int {
	if g.refs != nil {
		return len(g.refs)
	}
	return len(g.nodes)
}

func (g *snapshotGraph) at(i int) *snapshotRecord {
	if g.refs != nil {
		return g.refs[i]
	}
	return &g.nodes[i]
}

type snapshotRecord struct {
	kind                          *value.NodeKind
	start, end                    int32
	text                          string
	children                      []int
	fields                        value.Fields
	fresh, terminal, childChunked bool
	gen                           uint32
}

const snapshotChildChunkSize = 256

type snapshotChildChunks struct {
	parts  [][]int
	length int
}

func (g *snapshotGraph) childCount(r *snapshotRecord) int {
	if r.childChunked {
		return g.childChunks[r].length
	}
	return len(r.children)
}

func (g *snapshotGraph) hasChildren(r *snapshotRecord) bool {
	return r.children != nil || r.childChunked
}

func (g *snapshotGraph) childID(r *snapshotRecord, i int) int {
	if r.childChunked {
		return g.childChunks[r].parts[i/snapshotChildChunkSize][i%snapshotChildChunkSize]
	}
	return r.children[i]
}

func (g *snapshotGraph) childChunk(r *snapshotRecord, i, length int) []int {
	if r.childChunked {
		chunks := g.childChunks[r]
		if i >= len(chunks.parts) || len(chunks.parts[i]) != length {
			return nil
		}
		return chunks.parts[i]
	}
	start := i * snapshotChildChunkSize
	if start+length > len(r.children) {
		return nil
	}
	return r.children[start : start+length]
}

type snapshotRef int

// SnapshotField holds an immutable scalar or SnapshotNode, never a mutable Node.
type SnapshotField struct {
	Name  string
	Value any
}

// SnapshotCaptureError reports an unsupported dynamic field type. It retains
// type metadata and an owned parse error, never the unsupported field value.
type SnapshotCaptureError struct {
	Path       string
	ValueType  reflect.Type
	ParseError error
}

func (e *SnapshotCaptureError) Error() string {
	return fmt.Sprintf("cannot capture snapshot field %s with type %v", e.Path, e.ValueType)
}
func (e *SnapshotCaptureError) Unwrap() error { return e.ParseError }

// Version returns the captured text revision.
func (s *Snapshot) Version() DocumentVersion { return s.version }

// Unit returns the position unit of the captured graph.
func (s *Snapshot) Unit() PositionUnit { return s.unit }

// Text returns the captured text.
func (s *Snapshot) Text() string { return s.text }

// Root returns the captured root, or the zero handle for a nil result.
func (s *Snapshot) Root() SnapshotNode { return snapshotHandle(&s.graph, s.graph.root) }

// Stats returns the evaluation and memo counts for this capture's parse.
func (s *Snapshot) Stats() Stats { return s.stats }

// Error returns a defensive copy of the captured diagnostic.
func (s *Snapshot) Error() error { return copySnapshotError(nil, s.parseError) }

func snapshotHandle(g *snapshotGraph, id int) SnapshotNode {
	if id == 0 {
		return SnapshotNode{}
	}
	ref := g.at(id - 1)
	n := newSnapshotHandle(g, ref, id)
	if ref.childChunked {
		n.chunks = g.childChunks[ref]
	}
	return n
}

var zeroSnapshotRecord snapshotRecord

func (n SnapshotNode) record() *snapshotRecord {
	if !n.Valid() {
		return &zeroSnapshotRecord
	}
	return n.ref
}

// Valid reports whether the handle denotes a non-nil node.
func (n SnapshotNode) Valid() bool { return n.graph != nil && n.ref != nil }

// Type returns the node type, or an empty string for nil.
func (n SnapshotNode) Type() string { return n.record().kind.TypeName() }

// Rule returns the originating rule, or an empty string for nil.
func (n SnapshotNode) Rule() string { return n.record().kind.RuleName() }

// Text returns terminal text.
func (n SnapshotNode) Text() string { return n.record().text }

// IsTerminal reports whether the node is terminal.
func (n SnapshotNode) IsTerminal() bool { return n.record().terminal }

// ChildCount includes nil child slots.
func (n SnapshotNode) ChildCount() int {
	if !n.Valid() {
		return 0
	}
	if n.chunks != nil {
		return n.chunks.length
	}
	return len(n.ref.children)
}

// Child returns an indexed child; an invalid index panics like slice indexing.
func (n SnapshotNode) Child(i int) SnapshotNode {
	if n.chunks != nil {
		return snapshotHandle(n.graph, n.chunks.parts[i/snapshotChildChunkSize][i%snapshotChildChunkSize])
	}
	return snapshotHandle(n.graph, n.record().children[i])
}
func (n SnapshotNode) fieldValue(v any) any {
	if id, ok := v.(snapshotRef); ok {
		return snapshotHandle(n.graph, int(id))
	}
	return v
}

// Field distinguishes absent fields from present nil values.
func (n SnapshotNode) Field(name string) (any, bool) {
	v, ok := n.record().fields.Get(name)
	return n.fieldValue(v), ok
}

// Fields returns an owned slice in stored order. Typed nil nodes remain zero handles.
func (n SnapshotNode) Fields() []SnapshotField {
	fs := n.record().fields
	if fs == nil {
		return nil
	}
	out := make([]SnapshotField, len(fs))
	for i, f := range fs {
		out[i] = SnapshotField{f.Name, n.fieldValue(f.Value)}
	}
	return out
}

// Materialize makes an independent mutable graph, preserving sharing, cycles,
// nil child slots, and nil versus allocated-empty slices.
func (s *Snapshot) Materialize() *value.Node {
	if s.graph.root == 0 {
		return nil
	}
	nodes := make([]*value.Node, s.graph.count())
	for i := range nodes {
		r := s.graph.at(i)
		pos := s.graph.position(i, r)
		nodes[i] = value.Ptr(value.Data{Kind: r.kind, Start: pos.start, End: pos.end, Text: r.text, Fresh: r.fresh, Terminal: r.terminal, Gen: pos.gen})
	}
	node := func(id int) *value.Node {
		if id == 0 {
			return nil
		}
		return nodes[id-1]
	}
	for i := range nodes {
		r := s.graph.at(i)
		n := nodes[i]
		if r.childChunked {
			chunks := s.graph.childChunks[r]
			value.D(n).Children = make([]*value.Node, chunks.length)
			for part, ids := range chunks.parts {
				for j, id := range ids {
					value.D(n).Children[part*snapshotChildChunkSize+j] = node(id)
				}
			}
		} else if r.children != nil {
			value.D(n).Children = make([]*value.Node, len(r.children))
			for j, id := range r.children {
				value.D(n).Children[j] = node(id)
			}
		}
		if r.fields != nil {
			value.D(n).Fields = make(value.Fields, len(r.fields))
			for j, f := range r.fields {
				if id, ok := f.Value.(snapshotRef); ok {
					f.Value = node(int(id))
				}
				value.D(n).Fields[j] = f
			}
		}
	}
	return node(s.graph.root)
}

// snapshotContext observes actual cancellation without adding a field or a
// branch to ordinary parser controllers. It is private to a capture invocation.
type snapshotContext struct {
	context.Context
	interrupted bool
}

func (c *snapshotContext) Err() error {
	err := c.Context.Err()
	c.interrupted = c.interrupted || err != nil
	return err
}

// SnapshotWith parses and freezes the current text with fully resolved options.
// Cancellation and work limits cover both phases; interruption returns nil.
func (d *Document) SnapshotWith(o ParseOptions) (*Snapshot, error) {
	return d.snapshotWith(o, nil)
}

func (d *Document) snapshotWith(o ParseOptions, legacy **value.Node) (*Snapshot, error) {
	if d.prog.HasMissing || o.MissingTokens != 0 {
		return nil, compile.MissingUnsupported("Document parsing")
	}
	var observed *snapshotContext
	if o.Context != nil {
		observed = &snapshotContext{Context: o.Context}
		o.Context = observed
	}
	var result *Snapshot
	output := OutputUnavailable
	_, err := d.parseWithCapture(o, func(p *parser, root *value.Node, parseErr error) error {
		if observed != nil && observed.interrupted || errors.Is(parseErr, limits.ErrWorkLimit) {
			return parseErr
		}
		p.control.check()
		err := copySnapshotError(p, parseErr)
		graph, captureErr := freezeSnapshotGraph(p, root)
		if captureErr != nil {
			captureErr.ParseError = err
			p.control.check()
			return captureErr
		}
		if snapshotSharingEnabled && graph.root != 0 {
			graph = shareSnapshotGraph(p, d.previousSnapshot.Value(), graph)
		}
		// input.text may lazily build tables. Keep that work private to this
		// capture, rather than mutating Document-owned input during freezing.
		in := p.input
		in.control = p.control
		if in.pieces != nil {
			pieces := *in.pieces
			in.pieces = &pieces
		}
		text := in.text(0, in.loaded())
		// Always copy the full text through bounded input reservations.
		var out strings.Builder
		for off := 0; off < len(text); {
			n := min(4096, len(text)-off)
			p.control.charge(uint64(n), 1)
			if off == 0 {
				out.Grow(len(text))
			}
			out.WriteString(text[off : off+n])
			off += n
		}
		returnedError := copySnapshotError(p, err)
		p.control.check()
		candidate := &Snapshot{version: d.Version(), unit: d.Unit(), text: out.String(), graph: graph, parseError: err, stats: p.reportedStats()}
		nextOutput := OutputMayHaveChanged
		if d.changes != nil && d.changes.previous != nil {
			nextOutput = OutputChanged
			if equalChangeSnapshotsControlled(p, d.changes.previous, candidate) {
				nextOutput = OutputUnchanged
			}
		}
		p.control.check()
		result, output = candidate, nextOutput
		if legacy != nil {
			*legacy = root
		}
		if snapshotSharingEnabled {
			d.previousSnapshot = weak.Make(&result.graph)
		}
		return returnedError
	})
	d.recordOutcome(result, output)
	return result, err
}

func snapshotStrings(p *parser, in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	if p == nil {
		copy(out, in)
	} else {
		copyWork(p, out, in)
	}
	return out
}
func copySnapshotError(p *parser, err error) error {
	syntax := func(e *SyntaxError) *SyntaxError {
		if e == nil {
			return nil
		}
		if p != nil {
			p.stepWork()
		}
		out := *e
		out.Expected, out.Messages = snapshotStrings(p, e.Expected), snapshotStrings(p, e.Messages)
		return &out
	}
	switch e := err.(type) {
	case nil:
		return nil
	case *SyntaxError:
		return syntax(e)
	case SyntaxErrors:
		if e == nil {
			return SyntaxErrors(nil)
		}
		out := make(SyntaxErrors, len(e))
		for i, x := range e {
			out[i] = syntax(x)
		}
		return out
	case *limits.WorkLimitError:
		if e == nil {
			return (*limits.WorkLimitError)(nil)
		}
		out := *e
		return &out
	default:
		// Current non-syntax engine errors are immutable formatted messages.
		// Interrupted context errors never reach successful capture.
		return err
	}
}

type snapshotEdge struct {
	child bool
	index int
	name  string
}
type snapshotFrame struct {
	node     *value.Node
	id, next int
	edge     snapshotEdge
}

// freezeSnapshotGraph registers identities before following their edges. The
// explicit DFS stack gives deterministic child-then-field rejection paths and
// supports arbitrary depth and caller-created cycles without recursion.
func freezeSnapshotGraph(p *parser, root *value.Node) (snapshotGraph, *SnapshotCaptureError) {
	var g snapshotGraph
	if root == nil {
		return g, nil
	}
	seen := make(map[*value.Node]int)
	register := func(n *value.Node) (int, bool) {
		if n == nil {
			return 0, false
		}
		if id, ok := seen[n]; ok {
			return id, false
		}
		p.stepWork()
		r := snapshotRecord{kind: value.D(n).Kind, start: value.D(n).Start, end: value.D(n).End, text: value.D(n).Text, fresh: value.D(n).Fresh, terminal: value.D(n).Terminal, gen: value.D(n).Gen}
		if value.D(n).Children != nil {
			r.children = make([]int, len(value.D(n).Children))
		}
		if value.D(n).Fields != nil {
			r.fields = make(value.Fields, len(value.D(n).Fields))
		}
		g.nodes = growWork(p, g.nodes, 1)
		g.nodes = append(g.nodes, r)
		id := len(g.nodes)
		seen[n] = id
		return id, true
	}
	g.root, _ = register(root)
	stack := []snapshotFrame{{node: root, id: g.root}}
	for len(stack) > 0 {
		p.stepWork()
		f := &stack[len(stack)-1]
		var child *value.Node
		var edge snapshotEdge
		if f.next < len(value.D(f.node).Children) {
			edge = snapshotEdge{child: true, index: f.next}
			child = value.D(f.node).Children[f.next]
		} else {
			i := f.next - len(value.D(f.node).Children)
			if i >= len(value.D(f.node).Fields) {
				stack = stack[:len(stack)-1]
				continue
			}
			field := value.D(f.node).Fields[i]
			edge = snapshotEdge{index: i, name: field.Name}
			var val any
			switch v := field.Value.(type) {
			case *value.Node:
				child = v
			case nil, int, string, bool:
				val = v
			default:
				return snapshotGraph{}, &SnapshotCaptureError{Path: snapshotFieldPath(p, stack, edge), ValueType: reflect.TypeOf(v)}
			}
			g.nodes[f.id-1].fields[i] = value.NodeField{Name: field.Name, Value: val}
		}
		id, fresh := register(child)
		if edge.child {
			g.nodes[f.id-1].children[edge.index] = id
		} else if _, ok := value.D(f.node).Fields[edge.index].Value.(*value.Node); ok {
			g.nodes[f.id-1].fields[edge.index].Value = snapshotRef(id)
		}
		f.next++
		if fresh {
			stack = growWork(p, stack, 1)
			stack = append(stack, snapshotFrame{node: child, id: id, edge: edge})
		}
	}
	return g, nil
}
func snapshotFieldPath(p *parser, stack []snapshotFrame, edge snapshotEdge) string {
	var out strings.Builder
	out.WriteString("root")
	write := func(e snapshotEdge) {
		p.stepWork()
		if e.child {
			out.WriteString(".Children[")
			out.WriteString(strconv.Itoa(e.index))
			out.WriteByte(']')
			return
		}
		out.WriteString(".Fields[")
		out.WriteString(strconv.Itoa(e.index))
		out.WriteString("](")
		// Quote one rune at a time so even caller-supplied long field names
		// remain interruptible while building the rejection path.
		out.WriteByte('"')
		for _, r := range e.name {
			p.stepWork()
			q := strconv.QuoteRune(r)
			if r == '\'' {
				q = "'"
			} else if r == '"' {
				q = `\"`
			} else {
				q = q[1 : len(q)-1]
			}
			out.WriteString(q)
		}
		out.WriteString("\")")
	}
	for _, f := range stack[1:] {
		write(f.edge)
	}
	write(edge)
	return out.String()
}

// shareSnapshotGraph reuses immutable records after freezing the current parse.
// The default requires exact metadata; the private position-overlay candidate
// keeps changed metadata separately. No mutable Node or parser slice is shared.
func shareSnapshotGraph(p *parser, previous *snapshotGraph, next snapshotGraph) snapshotGraph {
	if previous == nil || previous.root != next.root {
		return next
	}
	matches := make([]uint64, (len(next.nodes)+63)/64)
	ownedCount := 0
	// Keep the flat-record path free of sidecar lookups and comparator wrappers.
	if snapshotPositionOverlayEnabled {
		relative := make([]uint64, len(matches))
		relativeCount := 0
		for i := range next.nodes {
			p.stepWork()
			if i < previous.count() && sameSnapshotPayload(p, previous, previous.at(i), &next, &next.nodes[i]) {
				old := previous.position(i, previous.at(i))
				current := next.position(i, &next.nodes[i])
				if old == current {
					matches[i/64] |= 1 << uint(i%64)
					continue
				}
				if _, fits := relativeSnapshotPosition(previous.at(i), current); fits {
					relative[i/64] |= 1 << uint(i%64)
					relativeCount++
				}
			}
			ownedCount++
		}
		// This is an allocation heuristic, not a retained-heap guarantee: a
		// pointer to one record can keep its entire old backing array alive.
		if uint64(relativeCount)*uint64(unsafe.Sizeof(snapshotRecord{})) > uint64(len(next.nodes))*uint64(unsafe.Sizeof(snapshotPositionDelta{}))*4/3 {
			for i := range matches {
				p.stepWork()
				matches[i] |= relative[i]
			}
			ownedCount -= relativeCount
		}
	} else if previous.childChunks == nil {
		for i := range next.nodes {
			p.stepWork()
			if i < previous.count() && sameFlatSnapshotRecord(p, previous.at(i), &next.nodes[i]) {
				matches[i/64] |= 1 << uint(i%64)
			} else {
				ownedCount++
			}
		}
	} else {
		for i := range next.nodes {
			p.stepWork()
			if i < previous.count() && sameSnapshotRecord(p, previous, previous.at(i), &next, &next.nodes[i]) {
				matches[i/64] |= 1 << uint(i%64)
			} else {
				ownedCount++
			}
		}
	}
	// The per-version pointer directory costs one word per node. Keep the
	// contiguous reference graph when too few records can be reused.
	if len(next.nodes) < 128 || (len(next.nodes)-ownedCount)*4 < len(next.nodes) {
		return next
	}
	shared := snapshotGraph{refs: make([]*snapshotRecord, len(next.nodes)), root: next.root}
	shared.nodes = make([]snapshotRecord, ownedCount)
	ownedAt := 0
	needsPositions := false
	for i := range next.nodes {
		p.stepWork()
		if matches[i/64]&(1<<uint(i%64)) != 0 {
			ref := previous.at(i)
			shared.refs[i] = ref
			if snapshotPositionOverlayEnabled && (ref.start != next.nodes[i].start || ref.end != next.nodes[i].end || ref.gen != next.nodes[i].gen) {
				needsPositions = true
			}
			if ref.childChunked {
				chunks := previous.childChunks[ref]
				if shared.childChunks == nil {
					shared.childChunks = make(map[*snapshotRecord]*snapshotChildChunks)
				}
				shared.childChunks[ref] = chunks
			}
		} else {
			shared.nodes[ownedAt] = next.nodes[i]
			ref := &shared.nodes[ownedAt]
			shared.refs[i] = ref
			if snapshotChildChunksEnabled && i < previous.count() {
				if chunks := shareSnapshotChildren(p, previous, previous.at(i), ref); chunks != nil {
					if shared.childChunks == nil {
						shared.childChunks = make(map[*snapshotRecord]*snapshotChildChunks)
					}
					shared.childChunks[ref] = chunks
				}
			}
			ownedAt++
		}
	}
	if snapshotPositionOverlayEnabled && needsPositions {
		shared.setPositions(p, &next)
	}
	return shared
}

func sameSnapshotPayload(p *parser, ag *snapshotGraph, a *snapshotRecord, bg *snapshotGraph, b *snapshotRecord) bool {
	if a.kind != b.kind || a.fresh != b.fresh || a.terminal != b.terminal ||
		ag.hasChildren(a) != bg.hasChildren(b) || (a.fields == nil) != (b.fields == nil) ||
		ag.childCount(a) != bg.childCount(b) || len(a.fields) != len(b.fields) || len(a.text) != len(b.text) {
		return false
	}
	if p.compareText(a.text, b.text) != 0 || !sameSnapshotChildren(p, ag, a, bg, b) {
		return false
	}
	for i, f := range a.fields {
		p.stepWork()
		other := b.fields[i]
		if p.compareText(f.Name, other.Name) != 0 || !sameSnapshotFieldValue(p, f.Value, other.Value) {
			return false
		}
	}
	return true
}

// shareSnapshotChildren keeps only immutable child-ID runs. A reused view may
// point into an older flat child slice, but never into an old graph or parser.
// Changed runs own exact-size backing arrays, so later snapshots do not retain
// a whole discarded child slice through one changed run.
func shareSnapshotChildren(p *parser, previousGraph *snapshotGraph, previous, next *snapshotRecord) *snapshotChildChunks {
	if len(next.children) < 4*snapshotChildChunkSize || !previousGraph.hasChildren(previous) {
		return nil
	}
	count := (len(next.children) + snapshotChildChunkSize - 1) / snapshotChildChunkSize
	equal := make([]bool, count)
	matches := 0
	for i := range equal {
		start := i * snapshotChildChunkSize
		end := min(start+snapshotChildChunkSize, len(next.children))
		p.control.charge(0, uint64(end-start))
		old := previousGraph.childChunk(previous, i, end-start)
		if len(old) != 0 && slices.Equal(old, next.children[start:end]) {
			equal[i] = true
			matches++
		}
	}
	if matches*4 < count*3 {
		return nil
	}
	chunks := make([][]int, count)
	for i := range chunks {
		p.stepWork()
		start := i * snapshotChildChunkSize
		end := min(start+snapshotChildChunkSize, len(next.children))
		if equal[i] {
			chunks[i] = previousGraph.childChunk(previous, i, end-start)
		} else {
			chunks[i] = make([]int, end-start)
			copyWork(p, chunks[i], next.children[start:end])
		}
	}
	length := len(next.children)
	next.children = nil
	next.childChunked = true
	return &snapshotChildChunks{parts: chunks, length: length}
}

func sameSnapshotRecord(p *parser, ag *snapshotGraph, a *snapshotRecord, bg *snapshotGraph, b *snapshotRecord) bool {
	if !a.childChunked && !b.childChunked {
		return sameFlatSnapshotRecord(p, a, b)
	}
	if a.kind != b.kind || a.start != b.start || a.end != b.end ||
		a.fresh != b.fresh || a.terminal != b.terminal ||
		a.gen != b.gen || ag.hasChildren(a) != bg.hasChildren(b) ||
		(a.fields == nil) != (b.fields == nil) || ag.childCount(a) != bg.childCount(b) ||
		len(a.fields) != len(b.fields) || len(a.text) != len(b.text) {
		return false
	}
	if p.compareText(a.text, b.text) != 0 {
		return false
	}
	// A large repetition root can have many edges even when no node changed.
	// Charge and compare bounded spans so a controller can interrupt the scan.
	if !sameSnapshotChildren(p, ag, a, bg, b) {
		return false
	}
	for i, f := range a.fields {
		p.stepWork()
		other := b.fields[i]
		if p.compareText(f.Name, other.Name) != 0 || !sameSnapshotFieldValue(p, f.Value, other.Value) {
			return false
		}
	}
	return true
}

func sameFlatSnapshotRecord(p *parser, a, b *snapshotRecord) bool {
	if a.kind != b.kind || a.start != b.start || a.end != b.end ||
		a.fresh != b.fresh || a.terminal != b.terminal ||
		a.gen != b.gen || (a.children == nil) != (b.children == nil) ||
		(a.fields == nil) != (b.fields == nil) || len(a.children) != len(b.children) ||
		len(a.fields) != len(b.fields) || len(a.text) != len(b.text) {
		return false
	}
	if p.compareText(a.text, b.text) != 0 {
		return false
	}
	for offset := 0; offset < len(a.children); offset += 4096 {
		end := min(offset+4096, len(a.children))
		p.control.charge(0, uint64(end-offset))
		if !slices.Equal(a.children[offset:end], b.children[offset:end]) {
			return false
		}
	}
	for i, f := range a.fields {
		p.stepWork()
		other := b.fields[i]
		if p.compareText(f.Name, other.Name) != 0 || !sameSnapshotFieldValue(p, f.Value, other.Value) {
			return false
		}
	}
	return true
}

func sameSnapshotChildren(p *parser, ag *snapshotGraph, a *snapshotRecord, bg *snapshotGraph, b *snapshotRecord) bool {
	count := ag.childCount(a)
	if !a.childChunked && !b.childChunked {
		for offset := 0; offset < count; offset += 4096 {
			end := min(offset+4096, count)
			p.control.charge(0, uint64(end-offset))
			if !slices.Equal(a.children[offset:end], b.children[offset:end]) {
				return false
			}
		}
		return true
	}
	for offset := 0; offset < count; offset += snapshotChildChunkSize {
		end := min(offset+snapshotChildChunkSize, count)
		p.control.charge(0, uint64(end-offset))
		if !slices.Equal(ag.childChunk(a, offset/snapshotChildChunkSize, end-offset), bg.childChunk(b, offset/snapshotChildChunkSize, end-offset)) {
			return false
		}
	}
	return true
}

func sameSnapshotFieldValue(p *parser, a, b any) bool {
	switch v := a.(type) {
	case nil:
		return b == nil
	case snapshotRef:
		w, ok := b.(snapshotRef)
		return ok && v == w
	case int:
		w, ok := b.(int)
		return ok && v == w
	case string:
		w, ok := b.(string)
		return ok && p.compareText(v, w) == 0
	case bool:
		w, ok := b.(bool)
		return ok && v == w
	default:
		panic("unsupported frozen snapshot field")
	}
}
