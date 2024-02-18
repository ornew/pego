// manual parser implementation.
package manual

import (
	"context"
	"io"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/memo"
	"github.com/ornew/pego/pkg/scanner"
)

const (
	Rule_root = iota
	Rule__root_1
	Rule_a
	Rule_b
	Rule_c
	Rule__c_0
	Rule__c_1
)

// Parse root rule.
//
//	start <- '.' / a
func Parse(ctx context.Context, s scanner.Scanner) *cst.Node {
	pos := s.Pos()
	n := cst.NewNode(Rule_root)
	if !s.IsEnd() && s.Peek() == '.' {
		s.Seek(1, io.SeekCurrent)
		n.AppendChild(cst.NewNode(Rule__root_1).SetRange(pos, s.Pos()))
		return n
	}
	s.Seek(pos.Int64(), io.SeekStart)
	if c := Parse_a(ctx, s); c != nil {
		return c
	}
	s.Seek(pos.Int64(), io.SeekStart)
	return nil
}

// Parse rule `a`.
//
//	a <- b
func _Parse_a_impl(ctx context.Context, s scanner.Scanner) *cst.Node {
	pos := s.Pos()
	n := cst.NewNode(Rule_a)
	if c := Parse_b(ctx, s); c != nil {
		n.AppendChild(c)
		return n
	}
	s.Seek(pos.Int64(), 0)
	return nil
}

// Parse rule `b`.
//
//	a <- b
func Parse_a(ctx context.Context, s scanner.Scanner) *cst.Node {
	return memo.MemoizeRec(Rule_a, _Parse_a_impl)(ctx, s)
}

func Parse_b(ctx context.Context, s scanner.Scanner) *cst.Node {
	// b <- c
	pos := s.Pos()
	// n := NewNode(Rule_b)
	if c := Parse_c(ctx, s); c != nil {
		// n.AppendChild(c)
		// return n
		return c
	}
	s.Seek(pos.Int64(), io.SeekStart)
	return nil
}

// Parse rule `c`.
//
//	c <- a "+" / "a" a / "x"+
func Parse_c(ctx context.Context, s scanner.Scanner) *cst.Node {
	pos := s.Pos()
	n := cst.NewNode(Rule_c)
	if c := Parse_a(ctx, s); c != nil {
		n.AppendChild(c)
		if !s.IsEnd() && s.Peek() == '+' {
			s.Seek(1, io.SeekCurrent)
			n.AppendChild(cst.NewNode(Rule__c_0).SetRange(pos, s.Pos()))
			return n
		}
	}
	s.Seek(pos.Int64(), io.SeekStart)
	if !s.IsEnd() && s.Peek() == 'a' {
		s.Seek(1, io.SeekCurrent)
		n.AppendChild(cst.NewNode(Rule__c_1).SetRange(pos, s.Pos()))
		if c := Parse_a(ctx, s); c != nil {
			n.AppendChild(c)
			return n
		}
	}
	s.Seek(pos.Int64(), io.SeekStart)
	if c := _Parse_c_1(ctx, s); c != nil {
		n.AppendChild(c)
		return n
	}
	s.Seek(pos.Int64(), io.SeekStart)
	return nil
}

// Parse inline rule `#c_1`.
//
//	"x"+
func _Parse_c_1_impl(ctx context.Context, p scanner.Scanner) *cst.Node {
	pos := p.Pos()
	n := cst.NewNode(Rule__c_1)
	for !p.IsEnd() {
		if p.Peek() == 'x' {
			end := pos + 1
			p.Seek(end.Int64(), io.SeekStart)
			c := cst.NewNode(Rule__c_1).SetRange(pos, end)
			n.AppendChild(c)
			pos = end
			continue
		}
		break
	}
	if len(n.Children) == 0 {
		return nil
	}
	return n
}

// Parse inline rule `#c_1`.
//
//	"x"+
//
// * Memoize
func _Parse_c_1(ctx context.Context, s scanner.Scanner) *cst.Node {
	return memo.Memoize(Rule__c_1, _Parse_c_1_impl)(ctx, s)
}
