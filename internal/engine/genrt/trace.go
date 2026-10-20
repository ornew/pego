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

// WithTrace observes actual generated calls synchronously. Callback panics
// propagate unchanged after cleanup; an aborted parse can omit exit events.
// Events do not expose input or parser state. ExaminedKnown is true only for
// Document parses, where Examined is the frontier used for edit reuse.
func WithTrace(trace func(TraceEvent)) ParseOption {
	return func(o *parseOptions) { o.observe.trace = trace }
}

// WithProfile accumulates complete generated rule-call metrics. It is independent
// of trace delivery filters. A Profile must not be shared by concurrent parses.
func WithProfile(profile *Profile) ParseOption {
	return func(o *parseOptions) { o.observe.profile = profile }
}

// WithTraceFilter selects named rules and their nested calls. Unknown names
// match nothing. With no names, every entered rule is selected.
func WithTraceFilter(rules ...string) ParseOption {
	names := append([]string(nil), rules...)
	return func(o *parseOptions) { o.observe.rules = names }
}

// WithTraceMaxDepth limits delivered events, counting the start rule as one.
// Zero and negative values leave delivery unlimited; parsing is unaffected.
func WithTraceMaxDepth(n int) ParseOption {
	return func(o *parseOptions) { o.observe.maxDepth = n }
}

// observeOptions are the parse options that select an observation.
type observeOptions struct {
	trace    func(TraceEvent)
	profile  *Profile
	rules    []string
	maxDepth int
}

// newObservation returns the observation that o selects for one parse, or nil.
func newObservation(o *observeOptions) *observation {
	if o.trace == nil && o.profile == nil {
		return nil
	}
	out := &observation{trace: o.trace, maxDepth: o.maxDepth, profile: newProfileObservation(o.profile)}
	if len(o.rules) != 0 {
		out.rules = make(map[string]bool, len(o.rules))
		for _, name := range o.rules {
			out.rules[name] = true
		}
	}
	return out
}

// release drops what the observation refers to once its parse returns.
func (o *observation) release() {
	if o != nil {
		if o.profile != nil {
			*o.profile = profileObservation{}
		}
		*o = observation{}
	}
}

// dropDiscarded forgets the profile's evaluation counts before pos, which a
// stream discarded: no rule is evaluated there again.
func (o *observation) dropDiscarded(pos int) {
	if o == nil || o.profile == nil {
		return
	}
	for k := range o.profile.seen {
		if k.pos < pos {
			delete(o.profile.seen, k)
		}
	}
}

// Of its fields and methods, Enter, Evaluated, Exit, Skipped are for generated parsers; not a stable API.
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

func (o *observation) Enter(rule string, pos, level int) {
	selected := o.selected(rule)
	e := TraceEvent{Kind: TraceEnter, Rule: rule, Pos: pos, Level: level, Depth: len(o.stack) + 1}
	o.stack = append(o.stack, observationCall{event: e, selected: selected})
	o.profile.enter(e)
	o.deliver(e, selected)
}

func (o *observation) Evaluated() {
	if o != nil && len(o.stack) > 0 {
		o.stack[len(o.stack)-1].event.Evals++
	}
}

func (o *observation) memoHit() {
	if o != nil && len(o.stack) > 0 {
		o.stack[len(o.stack)-1].event.Memo = true
	}
}

func (o *observation) Exit(matched bool, end, examined int, examinedKnown bool) {
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

func (o *observation) Skipped(rule string, pos, level int) {
	if o != nil {
		e := TraceEvent{Kind: TraceSkipped, Rule: rule, Pos: pos, Level: level,
			Depth: len(o.stack) + 1, SkipReason: "first-character"}
		o.profile.skipped(e)
		o.deliver(e, o.selected(rule))
	}
}
