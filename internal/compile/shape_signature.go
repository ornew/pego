package compile

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
)

// ShapeSignature is ShapeSignature.
func (p *Program) ShapeSignature(name string, poll func()) (string, bool) {
	if p == nil || p.Typed == nil {
		return "", false
	}
	r := p.ByName[name]
	if r == nil || r.Def == nil {
		return "", false
	}
	step := func() {
		if poll != nil {
			poll()
		}
	}
	// The subexpressions of lookaheads are left out.
	inside := map[grammar.Expr]bool{}
	WalkExpr(r.Def.Expr, func(e grammar.Expr) {
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
			WalkExpr(body, func(x grammar.Expr) { step(); inside[x] = true })
		}
	})
	var b strings.Builder
	WalkExpr(r.Def.Expr, func(e grammar.Expr) {
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
				fmt.Fprintf(&b, "<%s %v %v %v>", c.Name, c.Paths, c.Side, c.maybe)
				if c.T != nil {
					b.WriteString(c.T.String())
				}
			}
			b.WriteByte(';')
		}
		b.WriteString("}")
	})
	return b.String(), true
}
