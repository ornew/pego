package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

func (g *tsGen) tracePlainCall(r *staticRule, name string) string {
	if !g.observability {
		return name
	}
	body := name + "ObservedBody"
	fmt.Fprintf(&g.methods, `// Observes one actual generated rule call.
function %s(p: Parser): R {
  const o = p.observation;
  if (o === null) return %s(p);
  o.enter(%s, p.pos, 0);
  o.evaluated();
  const v = %s(p);
  o.exit(v !== undefined, p.pos);
  return v;
}

`, name, body, tsString(r.name), body)
	return body
}

func (g *tsGen) traceSkipped(expr grammar.Expr) string {
	if !g.observability {
		return ""
	}
	for {
		switch e := expr.(type) {
		case *grammar.Seq:
			if len(e.Items) == 0 {
				return ""
			}
			expr = e.Items[0]
		case *grammar.Capture:
			expr = e.Expr
		case *grammar.Atomic:
			expr = e.Expr
		case *grammar.Discard:
			expr = e.Expr
		case *grammar.Ref:
			min := 0
			if e.Level != "" {
				for i, l := range g.prog.byName[e.Name].def.Expr.(*grammar.OperatorsRule).Levels {
					if l.Name == e.Level {
						min = i
					}
				}
			}
			return fmt.Sprintf("    p.observation?.skipped(%s, p.pos, %d);\n", tsString(e.Name), min)
		default:
			return ""
		}
	}
}
