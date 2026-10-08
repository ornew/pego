package cel

import (
	"fmt"
	"unicode/utf8"
)

// Limits are limits on an expression, which a CEL implementation is free to choose (the specification only requires
// that a few levels of nesting and 32 repetitions work). A limit of zero or less is not checked.
type Limits struct {
	// MaxDepth is the depth of recursion that the parser of cel-go allows; see CheckLimits for how it is measured.
	MaxDepth int
	// MaxSize is the size of an expression in code points.
	MaxSize int
}

// DefaultLimits are the defaults of cel-go's parser: a depth of 250 and 100,000 code points.
var DefaultLimits = Limits{MaxDepth: 250, MaxSize: 100_000}

// CheckLimits reports whether the expression is within the limits, measuring its depth as cel-go does.
//
// The parser of cel-go (an ANTLR parser, with a visitor that builds the AST) rejects an expression whose recursion
// is too deep in two ways: while parsing, when more than MaxDepth contexts of one grammar rule are open at once, and
// while building the AST, when more than MaxDepth nested conditionals, binary operators, selections, receiver calls
// and index operations are visited. Nested expressions in parentheses, lists, maps, messages, calls and
// indexes count against the first; a chain of selections or operators, which ANTLR builds without recursion,
// against the second; and a run of ! or - against neither. CheckLimits computes both from the AST.
func CheckLimits(e Expr, l Limits) error {
	if l.MaxDepth <= 0 {
		return nil
	}
	d := &depth{max: l.MaxDepth}
	d.ex(e)
	if d.failed {
		return &CheckError{Span: SpanOf(e), Message: fmt.Sprintf("expression recursion limit exceeded: %d", l.MaxDepth)}
	}
	if v := visitDepth(e); v > l.MaxDepth {
		return &CheckError{Span: SpanOf(e), Message: "max recursion depth exceeded"}
	}
	return nil
}

// The rules of the ANTLR grammar of cel-go that nest.
const (
	rExpr = iota
	rOr
	rAnd
	rRel
	rCalc
	rUnary
	rMember
	rPrimary
	rExprList
	rListInit
	rOptExpr
	rFieldInits
	rMapInits
	nRules
)

// depth models the contexts of each rule of the grammar that are open while ANTLR parses the expression: each method
// is a rule, and enters and leaves the contexts that the parse enters. The AST says which rules a parse used, since
// it is the grammar's own structure.
type depth struct {
	max    int
	open   [nRules]int
	stack  []Expr
	failed bool
}

func (d *depth) enter(r int) {
	d.open[r]++
	if d.open[r] > d.max {
		d.failed = true
	}
}

func (d *depth) leave(r int) { d.open[r]-- }

// terms returns the operands of a left-nested chain of operators of one precedence level, which the operator test
// recognizes: a single operand if x is not such a Binary. They are on a stack of the depth, which the caller
// pops with release when it is done with them.
func (d *depth) terms(x Expr, isOp func(string) bool) []Expr {
	base := len(d.stack)
	d.collect(x, isOp)
	return d.stack[base:len(d.stack):len(d.stack)]
}

func (d *depth) collect(x Expr, isOp func(string) bool) {
	if b, ok := x.(*Binary); ok && isOp(b.Op.Text) {
		d.collect(b.Left, isOp)
		d.stack = append(d.stack, b.Right)
		return
	}
	d.stack = append(d.stack, x)
}

func (d *depth) release(ts []Expr) { d.stack = d.stack[:len(d.stack)-len(ts)] }

func isOr(op string) bool  { return op == "||" }
func isAnd(op string) bool { return op == "&&" }
func isRel(op string) bool {
	switch op {
	case "<", "<=", ">", ">=", "==", "!=", "in":
		return true
	}
	return false
}
func isAdd(op string) bool { return op == "+" || op == "-" }
func isMul(op string) bool { return op == "*" || op == "/" || op == "%" }

// ex is the rule expr: conditionalOr ('?' conditionalOr ':' expr)?
func (d *depth) ex(x Expr) {
	if d.failed {
		return
	}
	d.enter(rExpr)
	if c, ok := x.(*Conditional); ok {
		d.or(c.Cond)
		d.or(c.Then)
		d.ex(c.Else)
	} else {
		d.or(x)
	}
	d.leave(rExpr)
}

// or is conditionalOr: conditionalAnd ('||' conditionalAnd)*
func (d *depth) or(x Expr) {
	d.enter(rOr)
	ts := d.terms(x, isOr)
	for _, t := range ts {
		d.and(t)
	}
	d.release(ts)
	d.leave(rOr)
}

// and is conditionalAnd: relation ('&&' relation)*
func (d *depth) and(x Expr) {
	d.enter(rAnd)
	ts := d.terms(x, isAnd)
	for _, t := range ts {
		d.rel(t)
	}
	d.release(ts)
	d.leave(rAnd)
}

