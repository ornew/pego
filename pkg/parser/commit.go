package parser

import "github.com/ornew/pego/pkg/token"

type Commit struct {
	Range    token.Range
	Code     uint32
	Accepted bool
}
