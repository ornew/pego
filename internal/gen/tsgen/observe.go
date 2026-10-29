package tsgen

import (
	"fmt"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

func (g *Generator) tracePlainCall(r *compile.Rule, name string) string {
	if !g.Observability {
		return name
	}
	body := name + "ObservedBody"
	fmt.Fprintf(&g.Methods, `// Observes one actual generated rule call.
function %s(p: Parser): R {
  const o = p.observation;
  if (o === null) return %s(p);
  o.enter(%s, p.pos, 0);
  o.evaluated();
  const v = %s(p);
  o.exit(v !== undefined, p.pos);
  return v;
}

`, name, body, TsString(r.Name), body)
	return body
}

func (g *Generator) traceSkipped(expr grammar.Expr) string {
	if !g.Observability {
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
				for i, l := range g.Prog.ByName[e.Name].Def.Expr.(*grammar.OperatorsRule).Levels {
					if l.Name == e.Level {
						min = i
					}
				}
			}
			return fmt.Sprintf("    p.observation?.skipped(%s, p.pos, %d);\n", TsString(e.Name), min)
		default:
			return ""
		}
	}
}
