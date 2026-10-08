package cel

import "strings"

// Format returns the expression as source text on one line: the literals as they were written, the operators with a
// space around them, and no comments. It puts parentheses where the precedence of a node needs them, so it is also fit
// for a tree that was built without Paren nodes. Parsing the result gives the same expression, apart from the
// parentheses that it adds or drops and the positions.
func Format(e Expr) string {
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

func precedence(e Expr) int {
	switch e := e.(type) {
	case *Conditional:
		return precCond
	case *Binary:
		switch e.Op.Text {
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
	case *Unary:
		return precUnary
	}
	return precMember
}

type formatter struct{ b *strings.Builder }

func (f *formatter) s(s string) { f.b.WriteString(s) }

// expr writes e, in parentheses unless its precedence is at least min.
func (f *formatter) expr(e Expr, min int) {
	if precedence(e) < min {
		f.s("(")
		f.expr(e, precCond)
		f.s(")")
		return
	}
	switch e := e.(type) {
	case *Paren:
		f.s("(")
		f.expr(e.X, precCond)
		f.s(")")
	case *Ident:
		f.ident(e)
	case *IntLit:
		f.s(compact(e.Text))
	case *DoubleLit:
		f.s(compact(e.Text))
	case *UintLit, *StringLit, *BytesLit, *BoolLit, *NullLit:
		f.s(literalText(e))
	case *Select:
		f.expr(e.X, precMember)
		f.s(".")
		if e.Optional {
			f.s("?")
		}
		f.s(e.Field.Text)
	case *Index:
		f.expr(e.X, precMember)
		f.s("[")
		if e.Optional {
			f.s("?")
		}
		f.expr(e.Index, precCond)
		f.s("]")
	case *Call:
		if e.Target != nil {
			f.expr(e.Target, precMember)
			f.s(".")
		}
		f.ident(e.Func)
		f.args(e.Args)
	case *Unary:
		f.s(e.Op.Text)
		if u, ok := e.X.(*Unary); ok && u.Op.Text == e.Op.Text { // a run of the same operator, as written
			f.expr(u, precUnary)
			return
		}
		// An operand that starts with a sign (a negative literal) needs parentheses after a minus: two minus signs
		// are two negations of a positive literal.
		var operand strings.Builder
		(&formatter{&operand}).expr(e.X, precMember)
		if e.Op.Text == "-" && strings.HasPrefix(operand.String(), "-") {
			f.s("(" + operand.String() + ")")
		} else {
			f.s(operand.String())
		}
	case *Binary:
		p := precedence(e)
		f.expr(e.Left, p)
		f.s(" " + e.Op.Text + " ")
		f.expr(e.Right, p+1)
	case *Conditional:
		f.expr(e.Cond, precOr)
		f.s(" ? ")
		f.expr(e.Then, precOr)
		f.s(" : ")
		f.expr(e.Else, precCond)
	case *ListLit:
		f.s("[")
		for i, x := range e.Elems {
			if i > 0 {
				f.s(", ")
			}
			f.expr(x, precCond)
		}
		f.s("]")
	case *MapLit:
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
	case *MessageLit:
		f.ident(e.Type)
		f.s("{")
		for i, fl := range e.Fields {
			if i > 0 {
				f.s(", ")
			}
			if fl.Optional {
				f.s("?")
			}
			f.s(fl.Name.Text + ": ")
			f.expr(fl.Value, precCond)
		}
		f.s("}")
	case *Optional:
		f.s("?")
		f.expr(e.X, precCond)
	}
}

func (f *formatter) ident(id *Ident) {
	if id.Rooted() {
		f.s(".")
	}
	f.s(id.Name())
}

func (f *formatter) args(args []Expr) {
	f.s("(")
	for i, a := range args {
		if i > 0 {
			f.s(", ")
		}
		f.expr(a, precCond)
	}
	f.s(")")
}

func literalText(e Expr) string {
	switch e := e.(type) {
	case *UintLit:
		return e.Text
	case *StringLit:
		return e.Text
	case *BytesLit:
		return e.Text
	case *BoolLit:
		return e.Text
	case *NullLit:
		return e.Text
	}
	return ""
}
