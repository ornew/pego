package parser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ornew/pego/pkg/cst"
	"github.com/ornew/pego/pkg/grammer"
	"github.com/ornew/pego/pkg/grammer/symbol"
	"github.com/ornew/pego/pkg/token"
)

type Extractor struct {
	GetText func(r token.Range) []byte
}

func (e *Extractor) getString(n *cst.Node) (s string) {
	in := e.GetText(n.Range)
	err := json.Unmarshal(in, &s)
	if err != nil {
		panic(fmt.Errorf("%w: %s", err, string(in)))
	}
	return
}

func (e *Extractor) getText(n *cst.Node) string {
	in := e.GetText(n.Range)
	return string(in)
}

func (e *Extractor) getConstant(n *cst.Node) *grammer.Expression {
	switch len(n.Children) {
	case 0:
		return nil
	case 1:
		c := n.Children[0]
		code := c.Code
		switch code {
		case Code_terminal_symbol:
			return &grammer.Expression{
				TerminalSymbol: &symbol.Terminal{
					Text: e.getString(c),
				},
			}
		case Code_terminal_symbol_range:
			return &grammer.Expression{
				TerminalSymbolRange: &symbol.TerminalRange{
					Start: e.getText(c.Children[0]),
					End:   e.getText(c.Children[1]),
				},
			}
		case Code_ident:
			return &grammer.Expression{
				NonTerminalSymbol: &symbol.NonTerminal{
					Name: e.getText(c),
				},
			}
		case Code_term:
			return e.getTerm(c)
		default:
			return nil
		}
	default:
		panic(fmt.Errorf("two or more: %v", n.Children))
	}
}

func (e *Extractor) getGroup(n *cst.Node) *grammer.Expression {
	return &grammer.Expression{
		Group: &grammer.GroupOp{
			Expression: e.getExpression(n.Children[0]),
		},
	}
}

func (e *Extractor) getPrimary(n *cst.Node) *grammer.Expression {
	switch len(n.Children) {
	case 0:
		return nil
	case 1:
		c := n.Children[0]
		code := c.Code
		switch code {
		case Code_group:
			return e.getGroup(c)
		case Code_constant:
			return e.getConstant(c)
		case Code_any_char_op:
			return &grammer.Expression{
				AnyChar: &grammer.AnyCharOp{},
			}
		default:
			panic(fmt.Errorf("invalid primary: %v", n.Children))
		}
	default:
		panic(fmt.Errorf("two or more: %v", n.Children))
	}
}

func (e *Extractor) getFactor(n *cst.Node) *grammer.Expression {
	var o *grammer.Expression
	for _, c := range n.Children {
		switch c.Code {
		case Code_primary:
			o = e.getPrimary(c)
		}
	}
	for _, c := range n.Children {
		switch c.Code {
		case Code_primary_prefix_unary_op:
			op := e.getText(c)
			switch op[0] {
			case '&':
				o = &grammer.Expression{
					AndPredicate: &grammer.AndPredicateOp{
						Expression: o,
					},
				}
			case '!':
				o = &grammer.Expression{
					NotPredicate: &grammer.NotPredicateOp{
						Expression: o,
					},
				}
			default:
				panic(fmt.Errorf("unknown primary prefix unary op: %q", op))
			}
		case Code_primary_postfix_unary_op:
			op := e.getText(c)
			switch op[0] {
			case '*':
				o = &grammer.Expression{
					ZeroOrMore: &grammer.ZeroOrMoreOp{
						Expression: o,
					},
				}
			case '+':
				o = &grammer.Expression{
					OneOrMore: &grammer.OneOrMoreOp{
						Expression: o,
					},
				}
			case '?':
				o = &grammer.Expression{
					Optional: &grammer.OptionalOp{
						Expression: o,
					},
				}
			default:
				panic(fmt.Errorf("unknown primary postfix unary op: %q", op))
			}
		}
	}
	return o
}

func (e *Extractor) getTerm(n *cst.Node) *grammer.Expression {
	l := len(n.Children)
	right := e.getFactor(n.Children[l-1])
	if l == 1 {
		return right
	}
	l = l - 1
	for l >= 2 {
		op := e.getText(n.Children[l-1])
		if len(op) == 0 {
			panic(fmt.Errorf("empty factor binary op: %q", op))
		}
		left := e.getFactor(n.Children[l-2])
		switch op[0] {
		case ' ':
			right = &grammer.Expression{
				Sequence: &grammer.SequenceOp{
					A: left,
					B: right,
				},
			}
		default:
			panic(fmt.Errorf("unknown factor binary op: %q", op))
		}
		l = l - 2
	}
	return right
}

func (e *Extractor) getExpression(n *cst.Node) *grammer.Expression {
	l := len(n.Children)
	right := e.getTerm(n.Children[l-1])
	if l == 1 {
		return right
	}
	l = l - 1
	for l >= 2 {
		op := strings.TrimSpace(e.getText(n.Children[l-1]))
		if len(op) == 0 {
			panic(fmt.Errorf("empty term binary op: %q", op))
		}
		left := e.getTerm(n.Children[l-2])
		switch op {
		case "/":
			right = &grammer.Expression{
				Choice: &grammer.ChoiceOp{
					A: left,
					B: right,
				},
			}
		default:
			panic(fmt.Errorf("unknown term binary op: %q", op))
		}
		l = l - 2
	}
	return right
}

func (e *Extractor) getRule(n *cst.Node) *grammer.Rule {
	return &grammer.Rule{
		Name:       e.getText(n.Children[0]),
		Expression: e.getExpression(n.Children[1]),
	}
}

func (e *Extractor) GetFile(n *cst.Node) *grammer.Grammer {
	var rules []*grammer.Rule
	var pkg string
	for _, c := range n.Children {
		switch c.Code {
		case Code_package_statement:
			pkg = e.getText(c.Children[0])
		case Code_rule_statement:
			rules = append(rules, e.getRule(c))
		default:
			panic(fmt.Errorf("unknown statement: %v", c))
		}
	}
	return &grammer.Grammer{
		Package: pkg,
		Rules:   rules,
	}
}
