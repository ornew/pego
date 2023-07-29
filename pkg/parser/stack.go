package parser

import (
	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/token"
)

type StackData struct {
	Node  *cst.Node
	Code  uint32
	A     Commit
	B     Commit
	Range token.Range
}

type Stack struct {
	Top  *StackData
	Data []StackData
}

func NewStack() *Stack {
	return &Stack{
		Data: make([]StackData, 0, 256),
	}
}

func (s *Stack) top() {
	if len(s.Data) == 0 {
		s.Top = nil
		return
	}
	s.Top = &s.Data[len(s.Data)-1]
}

func (s *Stack) Push(d StackData) {
	// var last uint32
	// if s.Top != nil {
	// 	last = s.Top.Code
	// }
	s.Data = append(s.Data, d)
	// fmt.Printf("push: %d -> %d\n", last, d.Code)
	// fmt.Printf("push: %v\n", s.Data)
	s.top()
}

func (s *Stack) Pop(c int) StackData {
	last := s.Data[len(s.Data)-c]
	s.Data = s.Data[:len(s.Data)-c]
	// fmt.Printf("pop %d: %v: %v\n", c, last, s.Data)
	s.top()
	// var top uint32
	// if s.Top != nil {
	// 	top = s.Top.Code
	// }
	// fmt.Printf("pop: %d -> %d\n", last.Code, top)
	return last
}

func (s *Stack) Merge(branch *StackData) {
	if s.Top == nil {
		return
	}
	s.Top.Node.AddChild(branch.Node)
}

func (s *Stack) Branch(parent *StackData, code uint32) {
	d := StackData{
		Node: &cst.Node{
			Parent: parent.Node,
		},
		Code: code,
		Range: token.Range{
			Start: parent.Range.End,
			End:   parent.Range.End,
		},
	}
	s.Push(d)
}
