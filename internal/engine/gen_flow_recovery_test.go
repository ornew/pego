package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
)

// The bodies with recovery (#recover) print from Flow IR in the Node and recognizer tables, in the lines of
// Operators Rules and in the stream body of a start rule, as the other framed bodies do (flowFrameExprOK). Flow IR
// lowers a recovery inline, as the typed table does: the expectations of the body are isolated, and when it fails
// the state goes back to the mark, the skip must consume at least one character, and the error is recorded and is
// the value. The Node runtime's value is the error node that RecoverAttr builds (NewErrorNode, which RecoverAttr
// calls as well). No cut flag is saved around the body or the skip, as RecoverAttr saves none, so a cut inside
// them reaches the enclosing scope. The generated
// parsers of the corpus (recovery after a cut, of a leader, nested and with a skip that fails after consuming, with
// inputs that recover, make no progress and fail after a recovered error) run as the engine's, with observation,
// Documents and work controls, and ParseStream delivers the same elements and errors; the tests here assert the
// route.

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

// TestFlowRecoveryPrintsFromFlow checks the route of the rules with recovery: over the corpus and the ten
// grammars, in the ordinary and the work-controlled forms, every rule, Operators Rule line and stream start rule of the
// Node and recognizer tables is printed from Flow IR (otherwise the generation fails), and the rules with a recovery (alone, with a cut and
// with a leader) take the Flow route.
func TestFlowRecoveryPrintsFromFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(append(genCorpus(t), grammarCases(t)...), observeShapes...), streamCorpus...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			taken := map[string]int{}
			framedRouteHook = func(table string, r *staticRule) {
				if !hasRecovery(r.bodyExpr()) {
					return
				}
				taken[table+" recovery"]++
				if hasCut(r.bodyExpr()) {
					taken[table+" recovery with a cut"]++
				}
				if r.leader {
					taken[table+" recovery with a leader"]++
				}
			}
			operatorsRouteHook = func(table string, r *staticRule) { taken[table+" operators line"]++ }
			defer func() { framedRouteHook, operatorsRouteHook = nil, nil }()
			for _, c := range cases {
				g, err := parseTest(c.src)
				if err != nil {
					continue
				}
				for _, stream := range []bool{false, true} {
					if _, err := Generate(g, gen.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: gen.RuntimeImport, Recognize: true, Stream: stream, WorkControls: controls}); err != nil && (!stream || strings.Contains(err.Error(), "Flow IR does not print")) {
						t.Fatalf("%s: %v", c.name, err)
					}
				}
			}
			for _, table := range []string{"rules", "recRules"} {
				for _, k := range []string{" recovery", " recovery with a cut", " recovery with a leader", " operators line"} {
					if taken[table+k] == 0 {
						t.Errorf("no %s%s takes the Flow route (%v)", table, k, taken)
					}
				}
			}
		})
	}
}
