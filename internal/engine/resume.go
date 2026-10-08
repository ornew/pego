package engine

import (
	"slices"
	"sort"

	"github.com/ornew/pego/grammar"
)

// Resuming repetitions in a Document.
//
// A Document reparse evaluates again the rules whose results span the edit, and a long repetition
// in one of them (the lines of a file, the statements of a block) ran again element by element:
// every element but the edited one was a memo hit, but a hit per element is still work in
// proportion to the length of the repetition. During Document parses, such a repetition records
// its run: for each element, its positions, the input range it examined, its value and the
// expectations it recorded. When the repetition runs again after one edit, at the same position
// or the one the edit moved it to, it reuses the elements that examined only input before the edit
// as they are, and parses from there; once an element ends where an old element after the edit
// began (moved by the edit), it reuses the rest of the old run too, moving the nodes of their
// values (moveValue). The result,
// the expectations and the examined range are those of running every element.
//
// An element's result must depend only on the input it examined, as for a memo result: repetitions
// whose elements have predicates or call rules that read variables are not resumed, nor is a run
// that recovered errors or used a provisional result of a growing left recursion. Elements are
// moved past an edit only if their values contain no position values.

// runSite is a repetition that can be resumed (compiler.repeat).
type runSite struct {
	id              int
	m               matcher
	scope           *scope
	ownScope, build bool
	min, max        int
	shiftable       bool // the elements' values contain no position values
}

type runKey struct{ site, start int }

// runRecord is a run of a repetition in the parse of the generation gen.
type runRecord struct {
	gen   uint32
	elems []runElem
}

type runElem struct {
	start, end     int
	from, examined int // the input range [from, examined) the element examined
	far            int // the position of the expectations exp
	exp            []expID
	v              *Node
}

// resumable reports whether a repetition with the element e can be resumed. An element whose
// captures are written to the enclosing scope (those inside &, when the element has no captures of
// its own) cannot be: reusing it would not write them.
func (c *compiler) resumable(e grammar.Expr) bool {
	ok := hasCaptures(e) || !anyCapture(e)
	walkExpr(e, func(x grammar.Expr) {
		switch x := x.(type) {
		case *grammar.Predicate:
			ok = false
		case *grammar.Ref:
			if r := c.prog.byName[x.Name]; r == nil || len(r.vars) > 0 {
				ok = false
			}
		}
	})
	return ok
}

// anyCapture reports whether e contains a capture anywhere, lookaheads included.
func anyCapture(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(x grammar.Expr) {
		if _, ok := x.(*grammar.Capture); ok {
			found = true
		}
	})
	return found
}

// positional reports whether the value of e may contain position values.
func (c *compiler) positional(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(x grammar.Expr) {
		if x, ok := x.(*grammar.Ref); ok {
			if r := c.prog.byName[x.Name]; r == nil || r.positional {
				found = true
			}
		}
	})
	return found
}

// minRecorded is the number of elements below which a run is not recorded: a short repetition
// costs little to run again. It is a variable for tests.
var minRecorded = 16

// runState is a run of a repetition in progress in a Document parse (resumeRepeat, and the REPEAT
// instruction of the VMs).
type runState struct {
	key         runKey
	min, max    int
	shiftable   bool
	old         []runElem // the run of the previous parse that this one takes over, updated in place
	edit        docEdit   // the edit since (none if old is from this parse)
	count       int
	done        bool // the old run ended where it stopped
	moved       bool // old is the run that the edit moved here: none of it lies before the edit
	rec0, prov0 int
	// The run is old[:k], then the new elements mid, then old[j:j+t] (the elements after the edit,
	// moved), then the new elements post.
	k, j, t   int
	mid, post []runElem
	// The element being parsed: where it started, and the state its examined range and
	// expectations are kept apart from (beginElem).
	elemStart, savedHW, savedLW int
	mk                          expMark
}

