package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

func (g *generator) tracePlainWrapper(r *compile.Rule, name string) string {
	return g.traceCallWrapper(r, name, true)
}

func (g *generator) traceCallWrapper(r *compile.Rule, name string, evaluated bool) string {
	body := name + "ObservedBody"
	eval := ""
	if evaluated {
		eval = "p.Observation.Evaluated()\n"
	}
	fmt.Fprintf(&g.Methods, `// %[1]s observes one actual direct rule call.
func %[1]s(p *%[2]s) (%[3]s, bool) {
 if p.Observation == nil { return %[4]s(p) }
 p.Observation.Enter(%[5]q, p.Pos, 0)
 h := p.ExaminedBegin()
 %[6]sv, ok := %[4]s(p)
 x, known := p.ExaminedEnd(h)
 p.Observation.Exit(ok, p.Pos, x, known)
 return v, ok
}

`, name, g.recv(), g.valType(), body, r.Name, eval)
	return body
}

// A guard may suppress a whole sequence or a chain of rule references. Report
// only its first identifiable rule call site, without inventing nested calls.
func (g *generator) traceSkip(expr grammar.Expr) string {
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
			return fmt.Sprintf("\np.Observation.Skipped(%q, p.Pos, 0)\n", e.Name)
		default:
			return ""
		}
	}
}
