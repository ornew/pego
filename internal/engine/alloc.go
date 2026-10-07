package engine

// Nodes and child slices created during a parse are allocated in bulk from per-parse chunks.
// This reduces the number of allocations and the number of objects the GC has to track. A chunk
// stays alive as long as any of its nodes is referenced.

const (
	nodeChunk = 256
	ptrChunk  = 1024
)

// newNode allocates a copy of v from a chunk and returns it.
func (p *parser) newNode(v Node) *Node {
	if len(p.nodeSlab) == 0 {
		p.nodeSlab = make([]Node, nodeChunk)
	}
	n := &p.nodeSlab[0]
	p.nodeSlab = p.nodeSlab[1:]
	*n = v
	return n
}

// nodes allocates a child slice of length n (and capacity n) from a chunk and returns it.
func (p *parser) nodes(n int) []*Node {
	if n == 0 {
		return []*Node{}
	}
	if n > ptrChunk/4 {
		return make([]*Node, n)
	}
	if len(p.ptrSlab) < n {
		p.ptrSlab = make([]*Node, ptrChunk)
	}
	s := p.ptrSlab[:n:n]
	p.ptrSlab = p.ptrSlab[n:]
	return s
}

// kids returns p.kidStack[base:] as a child slice and resets the stack to base.
func (p *parser) kids(base int) []*Node {
	ks := p.nodes(len(p.kidStack) - base)
	copy(ks, p.kidStack[base:])
	clear(p.kidStack[base:])
	p.kidStack = p.kidStack[:base]
	return ks
}

// newFrame allocates a capture frame with n slots from a chunk and returns it.
func (p *parser) newFrame(n int) *frame {
	if n == 0 {
		return emptyFrame // shared because it is never written to
	}
	if len(p.frameSlab) == 0 {
		p.frameSlab = make([]frame, nodeChunk)
	}
	f := &p.frameSlab[0]
	p.frameSlab = p.frameSlab[1:]
	f.vals = p.nodes(n)
	return f
}

// useCtx places the evaluation context for actions and predicates in a single area owned by
// the parser and returns it. Evaluations never nest, and no reference to the context survives
// an evaluation (function values cannot be stored in struct fields or variables), so the area
// can be reused.
func (p *parser) useCtx(c evalCtx) *evalCtx {
	p.ectx = c
	return &p.ectx
}
