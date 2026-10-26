package engine

import (
	"fmt"
	"sort"
	"testing"

	"github.com/ornew/pego/grammar"
)

// The rules of the recognizer table (and the lean rules of the Node table) that have captures print from Flow IR,
// and so do the framed rules with a repetition that gathers the values of a capture. The recognizer's table
// writes its actions for the values of the unprojected repetition (generator.projections gives none), so its
// Core IR is built without projections (buildCoreProjecting): a Flow-printed recognizer body leaves in its frame
// what the direct printer leaves there. The text of the Flow route equals that of the direct printer
// (TestFlowNodeRulesMatchDirect), the recognizers run as the engine's (TestGeneratedFlowRecognizerParity and
// TestFlowNodeRecognizersOnGrammars), and the tests here assert the route.

// hasCaptureOfRepeat reports a capture of a repetition in e, the shape that Core IR projects in the Node table.
func hasCaptureOfRepeat(e grammar.Expr) (capture, repeated bool) {
	walkExpr(e, func(e grammar.Expr) {
		if c, ok := e.(*grammar.Capture); ok {
			capture = true
			if _, ok := c.Expr.(*grammar.Repeat); ok {
				repeated = true
			}
		}
	})
	return
}

// TestFlowRecognizerPrintsCapturesFromFlow checks the route of the rules with captures: over the corpus and the
// ten grammars, in the ordinary and the work-controlled forms, every rule of the Node and recognizer tables that
// the direct printer still prints is a left-recursion leader or has a recovery, and the rules with
// captures (and with captured repetitions) take the Flow route.
func TestFlowRecognizerPrintsCapturesFromFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(genCorpus(t), grammarCases(t)...), observeShapes...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			taken := map[string]int{}
			var bad []string
			// directRouteHook fires for both printers of a lean rule; directFallback runs before it, so a rule that is not
			// in fellBack took the Flow route.
			fellBack := map[*rule]bool{}
			directRouteHook = func(table string, r *rule) {
				if c, _ := hasCaptureOfRepeat(r.bodyExpr()); c && !fellBack[r] {
					taken[table+" lean with captures"]++
				}
			}
			framedRouteHook = func(table string, r *rule) {
				c, rep := hasCaptureOfRepeat(r.bodyExpr())
				if c {
					taken[table+" framed with captures"]++
				}
				if rep {
					taken[table+" framed with a captured repetition"]++
				}
			}
			directFallbackHook = func(table, route string, r *rule) {
				fellBack[r] = true
				if !r.leader && !hasRecovery(r.bodyExpr()) {
					bad = append(bad, fmt.Sprintf("%s %s (%s)", table, r.name, route))
				}
			}
			defer func() { directRouteHook, framedRouteHook, directFallbackHook = nil, nil, nil }()
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
					t.Fatalf("%s: rules without a leader or recovery that the direct printer prints: %v", c.name, bad)
				}
			}
			for _, k := range []string{"recRules lean with captures", "recRules framed with captures", "recRules framed with a captured repetition", "rules framed with a captured repetition"} {
				if taken[k] == 0 {
					t.Errorf("no rule takes the Flow route as %q (%v)", k, taken)
				}
			}
		})
	}
}
