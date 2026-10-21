// Package cel parses CEL, the Common Expression Language (https://github.com/google/cel-spec), with a parser generated
// by PEGO from cel.pego.
//
// ParseAST returns an expression as typed values, each with its Span in the input: Ident, IntLit, UintLit, DoubleLit,
// StringLit, BytesLit, BoolLit and NullLit for names and literals, Select, Index, Call, Unary, Binary and Conditional
// for operations, Paren, ListLit, MapLit and MessageLit for the rest, and Optional for an optional list element. The
// tree keeps what is written, parentheses included (a Paren), so that Format can print it back; Inspect and Children
// walk it, and the Value methods decode literals as cel-go does.
//
// The grammar accepts what the parser of cel-go (v0.32.0) accepts: the specification's syntax with the optional
// syntax (a.?b, a[?b], [?a], {?k: v}, Msg{?f: v}) and fields escaped with backquotes. Macros (has, all, exists,
// exists_one, map, filter) are calls in the tree, since the specification leaves the macros an application supports
// to the application; CheckMacros reports the calls that cel-go's standard macros reject.
//
// What cel-go reads after it has parsed is not in the grammar, so ParseAST accepts it: an integer literal out of the
// range of int64 or uint64, a double out of the range of float64, and expressions nested deeper than 250 levels or
// longer than 100,000 code points (cel-go's defaults). ParseExpr, which is ParseAST followed by Check, rejects them.
//
// The tests compare the AST that this parser returns for hundreds of thousands of expressions with the AST of cel-go,
// accept the 2,527 expressions of the conformance tests of cel-spec, and reject what cel-go rejects. The README
// has the numbers, the deviations (the messages and positions of syntax errors differ from cel-go's) and the
// benchmarks.
package cel

import (
	"github.com/ornew/pego/parsers/cel/types"
)

// SpanOf returns the range of the input that e covers, or the zero Span for nil or an Error left by recovery.
func SpanOf(e types.Expr) Span {
	switch e := e.(type) {
	case *types.Ident:
		return e.Span
	case *types.IntLit:
		return e.Span
	case *types.UintLit:
		return e.Span
	case *types.DoubleLit:
		return e.Span
	case *types.StringLit:
		return e.Span
	case *types.BytesLit:
		return e.Span
	case *types.BoolLit:
		return e.Span
	case *types.NullLit:
		return e.Span
	case *types.Paren:
		return e.Span
	case *types.Select:
		return e.Span
	case *types.Index:
		return e.Span
	case *types.Call:
		return e.Span
	case *types.Unary:
		return e.Span
	case *types.Binary:
		return e.Span
	case *types.Conditional:
		return e.Span
	case *types.ListLit:
		return e.Span
	case *types.MapLit:
		return e.Span
	case *types.MessageLit:
		return e.Span
	case *types.Optional:
		return e.Span
	}
	return Span{}
}

// Children calls f with each direct sub-expression of e, in source order.
func Children(e types.Expr, f func(types.Expr)) {
	switch e := e.(type) {
	case *types.Paren:
		f(e.X)
	case *types.Select:
		f(e.X)
	case *types.Index:
		f(e.X)
		f(e.Index)
	case *types.Call:
		if e.Target != nil {
			f(e.Target)
		}
		for _, a := range e.Args {
			f(a)
		}
	case *types.Unary:
		f(e.X)
	case *types.Binary:
		f(e.Left)
		f(e.Right)
	case *types.Conditional:
		f(e.Cond)
		f(e.Then)
		f(e.Else)
	case *types.ListLit:
		for _, x := range e.Elems {
			f(x)
		}
	case *types.MapLit:
		for _, en := range e.Entries {
			f(en.Key)
			f(en.Value)
		}
	case *types.MessageLit:
		for _, fl := range e.Fields {
			f(fl.Value)
		}
	case *types.Optional:
		f(e.X)
	}
}

// Inspect traverses the expression in depth-first order: it calls f(e), and if f returns true, inspects the
// children of e.
func Inspect(e types.Expr, f func(types.Expr) bool) {
	if e == nil || !f(e) {
		return
	}
	Children(e, func(c types.Expr) { Inspect(c, f) })
}
