package engine

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Profile accumulates the cost of each rule over the parses it is attached to (as their trace
// function, Profile.Trace). It is not safe for concurrent use.
type Profile struct {
	// Parses is the number of parses profiled.
	Parses int `json:"parses"`
	// Calls, Evals and MemoHits total the rules' counts.
	Calls    int `json:"calls"`
	Evals    int `json:"evals"`
	MemoHits int `json:"memoHits"`
	// Examined totals, over the parses, the end of the input the start rule examined.
	Examined int `json:"examined"`
	// Time totals the time of the parses' start rule calls (in nanoseconds in JSON).
	Time time.Duration `json:"time"`
	// Rules holds the rules called, in the order of their first call.
	Rules []*RuleProfile `json:"rules"`

	index map[string]int
	// State of the parse in progress
	stack []profileCall
	seen  map[profileKey]int32 // evaluated calls per rule, position and level
	open  []int32              // per rule, the calls in progress (for Time)
}

// RuleProfile is the cost of one rule. Calls of a rule's value-free twin count as calls of the rule.
type RuleProfile struct {
	Rule string `json:"rule"`
	// Calls is the number of calls. Each call either evaluated the rule body or took the result from
	// the memo (MemoHits); it matched (Matched) or failed (Failed).
	Calls    int `json:"calls"`
	MemoHits int `json:"memoHits"`
	Matched  int `json:"matched"`
	Failed   int `json:"failed"`
	// Evals is the number of body evaluations: one per call not answered by the memo, and one more
	// for each step a left-recursive match grows.
	Evals int `json:"evals"`
	// Consumed totals the input matched by the calls that matched.
	Consumed int `json:"consumed"`
	// Wasted totals the input examined by the calls that evaluated the body and failed: the work
	// that led to nothing. It is inclusive: a failed call counts what the calls it made examined.
	// LongestFail is the most input a single failed call examined, at LongestFailAt.
	Wasted        int      `json:"wasted"`
	LongestFail   int      `json:"longestFail"`
	LongestFailAt Location `json:"longestFailAt"`
	// Repeats is the number of calls that evaluated the body at a position (and binding level)
	// where an earlier call of the same parse had evaluated it already. MaxEvals is the most
	// evaluating calls at one position, at MaxEvalsAt. A memoized rule is evaluated at most twice
	// at a position (the memo keeps a result from the second call on); a rule that is not
	// memoized is evaluated each time its callers are.
	Repeats    int      `json:"repeats"`
	MaxEvals   int      `json:"maxEvals"`
	MaxEvalsAt Location `json:"maxEvalsAt"`
	// Time is the time spent in the calls, nested calls included. A call within a call of the
	// same rule counts once. SelfTime excludes the time of nested calls. Both include the
	// overhead of measuring them, so they are meaningful relative to each other. JSON gives them in
	// nanoseconds.
	Time     time.Duration `json:"time"`
	SelfTime time.Duration `json:"selfTime"`
}

// Location is a position with its line and column.
type Location struct {
	Pos  int `json:"pos"`
	Line int `json:"line"`
	Col  int `json:"col"`
}

// String returns line:col, or "position N" when the line is unknown (Line is 0: the position
// was in input that a stream parse had discarded; see TraceEvent.LineCol).
func (l Location) String() string {
	if l.Line == 0 {
		return fmt.Sprintf("position %d", l.Pos)
	}
	return fmt.Sprintf("%d:%d", l.Line, l.Col)
}

type profileCall struct {
	rule  int
	start time.Time
	child time.Duration
}

type profileKey struct {
	rule, level int32
	pos         int
}

// Trace records the event e. Pass it as the trace function of the parses to profile.
func (pr *Profile) Trace(e TraceEvent) {
	now := time.Now()
	if e.Kind == TraceEnter {
		if e.Depth == 1 {
			pr.Parses++
			pr.stack = pr.stack[:0]
			clear(pr.seen)
			clear(pr.open)
		}
		i := pr.rule(e.Rule)
		pr.open[i]++
		pr.stack = append(pr.stack, profileCall{rule: i})
		pr.stack[len(pr.stack)-1].start = time.Now()
		return
	}
	if len(pr.stack) == 0 {
		return
	}
	c := pr.stack[len(pr.stack)-1]
	pr.stack = pr.stack[:len(pr.stack)-1]
	d := now.Sub(c.start)
	r := pr.Rules[c.rule]
	r.SelfTime += d - c.child
	pr.open[c.rule]--
	if pr.open[c.rule] == 0 {
		r.Time += d
	}
	if n := len(pr.stack); n > 0 {
		pr.stack[n-1].child += d
	} else {
		pr.Time += d
		pr.Examined += e.Examined
	}
	r.Calls++
	pr.Calls++
	r.Evals += e.Evals
	pr.Evals += e.Evals
	if e.Memo {
		r.MemoHits++
		pr.MemoHits++
	}
	if e.Matched {
		r.Matched++
		r.Consumed += e.End - e.Pos
	} else {
		r.Failed++
		if !e.Memo {
			n := e.Examined - e.Pos
			r.Wasted += n
			if n > r.LongestFail {
				r.LongestFail = n
				r.LongestFailAt = location(e, e.Pos)
			}
		}
	}
	if !e.Memo {
		if pr.seen == nil {
			pr.seen = map[profileKey]int32{}
		}
		k := profileKey{rule: int32(c.rule), level: int32(e.Level), pos: e.Pos}
		n := pr.seen[k] + 1
		pr.seen[k] = n
		if n > 1 {
			r.Repeats++
		}
		if int(n) > r.MaxEvals {
			r.MaxEvals = int(n)
			r.MaxEvalsAt = location(e, e.Pos)
		}
	}
}

func location(e TraceEvent, pos int) Location {
	line, col := e.LineCol(pos)
	return Location{Pos: pos, Line: line, Col: col}
}

// rule returns the index of the rule name in Rules, adding it if needed.
func (pr *Profile) rule(name string) int {
	if i, ok := pr.index[name]; ok {
		return i
	}
	if pr.index == nil {
		pr.index = map[string]int{}
	}
	pr.index[name] = len(pr.Rules)
	pr.Rules = append(pr.Rules, &RuleProfile{Rule: name})
	pr.open = append(pr.open, 0)
	return len(pr.Rules) - 1
}

// Thresholds of Hints
const (
	// hintLongFail is the input a failed call must have examined to count as deep backtracking.
	hintLongFail = 16
	// hintRules is the most rules a hint names.
	hintRules = 3
)

// Hints returns observations about the profile that point at where a grammar does more work than
// it needs to, most important first. They are heuristics: each names what to look at, not what is
// wrong.
func (pr *Profile) Hints() []string {
	var hints []string
	list := func(rs []*RuleProfile, item func(*RuleProfile) string) string {
		parts := make([]string, len(rs))
		for i, r := range rs {
			parts[i] = item(r)
		}
		return strings.Join(parts, ", ")
	}
	if pr.Time > 0 {
		if top := pr.top(func(r *RuleProfile) int64 { return int64(r.SelfTime) }, hintRules); len(top) > 0 {
			hints = append(hints, fmt.Sprintf("Most time is spent in %s, not counting the rules they call.",
				list(top, func(r *RuleProfile) string {
					return fmt.Sprintf("%s (%.0f%%)", r.Rule, percent(int64(r.SelfTime), int64(pr.Time)))
				})))
		}
	}
	if top := pr.top(func(r *RuleProfile) int64 {
		if r.MaxEvals <= 2 {
			return 0
		}
		return int64(r.Repeats)
	}, hintRules); len(top) > 0 {
		hints = append(hints, fmt.Sprintf("Evaluated again where they had been evaluated before: %s. "+
			"A memoized rule is usually evaluated at most twice at a position; a rule that is not is evaluated "+
			"whenever its caller is retried there. Look for choices whose alternatives begin with the same "+
			"calls, and factor the common part out.",
			list(top, func(r *RuleProfile) string {
				return fmt.Sprintf("%s (%d of %d evaluations, up to %d times at %s)", r.Rule, r.Repeats, r.Evals, r.MaxEvals, r.MaxEvalsAt)
			})))
	}
	if top := pr.top(func(r *RuleProfile) int64 {
		if r.LongestFail < hintLongFail {
			return 0
		}
		return int64(r.LongestFail)
	}, hintRules); len(top) > 0 {
		hints = append(hints, fmt.Sprintf("Failed after examining much input: %s. "+
			"A call that fails late throws its work away, and what is tried next often matches the same input "+
			"again. Check whether an alternative shares a long prefix with a later one, and whether a cut (--) "+
			"after the part that identifies an alternative can stop the parser from trying the others.",
			list(top, func(r *RuleProfile) string {
				return fmt.Sprintf("%s (%d positions from %s)", r.Rule, r.LongestFail, r.LongestFailAt)
			})))
	}
	if top := pr.top(func(r *RuleProfile) int64 {
		if pr.Examined == 0 || r.Wasted < pr.Examined {
			return 0
		}
		return int64(r.Wasted)
	}, hintRules); len(top) > 0 {
		hints = append(hints, fmt.Sprintf("Failed calls examined more input in all than the parse did (%d positions): %s. "+
			"The rule is tried at many positions where it cannot match, or fails late; nested failures count in their "+
			"callers too, so look at the innermost of these first.",
			pr.Examined, list(top, func(r *RuleProfile) string {
				return fmt.Sprintf("%s (%d positions in %d failed calls)", r.Rule, r.Wasted, r.Failed)
			})))
	}
	if len(hints) <= 1 && pr.Calls > 0 {
		hints = append(hints, "Nothing else stands out: no rule is evaluated more than twice at a position, and no failed call examines much input.")
	}
	return hints
}

// top returns up to n rules with the highest positive key, highest first.
func (pr *Profile) top(key func(*RuleProfile) int64, n int) []*RuleProfile {
	var rs []*RuleProfile
	for _, r := range pr.Rules {
		if key(r) > 0 {
			rs = append(rs, r)
		}
	}
	slices.SortStableFunc(rs, func(a, b *RuleProfile) int { return cmp.Compare(key(b), key(a)) })
	return rs[:min(n, len(rs))]
}

func percent(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}
