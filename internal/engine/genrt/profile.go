package genrt

import (
	"sort"
	"time"
)

// Profile accumulates generated rule execution across invocations. Its zero
// value is ready to use. It must not be used concurrently; recursive parses on
// the same goroutine have independent accounting state. It retains no input,
// nodes, callbacks or per-position history after a parse returns.
type Profile struct {
	totals ProfileSnapshot
	rows   map[string]*RuleProfile
}

// ProfileSnapshot is an owned copy of a profile, with Rules sorted by name.
// Parses and Entered include calls interrupted before exit; Calls and the other
// completed-call metrics include only calls that emitted an exit. Time sums
// completed root rule calls, excluding input setup, end checks and conversion.
// Counters use each invocation's position unit without normalization.
type ProfileSnapshot struct {
	Parses   int           `json:"parses"`
	Entered  int           `json:"entered"`
	Calls    int           `json:"calls"`
	Evals    int           `json:"evals"`
	MemoHits int           `json:"memoHits"`
	Skipped  int           `json:"skipped"`
	Time     time.Duration `json:"time"`
	Rules    []RuleProfile `json:"rules"`
}

// RuleProfile describes actual generated calls of one rule. Skipped counts
// only identifiable optimized dispatch sites and is separate from Entered.
// Within one invocation, Time counts recursive calls of the same rule once;
// SelfTime excludes children. Reentrant invocations are accumulated separately.
// Both include instrumentation and callback costs within the timed boundary.
type RuleProfile struct {
	Rule          string            `json:"rule"`
	Entered       int               `json:"entered"`
	Calls         int               `json:"calls"`
	Evals         int               `json:"evals"`
	MemoHits      int               `json:"memoHits"`
	Matched       int               `json:"matched"`
	Failed        int               `json:"failed"`
	Consumed      int               `json:"consumed"`
	Skipped       int               `json:"skipped"`
	SkipReasons   []SkipReasonCount `json:"skipReasons"`
	Repeats       int               `json:"repeats"`
	MaxEvals      int               `json:"maxEvals"`
	MaxEvalsAt    ProfileLocation   `json:"maxEvalsAt"`
	Examined      int               `json:"examined"`
	ExaminedKnown bool              `json:"examinedKnown"`
	Wasted        int               `json:"wasted"`
	WastedKnown   bool              `json:"wastedKnown"`
	Time          time.Duration     `json:"time"`
	SelfTime      time.Duration     `json:"selfTime"`
}

// ProfileLocation identifies a position and Operators Rule binding level.
type ProfileLocation struct {
	Pos   int `json:"pos"`
	Level int `json:"level"`
}

// SkipReasonCount counts calls omitted by one identifiable dispatch reason.
type SkipReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Snapshot returns independent, deterministically ordered counters. Examined
// and Wasted are unavailable in the current generated runtime; their Known
// flags are false, rather than inferring examined input from matched ranges.
// Like parsing, Snapshot requires exclusive access to the Profile.
func (pr *Profile) Snapshot() ProfileSnapshot {
	if pr == nil {
		return ProfileSnapshot{Rules: []RuleProfile{}}
	}
	s := pr.totals
	s.Rules = make([]RuleProfile, 0, len(pr.rows))
	for _, row := range pr.rows {
		r := *row
		r.SkipReasons = append([]SkipReasonCount{}, r.SkipReasons...)
		sort.Slice(r.SkipReasons, func(i, j int) bool { return r.SkipReasons[i].Reason < r.SkipReasons[j].Reason })
		s.Rules = append(s.Rules, r)
	}
	sort.Slice(s.Rules, func(i, j int) bool { return s.Rules[i].Rule < s.Rules[j].Rule })
	return s
}

func (pr *Profile) rule(name string) *RuleProfile {
	if row := pr.rows[name]; row != nil {
		return row
	}
	if pr.rows == nil {
		pr.rows = make(map[string]*RuleProfile)
	}
	r := &RuleProfile{Rule: name}
	pr.rows[name] = r
	return r
}

// All execution state belongs to the invocation, never the accumulating sink.
type profileObservation struct {
	profile *Profile
	stack   []profileFrame
	seen    map[profileKey]int
	open    map[string]int
}

type profileFrame struct {
	row   *RuleProfile
	start time.Time
	child time.Duration
}

type profileKey struct {
	rule       string
	pos, level int
}

func newProfileObservation(pr *Profile) *profileObservation {
	if pr == nil {
		return nil
	}
	return &profileObservation{profile: pr, seen: make(map[profileKey]int), open: make(map[string]int)}
}

func (o *profileObservation) enter(e TraceEvent) {
	if o == nil {
		return
	}
	pr := o.profile
	if len(o.stack) == 0 {
		pr.totals.Parses++
	}
	r := pr.rule(e.Rule)
	r.Entered++
	pr.totals.Entered++
	o.open[e.Rule]++
	o.stack = append(o.stack, profileFrame{row: r, start: time.Now()})
}

func (o *profileObservation) exit(e TraceEvent) {
	if o == nil {
		return
	}
	now := time.Now()
	i := len(o.stack) - 1
	c := o.stack[i]
	o.stack = o.stack[:i]
	d := now.Sub(c.start)
	r, pr := c.row, o.profile
	r.SelfTime += d - c.child
	o.open[e.Rule]--
	if o.open[e.Rule] == 0 {
		r.Time += d
	}
	if i > 0 {
		o.stack[i-1].child += d
	} else {
		pr.totals.Time += d
	}
	r.Calls++
	pr.totals.Calls++
	r.Evals += e.Evals
	pr.totals.Evals += e.Evals
	if e.Memo {
		r.MemoHits++
		pr.totals.MemoHits++
	}
	if e.Matched {
		r.Matched++
		r.Consumed += e.End - e.Pos
	} else {
		r.Failed++
	}
	if !e.Memo && e.Evals > 0 {
		k := profileKey{rule: e.Rule, pos: e.Pos, level: e.Level}
		n := o.seen[k] + 1
		o.seen[k] = n
		if n > 1 {
			r.Repeats++
		}
		if n > r.MaxEvals {
			r.MaxEvals, r.MaxEvalsAt = n, ProfileLocation{Pos: e.Pos, Level: e.Level}
		}
	}
}

func (o *profileObservation) skipped(e TraceEvent) {
	if o == nil {
		return
	}
	r := o.profile.rule(e.Rule)
	o.profile.totals.Skipped++
	r.Skipped++
	for i := range r.SkipReasons {
		if r.SkipReasons[i].Reason == e.SkipReason {
			r.SkipReasons[i].Count++
			return
		}
	}
	r.SkipReasons = append(r.SkipReasons, SkipReasonCount{Reason: e.SkipReason, Count: 1})
}
