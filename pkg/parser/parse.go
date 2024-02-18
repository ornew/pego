package parser

import (
	"context"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/scanner"
)

type ParseFunc = func(context.Context, scanner.Scanner) *cst.Node
