// Package sourceast adapts canonical grammar-source metadata to the public AST.
package sourceast

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

// Metadata is the offset representation of a canonical SourceMetadata value.
// Start and End are zero-based rune offsets; End is exclusive. Comment ownership
// has already been determined by the grammar and is copied without relocation.
type Metadata struct {
	Start, End        int
	Leading, Trailing string
	Detached          []string
}

// Positions converts rune offsets using the manual frontend's newline policy.
// A CRLF advances the line at LF; a lone CR advances it at CR.
type Positions struct {
	boundaries []grammar.Pos
}

func NewPositions(input string) *Positions {
	rs := []rune(input)
	p := &Positions{boundaries: make([]grammar.Pos, len(rs)+1)}
	pos := grammar.Pos{Line: 1, Col: 1}
	p.boundaries[0] = pos
	for i, r := range rs {
		if r == '\n' || r == '\r' && (i+1 == len(rs) || rs[i+1] != '\n') {
			pos.Line++
			pos.Col = 1
		} else {
			pos.Col++
		}
		p.boundaries[i+1] = pos
	}
	return p
}

// Source converts a syntax span and retains its already-normalized comments.
// Invalid offsets are rejected instead of producing misleading source data.
func (p *Positions) Source(m Metadata) (grammar.SourceInfo, error) {
	if p == nil || m.Start < 0 || m.End < m.Start || m.End >= len(p.boundaries) {
		return grammar.SourceInfo{}, fmt.Errorf("invalid source span [%d, %d)", m.Start, m.End)
	}
	return grammar.SourceInfo{
		Pos: p.boundaries[m.Start], End: p.boundaries[m.End],
		LeadingComments: m.Leading, TrailingComments: m.Trailing,
		LeadingDetachedComments: append([]string(nil), m.Detached...),
	}, nil
}

// SetSpan copies a canonical syntax span without replacing existing ownership,
// diagnostic positions, or formatter layout. This supports source forms whose
// boundary ownership has not yet been added to the canonical grammar.
func (p *Positions) SetSpan(dst *grammar.SourceInfo, start, end int) error {
	if dst == nil {
		return fmt.Errorf("nil source information for syntax span")
	}
	s, err := p.Source(Metadata{Start: start, End: end})
	if err != nil {
		return err
	}
	dst.Pos, dst.End = s.Pos, s.End
	return nil
}

// PackageClause is the first supported source-AST adapter increment.
func (p *Positions) PackageClause(g *grammar.Grammar, name string, m Metadata) error {
	if g == nil {
		return fmt.Errorf("nil grammar for package clause")
	}
	s, err := p.Source(m)
	if err != nil {
		return err
	}
	g.File.Pkg.Name, g.File.Pkg.SourceInfo = name, s
	return nil
}

// Lead is the canonical record of the comment groups that lead the next owner
// of a boundary: the leading group, the detached groups and whether an owner
// follows. A boundary with no next owner leaves its groups unowned.
type Lead struct {
	Leading  string
	Detached []string
	HasNext  bool
}

// Item is a canonical list element. A statement cannot see the boundary before
// it, so the grammar records the boundary after it: the element's own trailing
// group and the Lead of the element that follows.
type Item struct {
	Start, End int
	Trailing   string
	Next       Lead
}

// Items converts the elements of one list to source information. An element
// takes its leading and detached groups from the previous element's Next, and the
// first from head, the boundary after the list's opening delimiter or the start
// of the file. A nil element, such as a recovered error, owns nothing and leaves
// its successor without leading groups. Comment text is copied as determined by
// the grammar; no group moves between boundaries.
func (p *Positions) Items(head Lead, items []*Item) ([]grammar.SourceInfo, error) {
	out := make([]grammar.SourceInfo, len(items))
	lead := head
	for i, it := range items {
		if it == nil {
			lead = Lead{}
			continue
		}
		m := Metadata{Start: it.Start, End: it.End, Trailing: it.Trailing}
		if lead.HasNext {
			m.Leading, m.Detached = lead.Leading, lead.Detached
		}
		s, err := p.Source(m)
		if err != nil {
			return nil, err
		}
		out[i] = s
		lead = it.Next
	}
	return out, nil
}
