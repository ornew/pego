package core

import "github.com/ornew/zeroparse/internal/value"

// Nodes and child slices created during a parse are allocated in bulk from per-parse chunks.
// This reduces the number of allocations and the number of objects the GC has to track. A chunk
// stays alive as long as any of its nodes is referenced.

const (
	NodeChunk = 256
	PtrChunk  = 1024
)

// splitChunks starts new chunks for the elements of a stream that follow, at an element boundary.
// A chunk stays alive while anything in it is referenced, and keeps alive what its objects point
// to; chunks shared by consecutive elements would therefore chain every element emitted so far to
// the live parser state. Splitting keeps each chain within a group of elements, so memory stays
// bounded. The unused rest of the chunks is lost at each split, so the split waits until the node
// chunk is nearly used up, or until the elements since the last split filled more than one chunk
// (large elements, after which the loss is small in comparison).
func (p *Parser) splitChunks() {
	if len(p.NodeSlab) < NodeChunk/8 || p.NodeChunks >= 2 {
		p.NodeSlab, p.PtrSlab, p.fieldSlab = nil, nil, nil
		if p.Backend != nil {
			p.Backend.SplitChunks()
		}
		if !StreamFrameScratch {
			p.FrameSlab = nil
		}
		p.NodeChunks = 0
	}
}

// PrivatePrefixAlloc reserves a small number of exact-size prefix objects.
// Larger prefixes fall back to slabs, whose tails are sealed at #stream.
// The existing chunk counter stores this bounded prefix budget until then.
func (p *Parser) PrivatePrefixAlloc() bool {
	if StreamPrefixIsolation && p.NodeChunks < 0 {
		p.NodeChunks++
		return true
	}
	return false
}

// NewNode allocates a node with the representation d from a chunk and returns it.
func (p *Parser) NewNode(d value.Data) *value.Node {
	d.Gen = p.Gen
	if len(p.NodeSlab) == 0 {
		if p.PrivatePrefixAlloc() {
			n := new(value.Node)
			*value.D(n) = d
			return n
		}
		p.NodeSlab = make([]value.Node, NodeChunk)
		p.NodeChunks++
	}
	n := &p.NodeSlab[0]
	p.NodeSlab = p.NodeSlab[1:]
	*value.D(n) = d
	return n
}

// Nodes allocates a child slice of length n (and capacity n) from a chunk and returns it.
func (p *Parser) Nodes(n int) []*value.Node {
	if n == 0 {
		return []*value.Node{}
	}
	if n > PtrChunk/4 {
		return make([]*value.Node, n)
	}
	if len(p.PtrSlab) < n {
		if p.PrivatePrefixAlloc() {
			return make([]*value.Node, n)
		}
		p.PtrSlab = make([]*value.Node, PtrChunk)
	}
	s := p.PtrSlab[:n:n]
	p.PtrSlab = p.PtrSlab[n:]
	return s
}

// Kids returns p.KidStack[base:] as a child slice and resets the stack to base.
func (p *Parser) Kids(base int) []*value.Node {
	ks := p.Nodes(len(p.KidStack) - base)
	CopySpend(p, ks, p.KidStack[base:])
	ClearSpend(p, p.KidStack[base:])
	p.KidStack = p.KidStack[:base]
	return ks
}

// DropKids discards p.KidStack[base:].
func (p *Parser) DropKids(base int) {
	ClearSpend(p, p.KidStack[base:])
	p.KidStack = p.KidStack[:base]
}

// fields allocates an empty field list with capacity n from a chunk and returns it. Appending
// beyond n reallocates, so neighbouring lists in the chunk are never overwritten.
func (p *Parser) fields(n int) value.Fields {
	if n > NodeChunk/4 {
		return make(value.Fields, 0, n)
	}
	if len(p.fieldSlab) < n {
		if p.PrivatePrefixAlloc() {
			return make(value.Fields, 0, n)
		}
		p.fieldSlab = make([]value.NodeField, NodeChunk)
	}
	fs := p.fieldSlab[:0:n]
	p.fieldSlab = p.fieldSlab[n:]
	return fs
}

