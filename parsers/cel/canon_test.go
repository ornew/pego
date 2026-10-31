package cel_test

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/zeroparse/parsers/cel"
	"github.com/ornew/zeroparse/parsers/cel/types"
)

// canon prints an expression in the canonical form that internal/refgen prints for the AST of cel-go, so that the
// two can be compared as text. The form is cel-go's AST with the offset of each node's token:
//
//	literals     i:-5@0 u:5@0 d:1.5@0 s:"x"@0 b:"x"@0 true@0 false@0 null@0
//	identifiers  a@0 (a name with a leading dot keeps it: .a@1)
//	selection    (sel@dot X name)
//	calls        (call@lparen name arg...) (mcall@lparen name target arg...)
//	operators    (call@op _+_ x y), a chain of && or || is one call with all its terms, and so is a
//	             conditional (_?_:_), an index (_[_]) and the optional forms (_?._, _[?_])
//	list         (list@[ elem...), an optional element is written ?elem
//	map          (map@{ (entry@colon key value)...) and (?entry@colon key value)
//	message      (msg@{ name (field@colon name value)...) and (?field@colon name value)
//
// Parentheses are not part of cel-go's AST: they are dropped here, and a run of ! or - is reduced to its parity
// (an even number of them is nothing).
type canon struct {
	src []rune
	b   strings.Builder
}

func canonical(src string, e types.Expr) string {
	c := &canon{src: []rune(src)}
	c.expr(e)
	return c.b.String()
}

// tok returns the offset of the first token at or after pos, past whitespace and comments.
func (c *canon) tok(pos int) int {
	for pos < len(c.src) {
		switch c.src[pos] {
		case ' ', '\t', '\r', '\n', '\f':
			pos++
		case '/':
			for pos < len(c.src) && c.src[pos] != '\n' {
				pos++
			}
		default:
			return pos
		}
	}
	return pos
}

func (c *canon) w(format string, args ...any) { fmt.Fprintf(&c.b, format, args...) }

// call writes (call@off fn arg...) from a node's operands.
func (c *canon) call(off int, fn string, args ...types.Expr) {
	c.w("(call@%d %s", off, fn)
	for _, a := range args {
		c.b.WriteByte(' ')
		c.expr(a)
	}
	c.b.WriteByte(')')
}

func (c *canon) identOff(id *types.Ident) int {
	off := id.Start
	if id.Rooted() {
		off = c.tok(off + 1)
	}
	return off
}

