package main

import (
	"fmt"
	"strconv"
	"strings"

	"cel.dev/cel-go/common"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/parser"
)

// The canonical form is cel-go's parsed AST as one line of text, with the source offset of each node (the offset of
// the token cel-go records for it), so that it can be compared with the form the tests of the parser print for its own
// AST (canon_test.go in the module):
//
//	literals     i:-5@0 u:5@0 d:1.5@0 s:"x"@0 b:"x"@0 true@0 false@0 null@0
//	identifiers  a@0 (a name with a leading dot keeps it: .a@1)
//	selection    (sel@dot X name)
//	calls        (call@lparen name arg...) (mcall@lparen name target arg...)
//	list         (list@[ elem...), an optional element is written ?elem
//	map          (map@{ (entry@colon key value)...) and (?entry@colon key value)
//	message      (msg@{ name (field@colon name value)...) and (?field@colon name value)
//
// Operators are calls of cel-go's function names (_+_, _?_:_, _[_], _?._, @in, ...), and a chain of && or || is one
// call with all its terms (the parser is configured with EnableVariadicOperatorASTs).

// newParser returns the parser the reference results come from: cel-go's parser with its default limits, the optional
// syntax and the escaped identifiers (both on by default in the conformance tests), no macros, and variadic logical
// operators.
func newParser() *parser.Parser {
	p, err := parser.NewParser(
		parser.EnableOptionalSyntax(true),
		parser.EnableIdentEscapeSyntax(true),
		parser.EnableVariadicOperatorASTs(true),
	)
	if err != nil {
		panic(err)
	}
	return p
}

// reference parses src with cel-go and returns its canonical form, or ERR:column of its first error.
func reference(p *parser.Parser, src string) (out string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			out, ok = "", false
		}
	}()
	a, errs := p.Parse(common.NewTextSource(src))
	if len(errs.GetErrors()) > 0 {
		l := errs.GetErrors()[0].Location
		return fmt.Sprintf("ERR@%d:%d", l.Line(), l.Column()+1), true
	}
	c := &canon{info: a.SourceInfo()}
	c.expr(a.Expr())
	return c.b.String(), true
}

type canon struct {
	info *ast.SourceInfo
	b    strings.Builder
}

func (c *canon) off(id int64) int32 {
	r, _ := c.info.GetOffsetRange(id)
	return r.Start
}

func (c *canon) w(format string, args ...any) { fmt.Fprintf(&c.b, format, args...) }

func (c *canon) expr(e ast.Expr) {
	off := c.off(e.ID())
	switch e.Kind() {
	case ast.LiteralKind:
		switch v := e.AsLiteral().(type) {
		case types.Int:
			c.w("i:%d@%d", int64(v), off)
		case types.Uint:
			c.w("u:%d@%d", uint64(v), off)
		case types.Double:
			c.w("d:%s@%d", strconv.FormatFloat(float64(v), 'g', -1, 64), off)
		case types.String:
			c.w("s:%s@%d", strconv.Quote(string(v)), off)
		case types.Bytes:
			c.w("b:%s@%d", strconv.Quote(string(v)), off)
		case types.Bool:
			c.w("%t@%d", bool(v), off)
		case types.Null:
			c.w("null@%d", off)
		default:
			c.w("ERR(literal %T)", v)
		}
	case ast.IdentKind:
		c.w("%s@%d", e.AsIdent(), off)
	case ast.SelectKind:
		s := e.AsSelect()
		c.w("(sel@%d ", off)
		c.expr(s.Operand())
		c.w(" %s)", s.FieldName())
	case ast.CallKind:
		call := e.AsCall()
		if call.IsMemberFunction() {
			c.w("(mcall@%d %s ", off, call.FunctionName())
			c.expr(call.Target())
		} else {
			c.w("(call@%d %s", off, call.FunctionName())
		}
		for _, a := range call.Args() {
			c.b.WriteByte(' ')
			c.expr(a)
		}
		c.b.WriteByte(')')
	case ast.ListKind:
		l := e.AsList()
		c.w("(list@%d", off)
		for i, el := range l.Elements() {
			c.b.WriteByte(' ')
			if l.IsOptional(int32(i)) {
				c.b.WriteByte('?')
			}
			c.expr(el)
		}
		c.b.WriteByte(')')
	case ast.MapKind:
		c.w("(map@%d", off)
		for _, en := range e.AsMap().Entries() {
			m := en.AsMapEntry()
			opt := ""
			if m.IsOptional() {
				opt = "?"
			}
			c.w(" (%sentry@%d ", opt, c.off(en.ID()))
			c.expr(m.Key())
			c.b.WriteByte(' ')
			c.expr(m.Value())
			c.b.WriteByte(')')
		}
		c.b.WriteByte(')')
	case ast.StructKind:
		s := e.AsStruct()
		c.w("(msg@%d %s", off, s.TypeName())
		for _, f := range s.Fields() {
			sf := f.AsStructField()
			opt := ""
			if sf.IsOptional() {
				opt = "?"
			}
			c.w(" (%sfield@%d %s ", opt, c.off(f.ID()), sf.Name())
			c.expr(sf.Value())
			c.b.WriteByte(')')
		}
		c.b.WriteByte(')')
	default:
		c.w("ERR(kind %d)", e.Kind())
	}
}
