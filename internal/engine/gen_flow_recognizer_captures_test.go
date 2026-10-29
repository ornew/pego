package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

// The rules of the recognizer table (and the lean rules of the Node table) that have captures print from Flow IR,
// and so do the framed rules with a repetition that gathers the values of a capture. The recognizer's table
// writes its actions for the values of the unprojected repetition (generator.projections gives none), so its
// Core IR is built without projections (buildCoreProjecting): a Flow-printed recognizer body leaves in its frame
// the captures the actions read. The recognizers run as the engine's (TestGeneratedFlowRecognizerParity and
// TestFlowNodeRecognizersOnGrammars), and the tests here assert the route.

// hasCaptureOfRepeat reports a capture of a repetition in e, the shape that Core IR projects in the Node table.
func hasCaptureOfRepeat(e grammar.Expr) (capture, repeated bool) {
	compile.WalkExpr(e, func(e grammar.Expr) {
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
// ten grammars, in the ordinary and the work-controlled forms, every rule of the Node and recognizer tables is printed from Flow IR (a rule
// it does not print fails the generation), and the rules with captures (and with captured repetitions) take the Flow route.
func TestFlowRecognizerPrintsCapturesFromFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(genCorpus(t), grammarCases(t)...), observeShapes...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			taken := map[string]int{}
			leanRouteHook = func(table string, r *compile.Rule) {
				if c, _ := hasCaptureOfRepeat(r.BodyExpr()); c {
					taken[table+" lean with captures"]++
				}
			}
			framedRouteHook = func(table string, r *compile.Rule) {
				c, rep := hasCaptureOfRepeat(r.BodyExpr())
				if c {
					taken[table+" framed with captures"]++
				}
				if rep {
					taken[table+" framed with a captured repetition"]++
				}
			}
			defer func() { leanRouteHook, framedRouteHook = nil, nil }()
			for _, c := range cases {
				g, err := parseTest(c.src)
				if err != nil {
					continue
				}
				if _, err := Generate(g, gen.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: gen.RuntimeImport, Recognize: true, WorkControls: controls}); err != nil {
					t.Fatalf("%s: %v", c.name, err)
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
