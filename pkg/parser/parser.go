package parser

import (
	"bytes"
	"io"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/token"
)

type Parser struct {
	stack        *Stack
	head         *cst.Node
	src          []byte
	stage        Commit
	srcEnd       uint32
	repeat       uint32
	ruleCodeSize uint32
}

func NewParser(r io.Reader) (*Parser, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	oend := uint32(len(src))
	stack := NewStack()
	stack.Push(StackData{
		Code: 1,
		Node: &cst.Node{},
	})
	return &Parser{
		src:    src,
		stack:  stack,
		srcEnd: oend,
	}, nil
}

func (p *Parser) SetRuleCodeSize(n uint32) {
	p.ruleCodeSize = n
}

func (p *Parser) Consume(n uint32) error {
	for i := uint32(0); i < n; i++ {
		p.stack.Top.Range.End.Offset++
		p.stack.Top.Range.End.Column++
		// fmt.Printf("consume %v %d\n", top.pos, n)
	}
	return nil
}

func (p *Parser) GetCurrentChar() byte {
	if p.stack.Top.Range.End.Offset == p.srcEnd {
		return 0
	}
	return p.src[p.stack.Top.Range.End.Offset]
}

func (p *Parser) GetCurrentText() []byte {
	if p.stack.Top.Range.End.Offset > p.srcEnd {
		return nil
	}
	return p.src[p.stack.Top.Range.Start.Offset:p.stack.Top.Range.End.Offset]
}

func (p *Parser) Reject() {
	// fmt.Printf("==== REJECT: %3d: %16v: %q\n", p.stack.Top.Code, p.stack.Top.Range, p.GetCurrentText())
	p.stack.Top.Range.End = p.stack.Top.Range.Start
	p.stage = Commit{
		Range: p.stack.Top.Range,
		Code:  p.stack.Top.Code,
	}
	_ = p.stack.Pop(1)
}

func Squash(n *cst.Node, max uint32) *cst.Node {
	var cs []*cst.Node
	for _, c := range n.Children {
		if c.Code <= max {
			cs = append(cs, c)
		} else {
			for _, gc := range c.Children {
				gc.Parent = n
				cs = append(cs, gc)
			}
		}
	}
	n.Children = cs
	return n
}

func (p *Parser) Accept() {
	// fmt.Printf("==== ACCEPT: %3d: %16v: %q\n", p.stack.Top.Code, p.stack.Top.Range, p.GetCurrentText())
	p.stage = Commit{
		Range:    p.stack.Top.Range,
		Code:     p.stack.Top.Code,
		Accepted: true,
	}
	branch := p.stack.Pop(1)
	branch.Node.Range = p.stage.Range
	branch.Node.Code = p.stage.Code
	branch.Node = Squash(branch.Node, p.ruleCodeSize)
	p.stack.Merge(&branch)
	if p.stack.Top != nil {
		p.stack.Top.Range.End = branch.Range.End
		p.head = p.stack.Top.Node
	}
}

func (p *Parser) Head() *cst.Node {
	return p.head
}

func (p *Parser) CommitA() {
	p.stack.Top.A = p.stage
	p.stage = Commit{}
}

func (p *Parser) CommitB() {
	p.stack.Top.B = p.stage
	p.stage = Commit{}
}

func (p *Parser) TerminalChar(a byte) {
	if p.GetCurrentChar() != a {
		p.Reject()
		return
	}
	p.Consume(1)
	p.Accept()
}

func (p *Parser) Terminal(a []byte) {
	p.Consume(uint32(len(a)))
	if !bytes.Equal(p.GetCurrentText(), a) {
		p.Reject()
		return
	}
	p.Accept()
}

func (p *Parser) TerminalRange(a, b byte) {
	c := p.GetCurrentChar()
	if a <= c && c <= b {
		p.Consume(1)
		p.Accept()
		return
	}
	p.Reject()
}

func (p *Parser) Alias(a uint32) {
	switch p.stage.Code {
	case a:
		p.CommitA()
	}
	switch {
	case p.stack.Top.A.Code == a:
		if !p.stack.Top.A.Accepted {
			p.Reject()
		} else {
			p.Accept()
		}
	case p.stack.Top.A.Code != a:
		p.stack.Branch(p.stack.Top, a)
	}
}

func (p *Parser) Sequence(a, b uint32) {
	switch p.stage.Code {
	case a:
		p.CommitA()
	case b:
		p.CommitB()
	}
	switch {
	case p.stack.Top.A.Code == a && !p.stack.Top.A.Accepted:
		p.Reject()
	case p.stack.Top.B.Code == b && !p.stack.Top.B.Accepted:
		p.Reject()
	case p.stack.Top.A.Code == a && p.stack.Top.B.Code == b:
		p.Accept()
	case p.stack.Top.A.Code != a:
		p.stack.Branch(p.stack.Top, a)
	case p.stack.Top.B.Code != b:
		p.stack.Branch(p.stack.Top, b)
	}
}

func (p *Parser) Choice(a, b uint32) {
	switch p.stage.Code {
	case a:
		p.CommitA()
	case b:
		p.CommitB()
	}
	switch {
	case p.stack.Top.A.Code == a && p.stack.Top.A.Accepted:
		p.Accept()
	case p.stack.Top.B.Code == b && p.stack.Top.B.Accepted:
		p.Accept()
	case p.stack.Top.A.Code == a && p.stack.Top.B.Code == b:
		p.Reject()
	case p.stack.Top.A.Code != a:
		p.stack.Branch(p.stack.Top, a)
	case p.stack.Top.B.Code != b:
		p.stack.Branch(p.stack.Top, b)
	}
}

func (p *Parser) ZeroOrMore(a uint32) {
	switch p.stage.Code {
	case a:
		p.CommitA()
	}
	switch {
	case p.stack.Top.A.Code == a:
		if p.stack.Top.A.Accepted {
			p.repeat++
			p.stack.Branch(p.stack.Top, a)
		} else {
			p.Accept()
		}
	case p.stack.Top.A.Code != a:
		p.repeat = 0
		p.stack.Branch(p.stack.Top, a)
	}
}

func (p *Parser) OneOrMore(a uint32) {
	switch p.stage.Code {
	case a:
		p.CommitA()
	}
	switch {
	case p.stack.Top.A.Code == a:
		if p.stack.Top.A.Accepted {
			p.repeat++
			p.stack.Branch(p.stack.Top, a)
		} else {
			if p.repeat > 0 {
				p.Accept()
				p.repeat = 0
			} else {
				p.Reject()
			}
		}
	case p.stack.Top.A.Code != a:
		p.repeat = 0
		p.stack.Branch(p.stack.Top, a)
	}
}

func (p *Parser) Optional(a uint32) {
	switch p.stage.Code {
	case a:
		p.CommitA()
	}
	switch {
	case p.stack.Top.A.Code == a:
		p.Accept()
	case p.stack.Top.A.Code != a:
		p.stack.Branch(p.stack.Top, a)
	}
}

func (p *Parser) Top() *StackData {
	return p.stack.Top
}

func (p *Parser) GetText(r token.Range) []byte {
	if r.End.Offset > p.srcEnd {
		return nil
	}
	return p.src[r.Start.Offset:r.End.Offset]
}