// one returns a slice holding only v, for the element list of an action. It is owned by the
// parser and valid until the next call: the list is read only while the action is evaluated
// (evaluations never nest), and $0 copies it.
func (p *Parser) one(v *value.Node) []*value.Node {
	p.item[0] = v
	return p.item[:]
}

// NewFrame allocates a capture frame with n slots from a chunk and returns it.
func (p *Parser) NewFrame(n int) *Frame {
	if n == 0 {
		return EmptyFrame // shared because it is never written to
	}
	var f *Frame
	if StreamFrameScratch {
		// Keep the used prefix so commits can clear and rewind it without
		// an extra buffer pointer in every parser.
		if len(p.FrameSlab) == cap(p.FrameSlab) {
			if p.PrivatePrefixAlloc() {
				return &Frame{Vals: make([]*value.Node, n)}
			}
			p.FrameSlab = make([]Frame, 0, NodeChunk)
		}
		p.FrameSlab = p.FrameSlab[:len(p.FrameSlab)+1]
		f = &p.FrameSlab[len(p.FrameSlab)-1]
	} else {
		if len(p.FrameSlab) == 0 {
			if p.PrivatePrefixAlloc() {
				return &Frame{Vals: make([]*value.Node, n)}
			}
			p.FrameSlab = make([]Frame, NodeChunk)
		}
		f = &p.FrameSlab[0]
		p.FrameSlab = p.FrameSlab[1:]
	}
	f.Vals = p.Nodes(n)
	return f
}

// ResetStreamFrames reuses capture-frame storage after an emitted element.
// The top-level frame stays live across the commit; nested calls and element
// scopes have finished. Results copy captures and never retain frame structs.
// Child-pointer storage remains immutable and follows the usual chunk split.
func (p *Parser) ResetStreamFrames() {
	keep := 0
	for i := range p.FrameSlab {
		p.Spend()
		if &p.FrameSlab[i] == p.Frame {
			keep = i + 1
			break
		}
	}
	ClearSpend(p, p.FrameSlab[keep:])
	p.FrameSlab = p.FrameSlab[:keep]
}

// UseCtx places the evaluation context for actions and predicates in a single area owned by
// the parser and returns it. Evaluations never nest, and no reference to the context survives
// an evaluation (function values cannot be stored in struct fields or variables), so the area
// can be reused.
func (p *Parser) UseCtx(c EvalCtx) *EvalCtx {
	p.ectx = c
	return &p.ectx
}

// Arena allocates values of type T in chunks and frees them in LIFO order (reset). Pointers to
// allocated values stay valid until they are freed, since chunks never move.
type Arena[T any] struct {
	Chunks []*[64]T
	N      int
}

func (a *Arena[T]) Alloc() *T {
	c := a.N / 64
	if c == len(a.Chunks) {
		a.Chunks = append(a.Chunks, new([64]T))
	}
	v := &a.Chunks[c][a.N%64]
	a.N++
	return v
}

// reset frees the values allocated after the first n, clearing them so that they keep nothing
// reachable.
func (a *Arena[T]) reset(n int) {
	for i := n; i < a.N; i++ {
		var zero T
		a.Chunks[i/64][i%64] = zero
	}
	a.N = n
}

// StartStreamChunks separates live prefix values from element storage.
// Only the top-level frame remains active at a stream repetition boundary.
func (p *Parser) StartStreamChunks() {
	p.NodeSlab, p.PtrSlab, p.fieldSlab = nil, nil, nil
	if p.Backend != nil {
		p.Backend.SplitChunks()
	}
	p.NodeChunks = 0
	if StreamFrameScratch {
		p.ResetStreamFrames()
	}
	p.FrameSlab = nil
}
