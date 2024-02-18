package memo

import (
	"context"
	"fmt"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/parser"
	"github.com/ornew/pego/pkg/scanner"
	"github.com/ornew/pego/pkg/token"
)

type ctxKeyType struct{}

var ctxKey = ctxKeyType{}

type Memo struct {
	items map[key]item
	depth int
}

func New() Memo {
	return Memo{
		items: map[key]item{},
		depth: 0,
	}
}

func From(ctx context.Context) Memo {
	ma := ctx.Value(ctxKey)
	m, ok := ma.(Memo)
	if !ok {
		return New()
	}
	return m
}

func To(ctx context.Context, m Memo) context.Context {
	return context.WithValue(ctx, ctxKey, m)
}

type key struct {
	id  int
	pos token.Pos
}

type item struct {
	node *cst.Node
	end  token.Pos
}

func Memoize(id int, f parser.ParseFunc) parser.ParseFunc {
	return func(ctx context.Context, s scanner.Scanner) *cst.Node {
		k := key{id: id, pos: s.Pos()}
		m := From(ctx)
		if mem, ok := m.items[k]; ok {
			s.Seek(mem.end.Int64(), 0)
			return mem.node
		}
		n := f(ctx, s)
		m.items[k] = item{node: n, end: s.Pos()}
		return n
	}
}

func MemoizeRec(id int, f parser.ParseFunc) parser.ParseFunc {
	return func(ctx context.Context, s scanner.Scanner) *cst.Node {
		start := s.Pos()
		k := key{id: id, pos: start}
		m := From(ctx)
		if mem, ok := m.items[k]; ok {
			fmt.Printf("cache hit: %v -> %v\n", k, mem)
			s.Seek(mem.end.Int64(), 0)
			return mem.node
		}
		fmt.Printf("cache miss: %v\n", k)
		last := item{node: nil, end: start}
		m.items[k] = last
		for {
			s.Seek(start.Int64(), 0)
			m.depth += 1
			n := f(ctx, s)
			end := s.Pos()
			if n == nil || end <= last.end {
				m.depth -= 1
				break
			}
			last = item{node: n, end: end}
			m.items[k] = last
		}
		s.Seek(last.end.Int64(), 0)
		n := last.node
		end := s.Pos()
		if n == nil {
			end = start
			s.Seek(end.Int64(), 0)
		}
		m.items[k] = item{node: n, end: end}
		return n
	}
}
