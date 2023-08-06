// Code generated by pego. DO NOT EDIT.

package calc

import (
	"fmt"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/parser"
)

/*
expr <- term
term <- factor (term_binary_op factor)*
factor <- value (factor_binary_op value)*
term_binary_op <- "+" / "-"
factor_binary_op <- "*" / "/"
value <- number / group
group <- "(" term ")"
number <- [0-9]+
*/

const (
	Code__unknown         uint32 = 0
	Code__root            uint32 = 1
	Code_expr             uint32 = 2
	Code_term             uint32 = 3
	Code_factor           uint32 = 4
	Code_term_binary_op   uint32 = 5
	Code_factor_binary_op uint32 = 6
	Code_value            uint32 = 7
	Code_group            uint32 = 8
	Code_number           uint32 = 9
)

var CodeNames = map[uint32]string{
	Code__unknown:         "<unknown>",
	Code__root:            "<root>",
	Code_expr:             "expr",
	Code_term:             "term",
	Code_factor:           "factor",
	Code_term_binary_op:   "term_binary_op",
	Code_factor_binary_op: "factor_binary_op",
	Code_value:            "value",
	Code_group:            "group",
	Code_number:           "number",
}

func Demangle(code uint32) (string, bool) {
	name, ok := CodeNames[code]
	if !ok {
		return "", false
	}
	return name, true
}

func Parse(p *parser.Parser) (node *cst.Node, err error) {
	p.SetRuleCodeSize(9)
	for {
		top := p.Top()
		if top == nil {
			return p.Head(), nil
		}
		switch top.Code {
		case 1:
			p.Alias(2)
		case 2:
			// term (from term)
			p.Alias(3)
		case 3:
			// factor (term_binary_op factor)*
			p.Sequence(10, 11)
		case 4:
			// value (factor_binary_op value)*
			p.Sequence(15, 16)
		case 5:
			// "+" / "-"
			p.Choice(20, 21)
		case 6:
			// "*" / "/"
			p.Choice(22, 23)
		case 7:
			// number / group
			p.Choice(24, 25)
		case 8:
			// "(" term ")"
			p.Sequence(26, 27)
		case 9:
			// [0-9]+
			p.OneOrMore(30)
		case 10:
			// factor (from factor)
			p.Alias(4)
		case 11:
			// (term_binary_op factor)*
			p.ZeroOrMore(12)
		case 12:
			// term_binary_op factor
			p.Sequence(13, 14)
		case 13:
			// term_binary_op (from term_binary_op)
			p.Alias(5)
		case 14:
			// factor (from factor)
			p.Alias(4)
		case 15:
			// value (from value)
			p.Alias(7)
		case 16:
			// (factor_binary_op value)*
			p.ZeroOrMore(17)
		case 17:
			// factor_binary_op value
			p.Sequence(18, 19)
		case 18:
			// factor_binary_op (from factor_binary_op)
			p.Alias(6)
		case 19:
			// value (from value)
			p.Alias(7)
		case 20:
			p.TerminalChar('+')
		case 21:
			p.TerminalChar('-')
		case 22:
			p.TerminalChar('*')
		case 23:
			p.TerminalChar('/')
		case 24:
			// number (from number)
			p.Alias(9)
		case 25:
			// group (from group)
			p.Alias(8)
		case 26:
			p.TerminalChar('(')
		case 27:
			// term ")"
			p.Sequence(28, 29)
		case 28:
			// term (from term)
			p.Alias(3)
		case 29:
			p.TerminalChar(')')
		case 30:
			p.TerminalRange('0', '9')
		default:
			return nil, fmt.Errorf("unknown code: %d", top.Code)
		}
	}
}