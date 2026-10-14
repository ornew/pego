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

// PackageClause is the first supported source-AST adapter increment.
func (p *Positions) PackageClause(g *grammar.Grammar, name string, m Metadata) error {
	if g == nil {
		return fmt.Errorf("nil grammar for package clause")
	}
	s, err := p.Source(m)
	if err != nil {
		return err
	}
	g.Package, g.PackageSource = name, s
	return nil
}
