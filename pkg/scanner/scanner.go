package scanner

import "github.com/ornew/pego/pkg/token"

type Scanner interface {
	Pos() token.Pos
	IsEnd() bool
	Peek() byte
	Seek(int64, int) (int64, error)
}
