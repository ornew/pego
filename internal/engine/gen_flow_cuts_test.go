package engine

import (
	"fmt"
	"sort"
	"testing"

	"github.com/ornew/pego/grammar"
)

// The rules whose bodies cut print from Flow IR in the Node and recognizer tables, as the framed bodies of
// the other rules do (flowFrameExprOK). Flow IR lowers a cut as in the typed table: a flag per backtracking
// point, which the function reads from and publishes to the runtime's cut flag at its outermost scope. The
// runtime keeps the frame, memo, depth and finish of the rule, so a cut changes no runtime semantics. What
// still prints from the direct printer is a left-recursion leader or a body with a recovery. The generated
// parsers of the corpus run as the engine's, in both forms and with observation, documents and work controls
// (the corpus cases "cuts in every scope" and "cut with captures and a projected repetition" fail near and
// after cuts, so that the errors show whether the alternatives after a cut were tried); the tests here
// assert the route.

// hasRecovery reports a recovery in e, read from the expression itself.
func hasRecovery(e grammar.Expr) (found bool) {
	walkExpr(e, func(e grammar.Expr) {
		switch e := e.(type) {
		case *grammar.Recovery:
			found = true
		case *grammar.Attributed:
			for _, a := range e.Attrs {
				found = found || a.Name == "recover"
			}
		}
	})
	return
}

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
// the ordinary and the work-controlled forms, a rule with a cut that is not a leader and has no recovery takes
// the Flow route in the Node and recognizer tables, and the direct printer prints no other rule with a cut.
func TestFlowCutsPrintFromFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(genCorpus(t), grammarCases(t)...), observeShapes...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			taken := map[string]int{}
			var bad []string
			framedRouteHook = func(table string, r *rule) {
				if hasCut(r.bodyExpr()) {
					taken[table]++
					if r.leader || hasRecovery(r.bodyExpr()) {
						bad = append(bad, fmt.Sprintf("%s %s printed from Flow IR but is a leader or recovers", table, r.name))
					}
				}
			}
			directFallbackHook = func(table, route string, r *rule) {
				if hasCut(r.bodyExpr()) && !r.leader && !hasRecovery(r.bodyExpr()) {
					bad = append(bad, fmt.Sprintf("%s %s (%s) cuts without a leader or recovery", table, r.name, route))
				}
			}
			defer func() { framedRouteHook, directFallbackHook = nil, nil }()
			for _, c := range cases {
				g, err := parseTest(c.src)
				if err != nil {
					continue
				}
				if _, err := Generate(g, GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: RuntimeImport, Recognize: true, WorkControls: controls}); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
				if len(bad) > 0 {
					sort.Strings(bad)
					t.Fatalf("%s: %v", c.name, bad)
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
