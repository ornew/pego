package sourceast

import (
	"fmt"

	"github.com/ornew/zeroparse/grammar"
)

// Gap is the canonical record of the trivia between two tokens when it holds a
// comment. Start and End are zero-based rune offsets of the trivia: Start is
// where the token before it ends and End where the token after it starts. The
// groups are determined by the grammar; Trailing lists the comments that trail
// the token before the gap, HasTrailing tells that there are some, and HasNext
// whether a token follows that can own the leading groups.
type Gap struct {
	Start, End  int
	Trailing    []string
	HasTrailing bool
	Leading     string
	Detached    []string
	HasNext     bool
}

// Unset is the offset of a position that a node does not have.
const Unset = -1

// Event is one time the syntax completes a node: its span at that time.
type Event struct{ Start, End int }

// Node is a source-bearing node in the order its syntax is completed, inner
// nodes before the nodes that contain them. Events lists each time the node was
// completed; a node that is completed again, such as one extended by another
// suffix, has more than one, and its span is that of the last. Later lists the
// ends of tokens consumed after the node, such as a separator, at which the
// node claims a trailing group once more. Created is the position the syntax
// gave the node before it completed it, Unset if none: when it differs from
// where the node starts, it is the position of the token a diagnostic about
// the node is about, such as an operator. Joined lists the ends of semicolons
// that follow the node: the trailing comments after one are appended to the node's
// instead of replacing them.
type Node struct {
	Events  []Event
	Later   []int
	Joined  []int
	Created int
}

// Claim assigns the comment groups of every gap to the nodes that own them.
// A comment at a token belongs to the outermost node that starts (leading and
// detached groups) or ends (trailing group) at that token: nodes claim the
// boundaries at their ends as they are completed, so a node completed later
// takes the groups of an inner node and the inner node loses them. A claim of
// a boundary with no next token takes no leading groups. A node that claims
// the boundary after a separator keeps the trailing group it took before the
// separator when the boundary after it has none.
//
// No group moves between boundaries and none is copied: a group a node does not
// end up owning belongs to nobody. The result has one entry per node.
func (p *Positions) Claim(gaps []Gap, nodes []Node) ([]grammar.SourceInfo, error) {
	byEnd := make(map[int]int, len(gaps))   // gap index by the offset where it ends
	byStart := make(map[int]int, len(gaps)) // gap index by the offset where it starts
	for i, g := range gaps {
		byEnd[g.End], byStart[g.Start] = i, i
	}
	lead := make([]int, len(gaps))
	trail := make([]int, len(gaps))
	for i := range gaps {
		lead[i], trail[i] = -1, -1
	}
	out := make([]grammar.SourceInfo, len(nodes))
	at := func(off int) grammar.Pos { return p.boundaries[off] }
	parts := make([][]int, len(nodes)) // the gaps whose trailing groups a node owns, in order
	render := func(n int) {
		out[n].TrailingComments = nil
		for _, gi := range parts[n] {
			out[n].TrailingComments = append(out[n].TrailingComments, gaps[gi].Trailing...)
		}
	}
	claimTrailing := func(n int, at int, join bool) {
		gi, ok := byStart[at]
		if !ok || !gaps[gi].HasTrailing || trail[gi] == n {
			return
		}
		if old := trail[gi]; old >= 0 {
			parts[old] = nil
			render(old)
		}
		if join && len(parts[n]) > 0 {
			parts[n] = append(parts[n], gi)
		} else {
			parts[n] = []int{gi}
		}
		render(n)
		trail[gi] = n
	}
	for n, node := range nodes {
		if len(node.Events) == 0 {
			return nil, fmt.Errorf("node %d is never completed", n)
		}
		if node.Created != Unset && (node.Created < 0 || node.Created >= len(p.boundaries)) {
			return nil, fmt.Errorf("node %d was created at invalid offset %d", n, node.Created)
		}
		pos, diag := node.Created, Unset
		for _, ev := range node.Events {
			s, err := p.Source(Metadata{Start: ev.Start, End: ev.End})
			if err != nil {
				return nil, err
			}
			// The diagnostic position is the one the node had before, when it differs.
			old := diag
			if old == Unset {
				old = pos
			}
			diag = Unset
			if old != Unset && old != ev.Start {
				diag = old
			}
			pos = ev.Start
			out[n].Pos, out[n].End = s.Pos, s.End
			out[n].DiagnosticPos = grammar.Pos{}
			if diag != Unset {
				out[n].DiagnosticPos = at(diag)
			}
			if gi, ok := byEnd[ev.Start]; ok && gaps[gi].HasNext && lead[gi] != n {
				if old := lead[gi]; old >= 0 {
					out[old].LeadingComments, out[old].LeadingDetachedComments = "", nil
				}
				out[n].LeadingComments = gaps[gi].Leading
				out[n].LeadingDetachedComments = append([]string(nil), gaps[gi].Detached...)
				lead[gi] = n
			}
			claimTrailing(n, ev.End, false)
		}
		for _, at := range node.Later {
			claimTrailing(n, at, false)
		}
		for _, at := range node.Joined {
			claimTrailing(n, at, true)
		}
	}
	return out, nil
}
