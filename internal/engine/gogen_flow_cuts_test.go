package engine

import (
	"fmt"
	"github.com/ornew/pego/internal/gentest"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
)

// The rules whose bodies cut print from Flow IR in the Node and recognizer tables, as the framed bodies of
// the other rules do (flowFrameExprOK). Flow IR lowers a cut as in the typed table: a flag per backtracking
// point, which the function reads from and publishes to the runtime's cut flag at its outermost scope. The
// runtime keeps the frame, memo, depth and finish of the rule, so a cut changes no runtime semantics. Leaders
// and recovery take the Flow route as well (gogen_flow_leaders_test.go and gogen_flow_recovery_test.go). The
// generated parsers of the corpus run as the engine's, in both forms and with observation, documents and work
// controls (the corpus cases "cuts in every scope" and "cut with captures and a projected repetition" fail near
// and after cuts, so that the errors show whether the alternatives after a cut were tried); the tests here
// assert the route.

// hasCut reports a cut in e.
func hasCut(e grammar.Expr) (found bool) {
	compile.WalkExpr(e, func(e grammar.Expr) {
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
	t.Parallel()
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(gentest.GenCorpus(t), gentest.GrammarCases(t)...), observeShapes...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			taken := map[string]int{}
			gh := &gogen.Hooks{}
			setGenHooks(t, gh)
			gh.FramedRoute = func(table string, r *compile.Rule) {
				if hasCut(r.BodyExpr()) {
					taken[table]++
				}
			}
			for _, c := range cases {
				g, err := parseTest(c.Src)
				if err != nil {
					continue
				}
				if _, err := Generate(t, g, gen.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: gen.RuntimeImport, Recognize: true, WorkControls: controls}); err != nil {
					t.Fatalf("%s: %v", c.Name, err)
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
