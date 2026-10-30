package core

import (
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// Matcher tries to match at the current position and returns the value and whether it
// succeeded. On failure the position is unspecified; the caller restores it.
type Matcher func(p *Parser) (*value.Node, bool)

// Tracked runs m with a separate record of expectations. It returns the farthest position and
// the expectations of that separate record (the expectations are valid only until the next
// recording; see unisolate).
func Tracked(p *Parser, m Matcher) (v *value.Node, ok bool, far int, expected []ExpID) {
	mark := p.Isolate(p.Pos)
	v, ok = m(p)
	far, expected = p.Unisolate(mark)
	return v, ok, far, expected
}

func RenamedExpected(p *Parser, expected []ExpID, name ExpID) []ExpID {
	// unisolate leaves this record beyond the active expectation stack. Rename it
	// before MergeExpected reuses the stack; this avoids an unbounded copy.
	for i, id := range expected {
		p.Spend()
		if id&MsgBit == 0 {
			expected[i] = name
		}
	}
	return expected
}

// ShapeTuple returns the tuple node, shaped by s, of the result v of a match of [start, p.pos).
func ShapeTuple(p *Parser, s compile.Shaper, v *value.Node, start int) *value.Node {
	if s.Elems {
		if s.Shared && v != nil {
			return p.NewNode(value.Data{Kind: value.SeqKind, Start: value.D(v).Start, End: value.D(v).End, Children: value.D(v).Children, Fresh: true})
		}
		return v
	}
	var kids []*value.Node
	if s.K == 1 {
		kids = p.Nodes(1)
		kids[0] = v
	}
	return p.NewNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true})
}

// ShapeSingle returns the value, shaped by s, of the result v where the expression takes one
// element; ok is false when it has no value.
func ShapeSingle(p *Parser, s compile.Shaper, mode compile.SingleMode, v *value.Node, start int) (*value.Node, bool) {
	switch mode {
	case compile.SinglePass:
		return s.One(v), true
	case compile.SingleTuple:
		return ShapeTuple(p, s, v, start), true
	}
	return nil, false
}
