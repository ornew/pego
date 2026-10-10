package engine

// Nodes and child slices created during a parse are allocated in bulk from per-parse chunks.
// This reduces the number of allocations and the number of objects the GC has to track. A chunk
// stays alive as long as any of its nodes is referenced.

const (
	nodeChunk = 256
	ptrChunk  = 1024
)

// splitChunks starts new chunks for the elements of a stream that follow, at an element boundary.
// A chunk stays alive while anything in it is referenced, and keeps alive what its objects point
// to; chunks shared by consecutive elements would therefore chain every element emitted so far to
// the live parser state. Splitting keeps each chain within a group of elements, so memory stays
// bounded. The unused rest of the chunks is lost at each split, so the split waits until the node
// chunk is nearly used up, or until the elements since the last split filled more than one chunk
// (large elements, after which the loss is small in comparison).
func (p *parser) splitChunks() {
	if len(p.nodeSlab) < nodeChunk/8 || p.nodeChunks >= 2 {
		p.nodeSlab, p.ptrSlab, p.fieldSlab, p.funcSlab = nil, nil, nil, nil
		if !streamFrameScratch {
			p.frameSlab = nil
		}
		p.nodeChunks = 0
	}
}

// newNode allocates a copy of v from a chunk and returns it.
func (p *parser) newNode(v Node) *Node {
	if len(p.nodeSlab) == 0 {
		p.nodeSlab = make([]Node, nodeChunk)
		p.nodeChunks++
	}
	n := &p.nodeSlab[0]
	p.nodeSlab = p.nodeSlab[1:]
	*n = v
	n.gen = p.gen
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

// newPos allocates a copy of the position pos from a chunk and returns it. Start positions are
// pushed onto the VM value stack as *int: converting an int to an interface would allocate each
// time.
func (p *parser) newPos(pos int) *int {
	if len(p.posSlab) == 0 {
		p.posSlab = make([]int, ptrChunk)
	}
	n := &p.posSlab[0]
	p.posSlab = p.posSlab[1:]
	*n = pos
	return n
}

// newFunc allocates a copy of the lambda f from a chunk and returns it.
func (p *parser) newFunc(f vmFunc) *vmFunc {
	if len(p.funcSlab) == 0 {
		p.funcSlab = make([]vmFunc, nodeChunk/4)
	}
	n := &p.funcSlab[0]
	p.funcSlab = p.funcSlab[1:]
	*n = f
	return n
}

// dropKids discards p.kidStack[base:].
func (p *parser) dropKids(base int) {
	clear(p.kidStack[base:])
	p.kidStack = p.kidStack[:base]
}

// fields allocates an empty field list with capacity n from a chunk and returns it. Appending
// beyond n reallocates, so neighbouring lists in the chunk are never overwritten.
func (p *parser) fields(n int) Fields {
	if n > nodeChunk/4 {
		return make(Fields, 0, n)
	}
	if len(p.fieldSlab) < n {
		p.fieldSlab = make([]NodeField, nodeChunk)
	}
	fs := p.fieldSlab[:0:n]
	p.fieldSlab = p.fieldSlab[n:]
	return fs
}

// one returns a slice holding only v, for the element list of an action. It is owned by the
// parser and valid until the next call: the list is read only while the action is evaluated
// (evaluations never nest), and $0 copies it.
func (p *parser) one(v *Node) []*Node {
	p.item[0] = v
	return p.item[:]
}

// newFrame allocates a capture frame with n slots from a chunk and returns it.
func (p *parser) newFrame(n int) *frame {
	if n == 0 {
		return emptyFrame // shared because it is never written to
	}
	var f *frame
	if streamFrameScratch {
		// Keep the used prefix so commits can clear and rewind it without
		// an extra buffer pointer in every parser.
		if len(p.frameSlab) == cap(p.frameSlab) {
			p.frameSlab = make([]frame, 0, nodeChunk)
		}
		p.frameSlab = p.frameSlab[:len(p.frameSlab)+1]
		f = &p.frameSlab[len(p.frameSlab)-1]
	} else {
		if len(p.frameSlab) == 0 {
			p.frameSlab = make([]frame, nodeChunk)
		}
		f = &p.frameSlab[0]
		p.frameSlab = p.frameSlab[1:]
	}
	f.vals = p.nodes(n)
	return f
}

// resetStreamFrames reuses capture-frame storage after an emitted element.
// The top-level frame stays live across the commit; nested calls and element
// scopes have finished. Results copy captures and never retain frame structs.
// Child-pointer storage remains immutable and follows the usual chunk split.
func (p *parser) resetStreamFrames() {
	keep := 0
	for i := range p.frameSlab {
		if &p.frameSlab[i] == p.frame {
			keep = i + 1
			break
		}
	}
	clear(p.frameSlab[keep:])
	p.frameSlab = p.frameSlab[:keep]
}

// useCtx places the evaluation context for actions and predicates in a single area owned by
// the parser and returns it. Evaluations never nest, and no reference to the context survives
// an evaluation (function values cannot be stored in struct fields or variables), so the area
// can be reused, and so can the lambdas of the previous evaluation.
func (p *parser) useCtx(c evalCtx) *evalCtx {
	p.ectx = c
	p.closures.reset(0)
	return &p.ectx
}

// arena allocates values of type T in chunks and frees them in LIFO order (reset). Pointers to
// allocated values stay valid until they are freed, since chunks never move.
type arena[T any] struct {
	chunks []*[64]T
	n      int
}

func (a *arena[T]) alloc() *T {
	c := a.n / 64
	if c == len(a.chunks) {
		a.chunks = append(a.chunks, new([64]T))
	}
	v := &a.chunks[c][a.n%64]
	a.n++
	return v
}

// reset frees the values allocated after the first n, clearing them so that they keep nothing
// reachable.
func (a *arena[T]) reset(n int) {
	for i := n; i < a.n; i++ {
		var zero T
		a.chunks[i/64][i%64] = zero
	}
	a.n = n
}
