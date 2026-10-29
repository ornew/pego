package core

import (
	"fmt"

	"github.com/ornew/pego/internal/value"
)

// MissingTokenLimitError reports exhaustion of the invocation's insertion budget.
// Position uses the selected position unit. Exhaustion aborts parsing, including
// when an alternative that used the budget would otherwise be abandoned.
type MissingTokenLimitError struct {
	Limit, Position int
}

func (e *MissingTokenLimitError) Error() string {
	return fmt.Sprintf("missing token insertion limit %d exceeded at position %d", e.Limit, e.Position)
}

type MissingTokenSite struct{ ID, Pos int }
type MissingTokenEvent struct {
	Site MissingTokenSite
	Err  *SyntaxError
}

// MissingTokenState belongs to one invocation. The path rolls back; attempts do
// not. Completed results depending on this state must never be memo-reused.
type MissingTokenState struct {
	Limit, Attempts int
	Path            []MissingTokenEvent
	installed       map[MissingTokenSite]bool
	Seeds           map[*MemoEntry][]MissingTokenEvent // no slice added to every ordinary memo entry
	label           ExpID                              // outermost enclosing @error, or zero
}

func (p *Parser) missingMark() int {
	return p.MissingLen
}

func (p *Parser) ResetMissing(mark int) {
	if p.Missing == nil {
		return
	}
	s := p.Missing
	for _, event := range s.Path[mark:] {
		p.StepWork()
		delete(s.installed, event.Site)
	}
	clear(s.Path[mark:])
	s.Path = s.Path[:mark]
	p.MissingLen = mark
}

func (p *Parser) MissingEvents(mark int) []MissingTokenEvent {
	if p.Missing == nil {
		return nil
	}
	return AppendWork(p, []MissingTokenEvent(nil), p.Missing.Path[mark:])
}

func (p *Parser) InsertMissing(event MissingTokenEvent) bool {
	return p.installMissing(event, true)
}

func (p *Parser) installMissing(event MissingTokenEvent, charge bool) bool {
	s := p.Missing
	if s == nil || s.installed[event.Site] {
		return false
	}
	if charge && s.Attempts >= s.Limit {
		panic(Fatal{&MissingTokenLimitError{Limit: s.Limit, Position: event.Site.Pos}})
	}
	if charge {
		s.Attempts++
	}
	if s.installed == nil {
		s.installed = make(map[MissingTokenSite]bool)
	}
	s.installed[event.Site] = true
	s.Path = append(s.Path, event)
	p.MissingLen = len(s.Path)
	return true
}

// Only active/selected LR seeds need insertion snapshots. Keep these in the
// opt-in invocation state rather than enlarging every ordinary memo entry.
func (p *Parser) MissingSeedEvents(e *MemoEntry) []MissingTokenEvent {
	if p.Missing == nil {
		return nil
	}
	return p.Missing.Seeds[e]
}

func (p *Parser) RecordMissingSeed(e *MemoEntry, mark int) {
	if p.Missing == nil {
		return
	}
	events := p.MissingEvents(mark)
	if len(events) == 0 {
		// Memo slabs recycle pointers; never inherit a retired seed's events.
		delete(p.Missing.Seeds, e)
		return
	}
	if p.Missing.Seeds == nil {
		p.Missing.Seeds = make(map[*MemoEntry][]MissingTokenEvent)
	}
	p.Missing.Seeds[e] = events
}

// ReplayMissing restores a chosen seed/candidate. Repeated events already on
// the path neither cost attempts nor duplicate their insertion diagnostic.
// Consuming recovery diagnostics remain subject to their existing replay rules.
func (p *Parser) ReplayMissing(events []MissingTokenEvent, errs []*SyntaxError) {
	p.restoreMissing(events, errs, true)
}

// Selecting an operator candidate restores an already charged synthesis; it is
// not a memo replay and does not charge that synthesis a second time.
func (p *Parser) restoreMissing(events []MissingTokenEvent, errs []*SyntaxError, charge bool) {
	var duplicate map[*SyntaxError]bool
	for _, event := range events {
		p.StepWork()
		if p.Missing.installed[event.Site] {
			if duplicate == nil {
				duplicate = make(map[*SyntaxError]bool)
			}
			duplicate[event.Err] = true
		} else {
			p.installMissing(event, charge)
		}
	}
	if duplicate == nil {
		p.Recovered = AppendWork(p, p.Recovered, errs)
		return
	}
	for _, err := range errs {
		p.StepWork()
		if !duplicate[err] {
			p.Recovered = append(p.Recovered, err)
		}
	}
}

// MissingErrorScope is emitted only for grammars using this feature; ordinary
// grammars retain the existing errorAttr path, without scope/defer machinery.
func MissingErrorScope(m Matcher, msg ExpID) Matcher {
	return func(p *Parser) (*value.Node, bool) {
		if p.Missing != nil && p.Missing.label == 0 {
			p.Missing.label = msg
			defer func() { p.Missing.label = 0 }()
		}
		return m(p)
	}
}

// MissingLiteral preserves the entire ordinary literal match before trying an
// insertion. Its stable compile-time site is independent of invocation state.
func MissingLiteral(literal Matcher, site int, expected string, build bool) Matcher {
	return func(p *Parser) (*value.Node, bool) {
		if p.Missing == nil || p.Silent > 0 {
			return literal(p)
		}
		m0 := p.Mark()
		v, ok, far, exp := Tracked(p, literal)
		if ok {
			p.MergeExpected(far, exp)
			return v, true
		}
		p.Reset(m0)
		key := MissingTokenSite{site, m0.Pos}
		if p.Missing.installed[key] {
			p.MergeExpected(far, exp)
			return nil, false
		}
		if p.Missing.label != 0 {
			exp = []ExpID{p.Missing.label}
		}
		e := p.MakeError(m0.Pos, exp)
		p.InsertMissing(MissingTokenEvent{key, e})
		p.Recovered = append(p.Recovered, e)
		if !build {
			return nil, true
		}
		return p.NewNode(value.Data{Kind: value.MissingKind, Start: int32(m0.Pos), End: int32(m0.Pos),
			Fields: value.Fields{{Name: "expected", Value: expected}, {Name: "message", Value: p.RecoveryErrorText(e)}}, Terminal: true, Fresh: true}), true
	}
}
