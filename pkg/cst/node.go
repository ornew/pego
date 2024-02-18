package cst

import "github.com/ornew/pego/pkg/token"

type Node struct {
	Parent   *Node
	Children []*Node
	Type     int
	pos      token.Pos
	end      token.Pos
}

func NewNode(t int) *Node {
	return &Node{Type: t}
}

func (n *Node) Pos() token.Pos {
	if n.pos != 0 {
		return n.pos
	}
	if len(n.Children) > 0 {
		return n.Children[0].Pos()
	}
	return 0
}

func (n *Node) End() token.Pos {
	if n.end != 0 {
		return n.end
	}
	if len(n.Children) > 0 {
		return n.Children[len(n.Children)-1].End()
	}
	return 0
}

func (n *Node) SetRange(pos, end token.Pos) *Node {
	n.pos = pos
	n.end = end
	return n
}

func (n *Node) AppendChild(c *Node) {
	c.Parent = n
	n.Children = append(n.Children, c)
}
