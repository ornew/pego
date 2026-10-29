package engine

import (
	"fmt"

	"github.com/ornew/pego/internal/compile"
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

type missingTokenSite struct{ id, pos int }
type missingTokenEvent struct {
	site missingTokenSite
	err  *SyntaxError
}

// missingTokenState belongs to one invocation. The path rolls back; attempts do
// not. Completed results depending on this state must never be memo-reused.
type missingTokenState struct {
	limit, attempts int
	path            []missingTokenEvent
	installed       map[missingTokenSite]bool
	seeds           map[*memoEntry][]missingTokenEvent // no slice added to every ordinary memo entry
	label           expID                              // outermost enclosing @error, or zero
}

func (p *parser) missingMark() int {
	return p.missingLen
}

func (p *parser) resetMissing(mark int) {
	if p.missing == nil {
		return
	}
	s := p.missing
	for _, event := range s.path[mark:] {
		p.stepWork()
		delete(s.installed, event.site)
	}
	clear(s.path[mark:])
	s.path = s.path[:mark]
	p.missingLen = mark
}

func (p *parser) missingEvents(mark int) []missingTokenEvent {
	if p.missing == nil {
		return nil
	}
	return appendWork(p, []missingTokenEvent(nil), p.missing.path[mark:])
}

func (p *parser) insertMissing(event missingTokenEvent) bool {
	return p.installMissing(event, true)
}

func (p *parser) installMissing(event missingTokenEvent, charge bool) bool {
	s := p.missing
	if s == nil || s.installed[event.site] {
		return false
	}
	if charge && s.attempts >= s.limit {
		panic(fatal{&MissingTokenLimitError{Limit: s.limit, Position: event.site.pos}})
	}
	if charge {
		s.attempts++
	}
	if s.installed == nil {
		s.installed = make(map[missingTokenSite]bool)
	}
	s.installed[event.site] = true
	s.path = append(s.path, event)
	p.missingLen = len(s.path)
	return true
}

// Only active/selected LR seeds need insertion snapshots. Keep these in the
// opt-in invocation state rather than enlarging every ordinary memo entry.
func (p *parser) missingSeedEvents(e *memoEntry) []missingTokenEvent {
	if p.missing == nil {
		return nil
	}
	return p.missing.seeds[e]
}

func (p *parser) recordMissingSeed(e *memoEntry, mark int) {
	if p.missing == nil {
		return
	}
	events := p.missingEvents(mark)
	if len(events) == 0 {
		// Memo slabs recycle pointers; never inherit a retired seed's events.
		delete(p.missing.seeds, e)
		return
	}
	if p.missing.seeds == nil {
		p.missing.seeds = make(map[*memoEntry][]missingTokenEvent)
	}
	p.missing.seeds[e] = events
}

// replayMissing restores a chosen seed/candidate. Repeated events already on
// the path neither cost attempts nor duplicate their insertion diagnostic.
// Consuming recovery diagnostics remain subject to their existing replay rules.
func (p *parser) replayMissing(events []missingTokenEvent, errs []*SyntaxError) {
	p.restoreMissing(events, errs, true)
}

// Selecting an operator candidate restores an already charged synthesis; it is
// not a memo replay and does not charge that synthesis a second time.
func (p *parser) restoreMissing(events []missingTokenEvent, errs []*SyntaxError, charge bool) {
	var duplicate map[*SyntaxError]bool
	for _, event := range events {
		p.stepWork()
		if p.missing.installed[event.site] {
			if duplicate == nil {
				duplicate = make(map[*SyntaxError]bool)
			}
			duplicate[event.err] = true
		} else {
			p.installMissing(event, charge)
		}
	}
	if duplicate == nil {
		p.recovered = appendWork(p, p.recovered, errs)
		return
	}
	for _, err := range errs {
		p.stepWork()
		if !duplicate[err] {
			p.recovered = append(p.recovered, err)
		}
	}
}

// missingErrorScope is emitted only for grammars using this feature; ordinary
// grammars retain the existing errorAttr path, without scope/defer machinery.
func missingErrorScope(m matcher, msg expID) matcher {
	return func(p *parser) (*value.Node, bool) {
		if p.missing != nil && p.missing.label == 0 {
			p.missing.label = msg
			defer func() { p.missing.label = 0 }()
		}
		return m(p)
	}
}

// missingLiteral preserves the entire ordinary literal match before trying an
// insertion. Its stable compile-time site is independent of invocation state.
func missingLiteral(literal matcher, site int, expected string, build bool) matcher {
	return func(p *parser) (*value.Node, bool) {
		if p.missing == nil || p.silent > 0 {
			return literal(p)
		}
		m0 := p.mark()
		v, ok, far, exp := tracked(p, literal)
		if ok {
			p.mergeExpected(far, exp)
			return v, true
		}
		p.reset(m0)
		key := missingTokenSite{site, m0.pos}
		if p.missing.installed[key] {
			p.mergeExpected(far, exp)
			return nil, false
		}
		if p.missing.label != 0 {
			exp = []expID{p.missing.label}
		}
		e := p.makeError(m0.pos, exp)
		p.insertMissing(missingTokenEvent{key, e})
		p.recovered = append(p.recovered, e)
		if !build {
			return nil, true
		}
		return p.newNode(value.Data{Kind: value.MissingKind, Start: int32(m0.pos), End: int32(m0.pos),
			Fields: value.Fields{{Name: "expected", Value: expected}, {Name: "message", Value: p.recoveryErrorText(e)}}, Terminal: true, Fresh: true}), true
	}
}

func (prog *Program) admitMissing(o ParseOptions, stream bool) error {
	if o.MissingTokens < 0 {
		return fmt.Errorf("missing token limit must be nonnegative")
	}
	if stream && (prog.HasMissing || o.MissingTokens > 0) {
		return compile.MissingUnsupported("stream parsing")
	}
	if prog.backend(o.Backend) != Closure && (prog.HasMissing || o.MissingTokens > 0) {
		return compile.MissingUnsupported("bytecode backends")
	}
	return nil
}