// rel is relation: calc | relation relop relation, which ANTLR turns into relation[0] (calc (relop relation[2])*): the
// first operand is a calc, and so is each of the others, in a relation of its own.
func (d *depth) rel(x Expr) {
	d.enter(rRel)
	ts := d.terms(x, isRel)
	for i, t := range ts {
		if i == 0 {
			d.calc(t)
			continue
		}
		d.enter(rRel)
		d.calc(t)
		d.leave(rRel)
	}
	d.release(ts)
	d.leave(rRel)
}

// calc is calc: unary | calc ('*'|'/'|'%') calc | calc ('+'|'-') calc, which ANTLR turns into calc[0] where the
// operand of + and - is calc[2] (which takes * on its own) and that of * is calc[3] (a unary).
func (d *depth) calc(x Expr) {
	d.enter(rCalc)
	ts := d.terms(x, isAdd)
	for i, t := range ts {
		if i == 0 {
			d.mul(t)
			continue
		}
		d.enter(rCalc)
		d.mul(t)
		d.leave(rCalc)
	}
	d.release(ts)
	d.leave(rCalc)
}

func (d *depth) mul(x Expr) {
	ts := d.terms(x, isMul)
	for i, t := range ts {
		if i == 0 {
			d.unary(t)
			continue
		}
		d.enter(rCalc)
		d.unary(t)
		d.leave(rCalc)
	}
	d.release(ts)
}

// unary is the rule unary: member | '!'+ member | '-'+ member
func (d *depth) unary(x Expr) {
	d.enter(rUnary)
	for {
		u, ok := x.(*Unary)
		if !ok {
			break
		}
		x = u.X
	}
	d.member(x)
	d.leave(rUnary)
}

// member is member: primary | member '.' name | member '.' name '(' exprList? ')' | member '[' expr ']'. It is
// left-recursive, so a chain of them stays one context deep.
func (d *depth) member(x Expr) {
	d.enter(rMember)
	d.suffixes(x)
	d.leave(rMember)
}

func (d *depth) suffixes(x Expr) {
	switch x := x.(type) {
	case *Select:
		d.suffixes(x.X)
	case *Index:
		d.suffixes(x.X)
		d.ex(x.Index)
	case *Call:
		if x.Target != nil {
			d.suffixes(x.Target)
			d.args(x.Args)
			return
		}
		d.primary(x)
	default:
		d.primary(x)
	}
}

func (d *depth) args(args []Expr) {
	if len(args) == 0 {
		return
	}
	d.enter(rExprList)
	for _, a := range args {
		d.ex(a)
	}
	d.leave(rExprList)
}

func (d *depth) primary(x Expr) {
	d.enter(rPrimary)
	switch x := x.(type) {
	case *Paren:
		d.ex(x.X)
	case *Call: // a call of a function
		d.args(x.Args)
	case *ListLit:
		if len(x.Elems) > 0 {
			d.enter(rListInit)
			for _, e := range x.Elems {
				if o, ok := e.(*Optional); ok {
					e = o.X
				}
				d.enter(rOptExpr)
				d.ex(e)
				d.leave(rOptExpr)
			}
			d.leave(rListInit)
		}
	case *MapLit:
		if len(x.Entries) > 0 {
			d.enter(rMapInits)
			for _, en := range x.Entries {
				d.enter(rOptExpr)
				d.ex(en.Key)
				d.leave(rOptExpr)
				d.ex(en.Value)
			}
			d.leave(rMapInits)
		}
	case *MessageLit:
		if len(x.Fields) > 0 {
			d.enter(rFieldInits)
			for _, f := range x.Fields {
				d.ex(f.Value)
			}
			d.leave(rFieldInits)
		}
	}
	d.leave(rPrimary)
}

// visitDepth returns the depth of nested conditionals, binary operators other than && and ||, selections, receiver
// calls and index operations: the nodes whose visit counts against the limit when cel-go builds the AST.
func visitDepth(e Expr) int {
	max := 0
	var walk func(e Expr, n int)
	walk = func(e Expr, n int) {
		switch e := e.(type) {
		case *Conditional, *Select, *Index:
			n++
		case *Binary:
			if e.Op.Text != "&&" && e.Op.Text != "||" {
				n++
			}
		case *Call:
			if e.Target != nil {
				n++
			}
		}
		if n > max {
			max = n
		}
		Children(e, func(c Expr) { walk(c, n) })
	}
	walk(e, 0)
	return max
}

// checkSize reports whether the input is within the size limit.
func checkSize(input string, l Limits) error {
	if l.MaxSize <= 0 {
		return nil
	}
	if n := utf8.RuneCountInString(input); n > l.MaxSize {
		return &CheckError{Line: 1, Col: 1, Message: fmt.Sprintf("expression code point size exceeds limit: size: %d, limit %d", n, l.MaxSize)}
	}
	return nil
}