// startRun starts a run of the repetition site id with the given bounds at the current position:
// it looks for the run to take over, the run here or the one that the edit since moved here.
func (p *parser) startRun(r *runState, id, min, max int, shiftable bool) {
	start := p.pos
	*r = runState{key: runKey{id, start}, min: min, max: max, shiftable: shiftable, j: -1,
		edit: docEdit{start: 1<<62 - 1, end: 1<<62 - 1}, rec0: len(p.recovered), prov0: p.provisional,
		mid: r.mid[:0], post: r.post[:0]}
	if rec := p.runs[r.key]; rec != nil {
		r.old = rec.elems // the same repetition ran here earlier in this parse
	} else if p.lastRuns != nil && p.gen > 0 {
		r.edit = p.edits[p.gen-1]
		at := r.key
		switch {
		case start <= r.edit.start:
		case start >= r.edit.end+r.edit.delta: // after the inserted text: where the edit moved it
			at.start -= r.edit.delta
			r.moved = true
		default:
			return // inside the inserted text: no run was there
		}
		if rec := p.lastRuns[at]; rec != nil && rec.gen+1 == p.gen {
			r.old = rec.elems
			delete(p.lastRuns, at)
		}
	}
}

// more reports whether the run may take another element.
func (r *runState) more() bool { return !r.done && (r.max < 0 || r.count < r.max) }

// nextPrefix reuses the next old element if it examined only input before the edit, and returns
// it (its value is for the caller to push), or nil.
func (p *parser) nextPrefix(r *runState) *runElem {
	if r.moved || r.k >= len(r.old) || r.old[r.k].examined > r.edit.start || r.done {
		return nil
	}
	el := &r.old[r.k]
	p.reuseElem(r, el, 0, 0)
	r.k++
	return el
}

// resync reports whether the run is back in step with the old run after the edit: an old element
// after the edit starts where the next element would. If so, the rest of the old run is reused
// (nextTail), and the run goes on from its end as usual. An element after the edit examined only
// input after it, so whichever run it belongs to, parsing at its moved position gives it moved.
func (p *parser) resync(r *runState) bool {
	if r.j >= 0 || r.old == nil || p.pos-r.edit.delta < r.edit.end || !r.shiftable && r.edit.delta != 0 {
		return false
	}
	at := p.pos - r.edit.delta
	i := r.k + sort.Search(len(r.old)-r.k, func(i int) bool { return r.old[r.k+i].start >= at })
	if i == len(r.old) || r.old[i].start != at || !after(r.old[i:], r.edit.end) {
		return false
	}
	r.j = i
	return true
}

// nextTail reuses the next old element after the edit (after resync), moved by the edit, and
// returns it, or nil.
func (p *parser) nextTail(r *runState) *runElem {
	i := r.j + r.t
	if r.j < 0 || i >= len(r.old) || !r.more() {
		return nil
	}
	el := &r.old[i]
	p.reuseElem(r, el, r.edit.delta, p.gen-1)
	r.t++
	return el
}

// reuseElem reuses the element el of an old run, recorded in the generation gen and moved by shift
// since: it replays what running it did, except pushing its value.
func (p *parser) reuseElem(r *runState, el *runElem, shift int, gen uint32) {
	if shift != 0 {
		el.v = p.moveValue(el.v, shift, gen)
		el.start += shift
		el.end += shift
		el.from += shift
		el.examined += shift
		el.far += shift
	}
	p.touch(el.examined)
	p.lw = min2(p.lw, el.from)
	if len(el.exp) > 0 && el.far >= p.farthest {
		p.mergeExpected(el.far, el.exp)
	}
	p.pos = el.end
	p.stats.Reused++
	p.resumed++
	r.count++
	// The old run ended after an empty element or at its maximum.
	r.done = el.end == el.start && r.count >= r.min || r.max >= 0 && r.count >= r.max
}

// beginElem starts parsing an element: its examined range and expectations are recorded apart.
func (p *parser) beginElem(r *runState) {
	r.elemStart, r.savedHW, r.savedLW = p.pos, p.hw, p.lw
	p.hw, p.lw = p.pos, p.pos
	r.mk = p.isolate(p.pos)
}

// endElem ends parsing an element (before the state is reset if it failed), and records it if it
// matched with the value v.
func (p *parser) endElem(r *runState, ok bool, v *Node) {
	far, inner := p.unisolate(r.mk)
	var exp []expID
	if ok {
		exp = p.keep(inner)
	}
	p.mergeExpected(far, inner)
	from, examined := p.lw, p.hw
	p.hw, p.lw = max(r.savedHW, p.hw), min2(r.savedLW, p.lw)
	if !ok {
		return
	}
	r.count++
	el := runElem{start: r.elemStart, end: p.pos, from: from, examined: examined, far: far, exp: exp, v: v}
	if r.j < 0 {
		r.mid = append(r.mid, el)
	} else {
		r.post = append(r.post, el)
	}
}

