package genrt

import (
	"maps"
	"slices"
	"time"
)

// TraceKind distinguishes calls that ran from dispatch decisions that skipped them.
type TraceKind uint8

const (
	TraceEnter TraceKind = iota
	TraceExit
	TraceSkipped
)

// TraceEvent is a copied observation with positions in the parse's selected unit.
// Skips are unpaired and carry no result. Examined is valid only when ExaminedKnown.
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

// Profile accumulates completed calls and explicit dispatch skips. It is not safe
// for concurrent use. Unknown examined ranges never contribute an estimate.
type Profile struct {
	Parses, Entered, Calls, Evals, MemoHits, Skipped int
	SkipReasons                                      map[string]int
	Time                                             time.Duration
	Rules                                            []RuleProfile
	index                                            map[string]int
}

// RuleProfile separates entered calls, evaluated bodies and skipped call sites.
// Completed calls alone contribute result counts and timing.
type RuleProfile struct {
	Rule                                                      string
	Entered, Calls, Evals, MemoHits, Matched, Failed, Skipped int
	SkipReasons                                               map[string]int
	Consumed, Wasted, UnknownExamined, Repeats                int
	Time, SelfTime                                            time.Duration
}

// Snapshot returns independent rows in deterministic rule-name order.
func (p *Profile) Snapshot() []RuleProfile {
	rows := slices.Clone(p.Rules)
	for i := range rows {
		rows[i].SkipReasons = maps.Clone(rows[i].SkipReasons)
	}
	slices.SortFunc(rows, func(a, b RuleProfile) int {
		if a.Rule < b.Rule {
			return -1
		}
		if a.Rule > b.Rule {
			return 1
		}
		return 0
	})
	return rows
}

func (p *Profile) row(name string) *RuleProfile {
	if p.index == nil {
		p.index = make(map[string]int)
	}
	i, ok := p.index[name]
	if !ok {
		i = len(p.Rules)
		p.index[name] = i
		p.Rules = append(p.Rules, RuleProfile{Rule: name})
	}
	return &p.Rules[i]
}

// observation owns one invocation. It is deliberately separate from parser:
// ordinary generated parsers need not contain this state or its call sites.
// Hooks must run on every actual generated entry/body/exit, including direct
// typed and recognition paths, before this prototype can be exposed as an API.
type observation struct {
	trace    func(TraceEvent)
	profile  *Profile
	rules    map[string]bool
	maxDepth int
	stack    []observationCall
	seen     map[observationKey]bool
}

type observationKey struct {
	rule       string
	pos, level int
}
type observationCall struct {
	event    TraceEvent
	selected bool
	start    time.Time
	child    time.Duration
}

func (o *observation) deliver(e TraceEvent, selected bool) {
	if o.trace != nil && selected && (o.maxDepth <= 0 || e.Depth <= o.maxDepth) {
		// No recovery here: the caller must preserve callback panic identity
		// through its parse recovery boundary and clean invocation state.
		o.trace(e)
	}
}

func (o *observation) selected(rule string) bool {
	return len(o.rules) == 0 || o.rules[rule] || len(o.stack) > 0 && o.stack[len(o.stack)-1].selected
}

func (o *observation) enter(rule string, level, pos int) {
	e := TraceEvent{Kind: TraceEnter, Rule: rule, Level: level, Pos: pos, Depth: len(o.stack) + 1}
	c := observationCall{event: e, selected: o.selected(rule)}
	if o.profile != nil {
		c.start = time.Now()
		o.profile.Entered++
		o.profile.row(rule).Entered++
		if len(o.stack) == 0 {
			o.profile.Parses++
		}
	}
	o.stack = append(o.stack, c)
	o.deliver(e, c.selected)
}

// evaluate counts one evaluation owned by the current call, including each LR
// growth attempt. It does not count nested evaluations or memo lookups.
func (o *observation) evaluate() { o.stack[len(o.stack)-1].event.Evals++ }

func (o *observation) skip(rule string, level, pos int, reason string) {
	e := TraceEvent{Kind: TraceSkipped, Rule: rule, Level: level, Pos: pos, Depth: len(o.stack) + 1, SkipReason: reason}
	if o.profile != nil {
		o.profile.Skipped++
		r := o.profile.row(rule)
		r.Skipped++
		if o.profile.SkipReasons == nil {
			o.profile.SkipReasons = make(map[string]int)
		}
		if r.SkipReasons == nil {
			r.SkipReasons = make(map[string]int)
		}
		o.profile.SkipReasons[reason]++
		r.SkipReasons[reason]++
	}
	o.deliver(e, o.selected(rule))
}

// exit requires the runtime's explicit memo-hit fact. Zero evaluations alone
// does not establish memo reuse (for example a depth check may abort a call).
func (o *observation) exit(matched, memo bool, end, examined int, known bool) {
	i := len(o.stack) - 1
	c := o.stack[i]
	e := c.event
	e.Kind, e.Matched, e.Memo = TraceExit, matched, memo
	e.End = e.Pos
	if matched {
		e.End = end
	}
	e.Examined, e.ExaminedKnown = examined, known
	if o.profile != nil {
		p := o.profile
		d := time.Since(c.start)
		r := p.row(e.Rule)
		p.Calls++
		r.Calls++
		p.Evals += e.Evals
		r.Evals += e.Evals
		if memo {
			p.MemoHits++
			r.MemoHits++
		}
		if matched {
			r.Matched++
			r.Consumed += e.End - e.Pos
		} else {
			r.Failed++
			if !memo && e.Evals > 0 && known {
				r.Wasted += max(0, examined-e.Pos)
			}
		}
		if !known {
			r.UnknownExamined++
		}
		if e.Evals > 0 {
			if o.seen == nil {
				o.seen = make(map[observationKey]bool)
			}
			key := observationKey{e.Rule, e.Pos, e.Level}
			if o.seen[key] {
				r.Repeats++
			}
			o.seen[key] = true
		}
		r.SelfTime += d - c.child
		// Same-rule recursive nesting contributes inclusive time only once.
		nested := false
		for _, outer := range o.stack[:i] {
			if outer.event.Rule == e.Rule {
				nested = true
				break
			}
		}
		if !nested {
			r.Time += d
		}
		if i > 0 {
			o.stack[i-1].child += d
		} else {
			p.Time += d
		}
	}
	// Delivery precedes removing the call, preserving depth and abort semantics.
	o.deliver(e, c.selected)
	o.stack = o.stack[:i]
}

// clear releases callbacks and all invocation-owned scratch after return or
// panic. A profile contains only strings, scalar counters and timings.
func (o *observation) clear() {
	o.trace, o.profile, o.rules, o.stack, o.seen = nil, nil, nil, nil, nil
}
