package genrt

import (
	"fmt"
	"sync"
)

// The typed runtime, appended to generated parsers made with pego gen -types. It parses like the
// runtime in runtime.go, but builds the values of the grammar's types directly, as Go values,
// instead of Node trees: the value of an expression is an any holding a pointer to a struct or
// terminal type, *Match, *Error, an int, string or bool, nil, or a *tnode for the CST values that
// actions consume (Seq, List and Operator nodes). Every step mirrors runtime.go, so the result is
// that of converting the Node tree that Parse returns.
//
// tparser embeds the parser and redefines the methods that handle values; the generated
// methods of the typed rules (trules) run on it.

// Span is the range of input a typed value covers, in the parse's position unit (End is exclusive).
type Span struct{ Start, End int }

func (s *Span) tspan() (int, int)       { return s.Start, s.End }
func (s *Span) tsetSpan(start, end int) { s.Start, s.End = start, end }

// Match is a terminal made by a literal, a character class, ., @a or _.
type Match struct {
	Span
	Text string
}

// Error is input skipped by error recovery (#recover).
type Error struct {
	Span
	Text    string
	Message string
}

// tmatch and terror are a Match and an Error while the typed runtime handles them: whether the
// value is fresh (see Node.fresh) is part of the state of the parse, not of the result. The
// result holds pointers to the embedded Match and Error (tpub).
type tmatch struct {
	Match
	fresh bool
}

type terror struct {
	Error
	fresh bool
}

// tpub returns the public form of a value: *Match and *Error for tmatch and terror.
func tpub(v any) any {
	switch x := v.(type) {
	case *tmatch:
		return &x.Match
	case *terror:
		return &x.Error
	}
	return v
}

func tpubMatch(v any) *Match {
	if x, ok := v.(*tmatch); ok {
		return &x.Match
	}
	return nil
}

func tpubError(v any) *Error {
	if x, ok := v.(*terror); ok {
		return &x.Error
	}
	return nil
}

// tval is a node value of the typed runtime: a value of a struct or terminal type, *tmatch,
// *terror or *tnode.
type tval interface {
	tspan() (int, int)
	tsetSpan(start, end int)
	tname() string                  // type name
	ttext() (string, bool)          // text of a terminal
	tkids() []any                   // children of a CST node
	tfield(name string) (any, bool) // field value, and whether the type has the field (structs)
	tstruct() bool                  // a struct type
	tfresh() bool
	tsetFresh(bool)
}

func (m *tmatch) tname() string             { return "Match" }
func (m *tmatch) ttext() (string, bool)     { return m.Text, true }
func (m *tmatch) tkids() []any              { return nil }
func (m *tmatch) tfield(string) (any, bool) { return nil, true }
func (m *tmatch) tstruct() bool             { return false }
func (m *tmatch) tfresh() bool              { return m.fresh }
func (m *tmatch) tsetFresh(f bool)          { m.fresh = f }
func (e *terror) tname() string             { return "Error" }
func (e *terror) ttext() (string, bool)     { return e.Text, true }
func (e *terror) tkids() []any              { return nil }
func (e *terror) tstruct() bool             { return false }
func (e *terror) tfresh() bool              { return e.fresh }
func (e *terror) tsetFresh(f bool)          { e.fresh = f }
func (e *terror) tfield(name string) (any, bool) {
	if name == "message" {
		return e.Message, true
	}
	return nil, true
}

// tnode is a Seq, List or Operator node of the typed runtime.
type tnode struct {
	Span
	typ    string
	kids   []any
	fields []tfield
	fresh  bool
}

type tfield struct {
	name string
	val  any
}

func (n *tnode) tname() string         { return n.typ }
func (n *tnode) ttext() (string, bool) { return "", false }
func (n *tnode) tkids() []any          { return n.kids }
func (n *tnode) tstruct() bool         { return false }
func (n *tnode) tfresh() bool          { return n.fresh }
func (n *tnode) tsetFresh(f bool)      { n.fresh = f }
func (n *tnode) tfield(name string) (any, bool) {
	for _, f := range n.fields {
		if f.name == name {
			return f.val, true
		}
	}
	return nil, true
}

func (n *tnode) set(name string, v any) {
	for i := range n.fields {
		if n.fields[i].name == name {
			n.fields[i].val = v
			return
		}
	}
	n.fields = append(n.fields, tfield{name, v})
}

// asTval returns v as a node value, or nil. Values never hold nil pointers: nil is always the
// untyped nil.
func asTval(v any) tval {
	n, _ := v.(tval)
	return n
}

// --- Rules ---

// trule is a rule of the typed runtime: the rule's analysis results and its typed body.
type trule struct {
	*rule
	body   func(p *tparser, min int) (any, bool)
	action func(c *tctx) any
	pratt  *tpratt
	// term makes the value of a rule of a terminal type.
	term func(p *tparser, start, end int, text string) tval
}

// trules is the typed rule table set up by the generated code.
var trules []*trule

// tAs returns v as a T, or the zero T (nil) if it is not one.
func tAs[T any](v any) T {
	t, _ := v.(T)
	return t
}

