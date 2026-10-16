package genrt

// TraceKind distinguishes entered calls, completed calls and skipped dispatch.
type TraceKind uint8

const (
	TraceEnter TraceKind = iota
	TraceExit
	TraceSkipped
)

// TraceEvent observes actual generated rule execution. Positions use the
// invocation's selected unit. Examined is valid only when ExaminedKnown, which
// holds for exits of Document parses: it is then the exclusive end of the input
// the Document treats the call as having examined when deciding reuse after an
// edit. That frontier is conservative and can exceed the input actually read.
// An aborted parse may leave enters without exits.
type TraceEvent struct {
	Kind              TraceKind
	Rule              string
	Level, Depth, Pos int
	Matched           bool
	End               int
	Memo              bool
	Evals             int
	Examined          int
	ExaminedKnown     bool
	SkipReason        string
}

type observation struct {
	trace    func(TraceEvent)
	profile  *profileObservation
	rules    map[string]bool
	maxDepth int
	stack    []observationCall
}

type observationCall struct {
	event    TraceEvent
	selected bool
}

// Only callback panics are wrapped. The parse recovery boundary unwraps before
// handling its own private interruption types, preserving every callback value.
type tracePanic struct{ value any }

func (o *observation) deliver(e TraceEvent, selected bool) {
	if o.trace == nil || !selected || o.maxDepth > 0 && e.Depth > o.maxDepth {
		return
	}
	defer func() {
		if x := recover(); x != nil {
			panic(tracePanic{x})
		}
	}()
	o.trace(e)
}

func (o *observation) selected(rule string) bool {
	return len(o.rules) == 0 || o.rules[rule] || len(o.stack) > 0 && o.stack[len(o.stack)-1].selected
}

func (o *observation) enter(rule string, pos, level int) {
	selected := o.selected(rule)
	e := TraceEvent{Kind: TraceEnter, Rule: rule, Pos: pos, Level: level, Depth: len(o.stack) + 1}
	o.stack = append(o.stack, observationCall{event: e, selected: selected})
	o.profile.enter(e)
	o.deliver(e, selected)
}

func (o *observation) evaluated() {
	if o != nil && len(o.stack) > 0 {
		o.stack[len(o.stack)-1].event.Evals++
	}
}

func (o *observation) memoHit() {
	if o != nil && len(o.stack) > 0 {
		o.stack[len(o.stack)-1].event.Memo = true
	}
}

func (o *observation) exit(matched bool, end, examined int, examinedKnown bool) {
	i := len(o.stack) - 1
	call := o.stack[i]
	o.stack = o.stack[:i]
	e := call.event
	e.Kind, e.Matched, e.End = TraceExit, matched, end
	if examinedKnown {
		e.Examined, e.ExaminedKnown = examined, true
	}
	if o.profile == nil {
		o.deliver(e, call.selected)
		return
	}
	// Include the exit callback in this call, even when it panics. Its parent
	// then excludes the same interval from self time.
	defer o.profile.exit(e)
	o.deliver(e, call.selected)
}

func (o *observation) skipped(rule string, pos, level int) {
	if o != nil {
		e := TraceEvent{Kind: TraceSkipped, Rule: rule, Pos: pos, Level: level,
			Depth: len(o.stack) + 1, SkipReason: "first-character"}
		o.profile.skipped(e)
		o.deliver(e, o.selected(rule))
	}
}

// examinedBegin starts measuring an observed call's examined frontier in a
// Document parse, returning the enclosing frontier for examinedEnd. The call
// itself, and any memo entry it reads or records, extends the frontier as in an
// unobserved parse, so observation does not change reuse.
func (p *parser) examinedBegin() int {
	h := p.docHigh
	if documentEnabled && p.document != nil {
		p.docHigh = p.pos
	}
	return h
}

// examinedEnd returns the call's frontier and merges it into the enclosing one.
// Outside a Document parse the frontier is unknown. A call that reuses a
// growing left-recursive seed sees the frontier recorded with an earlier seed;
// such results are provisional and never reused after an edit, so in a
// provisional context the report is raised to the call's end without changing the frontier the Document keeps.
func (p *parser) examinedEnd(enclosing int) (int, bool) {
	if !documentEnabled || p.document == nil {
		return 0, false
	}
	x := p.docHigh
	p.docHigh = max(enclosing, x)
	if p.docProvisional {
		x = max(x, p.pos)
	}
	return x, true
}
