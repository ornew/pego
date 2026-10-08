package engine

// Tracing reports every rule call of a parse to a function (ParseOptions.Trace). It is built
// around the one function all backends call for a rule (parser.call), so that an ordinary parse
// pays for it with a single nil check per call of a rule that is not plain; plain calls, which
// skip call, take that path only when the parser allows it (parser.noPlain), which a traced
// parse does not. See docs/design/014-tracing-and-profiling.md.

// TraceKind tells whether a TraceEvent starts or ends a rule call.
type TraceKind uint8

const (
	// TraceEnter is reported when a rule is called, before the memo is consulted.
	TraceEnter TraceKind = iota
	// TraceExit is reported when the call returns.
	TraceExit
)

func (k TraceKind) String() string {
	if k == TraceExit {
		return "exit"
	}
	return "enter"
}

// TraceEvent describes the start or the end of a rule call. Events nest: every TraceEnter is
// followed, after the events of the calls the rule makes, by its TraceExit, unless the parse is
// aborted (by a runtime error in an action, an error returned by a stream's emit function, or the
// nesting limit).
//
// The methods LineCol, Text and Failure read the parser's state: call them only from the trace
// function, while it handles the event.
type TraceEvent struct {
	Kind TraceKind
	// Rule is the name of the rule called.
	Rule string
	// Level is the binding level the rule was called with: for a Pratt rule called as e:level,
	// the operators that bind no tighter than that level are left to the caller. It is 0 for
	// calls without a level.
	Level int
	// Depth is the nesting depth of the call: 1 for the start rule.
	Depth int
	// Pos is the position at which the call starts.
	Pos int
	// Lookahead reports that the call is inside a lookahead (& or !), where failures record no
	// expectations.
	Lookahead bool

	// The remaining fields are set on TraceExit only.

	// Matched reports whether the rule matched.
	Matched bool
	// End is the position after the match (Pos if the rule failed).
	End int
	// Memo reports that the result came from the memo, without evaluating the rule body.
	Memo bool
	// Evals is the number of times this call evaluated the rule body: 0 for a result from the
	// memo, 1 usually, and one more per step for a left-recursive rule whose match grows. Bodies of
	// nested calls are not included.
	Evals int
	// Examined is the end of the input the call examined (exclusive): looking at a character to
	// reject it counts, as do lookaheads.
	Examined int

	p   *parser
	far int
	exp []expID
	rec []*SyntaxError
}

// LineCol returns the 1-based line and column of the position pos. In a stream parse, it returns
// 0, 0 for a position (other than 0) in input that has been discarded or not read yet: keeping
// the line starts of discarded input would make the memory of a stream grow with its length.
func (e TraceEvent) LineCol(pos int) (line, col int) {
	switch {
	case e.p == nil:
		return 0, 0
	case pos == 0:
		return 1, 1
	case pos < e.p.base || pos > e.p.loaded():
		return 0, 0
	}
	return e.p.lineCol(pos)
}

// Text returns the input the call matched (empty for an enter event or a failure). Input that a
// stream parse has already discarded is not available.
func (e TraceEvent) Text() string {
	if e.p == nil || e.Kind != TraceExit || !e.Matched {
		return ""
	}
	return e.p.text(e.Pos, e.End)
}

// Failure returns, for an exit event, the farthest failure recorded during the call: the position
// and what was expected there, as a parse that failed there would report it. It is nil when the
// call recorded no expectation (inside a lookahead, or when nothing failed). A call that matched
// can have one too: the input after its match was tried and rejected.
func (e TraceEvent) Failure() *SyntaxError {
	if e.p == nil || len(e.exp) == 0 {
		return nil
	}
	return e.p.makeError(e.far, e.exp)
}

// Recovered returns, for an exit event of a call that matched, the syntax errors that #recover
// recovered from during the call, in nested calls too, or taken from the memo with the call's
// result. The expectations of a recovered error are not part of Failure: recovering removes them
// from the record of the call that contains the #recover.
func (e TraceEvent) Recovered() []*SyntaxError {
	return e.rec
}

// tracer is the tracing state of a parse.
type tracer struct {
	fn func(TraceEvent)
	// direct is set by tracedCall just before it calls call for the call it has reported, so that
	// call runs it instead of reporting it again.
	direct bool
	stack  []tracedCallState
}

// tracedCallState is the state of a traced call in progress.
type tracedCallState struct {
	r     *rule
	min   int
	start int
	// evals is the number of body evaluations of the parse when the call started, and nested
	// those made by nested calls since.
	evals, nested int
	// hw is the end of the examined input when the call started, and rec the number of
	// recovered errors.
	hw, rec int
	exp     expMark
}

// setTrace makes the parse report its rule calls to fn (nothing if fn is nil).
func (p *parser) setTrace(fn func(TraceEvent)) {
	if fn == nil {
		return
	}
	p.tr = &tracer{fn: fn}
	p.noPlain = true
}

// tracedCall reports a call of r, runs it with call, and reports its result.
func (p *parser) tracedCall(r *rule, min int) (*Node, bool) {
	p.traceEnter(r, min)
	p.tr.direct = true
	v, ok := p.call(r, min)
	p.traceExit(ok)
	return v, ok
}

// traceEnter reports the start of a call of r and starts measuring it. The call's examined range
// and expectations are recorded separately, as callBegin does for a memoized call, and merged
// back by traceExit; doing so for any call leaves the parse's own records unchanged (both only
// keep a maximum).
func (p *parser) traceEnter(r *rule, min int) {
	t := p.tr
	t.stack = append(t.stack, tracedCallState{r: r, min: min, start: p.pos, evals: p.stats.Evaluated, hw: p.hw, rec: len(p.recovered)})
	p.hw = p.pos
	t.stack[len(t.stack)-1].exp = p.isolate(p.pos)
	t.fn(TraceEvent{Kind: TraceEnter, Rule: r.name, Level: min, Depth: len(t.stack), Pos: p.pos, Lookahead: p.silent > 0, p: p})
}

// traceExit reports the end of the innermost call in progress, whose result is ok.
func (p *parser) traceExit(ok bool) {
	t := p.tr
	c := t.stack[len(t.stack)-1]
	all := p.stats.Evaluated - c.evals
	far, inner := p.unisolate(c.exp)
	ev := TraceEvent{Kind: TraceExit, Rule: c.r.name, Level: c.min, Depth: len(t.stack), Pos: c.start,
		Lookahead: p.silent > 0, Matched: ok, End: c.start, Memo: all == 0, Evals: all - c.nested,
		Examined: p.hw, p: p, far: far, exp: inner}
	if ok {
		ev.End = p.pos
		if len(p.recovered) > c.rec {
			ev.rec = p.recovered[c.rec:len(p.recovered):len(p.recovered)]
		}
	}
	t.fn(ev)
	p.mergeExpected(far, inner)
	p.hw = max(c.hw, p.hw)
	t.stack = t.stack[:len(t.stack)-1]
	if n := len(t.stack); n > 0 {
		t.stack[n-1].nested += all
	}
}

// traceFrame is a traced rule call in the iterative-model VM: it reports the call, runs the frame
// that bodyFrame would have pushed for it, and reports the result.
type traceFrame struct {
	r       *rule
	min     int
	started bool
}

func (f *traceFrame) next(p *parser, vm *vmProgram, res iresult) (iframe, iresult, bool) {
	if !f.started {
		f.started = true
		p.traceEnter(f.r, f.min)
		return p.callChild(f.r, f.min), iresult{}, false
	}
	p.traceExit(res.ok)
	return nil, res, true
}
