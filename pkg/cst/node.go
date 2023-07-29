package cst

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/pkg/token"
)

type NodeType uint32

type Node struct {
	Parent   *Node
	Children []*Node
	Code     uint32
	Range    token.Range
}

func (n *Node) AddChild(c *Node) {
	n.Children = append(n.Children, c)
}

func (n *Node) String() string {
	var b strings.Builder
	c := n.Parent
	for c != nil {
		c = c.Parent
		b.WriteString("  ")
	}
	b.WriteString(fmt.Sprintf("%d (%s)\n", n.Code, n.Range))
	for _, c := range n.Children {
		b.WriteString(c.String())
	}
	return b.String()
}
