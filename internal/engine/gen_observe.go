package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

func (g *generator) tracePlainWrapper(r *rule, name string) string {
	return g.traceCallWrapper(r, name, true)
}

func (g *generator) traceCallWrapper(r *rule, name string, evaluated bool) string {
	body := name + "ObservedBody"
	eval := ""
	if evaluated {
		eval = "p.observation.evaluated()\n"
	}
	fmt.Fprintf(&g.methods, `// %s observes one actual direct rule call.
func (p *%s) %s() (%s, bool) {
 if p.observation == nil { return p.%s() }
 p.observation.enter(%q, p.pos, 0)
 h := p.examinedBegin()
 %sv, ok := p.%s()
 x, known := p.examinedEnd(h)
 p.observation.exit(ok, p.pos, x, known)
 return v, ok
}

`, name, g.recv(), name, g.valType(), body, r.name, eval, body)
	return body
}

// A guard may suppress a whole sequence or a chain of rule references. Report
// only its first identifiable rule call site, without inventing nested calls.
func (g *generator) traceSkip(expr grammar.Expr) string {
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
			return fmt.Sprintf("\np.observation.skipped(%q, p.pos, 0)\n", e.Name)
		default:
			return ""
		}
	}
}
