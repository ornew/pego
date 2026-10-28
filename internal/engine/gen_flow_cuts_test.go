package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
)

// The rules whose bodies cut print from Flow IR in the Node and recognizer tables, as the framed bodies of
// the other rules do (flowFrameExprOK). Flow IR lowers a cut as in the typed table: a flag per backtracking
// point, which the function reads from and publishes to the runtime's cut flag at its outermost scope. The
// runtime keeps the frame, memo, depth and finish of the rule, so a cut changes no runtime semantics. Leaders
// and recovery take the Flow route as well (gen_flow_leaders_test.go and gen_flow_recovery_test.go). The
// generated parsers of the corpus run as the engine's, in both forms and with observation, documents and work
// controls (the corpus cases "cuts in every scope" and "cut with captures and a projected repetition" fail near
// and after cuts, so that the errors show whether the alternatives after a cut were tried); the tests here
// assert the route.

// hasCut reports a cut in e.
func hasCut(e grammar.Expr) (found bool) {
	walkExpr(e, func(e grammar.Expr) {
		if _, ok := e.(*grammar.Cut); ok {
			found = true
		}
	})
	return
}

// TestFlowCutsPrintFromFlow checks the route of the rules with cuts: over the corpus and the ten grammars, in
// the ordinary and the work-controlled forms, a rule with a cut takes
// the Flow route in the Node and recognizer tables (a rule Flow IR does not print fails the generation).
func TestFlowCutsPrintFromFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(genCorpus(t), grammarCases(t)...), observeShapes...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			taken := map[string]int{}
			framedRouteHook = func(table string, r *staticRule) {
				if hasCut(r.bodyExpr()) {
					taken[table]++
				}
			}
			defer func() { framedRouteHook = nil }()
			for _, c := range cases {
				g, err := parseTest(c.src)
				if err != nil {
					continue
				}
				if _, err := Generate(g, gen.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: gen.RuntimeImport, Recognize: true, WorkControls: controls}); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
			}
			for _, table := range []string{"rules", "recRules"} {
				if taken[table] == 0 {
					t.Errorf("no rule with a cut takes the Flow route in the %s table (%v)", table, taken)
				}
			}
		})
	}
}
