package types

import (
	"github.com/ornew/pego/parsers/xml/internal/text"
	"strings"
)

// Name returns the name of the entity.
func (r *EntityRef) Name() string { return r.Value[1 : len(r.Value)-1] }

// Name returns the name of the parameter entity.
func (r *PERef) Name() string { return r.Value[1 : len(r.Value)-1] }

// Rune returns the character the reference denotes, and false if it is not a legal character (the
// constraint "Legal Character").
func (r *CharRef) Rune() (rune, bool) { return text.CharRef(r.Value[2 : len(r.Value)-1]) }

// Value returns the character data with line ends normalized (CR LF and CR become LF).
func (t *CharData) Value() string { return text.Newlines(t.TNode.Value) }

// Value returns the text of the CDATA section with line ends normalized.
func (t *CDSect) Value() string { return text.Newlines(t.TNode.Value) }

// Value returns the text of the comment with line ends normalized.
func (c *Comment) Value() string { return text.Newlines(c.TNode.Value) }

// Value returns the data of the processing instruction with line ends normalized.
func (d *PIData) Value() string { return text.Newlines(d.TNode.Value) }

// Value returns the literal with line ends normalized.
func (l *SystemLiteral) Value() string { return text.Newlines(l.TNode.Value) }

// Value returns the public identifier normalized: line ends and runs of spaces become one space, and
// leading and trailing spaces are removed.
func (l *PubidLiteral) Value() string {
	return strings.Join(strings.FieldsFunc(l.TNode.Value, func(r rune) bool { return r == ' ' || r == '\r' || r == '\n' }), " ")
}