// tptr and tderef return the value of a field of a pointer type as a value of the typed runtime.
func tptr[T any](p *T) any {
	if p == nil {
		return nil
	}
	return p
}

func tderef[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// tlistOf returns a List node of kids, for reading a list field of a struct in an action.
func tlistOf(kids []any) any {
	n := &tnode{typ: "List", kids: kids}
	first := true
	for _, it := range kids {
		t := asTval(it)
		if t == nil {
			continue
		}
		s, e := t.tspan()
		if first || s < n.Start {
			n.Start = s
		}
		if first || e > n.End {
			n.End = e
		}
		first = false
	}
	return n
}

// tparse parses the whole input with the typed rule r, and returns the result converted by conv
// (before the parser's scratch memory, which the result may be in, is cleared). ext holds the
// generated code's chunks.
func tparse[T any](r *trule, input string, units []Unit, ext any, conv func(any) T) (T, error) {
	p, _ := tpool.Get().(*tparser)
	if p == nil {
		p = &tparser{parser: &parser{}}
	}
	v, err := p.run(r, input, units, ext)
	res := conv(v)
	p.recycle()
	tpool.Put(p)
	return res, err
}

// run parses the whole input with the typed rule r.
func (p *tparser) run(r *trule, input string, units []Unit, ext any) (v any, err error) {
	p.ext = ext
	p.memo.stride = nseen
	if len(units) > 0 && units[0] == Bytes {
		p.unit, p.bs, p.n = Bytes, input, len(input)
	} else {
		p.setSource(input)
	}
	defer func() {
		if x := recover(); x != nil {
			switch x := x.(type) {
			case fatal:
				v, err = nil, x.err
			case evalError: // in an action (predicates recover their own)
				v, err = nil, fmt.Errorf("action in %s: %s", p.where, x.msg)
			default:
				panic(x)
			}
		}
	}()
	v, ok := p.call(r, 0)
	if ok && p.pos == p.n {
		if len(p.recovered) > 0 {
			return v, SyntaxErrors(append([]*SyntaxError(nil), p.recovered...))
		}
		return v, nil
	}
	if ok {
		p.expect(p.pos, idEndInput)
	}
	return nil, p.makeError(p.farthest, p.exp[p.expBase:])
}

// tpool keeps parsers of the typed runtime, with their scratch memory, for later parses. What a
// parse returns never refers to that memory: CST nodes, value lists, frames and memo entries are
// consumed by the parse itself (typed values come from the generated code's chunks, ext).
var tpool sync.Pool

// recycle clears the parser for the next parse, keeping its scratch memory.
func (p *tparser) recycle() {
	q := p.parser
	seen, calls := q.memo.seen, q.memo.calls
	clear(seen)
	clear(calls)
	*q = parser{in: q.in[:0], offs: q.offs[:0], exp: q.exp[:0], arena: q.arena[:0]}
	q.memo.seen, q.memo.calls = seen, calls
	p.nodes.reset()
	p.vals.reset()
	p.frames.reset()
	p.fields.reset()
	p.memoSlab.reset()
	clear(p.trail)
	clear(p.kidStack)
	clear(p.saved)
	clear(p.memoSlots)
	free := p.free
	for i := range free {
		free[i] = free[i][:0]
	}
	*p = tparser{parser: q, trail: p.trail[:0], kidStack: p.kidStack[:0], saved: p.saved[:0], memoSlots: p.memoSlots[:0],
		nodes: p.nodes, vals: p.vals, frames: p.frames, fields: p.fields, memoSlab: p.memoSlab, free: free}
}

// tarena allocates values of type T in chunks that are kept from one parse to the next (tpool):
// reset clears what was used and starts again at the first chunk.
type tarena[T any] struct {
	chunks [][]T
	ci     int // the chunk in use
	off    int // the next free element in it
}

const tchunk = 512

// slice returns n zero elements (capacity n).
func (a *tarena[T]) slice(n int) []T {
	if n > tchunk/4 {
		return make([]T, n)
	}
	if a.ci < len(a.chunks) && a.off+n > len(a.chunks[a.ci]) {
		a.ci++
		a.off = 0
	}
	if a.ci == len(a.chunks) {
		a.chunks = append(a.chunks, make([]T, tchunk))
	}
	s := a.chunks[a.ci][a.off : a.off+n : a.off+n]
	a.off += n
	return s
}

func (a *tarena[T]) one() *T { return &a.slice(1)[0] }

func (a *tarena[T]) reset() {
	for i := 0; i <= a.ci && i < len(a.chunks); i++ {
		clear(a.chunks[i])
	}
	a.ci, a.off = 0, 0
}

// tparser is the parser state of the typed runtime.
type tparser struct {
	*parser
	ext      any    // the generated code's allocation chunks
	where    string // the rule whose action is being evaluated
	frame    *tframe
	trail    []tundo
	created  []tval // struct values made by the action being evaluated
	kidStack []any
	ac       tctx
	saved    []any
	matches  []tmatch // returned in values, so not kept for the next parse
	// Scratch memory, kept for the next parse (recycle).
	nodes     tarena[tnode]
	vals      tarena[any]
	frames    tarena[tframe]
	fields    tarena[tfield]
	memoSlab  tarena[tmemoEntry]
	memoSlots []*tmemoEntry // memo entries by position (chains)
	free      [8][]*tframe  // frames freed by freeFrame, by size
}

type tframe struct{ vals []any }

// freeFrame makes the frame f, which nothing refers to any more, available to newFrame. A rule's
// frame is dead when the rule returns: the values have been copied into its result, and the
// trail entries that refer to it are gone.
func (p *tparser) freeFrame(f *tframe) {
	n := len(f.vals)
	if n == 0 || n >= len(p.free) {
		return
	}
	clear(f.vals)
	p.free[n] = append(p.free[n], f)
}

var emptyTFrame = &tframe{}

type tundo struct {
	f    *tframe
	slot int
	old  any
}

func (p *tparser) mark() mark {
	return mark{pos: p.pos, env: p.env, trail: len(p.trail), recovered: len(p.recovered)}
}

func (p *tparser) reset(m mark) {
	p.pos = m.pos
	p.env = m.env
	p.recovered = p.recovered[:m.recovered]
	for i := len(p.trail) - 1; i >= m.trail; i-- {
		u := p.trail[i]
		u.f.vals[u.slot] = u.old
	}
	p.trail = p.trail[:m.trail]
}

func (p *tparser) setCapture(slot int, v any) {
	p.trail = append(p.trail, tundo{f: p.frame, slot: slot, old: p.frame.vals[slot]})
	p.frame.vals[slot] = v
}

// --- Allocation ---

func (p *tparser) newNode(typ string, start, end int, kids []any) *tnode {
	n := p.nodes.one()
	*n = tnode{Span: Span{start, end}, typ: typ, kids: kids, fresh: true}
	return n
}

func (p *tparser) newMatch(start, end int, text string, fresh bool) *tmatch {
	if len(p.matches) == 0 {
		p.matches = make([]tmatch, nodeChunk)
	}
	m := &p.matches[0]
	p.matches = p.matches[1:]
	*m = tmatch{Match{Span{start, end}, text}, fresh}
	return m
}

// newVals returns a value list of length and capacity n.
func (p *tparser) newVals(n int) []any {
	if n == 0 {
		return []any{}
	}
	return p.vals.slice(n)
}

func (p *tparser) tfields(n int) []tfield {
	return p.fields.slice(n)[:0]
}

// kids returns p.kidStack[base:] as a value list and pops it.
func (p *tparser) kids(base int) []any {
	ks := p.newVals(len(p.kidStack) - base)
	copy(ks, p.kidStack[base:])
	p.dropKids(base)
	return ks
}

func (p *tparser) dropKids(base int) {
	clear(p.kidStack[base:])
	p.kidStack = p.kidStack[:base]
}

func (p *tparser) newFrame(n int) *tframe {
	if n == 0 {
		return emptyTFrame
	}
	if n < len(p.free) {
		if fs := p.free[n]; len(fs) > 0 {
			f := fs[len(fs)-1]
			p.free[n] = fs[:len(fs)-1]
			return f
		}
	}
	f := p.frames.one()
	f.vals = p.newVals(n)
	return f
}

// --- Memo ---

// The memo of typed results (tparser.memoSlots, chains of entries by position) is keyed like
// memoTable, whose firstCall bookkeeping the typed runtime shares.
type tmemoEntry struct {
	val      any
	errs     []*SyntaxError
	expected []expID
	next     *tmemoEntry
	env      []any
	end      int
	far      int
	rule     int32
	min      int32
	ok       bool
	silent   bool
	growing  bool
}

func (p *tparser) memoGet(rule, pos, min int, env []any) *tmemoEntry {
	if pos >= len(p.memoSlots) {
		return nil
	}
	for e := p.memoSlots[pos]; e != nil; e = e.next {
		if e.rule == int32(rule) && e.min == int32(min) && sameValues(e.env, env) {
			return e
		}
	}
	return nil
}

func (p *tparser) memoPut(rule, pos, min int, env []any, e *tmemoEntry) {
	if pos >= len(p.memoSlots) {
		if pos < cap(p.memoSlots) {
			p.memoSlots = p.memoSlots[:pos+1]
		} else {
			grown := make([]*tmemoEntry, pos+1, max(2*cap(p.memoSlots), pos+1, 64))
			copy(grown, p.memoSlots)
			p.memoSlots = grown
		}
	}
	e.rule, e.min, e.env = int32(rule), int32(min), env
	link := &p.memoSlots[pos]
	for x := *link; x != nil; link, x = &x.next, x.next {
		if x.rule == e.rule && x.min == e.min && sameValues(x.env, env) {
			e.next = x.next
			*link = e
			return
		}
	}
	e.next = p.memoSlots[pos]
	p.memoSlots[pos] = e
}

// --- Rule calls ---

func (p *tparser) call(r *trule, min int) (any, bool) {
	start := p.pos
	if !r.memo && !r.leader || p.firstCall(r.rule) {
		if len(r.scope) == 0 {
			return p.invokePlain(r, min)
		}
		rec := len(p.recovered)
		v, ok := p.invoke(r, min)
		if !ok {
			p.pos = start
			p.recovered = p.recovered[:rec]
		}
		return v, ok
	}
	env := p.envValues(r.vars)
	if e := p.memoGet(r.id, start, min, env); e != nil && (e.growing || (!e.silent || p.silent > 0) && (len(e.errs) == 0 || e.silent == (p.silent > 0))) {
		if !e.growing {
			p.mergeExpected(e.far, e.expected)
		}
		if !e.ok {
			return nil, false
		}
		p.pos = e.end
		p.recovered = append(p.recovered, e.errs...)
		return e.val, true
	}
	rec := len(p.recovered)
	m := p.isolate(start)
	var v any
	var ok bool
	var e *tmemoEntry
	if r.leader {
		v, ok = p.grow(r, min, env)
		e = p.memoGet(r.id, start, min, env)
	} else {
		v, ok = p.invoke(r, min)
		if !ok {
			p.recovered = p.recovered[:rec]
		}
		e = p.memoSlab.one()
		*e = tmemoEntry{val: v, ok: ok, end: p.pos, silent: p.silent > 0,
			errs: append([]*SyntaxError(nil), p.recovered[rec:]...)}
		p.memoPut(r.id, start, min, env, e)
	}
	far, inner := p.unisolate(m)
	e.far, e.expected = far, p.keep(inner)
	p.mergeExpected(far, inner)
	if !ok {
		p.pos = start
	}
	return v, ok
}

// callMemo is the memoized part of call, for the rule r invoked by inv (the generated i<id>):
// the rule is called at the position again, or its memoization is no longer deferred.
func (p *tparser) callMemo(r *trule, inv func(*tparser) (any, bool)) (any, bool) {
	start := p.pos
	env := p.envValues(r.vars)
	if e := p.memoGet(r.id, start, 0, env); e != nil && (e.growing || (!e.silent || p.silent > 0) && (len(e.errs) == 0 || e.silent == (p.silent > 0))) {
		if !e.growing {
			p.mergeExpected(e.far, e.expected)
		}
		if !e.ok {
			return nil, false
		}
		p.pos = e.end
		p.recovered = append(p.recovered, e.errs...)
		return e.val, true
	}
	rec := len(p.recovered)
	m := p.isolate(start)
	v, ok := inv(p)
	if !ok {
		p.recovered = p.recovered[:rec]
	}
	e := p.memoSlab.one()
	*e = tmemoEntry{val: v, ok: ok, end: p.pos, silent: p.silent > 0,
		errs: append([]*SyntaxError(nil), p.recovered[rec:]...)}
	p.memoPut(r.id, start, 0, env, e)
	far, inner := p.unisolate(m)
	e.far, e.expected = far, p.keep(inner)
	p.mergeExpected(far, inner)
	if !ok {
		p.pos = start
	}
	return v, ok
}

func (p *tparser) grow(r *trule, min int, env []any) (any, bool) {
	start := p.pos
	rec := len(p.recovered)
	best := p.memoSlab.one()
	*best = tmemoEntry{end: start, growing: true}
	p.memoPut(r.id, start, min, env, best)
	for {
		v, ok := p.invoke(r, min)
		if !ok || (best.ok && p.pos <= best.end) {
			break
		}
		best = p.memoSlab.one()
		*best = tmemoEntry{val: v, ok: true, end: p.pos, growing: true, errs: append([]*SyntaxError(nil), p.recovered[rec:]...)}
		p.memoPut(r.id, start, min, env, best)
		p.pos = start
		p.recovered = p.recovered[:rec]
	}
	best.growing = false
	best.silent = p.silent > 0
	p.pos = best.end
	p.recovered = append(p.recovered[:rec], best.errs...)
	return best.val, best.ok
}

func (p *tparser) invoke(r *trule, min int) (any, bool) {
	f := p.newFrame(len(r.scope))
	prevFrame, prevEnv, prevCut, trail := p.frame, p.env, p.cut, len(p.trail)
	p.frame, p.cut = f, false
	p.depth++
	if p.depth > maxDepth {
		p.tooDeep()
	}
	start := p.pos
	v, ok := r.body(p, min)
	p.depth--
	p.frame, p.cut = prevFrame, prevCut
	p.trail = p.trail[:trail]
	if ok {
		v = p.finish(r, f, v, start)
	}
	p.env = prevEnv
	p.freeFrame(f)
	return v, ok
}

func (p *tparser) invokePlain(r *trule, min int) (any, bool) {
	start, rec, trail := p.pos, len(p.recovered), len(p.trail)
	prevEnv, prevCut := p.env, p.cut
	p.cut = false
	p.depth++
	if p.depth > maxDepth {
		p.tooDeep()
	}
	v, ok := r.body(p, min)
	p.depth--
	p.cut = prevCut
	p.trail = p.trail[:trail]
	if ok {
		v = p.finish(r, emptyTFrame, v, start)
	} else {
		p.pos = start
		p.recovered = p.recovered[:rec]
	}
	p.env = prevEnv
	return v, ok
}

// finish makes the rule's value from the value of its body.
func (p *tparser) finish(r *trule, f *tframe, v any, start int) any {
	if r.novalue {
		return nil
	}
	if r.action != nil {
		c := p.useCtx(tctx{p: p, f: f, start: start, end: p.pos, cbase: len(p.created)})
		n := asTval(v)
		if r.bodyIsSeq && n != nil {
			c.items = n.tkids()
		} else {
			c.one[0] = n
			c.items = c.one[:]
		}
		return c.result(r.action, r.name)
	}
	if r.terminalType != "" {
		return r.term(p, start, p.pos, p.text(start, p.pos))
	}
	if len(r.scope) > 0 {
		v = p.attachCaptures(v, r.scope, f, start, p.pos)
	}
	n := asTval(v)
	if n == nil {
		return nil
	}
	n.tsetFresh(false)
	return n
}

// attachCaptures attaches the non-nil captures as fields of the value (a CST node), first
// wrapping it in a Seq node unless it is a fresh one that is not itself captured.
func (p *tparser) attachCaptures(v any, names []string, f *tframe, start, end int) any {
	n := asTval(v)
	found := false
	wrap := n == nil || !n.tfresh()
	for _, c := range f.vals {
		if c != nil {
			found = true
		}
		if c != nil && n != nil && c == any(n) {
			wrap = true
		}
	}
	if !found {
		return v
	}
	t, isNode := n.(*tnode)
	if wrap || !isNode {
		kids := p.newVals(1)
		kids[0] = v
		t = p.newNode("Seq", start, end, kids)
	}
	if t.fields == nil {
		t.fields = p.tfields(len(names))
	}
	for i, name := range names {
		if f.vals[i] != nil {
			t.set(name, f.vals[i])
		}
	}
	return t
}

// --- Terminals ---

func (p *tparser) matchLiteral(rs []rune, text string, desc expID, build bool) (any, bool) {
	start := p.pos
	if _, ok := p.parser.matchLiteral(rs, text, desc, false); !ok {
		return nil, false
	}
	if !build {
		return nil, true
	}
	return p.newMatch(start, p.pos, text, true), true
}

func (p *tparser) matchAny(build bool) (any, bool) {
	_, size, ok := p.peek()
	if !ok {
		p.expect(p.pos, idAny)
		return nil, false
	}
	return p.single(size, build)
}

func (p *tparser) single(size int, build bool) (any, bool) {
	start := p.pos
	p.pos += size
	if !build {
		return nil, true
	}
	return p.newMatch(start, p.pos, p.text(start, p.pos), true), true
}

func (p *tparser) anchor(ok bool, desc expID) (any, bool) {
	if ok {
		return nil, true
	}
	p.expect(p.pos, desc)
	return nil, false
}

// --- Attributes ---

type tmatcher = func(p *tparser) (any, bool)

func (p *tparser) tracked(m tmatcher) (v any, ok bool, far int, expected []expID) {
	mark := p.isolate(p.pos)
	v, ok = m(p)
	far, expected = p.unisolate(mark)
	return v, ok, far, expected
}

func (p *tparser) errorAttr(m tmatcher, msg expID) (any, bool) {
	v, ok, far, expected := p.tracked(m)
	if ok {
		p.mergeExpected(far, expected)
		return v, true
	}
	p.expect(far, msg)
	return nil, false
}

func (p *tparser) recoverAttr(m, skip tmatcher, build bool) (any, bool) {
	m0 := p.mark()
	v, ok, far, expected := p.tracked(m)
	if ok {
		p.mergeExpected(far, expected)
		return v, true
	}
	expected = p.keep(expected)
	p.reset(m0)
	if _, ok := skip(p); !ok || p.pos == m0.pos {
		p.reset(m0)
		p.mergeExpected(far, expected)
		return nil, false
	}
	e := p.makeError(far, expected)
	p.recovered = append(p.recovered, e)
	if !build {
		return nil, true
	}
	return &terror{Error{Span: Span{m0.pos, p.pos}, Text: p.text(m0.pos, p.pos), Message: e.Error()}, true}, true
}

// --- Actions and predicates ---

// tctx is the context of evaluating an action or a predicate (actx in the typed runtime).
type tctx struct {
	p            *tparser
	f            *tframe
	items        []any
	start, end   int
	cbase        int
	lhs, rhs, op any
	one          [1]any
}

func (p *tparser) useCtx(c tctx) *tctx {
	p.ac = c
	return &p.ac
}

// result evaluates an action. Its errors fail the parse: tparse reports them with the rule
// name, which result records in p.where instead of deferring a recovery for each action.
func (c *tctx) result(action func(*tctx) any, where string) (n any) {
	prev := c.p.where
	c.p.where = where
	v := action(c)
	c.p.where = prev
	if v == nil {
		return nil
	}
	t := asTval(v)
	if t == nil {
		evalErrorf("result must be a node, got %s", ttypeName(v))
	}
	for _, x := range c.p.created[c.cbase:] {
		if x == t {
			t.tsetSpan(c.start, c.end)
			break
		}
	}
	c.p.created = c.p.created[:c.cbase]
	t.tsetFresh(false)
	return t
}

func (p *tparser) predicate(t func(*tctx) any) (ok bool) {
	base, kids := len(p.created), len(p.kidStack)
	defer func() {
		p.created = p.created[:base]
		if x := recover(); x != nil {
			if _, isEval := x.(evalError); !isEval {
				panic(x)
			}
			p.dropKids(kids)
			ok = false
		}
	}()
	c := p.useCtx(tctx{p: p, f: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
	v := t(c)
	if b, isBool := v.(bool); isBool && !b {
		return false
	}
	return true
}

func (p *tparser) assign(name string, t func(*tctx) any) (ok bool) {
	base, kids := len(p.created), len(p.kidStack)
	defer func() {
		p.created = p.created[:base]
		if x := recover(); x != nil {
			if _, isEval := x.(evalError); !isEval {
				panic(x)
			}
			p.dropKids(kids)
			ok = false
		}
	}()
	c := p.useCtx(tctx{p: p, f: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
	v := t(c)
	if _, isFn := v.(func(...any) any); isFn {
		return false
	}
	p.env = &env{name: name, val: v, next: p.env}
	return true
}

// tvalOrNil is the typed runtime's nodeOrNil: values are already any, with the untyped nil.
func tvalOrNil(v any) any { return v }

func (c *tctx) cap(slot int) any { return tvalOrNil(c.f.vals[slot]) }

func (c *tctx) item(n int) any {
	if n == 0 {
		return c.newList(c.items)
	}
	if n > len(c.items) {
		evalErrorf("$%d is out of range (%d elements)", n, len(c.items))
	}
	return tvalOrNil(c.items[n-1])
}

func (c *tctx) lookup(name string) any {
	for e := c.p.env; e != nil; e = e.next {
		if e.name == name {
			return e.val
		}
	}
	evalErrorf("variable %s is not defined", name)
	return nil
}

// ttypeName is typeName for values of the typed runtime.
func ttypeName(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	case string:
		return "string"
	case bool:
		return "bool"
	}
	if t, ok := v.(tval); ok {
		return t.tname()
	}
	return fmt.Sprintf("%T", v)
}

// spanOf widens the range [*start, *end) to cover the field value v if it is a node (first is
// set until a node has been seen), as newStruct does.
func spanOf(v any, first *bool, start, end *int) {
	child := asTval(v)
	if child == nil {
		return
	}
	s, e := child.tspan()
	if *first || s < *start {
		*start = s
	}
	if *first || e > *end {
		*end = e
	}
	*first = false
}

// made finishes a struct value n made by an action with the given range (see newStruct).
func (c *tctx) made(n tval, first bool, start, end int) any {
	if first {
		start, end = c.start, c.end
	}
	n.tsetSpan(start, end)
	c.p.created = append(c.p.created, n)
	return n
}

func (c *tctx) newList(items []any) any {
	return c.listNode(append(c.p.newVals(len(items))[:0], items...))
}

// listNode returns a List node with the child list kids (which it takes over).
func (c *tctx) listNode(kids []any) *tnode {
	n := c.p.newNode("List", c.start, c.start, kids)
	n.fresh = false
	first := true
	for _, it := range kids {
		t := asTval(it)
		if t == nil {
			continue
		}
		s, e := t.tspan()
		if first || s < n.Start {
			n.Start = s
		}
		if first || e > n.End {
			n.End = e
		}
		first = false
	}
	return n
}

func (c *tctx) member(x any, name string) any {
	n := asTval(x)
	if n == nil {
		evalErrorf("cannot access .%s of %s", name, ttypeName(x))
	}
	switch name {
	case "startPos":
		s, _ := n.tspan()
		return s
	case "endPos":
		_, e := n.tspan()
		return e
	case "children":
		return c.newList(n.tkids())
	}
	v, ok := n.tfield(name)
	if !ok {
		evalErrorf("%s has no field %s", n.tname(), name)
	}
	return tvalOrNil(v)
}

func trtUnary(op string, x any) any {
	switch op {
	case "-":
		if i, ok := x.(int); ok {
			return -i
		}
	case "!":
		if b, ok := x.(bool); ok {
			return !b
		}
	}
	evalErrorf("invalid operand %s for unary %s", ttypeName(x), op)
	return nil
}

func trtBool(op string, v any) bool {
	b, ok := v.(bool)
	if !ok {
		evalErrorf("invalid operand %s for %s", ttypeName(v), op)
	}
	return b
}

func trtAnd(l any, r func() any) any {
	if !trtBool("&&", l) {
		return false
	}
	return trtBool("&&", r())
}

func trtOr(l any, r func() any) any {
	if trtBool("||", l) {
		return true
	}
	return trtBool("||", r())
}

func trtEqual(a, b any) bool {
	switch a.(type) {
	case int, string, bool:
		return a == b
	case nil:
		return b == nil
	}
	an, ok := a.(tval)
	if !ok {
		return false
	}
	bn, ok := b.(tval)
	return ok && an == bn
}

func trtBinary(op string, l, r any) any {
	switch op {
	case "==":
		return trtEqual(l, r)
	case "!=":
		return !trtEqual(l, r)
	}
	switch l := l.(type) {
	case int:
		if r, ok := r.(int); ok {
			switch op {
			case "+":
				return l + r
			case "-":
				return l - r
			case "*":
				return l * r
			case "/", "%":
				if r == 0 {
					evalErrorf("division by zero")
				}
				if op == "/" {
					return l / r
				}
				return l % r
			case "<":
				return l < r
			case "<=":
				return l <= r
			case ">":
				return l > r
			case ">=":
				return l >= r
			}
		}
	case string:
		if r, ok := r.(string); ok {
			switch op {
			case "+":
				return l + r
			case "<":
				return l < r
			case "<=":
				return l <= r
			case ">":
				return l > r
			case ">=":
				return l >= r
			}
		}
	}
	evalErrorf("invalid operands %s and %s for %s", ttypeName(l), ttypeName(r), op)
	return nil
}

func (c *tctx) length(x any) any {
	switch x := x.(type) {
	case string:
		return c.p.unitLen(x)
	case nil:
		return 0
	}
	if n, ok := x.(tval); ok {
		if text, ok := n.ttext(); ok {
			return c.p.unitLen(text)
		}
		return len(n.tkids())
	}
	evalErrorf("len: invalid argument %s", ttypeName(x))
	return nil
}

func (c *tctx) text(x any) any { return c.textStr(x) }

func (c *tctx) textStr(x any) string {
	switch x := x.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	if n, ok := x.(tval); ok {
		if text, ok := n.ttext(); ok {
			return text
		}
		s, e := n.tspan()
		return c.p.text(s, e)
	}
	evalErrorf("text: invalid argument %s", ttypeName(x))
	return ""
}

func tlistItems(fn string, v any) []any {
	if v == nil {
		return nil
	}
	if n, ok := v.(tval); ok {
		return n.tkids()
	}
	evalErrorf("%s: expected a list, got %s", fn, ttypeName(v))
	return nil
}

func tasNode(fn string, v any) any {
	if v == nil {
		return nil
	}
	if _, ok := v.(tval); ok {
		return v
	}
	evalErrorf("%s: list elements must be nodes, got %s", fn, ttypeName(v))
	return nil
}

func trtFold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	items := tlistItems(fn, list)
	for i := range items {
		it := items[i]
		if right {
			it = items[len(items)-1-i]
		}
		acc = f(acc, tvalOrNil(it))
	}
	return acc
}

func (c *tctx) mapList(list any, f func(item any) any) any {
	base := len(c.p.kidStack)
	c.pushMap(list, f)
	return c.endList(base)
}

func (c *tctx) list(args ...any) any {
	base := len(c.p.kidStack)
	c.pushList(args...)
	return c.endList(base)
}

func (c *tctx) concat(args ...any) any {
	base := len(c.p.kidStack)
	for _, a := range args {
		c.pushItems(a)
	}
	return c.endList(base)
}

func (c *tctx) pushMap(list any, f func(item any) any) {
	for _, it := range tlistItems("map", list) {
		c.p.kidStack = append(c.p.kidStack, tasNode("map", f(tvalOrNil(it))))
	}
}

func (c *tctx) pushList(args ...any) {
	for _, a := range args {
		c.p.kidStack = append(c.p.kidStack, tasNode("list", a))
	}
}

func (c *tctx) pushItems(v any) {
	c.p.kidStack = append(c.p.kidStack, tlistItems("concat", v)...)
}

func (c *tctx) endList(base int) any {
	return c.listNode(c.p.kids(base))
}

// --- Pratt expressions ---

type tpratt struct {
	skip     tmatcher
	operands []*tprattLine
	prefix   []*tprattOp
	led      []*tprattOp
}

type tprattLine struct {
	scope  []string
	m      tmatcher
	action func(*tctx) any
	isSeq  bool
}

type tprattOp struct {
	id    int
	kind  string
	assoc string
	level int
	line  *tprattLine
}

type tprattAttempt struct {
	op         *tprattOp
	frame      *tframe
	v          any
	start, end int
	env        *env
	cut        bool
	errs       []*SyntaxError
}

func (p *tparser) skipTrivia(pr *tpratt) {
	if pr.skip == nil {
		return
	}
	m := p.mark()
	p.silent++
	_, ok := pr.skip(p)
	p.silent--
	if !ok {
		p.reset(m)
	}
}

func (p *tparser) longest(ops []*tprattOp) (best tprattAttempt, cutFailed bool) {
	m0 := p.mark()
	for _, op := range ops {
		prevFrame, prevCut := p.frame, p.cut
		f := p.newFrame(len(op.line.scope))
		p.frame, p.cut = f, false
		v, ok := op.line.m(p)
		cut := p.cut
		p.frame, p.cut = prevFrame, prevCut
		if ok && p.pos == m0.pos && op.kind != "infix" {
			ok = false
		}
		if ok && (best.op == nil || p.pos > best.end) {
			best = tprattAttempt{op: op, frame: f, v: v, start: m0.pos, end: p.pos, env: p.env, cut: cut,
				errs: append([]*SyntaxError(nil), p.recovered[m0.recovered:]...)}
		}
		if !ok && cut {
			cutFailed = true
		}
		if len(f.vals) == 0 {
			p.reset(m0)
			continue
		}
		p.saved = append(p.saved[:0], f.vals...)
		p.reset(m0)
		copy(f.vals, p.saved)
	}
	return best, cutFailed
}

func (p *tparser) apply(a *tprattAttempt) {
	p.pos, p.env = a.end, a.env
	p.recovered = append(p.recovered, a.errs...)
}

func (p *tparser) prattParse(r *trule, min int) (any, bool) {
	pr := r.pratt
	lhs, ok := p.prattNud(r)
	if !ok {
		return nil, false
	}
	lastNone := -1
	for {
		m0 := p.mark()
		p.skipTrivia(pr)
		a, cutFailed := p.longest(pr.led)
		if a.op == nil {
			p.reset(m0)
			if cutFailed {
				return nil, false
			}
			break
		}
		if a.op.level <= min || a.op.assoc == "none" && a.op.level == lastNone {
			p.reset(m0)
			break
		}
		p.apply(&a)
		if a.op.kind == "postfix" {
			lhs = p.prattBuild(r, &a, lhs, nil)
			lastNone = -1
			continue
		}
		rmin := a.op.level
		if a.op.assoc == "right" {
			rmin--
		}
		rhs, ok := p.prattParse(r, rmin)
		if !ok {
			if a.cut {
				return nil, false
			}
			p.reset(m0)
			break
		}
		lhs = p.prattBuild(r, &a, lhs, rhs)
		lastNone = -1
		if a.op.assoc == "none" {
			lastNone = a.op.level
		}
	}
	return lhs, true
}

func (p *tparser) prattNud(r *trule) (any, bool) {
	pr := r.pratt
	p.skipTrivia(pr)
	m0 := p.mark()
	if a, _ := p.longest(pr.prefix); a.op != nil {
		p.apply(&a)
		rhs, ok := p.prattParse(r, a.op.level)
		if ok {
			return p.prattBuild(r, &a, nil, rhs), true
		}
		if a.cut {
			return nil, false
		}
		p.reset(m0)
	}
	for _, o := range pr.operands {
		prevFrame, prevCut := p.frame, p.cut
		f := p.newFrame(len(o.scope))
		p.frame, p.cut = f, false
		v, ok := o.m(p)
		cut := p.cut
		p.frame, p.cut = prevFrame, prevCut
		if ok {
			return p.lineResult(r, o, f, v, m0.pos), true
		}
		p.reset(m0)
		if cut {
			return nil, false
		}
	}
	return nil, false
}

func (p *tparser) lineResult(r *trule, l *tprattLine, f *tframe, v any, start int) any {
	if r.novalue {
		return nil
	}
	if l.action != nil {
		c := p.useCtx(tctx{p: p, f: f, start: start, end: p.pos, cbase: len(p.created)})
		n := asTval(v)
		if l.isSeq && n != nil {
			c.items = n.tkids()
		} else {
			c.one[0] = n
			c.items = c.one[:]
		}
		return c.result(l.action, r.name)
	}
	if len(l.scope) > 0 {
		v = p.attachCaptures(v, l.scope, f, start, p.pos)
	}
	if n := asTval(v); n != nil {
		n.tsetFresh(false)
		return n
	}
	return nil
}

func (p *tparser) prattBuild(r *trule, a *tprattAttempt, lhs, rhs any) any {
	if r.novalue {
		return nil
	}
	start, end := a.start, p.pos
	if l := asTval(lhs); l != nil {
		start, _ = l.tspan()
	}
	if a.op.kind == "postfix" {
		end = a.end
	}
	l := a.op.line
	if l.action != nil {
		op := p.newMatch(a.start, a.end, p.text(a.start, a.end), false)
		c := p.useCtx(tctx{p: p, f: a.frame, start: start, end: end, cbase: len(p.created),
			op: op, lhs: tvalOrNil(lhs), rhs: tvalOrNil(rhs)})
		return c.result(l.action, r.name)
	}
	opv := a.v
	if len(l.scope) > 0 {
		opv = p.attachCaptures(opv, l.scope, a.frame, a.start, a.end)
	}
	if n := asTval(opv); n != nil {
		n.tsetFresh(false)
	}
	var kids []any
	switch a.op.kind {
	case "prefix":
		kids = p.newVals(2)
		kids[0], kids[1] = tvalOrNil(opv), tvalOrNil(rhs)
	case "postfix":
		kids = p.newVals(2)
		kids[0], kids[1] = tvalOrNil(lhs), tvalOrNil(opv)
	default:
		kids = p.newVals(3)
		kids[0], kids[1], kids[2] = tvalOrNil(lhs), tvalOrNil(opv), tvalOrNil(rhs)
	}
	n := p.newNode("Operator", start, end, kids)
	n.fresh = false
	n.fields = []tfield{{"operator", a.op.id}}
	return n
}
