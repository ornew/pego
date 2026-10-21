package cel

import (
	"strings"

	"github.com/ornew/pego/parsers/cel/types"
)

// Format returns the expression as source text: the literals as they were written (a triple-quoted string keeps its
// line breaks), the operators with a space around them, and no comments. It puts parentheses where the precedence of
// a node needs them, so it is also fit for a tree that was built without Paren nodes. Parsing the result gives the
// same expression, apart from the parentheses that it adds and the positions.
func Format(e types.Expr) string {
	var b strings.Builder
	f := &formatter{&b}
	f.expr(e, precCond)
	return b.String()
}

// Precedences, from the loosest: an expression needs parentheses where a looser one is meant.
const (
	precCond = iota + 1
	precOr
	precAnd
	precRel
	precAdd
	precMul
	precUnary
	precMember
)

func precedence(e types.Expr) int {
	switch e := e.(type) {
	case *types.Conditional:
		return precCond
	case *types.Binary:
		switch e.Op.Value {
		case "||":
			return precOr
		case "&&":
			return precAnd
		case "+", "-":
			return precAdd
		case "*", "/", "%":
			return precMul
		}
		return precRel
	case *types.Unary:
		return precUnary
	}
	return precMember
}

type formatter struct{ b *strings.Builder }

func (f *formatter) s(s string) { f.b.WriteString(s) }

// expr writes e, in parentheses unless its precedence is at least min.
func (f *formatter) expr(e types.Expr, min int) {
	if precedence(e) < min {
		f.s("(")
		f.expr(e, precCond)
		f.s(")")
		return
	}
	switch e := e.(type) {
	case *types.Paren:
		f.s("(")
		f.expr(e.X, precCond)
		f.s(")")
	case *types.Ident:
		f.ident(e)
	case *types.IntLit:
		f.s(e.Text())
	case *types.DoubleLit:
		f.s(e.Text())
	case *types.UintLit, *types.StringLit, *types.BytesLit, *types.BoolLit, *types.NullLit:
		f.s(literalText(e))
	case *types.Select:
		f.expr(e.X, precMember)
		f.s(".")
		if e.Optional {
			f.s("?")
		}
		f.s(e.Field.TNode.Value)
	case *types.Index:
		f.expr(e.X, precMember)
		f.s("[")
		if e.Optional {
			f.s("?")
		}
		f.expr(e.Index, precCond)
		f.s("]")
	case *types.Call:
		if e.Target != nil {
			f.expr(e.Target, precMember)
			f.s(".")
		}
		f.ident(e.Func)
		f.args(e.Args)
	case *types.Unary:
		f.s(e.Op.Value)
		if u, ok := e.X.(*types.Unary); ok && u.Op.Value == e.Op.Value { // a run of the same operator, as written
			f.expr(u, precUnary)
			return
		}
		// An operand that starts with a sign (a negative literal) needs parentheses after a minus: two minus signs
		// are two negations of a positive literal.
		var operand strings.Builder
		(&formatter{&operand}).expr(e.X, precMember)
		if e.Op.Value == "-" && strings.HasPrefix(operand.String(), "-") {
			f.s("(" + operand.String() + ")")
		} else {
			f.s(operand.String())
		}
	case *types.Binary:
		p := precedence(e)
		f.expr(e.Left, p)
		f.s(" " + e.Op.Value + " ")
		f.expr(e.Right, p+1)
	case *types.Conditional:
		f.expr(e.Cond, precOr)
		f.s(" ? ")
		f.expr(e.Then, precOr)
		f.s(" : ")
		f.expr(e.Else, precCond)
	case *types.ListLit:
		f.s("[")
		for i, x := range e.Elems {
			if i > 0 {
				f.s(", ")
			}
			f.expr(x, precCond)
		}
		f.s("]")
	case *types.MapLit:
		f.s("{")
		for i, en := range e.Entries {
			if i > 0 {
				f.s(", ")
			}
			if en.Optional {
				f.s("?")
			}
			f.expr(en.Key, precCond)
			f.s(": ")
			f.expr(en.Value, precCond)
		}
		f.s("}")
	case *types.MessageLit:
		f.ident(e.Type)
		f.s("{")
		for i, fl := range e.Fields {
			if i > 0 {
				f.s(", ")
			}
			if fl.Optional {
				f.s("?")
			}
			f.s(fl.Name.TNode.Value + ": ")
			f.expr(fl.Value, precCond)
		}
		f.s("}")
	case *types.Optional:
		f.s("?")
		f.expr(e.X, precCond)
	}
}

func (f *formatter) ident(id *types.Ident) {
	if id.Rooted() {
		f.s(".")
	}
	f.s(id.Name())
}

func (f *formatter) args(args []types.Expr) {
	f.s("(")
	for i, a := range args {
		if i > 0 {
			f.s(", ")
		}
		f.expr(a, precCond)
	}
	f.s(")")
}

func literalText(e types.Expr) string {
	switch e := e.(type) {
	case *types.UintLit:
		return e.TNode.Value
	case *types.StringLit:
		return e.TNode.Value
	case *types.BytesLit:
		return e.TNode.Value
	case *types.BoolLit:
		return e.TNode.Value
	case *types.NullLit:
		return e.Value
	}
	return ""
}
