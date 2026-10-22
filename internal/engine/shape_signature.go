package engine

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
)

// ShapeSignature returns a text that describes the elements and captures that every subexpression
// of the rule name contributes to the tuple of its scope, in preorder, leaving out the lookaheads
// and what they hold. The type of a rule hides a change inside an action, such as a discard that
// moved to another element, but the shapes of the subexpressions show it. It reports false if
// the program has no such rule or its types were not checked. poll, when not nil, is called at
// every subexpression.
func (p *Program) ShapeSignature(name string, poll func()) (string, bool) {
	if p == nil || p.typed == nil {
		return "", false
	}
	r := p.byName[name]
	if r == nil || r.def == nil {
		return "", false
	}
	step := func() {
		if poll != nil {
			poll()
		}
	}
	// The subexpressions of lookaheads are left out.
	inside := map[grammar.Expr]bool{}
	walkExpr(r.def.Expr, func(e grammar.Expr) {
		step()
		var body grammar.Expr
		switch x := e.(type) {
		case *grammar.And:
			body = x.Expr
		case *grammar.Not:
			body = x.Expr
		}
		if body != nil {
			inside[e] = true
			walkExpr(body, func(x grammar.Expr) { step(); inside[x] = true })
		}
	})
	var b strings.Builder
	walkExpr(r.def.Expr, func(e grammar.Expr) {
		step()
		if inside[e] {
			return
		}
		fmt.Fprintf(&b, "%T{", e)
		for _, el := range shapeElems(p, e) {
			step()
			if el.t != nil {
				b.WriteString(el.t.String())
			}
			for _, c := range el.caps {
				fmt.Fprintf(&b, "<%s %v %v %v>", c.name, c.paths, c.side, c.maybe)
				if c.t != nil {
					b.WriteString(c.t.String())
				}
			}
			b.WriteByte(';')
		}
		b.WriteString("}")
	})
	return b.String(), true
}
