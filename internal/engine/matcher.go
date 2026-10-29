package engine

import (
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// matcher tries to match at the current position and returns the value and whether it
// succeeded. On failure the position is unspecified; the caller restores it.
type matcher func(p *parser) (*value.Node, bool)

// tracked runs m with a separate record of expectations. It returns the farthest position and
// the expectations of that separate record (the expectations are valid only until the next
// recording; see unisolate).
func tracked(p *parser, m matcher) (v *value.Node, ok bool, far int, expected []expID) {
	mark := p.isolate(p.pos)
	v, ok = m(p)
	far, expected = p.unisolate(mark)
	return v, ok, far, expected
}

func renamedExpected(p *parser, expected []expID, name expID) []expID {
	// unisolate leaves this record beyond the active expectation stack. Rename it
	// before mergeExpected reuses the stack; this avoids an unbounded copy.
	for i, id := range expected {
		p.stepWork()
		if id&msgBit == 0 {
			expected[i] = name
		}
	}
	return expected
}

// shapeTuple returns the tuple node, shaped by s, of the result v of a match of [start, p.pos).
func shapeTuple(p *parser, s compile.Shaper, v *value.Node, start int) *value.Node {
	if s.Elems {
		if s.Shared && v != nil {
			return p.newNode(value.Data{Kind: value.SeqKind, Start: value.D(v).Start, End: value.D(v).End, Children: value.D(v).Children, Fresh: true})
		}
		return v
	}
	var kids []*value.Node
	if s.K == 1 {
		kids = p.nodes(1)
		kids[0] = v
	}
	return p.newNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(p.pos), Children: kids, Fresh: true})
}

// shapeSingle returns the value, shaped by s, of the result v where the expression takes one
// element; ok is false when it has no value.
func shapeSingle(p *parser, s compile.Shaper, mode compile.SingleMode, v *value.Node, start int) (*value.Node, bool) {
	switch mode {
	case compile.SinglePass:
		return s.One(v), true
	case compile.SingleTuple:
		return shapeTuple(p, s, v, start), true
	}
	return nil, false
}
