package recursive

import (
	"fmt"
	"io"
	"strings"

	"github.com/ornew/pego/pkg/cst"
)

type Parser struct {
	memos map[memoKey]memoItem
	src   []byte
	pos   int
	depth int
}

func New(i io.Reader) (*Parser, error) {
	src, err := io.ReadAll(i)
	if err != nil {
		return nil, err
	}
	return &Parser{
		src:   src,
		pos:   0,
		memos: make(map[memoKey]memoItem),
	}, nil
}

func (p *Parser) NodeToString(n *cst.Node, i int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d: %q [%d:%d]", n.Type, p.src[n.Pos():n.End()], n.Pos(), n.End())
	if len(n.Children) > 0 {
		b.WriteString(" {\n")
		for _, c := range n.Children {
			for j := 0; j <= i; j++ {
				b.WriteString(". ")
			}
			b.WriteString(p.NodeToString(c, i+1))
			b.WriteByte('\n')
		}
		for j := 0; j <= i-1; j++ {
			b.WriteString(". ")
		}
		b.WriteString("}")
	}
	return b.String()
}

func (p *Parser) Pos() int {
	return p.pos
}

func (p *Parser) IsEnd() bool {
	return len(p.src) <= p.pos
}

func (p *Parser) Peek() byte {
	return p.src[p.pos]
}

func (p *Parser) Seek(pos int) {
	p.pos = pos
}
