package engine

import (
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/flow"
)

// TestFlowVerifiesCorpus lowers every typed rule of the corpus that Flow IR covers, from the Core
// IR of each mode of the Core comparison, and verifies the functions.
func TestFlowVerifiesCorpus(t *testing.T) {
	for _, mode := range coreGenModes {
		t.Run(mode.name, func(t *testing.T) {
			lowered := 0
			for _, gen := range coreGenerations(t, mode) {
				g := gen.dec.gen
				m, err := g.typedCore(map[core.Node]grammar.Expr{})
				if err != nil {
					t.Fatalf("%s: %v", gen.name, err)
				}
				opts := flow.Options{Work: !g.noWork, Recovers: flow.Recovers(m), ClassTables: g.lean,
					GuardedClasses: g.lean && !g.disableGuardedClasses, InlineLiterals: maxInlineLiteral}
				for _, r := range m.Rules {
					if !flow.LocalRule(r) {
						continue
					}
					f, err := flow.Lower(m, r, r.Name, opts)
					if err == nil {
						err = flow.Verify(f)
					}
					if err != nil {
						t.Errorf("%s: %v", gen.name, err)
					}
					lowered++
				}
			}
			if lowered < 1000 {
				t.Errorf("lowered %d rules", lowered)
			}
		})
	}
}
