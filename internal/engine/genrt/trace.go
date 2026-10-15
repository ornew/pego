package genrt

// TraceKind distinguishes entered calls, completed calls and skipped dispatch.
type TraceKind uint8

const (
	TraceEnter TraceKind = iota
	TraceExit
	TraceSkipped
)

// TraceEvent observes actual generated rule execution. Positions use the
// invocation's selected unit. Examined is valid only when ExaminedKnown.
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
	if !selected || o.maxDepth > 0 && e.Depth > o.maxDepth {
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

func (o *observation) exit(matched bool, end int) {
	i := len(o.stack) - 1
	call := o.stack[i]
	o.stack = o.stack[:i]
	e := call.event
	e.Kind, e.Matched, e.End = TraceExit, matched, end
	o.deliver(e, call.selected)
}

func (o *observation) skipped(rule string, pos, level int) {
	if o != nil {
		o.deliver(TraceEvent{Kind: TraceSkipped, Rule: rule, Pos: pos, Level: level,
			Depth: len(o.stack) + 1, SkipReason: "first-character"}, o.selected(rule))
	}
}
