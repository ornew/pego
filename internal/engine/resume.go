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

// resumable reports whether a repetition with the element e can be resumed.
func (c *compiler) resumable(e grammar.Expr) bool {
	ok := true
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

// resumeRepeat runs the repetition rs (see compiler.repeat) in a Document parse, resuming its run
// from the previous parse when it can.
func (p *parser) resumeRepeat(rs *runSite) (*Node, bool) {
	start := p.pos
	key := runKey{rs.id, start}
	// The run of the previous parse, if one edit was made since: the run here, or the one that
	// the edit moved here. This run takes it over, and updates its elements in place.
	var old []runElem
	edit := docEdit{start: 1<<62 - 1, end: 1<<62 - 1} // no edit
	if rec := p.runs[key]; rec != nil {
		old = rec.elems // the same repetition ran here earlier in this parse
	} else if p.lastRuns != nil && p.gen > 0 {
		edit = p.edits[p.gen-1]
		at := runKey{rs.id, start}
		if start > edit.start {
			at.start -= edit.delta
		}
		if rec := p.lastRuns[at]; rec != nil && rec.gen+1 == p.gen {
			old = rec.elems
			delete(p.lastRuns, at)
		}
	}
	base := len(p.kidStack)
	rec0, prov0 := len(p.recovered), p.provisional
	count := 0
	done := false // the old run ended where it stopped
	// The run is old[:k], then the new elements mid, then old[j:j+t] (the elements after the edit,
	// moved), then the new elements post.
	k, j, t := 0, -1, 0
	var mid, post []runElem
	if rs.build {
		p.kidStack = slices.Grow(p.kidStack, len(old))
	}
	for k < len(old) && old[k].examined <= edit.start && !done {
		done = p.reuseElem(rs, &old[k], 0, 0, count)
		k++
		count++
	}
	for !done && (rs.max < 0 || count < rs.max) {
		// Back in step with the old run after the edit: reuse the rest of it, and go on from its end
		// as usual. An element after the edit examined only input after it, so whichever run it
		// belongs to, parsing at its moved position gives it moved.
		if j < 0 && old != nil && p.pos-edit.delta >= edit.end && (rs.shiftable || edit.delta == 0) {
			at := p.pos - edit.delta
			i := k + sort.Search(len(old)-k, func(i int) bool { return old[k+i].start >= at })
			if i < len(old) && old[i].start == at && after(old[i:], edit.end) {
				j = i
				for i := j; i < len(old) && !done && (rs.max < 0 || count < rs.max); i++ {
					done = p.reuseElem(rs, &old[i], edit.delta, p.gen-1, count)
					count++
					t++
				}
				continue
			}
		}
		elemStart := p.pos
		m0 := p.mark()
		prevCut, prevFrame := p.cut, p.frame
		p.cut = false
		var f *frame
		if rs.ownScope {
			f = p.newFrame(len(rs.scope.names))
			p.frame = f
		}
		// Record the element's examined range and expectations separately.
		savedHW, savedLW := p.hw, p.lw
		p.hw, p.lw = elemStart, elemStart
		mk := p.isolate(elemStart)
		v, ok := rs.m(p)
		far, inner := p.unisolate(mk)
		var exp []expID
		if ok {
			exp = p.keep(inner)
		}
		p.mergeExpected(far, inner)
		from, examined := p.lw, p.hw
		p.hw, p.lw = max(savedHW, p.hw), min2(savedLW, p.lw)
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
		if rs.ownScope && rs.build {
			v = p.attachCaptures(v, rs.scope, f, m0.pos, p.pos)
		}
		count++
		if rs.build {
			p.kidStack = append(p.kidStack, v)
		}
		el := runElem{start: elemStart, end: p.pos, from: from, examined: examined, far: far, exp: exp, v: v}
		if j < 0 {
			mid = append(mid, el)
		} else {
			post = append(post, el)
		}
		if p.pos == elemStart && count >= rs.min {
			break
		}
	}
	if count < rs.min {
		p.kids(base)
		return nil, false
	}
	if count >= minRecorded && len(p.recovered) == rec0 && p.provisional == prov0 {
		var elems []runElem
		if j < 0 {
			elems = append(old[:k], mid...)
		} else {
			elems = append(slices.Replace(old[:j+t], k, j, mid...), post...)
		}
		p.runs[key] = &runRecord{gen: p.gen, elems: elems}
	}
	if !rs.build {
		return nil, true
	}
	return p.newNode(Node{Type: TypeList, Start: start, End: p.pos, Children: p.kids(base), fresh: true}), true
}

// reuseElem reuses the element el of an old run of rs, recorded in the generation gen and moved by
// shift since, as the element number n of the run (counting from 0), and reports whether the run
// ends after it (as it did before).
func (p *parser) reuseElem(rs *runSite, el *runElem, shift int, gen uint32, n int) bool {
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
	if rs.build {
		p.kidStack = append(p.kidStack, el.v)
	}
	p.pos = el.end
	p.stats.Reused++
	p.resumed++
	return el.end == el.start && n+1 >= rs.min || rs.max >= 0 && n+1 >= rs.max
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