// finishRun records the run of a repetition that matched, for the next parse.
func (p *parser) finishRun(r *runState) {
	if r.count < minRecorded || len(p.recovered) != r.rec0 || p.provisional != r.prov0 {
		return
	}
	var elems []runElem
	if r.j < 0 {
		elems = append(r.old[:r.k], r.mid...)
	} else {
		elems = append(slices.Replace(r.old[:r.j+r.t], r.k, r.j, r.mid...), r.post...)
	}
	p.runs[r.key] = &runRecord{gen: p.gen, elems: elems}
}

// resumeRepeat runs the repetition rs (see compiler.repeat) in a Document parse, resuming its run
// from the previous parse when it can.
func (p *parser) resumeRepeat(rs *runSite) (*Node, bool) {
	start := p.pos
	var r runState
	p.startRun(&r, rs.id, rs.min, rs.max, rs.shiftable)
	base := len(p.kidStack)
	if rs.build {
		p.kidStack = slices.Grow(p.kidStack, len(r.old))
	}
	for el := p.nextPrefix(&r); el != nil; el = p.nextPrefix(&r) {
		if rs.build {
			p.kidStack = append(p.kidStack, el.v)
		}
	}
	for r.more() {
		if p.resync(&r) {
			for el := p.nextTail(&r); el != nil; el = p.nextTail(&r) {
				if rs.build {
					p.kidStack = append(p.kidStack, el.v)
				}
			}
			continue
		}
		m0 := p.mark()
		prevCut, prevFrame := p.cut, p.frame
		p.cut = false
		var f *frame
		if rs.ownScope {
			f = p.newFrame(len(rs.scope.names))
			p.frame = f
		}
		p.beginElem(&r)
		v, ok := rs.m(p)
		if ok && rs.ownScope && rs.build {
			v = p.attachCaptures(v, rs.scope, f, m0.pos, p.pos)
		}
		p.endElem(&r, ok, v)
		cut := p.cut
		p.cut, p.frame = prevCut, prevFrame
		if !ok {
			p.reset(m0)
			if cut {
				p.kids(base)
				return nil, false
			}
			break
		}
		if rs.build {
			p.kidStack = append(p.kidStack, v)
		}
		if p.pos == m0.pos && r.count >= rs.min {
			break
		}
	}
	if r.count < rs.min {
		p.kids(base)
		return nil, false
	}
	p.finishRun(&r)
	if !rs.build {
		return nil, true
	}
	return p.newNode(Node{kind: kindList, Start: int32(start), End: int32(p.pos), Children: p.kids(base), fresh: true}), true
}

// after reports whether every element examined only input from end on.
func after(elems []runElem, end int) bool {
	for _, el := range elems {
		if el.from < end {
			return false
		}
	}
	return true
}

// moveValue moves the nodes of the value v of an element recorded in the generation base by
// shift, as moveResult moves the result of a memo entry, and returns it or its copy.
func (p *parser) moveValue(v *Node, shift int, base uint32) *Node {
	if v == nil {
		return nil
	}
	if p.moved == nil {
		p.moved = map[*Node]*Node{}
	}
	p.moveBase, p.baseShift = base, shift
	if v.Start == v.End {
		v = p.moveEmpty(v, shift)
	} else if v.gen < p.gen {
		p.moveTree(v, p.shiftOf(v))
	}
	if len(p.moved) > 1024 {
		p.moved = nil
	} else {
		clear(p.moved)
	}
	clear(p.shifts)
	return v
}

// newRunState returns a runState for a VM repetition, reusing a freed one.
func (p *parser) newRunState() *runState {
	if n := len(p.freeRuns); n > 0 {
		r := p.freeRuns[n-1]
		p.freeRuns = p.freeRuns[:n-1]
		return r
	}
	return &runState{}
}

// freeRunState makes the runState of a finished repetition available to later ones.
func (p *parser) freeRunState(r *runState) {
	r.old = nil
	clear(r.mid)
	clear(r.post)
	p.freeRuns = append(p.freeRuns, r)
}
