package engine

import (
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/flow"
)

// TestFlowVerifiesCorpus lowers every typed rule of the corpus that Flow IR covers, from the Core
// IR of each mode of the Core comparison, and verifies the functions: the rules that LocalRule
// accepts to local functions, and the bodies of the other rules and the lines of Operators Rules
// to framed ones.
func TestFlowVerifiesCorpus(t *testing.T) {
	for _, mode := range coreGenModes {
		t.Run(mode.name, func(t *testing.T) {
			lowered, framed := 0, 0
			for _, gen := range coreGenerations(t, mode) {
				g := gen.dec.gen
				m, err := g.typedCore(map[core.Node]grammar.Expr{})
				if err != nil {
					t.Fatalf("%s: %v", gen.name, err)
				}
				opts := flow.Options{Work: !g.noWork, Recovers: flow.Recovers(m), ClassTables: g.lean,
					GuardedClasses: g.lean && !g.disableGuardedClasses, InlineLiterals: maxInlineLiteral}
				check := func(f *flow.Func, err error) {
					if err == nil {
						err = flow.Verify(f)
					}
					if err != nil {
						t.Errorf("%s: %v", gen.name, err)
					}
				}
				for _, r := range m.Rules {
					if flow.LocalRule(r) {
						check(flow.Lower(m, r, r.Name, opts))
						lowered++
						continue
					}
					if o, ok := r.Body.(*core.OperatorsRule); ok {
						for _, l := range o.Lines() {
							if flow.FramedBody(l.Body) {
								check(flow.LowerFrame(m, r, l.Body, l.Scope, l.Body.Attrs().Want, r.Name, opts))
								framed++
							}
						}
					} else if flow.FramedBody(r.Body) {
						check(flow.LowerFrame(m, r, r.Body, r.Scope, r.Want, r.Name, opts))
						framed++
					}
				}
			}
			if lowered < 1000 || framed < 100 {
				t.Errorf("lowered %d rules and %d framed bodies", lowered, framed)
			}
		})
	}
}
