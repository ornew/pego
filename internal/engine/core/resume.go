package core

import (
	"slices"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
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

// RunSite is a repetition that can be resumed (compiler.repeat).
type RunSite struct {
	Id              int
	M               Matcher
	Scope           *compile.Scope
	OwnScope, Build bool
	Min, Max        int
	Shiftable       bool // the elements' values contain no position values
	// Value makes an element's value from the value of the element expression.
	Value func(p *Parser, v *value.Node, start int) *value.Node
}

type RunKey struct{ Site, start int }

// RunRecord is a run of a repetition in the parse of the generation gen.
type RunRecord struct {
	gen   uint32
	Elems []RunElem
}

type RunElem struct {
	Start, end     int
	From, examined int // the input range [from, examined) the element examined
	far            int // the position of the expectations exp
	exp            []ExpID
	V              *value.Node
}

// MinRecorded is the number of elements below which a run is not recorded: a short repetition
// costs little to run again. It is a variable for tests.
var MinRecorded = 16

// RunState is a run of a repetition in progress in a Document parse (ResumeRepeat, and the REPEAT
// instruction of the VMs).
type RunState struct {
	Key         RunKey
	min, max    int
	shiftable   bool
	Old         []RunElem // the run of the previous parse that this one takes over, updated in place
	edit        DocEdit   // the edit since (none if old is from this parse)
	Count       int
	done        bool // the old run ended where it stopped
	moved       bool // old is the run that the edit moved here: none of it lies before the edit
	rec0, prov0 int
	// The run is old[:k], then the new elements mid, then old[j:j+t] (the elements after the edit,
	// moved), then the new elements post.
	K, J, T   int
	Mid, Post []RunElem
	// The element being parsed: where it started, and the state its examined range and
	// expectations are kept apart from (BeginElem).
	elemStart, savedHW, savedLW int
	mk                          ExpMark
}

// StartRun starts a run of the repetition site id with the given bounds at the current position:
// it looks for the run to take over, the run here or the one that the edit since moved here.
func (p *Parser) StartRun(r *RunState, id, min, max int, shiftable bool) {
	start := p.Pos
	*r = RunState{Key: RunKey{id, start}, min: min, max: max, shiftable: shiftable, J: -1,
		edit: DocEdit{Start: int(^uint(0) >> 1), End: int(^uint(0) >> 1)}, rec0: len(p.Recovered), prov0: p.Provisional,
		Mid: r.Mid[:0], Post: r.Post[:0]}
	if rec := p.Runs[r.Key]; rec != nil {
		r.Old = rec.Elems // the same repetition ran here earlier in this parse
	} else if p.LastRuns != nil && p.Gen > 0 {
		r.edit = p.Edits[p.Gen-1]
		at := r.Key
		switch {
		case start <= r.edit.Start:
		case start >= r.edit.End+r.edit.Delta: // after the inserted text: where the edit moved it
			at.start -= r.edit.Delta
			r.moved = true
		default:
			return // inside the inserted text: no run was there
		}
		if rec := p.LastRuns[at]; rec != nil && rec.gen+1 == p.Gen {
			r.Old = rec.Elems
			delete(p.LastRuns, at)
		}
	}
}

// More reports whether the run may take another element.
func (r *RunState) More() bool { return !r.done && (r.max < 0 || r.Count < r.max) }

// NextPrefix reuses the next old element if it examined only input before the edit, and returns
// it (its value is for the caller to push), or nil.
func (p *Parser) NextPrefix(r *RunState) *RunElem {
	p.Work()
	if r.moved || r.K >= len(r.Old) || r.Old[r.K].examined > r.edit.Start || r.done {
		return nil
	}
	el := &r.Old[r.K]
	p.reuseElem(r, el, 0, 0)
	r.K++
	return el
}

// Resync reports whether the run is back in step with the old run after the edit: an old element
// after the edit starts where the next element would. If so, the rest of the old run is reused
// (NextTail), and the run goes on from its end as usual. An element after the edit examined only
// input after it, so whichever run it belongs to, parsing at its moved position gives it moved.
func (p *Parser) Resync(r *RunState) bool {
	if r.J >= 0 || r.Old == nil || p.Pos-r.edit.Delta < r.edit.End || !r.shiftable && r.edit.Delta != 0 {
		return false
	}
	at := p.Pos - r.edit.Delta
	i := p.RunSearch(r.Old, r.K, at)
	if i == len(r.Old) || r.Old[i].Start != at || !p.RunAfter(r.Old[i:], r.edit.End) {
		return false
	}
	r.J = i
	return true
}

// NextTail reuses the next old element after the edit (after resync), moved by the edit, and
// returns it, or nil.
func (p *Parser) NextTail(r *RunState) *RunElem {
	p.Work()
	i := r.J + r.T
	if r.J < 0 || i >= len(r.Old) || !r.More() {
		return nil
	}
	el := &r.Old[i]
	p.reuseElem(r, el, r.edit.Delta, p.Gen-1)
	r.T++
	return el
}

// reuseElem reuses the element el of an old run, recorded in the generation gen and moved by shift
// since: it replays what running it did, except pushing its value.
func (p *Parser) reuseElem(r *RunState, el *RunElem, shift int, gen uint32) {
	if shift != 0 {
		el.V = p.moveValue(el.V, shift, gen)
		el.Start += shift
		el.end += shift
		el.From += shift
		el.examined += shift
		el.far += shift
	}
	p.touch(el.examined)
	p.lw = Min2(p.lw, el.From)
	if len(el.exp) > 0 && el.far >= p.Farthest {
		p.MergeExpected(el.far, el.exp)
	}
	p.Pos = el.end
	p.Stats.Reused++
	p.Resumed++
	r.Count++
	// The old run ended after an empty element or at its maximum.
	r.done = el.end == el.Start || r.max >= 0 && r.Count >= r.max
}

// BeginElem starts parsing an element: its examined range and expectations are recorded apart.
func (p *Parser) BeginElem(r *RunState) {
	r.elemStart, r.savedHW, r.savedLW = p.Pos, p.Hw, p.lw
	p.Hw, p.lw = p.Pos, p.Pos
	r.mk = p.Isolate(p.Pos)
}

// EndElem ends parsing an element (before the state is reset if it failed), and records it if it
// matched with the value v.
func (p *Parser) EndElem(r *RunState, ok bool, v *value.Node) {
	far, inner := p.Unisolate(r.mk)
	var exp []ExpID
	if ok {
		exp = p.Keep(inner)
	}
	p.MergeExpected(far, inner)
	from, examined := p.lw, p.Hw
	p.Hw, p.lw = max(r.savedHW, p.Hw), Min2(r.savedLW, p.lw)
	if !ok {
		return
	}
	r.Count++
	el := RunElem{Start: r.elemStart, end: p.Pos, From: from, examined: examined, far: far, exp: exp, V: v}
	if r.J < 0 {
		if WorkControlsEnabled && p.Control != nil {
			r.Mid = GrowWork(p, r.Mid, 1)
		}
		r.Mid = append(r.Mid, el)
	} else {
		if WorkControlsEnabled && p.Control != nil {
			r.Post = GrowWork(p, r.Post, 1)
		}
		r.Post = append(r.Post, el)
	}
}

// FinishRun records the run of a repetition that matched, for the next parse.
func (p *Parser) FinishRun(r *RunState) {
	if r.Count < MinRecorded || len(p.Recovered) != r.rec0 || p.Provisional != r.prov0 {
		return
	}
	var elems []RunElem
	if WorkControlsEnabled && p.Control != nil {
		// Keep assembling storage private until every counted copy finishes.
		// The ordinary path retains its in-place replacement and reuse.
		total := r.K + len(r.Mid)
		if r.J >= 0 {
			total += r.T + len(r.Post)
		}
		elems = make([]RunElem, total)
		off := 0
		parts := [][]RunElem{r.Old[:r.K], r.Mid}
		if r.J >= 0 {
			parts = append(parts, r.Old[r.J:r.J+r.T], r.Post)
		}
		for _, part := range parts {
			CopyWork(p, elems[off:], part)
			off += len(part)
		}
	} else if r.J < 0 {
		elems = append(r.Old[:r.K], r.Mid...)
	} else {
		elems = append(slices.Replace(r.Old[:r.J+r.T], r.K, r.J, r.Mid...), r.Post...)
	}
	p.Runs[r.Key] = &RunRecord{gen: p.Gen, Elems: elems}
}

// ResumeRepeat runs the repetition rs (see compiler.repeat) in a Document parse, resuming its run
// from the previous parse when it can.
func (p *Parser) ResumeRepeat(rs *RunSite) (*value.Node, bool) {
	start := p.Pos
	var r RunState
	p.StartRun(&r, rs.Id, rs.Min, rs.Max, rs.Shiftable)
	base := len(p.KidStack)
	if rs.Build {
		p.KidStack = GrowWork(p, p.KidStack, len(r.Old))
	}
	for el := p.NextPrefix(&r); el != nil; el = p.NextPrefix(&r) {
		if rs.Build {
			p.KidStack = append(p.KidStack, el.V)
		}
	}
	for r.More() {
		p.Work()
		if p.Resync(&r) {
			for el := p.NextTail(&r); el != nil; el = p.NextTail(&r) {
				if rs.Build {
					p.KidStack = append(p.KidStack, el.V)
				}
			}
			continue
		}
		m0 := p.Mark()
		prevCut, prevFrame := p.Cut, p.Frame
		p.Cut = false
		var f *Frame
		if rs.OwnScope {
			f = p.NewFrame(len(rs.Scope.Names))
			p.Frame = f
		}
		p.BeginElem(&r)
		v, ok := rs.M(p)
		if ok && rs.Build {
			v = rs.Value(p, v, m0.Pos)
			if rs.OwnScope {
				v = p.AttachCaptures(v, rs.Scope, f, m0.Pos, p.Pos)
			}
		}
		p.EndElem(&r, ok, v)
		cut := p.Cut
		p.Cut, p.Frame = prevCut, prevFrame
		if !ok {
			p.Reset(m0)
			if cut {
				p.Kids(base)
				return nil, false
			}
			break
		}
		if rs.Build {
			p.KidStack = append(p.KidStack, v)
		}
		if p.Pos == m0.Pos {
			break
		}
	}
	if r.Count < rs.Min {
		p.Kids(base)
		return nil, false
	}
	p.FinishRun(&r)
	if !rs.Build {
		return nil, true
	}
	return p.NewNode(value.Data{Kind: value.ListKind, Start: int32(start), End: int32(p.Pos), Children: p.Kids(base), Fresh: true}), true
}

// after reports whether every element examined only input from end on.
func after(elems []RunElem, end int) bool {
	for _, el := range elems {
		if el.From < end {
			return false
		}
	}
	return true
}

// moveValue moves the nodes of the value v of an element recorded in the generation base by
// shift, as moveResult moves the result of a memo entry, and returns it or its copy.
func (p *Parser) moveValue(v *value.Node, shift int, base uint32) *value.Node {
	if v == nil {
		return nil
	}
	if p.Moved == nil {
		p.Moved = map[*value.Node]*value.Node{}
	}
	p.moveBase, p.baseShift = base, shift
	if value.D(v).Start == value.D(v).End {
		v = p.MoveEmpty(v, shift)
	} else if value.D(v).Gen < p.Gen {
		p.MoveTree(v, p.ShiftOf(v))
	}
	p.ClearMovementWork()
	return v
}
