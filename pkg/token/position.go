package token

import "fmt"

type Position struct {
	Offset uint32
	Line   uint32
	Column uint32
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

type Range struct {
	Start Position
	End   Position
}

func (p Range) String() string {
	return fmt.Sprintf("%s-%s", &p.Start, &p.End)
}
