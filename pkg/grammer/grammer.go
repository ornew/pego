package grammer

import (
	"strings"

	"github.com/ornew/pego/pkg/grammer/symbol"
)

type Expression struct {
	TerminalSymbol      *symbol.Terminal      `json:"terminalSymbol,omitempty"`
	TerminalSymbolRange *symbol.TerminalRange `json:"terminalSymbolRange,omitempty"`
	NonTerminalSymbol   *symbol.NonTerminal   `json:"nonTerminalSymbol,omitempty"`
	Sequence            *SequenceOp           `json:"sequence,omitempty"`
	Choice              *ChoiceOp             `json:"choice,omitempty"`
	ZeroOrMore          *ZeroOrMoreOp         `json:"zeroOrMore,omitempty"`
	OneOrMore           *OneOrMoreOp          `json:"oneOrMore,omitempty"`
	Optional            *OptionalOp           `json:"optional,omitempty"`
	AndPredicate        *AndPredicateOp       `json:"andPredicate,omitempty"`
	NotPredicate        *NotPredicateOp       `json:"notPredicate,omitempty"`
	Group               *GroupOp              `json:"group,omitempty"`
	AnyChar             *AnyCharOp            `json:"anyChar,omitempty"`
}

func (e *Expression) String() string {
	if e == nil {
		return "<nil>"
	}
	switch {
	case e.TerminalSymbol != nil:
		return e.TerminalSymbol.String()
	case e.TerminalSymbolRange != nil:
		return e.TerminalSymbolRange.String()
	case e.NonTerminalSymbol != nil:
		return e.NonTerminalSymbol.String()
	case e.Sequence != nil:
		return e.Sequence.String()
	case e.Choice != nil:
		return e.Choice.String()
	case e.ZeroOrMore != nil:
		return e.ZeroOrMore.String()
	case e.OneOrMore != nil:
		return e.OneOrMore.String()
	case e.Optional != nil:
		return e.Optional.String()
	case e.AndPredicate != nil:
		return e.AndPredicate.String()
	case e.NotPredicate != nil:
		return e.NotPredicate.String()
	case e.Group != nil:
		return e.Group.String()
	case e.AnyChar != nil:
		return e.AnyChar.String()
	default:
		return "<invalid>"
	}
}

type SequenceOp struct {
	A *Expression `json:"a"`
	B *Expression `json:"b"`
}

func (o *SequenceOp) String() string {
	return o.A.String() + " " + o.B.String()
}

type ChoiceOp struct {
	A *Expression `json:"a"`
	B *Expression `json:"b"`
}

func (o *ChoiceOp) String() string {
	return o.A.String() + " / " + o.B.String()
}

type ZeroOrMoreOp struct {
	*Expression `json:",inline"`
}

func (o *ZeroOrMoreOp) String() string {
	if o.Sequence != nil || o.Choice != nil {
		return "(" + o.Expression.String() + ")*"
	}
	return o.Expression.String() + "*"
}

type OneOrMoreOp struct {
	*Expression `json:",inline"`
}

func (o *OneOrMoreOp) String() string {
	if o.Sequence != nil || o.Choice != nil {
		return "(" + o.Expression.String() + ")+"
	}
	return o.Expression.String() + "+"
}

type OptionalOp struct {
	*Expression `json:",inline"`
}

func (o *OptionalOp) String() string {
	if o.Sequence != nil || o.Choice != nil {
		return "(" + o.Expression.String() + ")?"
	}
	return o.Expression.String() + "?"
}

type AndPredicateOp struct {
	*Expression `json:",inline"`
}

func (o *AndPredicateOp) String() string {
	return "&" + o.Expression.String()
}

type NotPredicateOp struct {
	*Expression `json:",inline"`
}

func (o *NotPredicateOp) String() string {
	return "!" + o.Expression.String()
}

type GroupOp struct {
	*Expression `json:",inline"`
}

func (o *GroupOp) String() string {
	return "(" + o.Expression.String() + ")"
}

type AnyCharOp struct{}

func (o *AnyCharOp) String() string {
	return "."
}

type Rule struct {
	Name       string      `json:"name"`
	Expression *Expression `json:"expression"`
}

func (o *Rule) String() string {
	return o.Name + " <- " + o.Expression.String()
}

type Grammer struct {
	Package string `json:"package"`
	Rules   []Rule `json:"rules"`
}

func (o *Grammer) String() string {
	var b strings.Builder
	for i, r := range o.Rules {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(r.String())
	}
	return b.String()
}
