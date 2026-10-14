package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"weak"
)

// Snapshot owns an immutable parse outcome for one document revision. Readers
// may use it concurrently with later edits of its single-owner Document.
type Snapshot struct {
	version    DocumentVersion
	unit       Unit
	text       string
	graph      snapshotGraph
	parseError error
	stats      Stats
}

type snapshotGraph struct {
	nodes []snapshotRecord
	refs  []*snapshotRecord // candidate mode; immutable records may come from older graphs
	root  int
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
	kind            *nodeKind
	start, end      int32
	text            string
	children        []int
	fields          Fields
	fresh, terminal bool
	gen             uint32
}
type snapshotRef int

// SnapshotNode is a comparable read-only handle. Its zero value denotes nil.
type SnapshotNode struct {
	graph *snapshotGraph
	id    int
}

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
func (s *Snapshot) Unit() Unit { return s.unit }

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
	return SnapshotNode{g, id}
}
func (n SnapshotNode) record() snapshotRecord {
	if !n.Valid() {
		return snapshotRecord{}
	}
	return *n.graph.at(n.id - 1)
}

// Valid reports whether the handle denotes a non-nil node.
func (n SnapshotNode) Valid() bool { return n.graph != nil && n.id != 0 }

// Type returns the node type, or an empty string for nil.
func (n SnapshotNode) Type() string { return n.record().kind.typeName() }

// Rule returns the originating rule, or an empty string for nil.
func (n SnapshotNode) Rule() string { return n.record().kind.ruleName() }

// Start returns the inclusive start position.
func (n SnapshotNode) Start() int32 { return n.record().start }

// End returns the exclusive end position.
func (n SnapshotNode) End() int32 { return n.record().end }

// Text returns terminal text.
func (n SnapshotNode) Text() string { return n.record().text }

// IsTerminal reports whether the node is terminal.
func (n SnapshotNode) IsTerminal() bool { return n.record().terminal }

// ChildCount includes nil child slots.
func (n SnapshotNode) ChildCount() int { return len(n.record().children) }

// Child returns an indexed child; an invalid index panics like slice indexing.
func (n SnapshotNode) Child(i int) SnapshotNode {
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
func (s *Snapshot) Materialize() *Node {
	if s.graph.root == 0 {
		return nil
	}
	nodes := make([]*Node, s.graph.count())
	for i := range nodes {
		r := s.graph.at(i)
		nodes[i] = &Node{kind: r.kind, Start: r.start, End: r.end, Text: r.text, fresh: r.fresh, terminal: r.terminal, gen: r.gen}
	}
	node := func(id int) *Node {
		if id == 0 {
			return nil
		}
		return nodes[id-1]
	}
	for i := range nodes {
		r := s.graph.at(i)
		n := nodes[i]
		if r.children != nil {
			n.Children = make([]*Node, len(r.children))
			for j, id := range r.children {
				n.Children[j] = node(id)
			}
		}
		if r.fields != nil {
			n.Fields = make(Fields, len(r.fields))
			for j, f := range r.fields {
				if id, ok := f.Value.(snapshotRef); ok {
					f.Value = node(int(id))
				}
				n.Fields[j] = f
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
	var observed *snapshotContext
	if o.Context != nil {
		observed = &snapshotContext{Context: o.Context}
		o.Context = observed
	}
	var result *Snapshot
	_, err := d.parseWithCapture(o, func(p *parser, root *Node, parseErr error) error {
		if observed != nil && observed.interrupted || errors.Is(parseErr, ErrWorkLimit) {
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
		result = &Snapshot{version: d.Version(), unit: d.Unit(), text: out.String(), graph: graph, parseError: err, stats: p.stats}
		if snapshotSharingEnabled {
			d.previousSnapshot = weak.Make(&result.graph)
		}
		return returnedError
	})
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
	case *WorkLimitError:
		if e == nil {
			return (*WorkLimitError)(nil)
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
	node     *Node
	id, next int
	edge     snapshotEdge
}

// freezeSnapshotGraph registers identities before following their edges. The
// explicit DFS stack gives deterministic child-then-field rejection paths and
// supports arbitrary depth and caller-created cycles without recursion.
func freezeSnapshotGraph(p *parser, root *Node) (snapshotGraph, *SnapshotCaptureError) {
	var g snapshotGraph
	if root == nil {
		return g, nil
	}
	seen := make(map[*Node]int)
	register := func(n *Node) (int, bool) {
		if n == nil {
			return 0, false
		}
		if id, ok := seen[n]; ok {
			return id, false
		}
		p.stepWork()
		r := snapshotRecord{kind: n.kind, start: n.Start, end: n.End, text: n.Text, fresh: n.fresh, terminal: n.terminal, gen: n.gen}
		if n.Children != nil {
			r.children = make([]int, len(n.Children))
		}
		if n.Fields != nil {
			r.fields = make(Fields, len(n.Fields))
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
		var child *Node
		var edge snapshotEdge
		if f.next < len(f.node.Children) {
			edge = snapshotEdge{child: true, index: f.next}
			child = f.node.Children[f.next]
		} else {
			i := f.next - len(f.node.Children)
			if i >= len(f.node.Fields) {
				stack = stack[:len(stack)-1]
				continue
			}
			field := f.node.Fields[i]
			edge = snapshotEdge{index: i, name: field.Name}
			var value any
			switch v := field.Value.(type) {
			case *Node:
				child = v
			case nil, int, string, bool:
				value = v
			default:
				return snapshotGraph{}, &SnapshotCaptureError{Path: snapshotFieldPath(p, stack, edge), ValueType: reflect.TypeOf(v)}
			}
			g.nodes[f.id-1].fields[i] = NodeField{Name: field.Name, Value: value}
		}
		id, fresh := register(child)
		if edge.child {
			g.nodes[f.id-1].children[edge.index] = id
		} else if _, ok := f.node.Fields[edge.index].Value.(*Node); ok {
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

// shareSnapshotGraph reuses exact immutable records. The current parse
// is always frozen first; no mutable Node or parser-owned slice is shared.
func shareSnapshotGraph(p *parser, previous *snapshotGraph, next snapshotGraph) snapshotGraph {
	if previous == nil || previous.root != next.root {
		return next
	}
	matches := make([]uint64, (len(next.nodes)+63)/64)
	ownedCount := 0
	for i := range next.nodes {
		p.stepWork()
		if i < previous.count() && sameSnapshotRecord(p, previous.at(i), &next.nodes[i]) {
			matches[i/64] |= 1 << uint(i%64)
		} else {
			ownedCount++
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
	for i := range next.nodes {
		p.stepWork()
		if matches[i/64]&(1<<uint(i%64)) != 0 {
			shared.refs[i] = previous.at(i)
		} else {
			shared.nodes[ownedAt] = next.nodes[i]
			shared.refs[i] = &shared.nodes[ownedAt]
			ownedAt++
		}
	}
	return shared
}

func sameSnapshotRecord(p *parser, a, b *snapshotRecord) bool {
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
	// A large repetition root can have many edges even when no node changed.
	// Charge and compare bounded spans so a controller can interrupt the scan.
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