func (c *canon) expr(e types.Expr) {
	switch e := e.(type) {
	case *types.Paren:
		c.expr(e.X)
	case *types.Ident:
		name := e.Name()
		if e.Rooted() {
			name = "." + name
		}
		c.w("%s@%d", name, c.identOff(e))
	case *types.IntLit:
		v, err := e.Value()
		if err != nil {
			c.w("ERR(%v)", err)
			return
		}
		c.w("i:%d@%d", v, e.Start)
	case *types.UintLit:
		v, err := e.Value()
		if err != nil {
			c.w("ERR(%v)", err)
			return
		}
		c.w("u:%d@%d", v, e.Start)
	case *types.DoubleLit:
		v, err := e.Value()
		if err != nil {
			c.w("ERR(%v)", err)
			return
		}
		c.w("d:%s@%d", strconv.FormatFloat(v, 'g', -1, 64), e.Start)
	case *types.StringLit:
		c.w("s:%s@%d", strconv.Quote(e.Value()), e.Start)
	case *types.BytesLit:
		c.w("b:%s@%d", strconv.Quote(string(e.Value())), e.Start)
	case *types.BoolLit:
		c.w("%s@%d", e.TNode.Value, e.Start)
	case *types.NullLit:
		c.w("null@%d", e.Start)
	case *types.Select:
		dot := c.tok(cel.SpanOf(e.X).End)
		if e.Optional {
			c.w("(call@%d _?._ ", dot)
			c.expr(e.X)
			c.w(" s:%s@%d)", strconv.Quote(e.Field.Value()), e.Field.Start)
			return
		}
		c.w("(sel@%d ", dot)
		c.expr(e.X)
		c.w(" %s)", e.Field.Value())
	case *types.Index:
		fn := "_[_]"
		if e.Optional {
			fn = "_[?_]"
		}
		c.call(c.tok(cel.SpanOf(e.X).End), fn, e.X, e.Index)
	case *types.Call:
		name := e.Func.Name()
		if e.Func.Rooted() {
			name = "." + name
		}
		lparen := c.tok(e.Func.End)
		if e.Target == nil {
			c.call(lparen, name, e.Args...)
			return
		}
		c.w("(mcall@%d %s ", lparen, name)
		c.expr(e.Target)
		for _, a := range e.Args {
			c.b.WriteByte(' ')
			c.expr(a)
		}
		c.b.WriteByte(')')
	case *types.Unary:
		// A run of the same operator: the parity decides.
		n := 1
		inner := e.X
		for {
			u, ok := inner.(*types.Unary)
			if !ok || u.Op.Value != e.Op.Value {
				break
			}
			n++
			inner = u.X
		}
		if n%2 == 0 {
			c.expr(inner)
			return
		}
		fn := "!_"
		if e.Op.Value == "-" {
			fn = "-_"
		}
		c.call(e.Op.Start, fn, inner)
	case *types.Binary:
		fn := "_" + e.Op.Value + "_"
		if e.Op.Value == "in" {
			fn = "@in"
		}
		if e.Op.Value == "&&" || e.Op.Value == "||" {
			// The terms of a chain: the left operand while it is a Binary of the same operator.
			terms := []types.Expr{e.Right}
			first := e
			for {
				l, ok := first.Left.(*types.Binary)
				if !ok || l.Op.Value != e.Op.Value {
					break
				}
				terms = append(terms, l.Right)
				first = l
			}
			terms = append(terms, first.Left)
			for i, j := 0, len(terms)-1; i < j; i, j = i+1, j-1 {
				terms[i], terms[j] = terms[j], terms[i]
			}
			c.call(first.Op.Start, fn, terms...)
			return
		}
		c.call(e.Op.Start, fn, e.Left, e.Right)
	case *types.Conditional:
		c.call(c.tok(cel.SpanOf(e.Cond).End), "_?_:_", e.Cond, e.Then, e.Else)
	case *types.ListLit:
		c.w("(list@%d", e.Start)
		for _, el := range e.Elems {
			c.b.WriteByte(' ')
			if o, ok := el.(*types.Optional); ok {
				c.b.WriteByte('?')
				el = o.X
			}
			c.expr(el)
		}
		c.b.WriteByte(')')
	case *types.MapLit:
		c.w("(map@%d", e.Start)
		for _, en := range e.Entries {
			opt := ""
			if en.Optional {
				opt = "?"
			}
			c.w(" (%sentry@%d ", opt, c.tok(cel.SpanOf(en.Key).End))
			c.expr(en.Key)
			c.b.WriteByte(' ')
			c.expr(en.Value)
			c.b.WriteByte(')')
		}
		c.b.WriteByte(')')
	case *types.MessageLit:
		name := e.Type.Name()
		if e.Type.Rooted() {
			name = "." + name
		}
		c.w("(msg@%d %s", c.tok(e.Type.End), name)
		for _, f := range e.Fields {
			opt := ""
			if f.Optional {
				opt = "?"
			}
			c.w(" (%sfield@%d %s ", opt, c.tok(f.Name.End), f.Name.Value())
			c.expr(f.Value)
			c.b.WriteByte(')')
		}
		c.b.WriteByte(')')
	case *types.Optional:
		c.w("?")
		c.expr(e.X)
	default:
		c.w("ERR(unexpected %T)", e)
	}
}
