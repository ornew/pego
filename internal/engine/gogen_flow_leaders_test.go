package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
)

// The bodies of left-recursion leaders print from Flow IR in the Node and recognizer tables, and in the stream
// body of a start rule that is a leader, as the framed bodies of the other rules do (flowFrameExprOK). The
// runtime owns the growth of a leader's seed (call, growBegin, growStep and growEnd around the body, the memo
// entry that a left-recursive call reads as the seed, the dependency counts of the seed reads, and the merging of
// the expected sets of the iterations); the generated code supplies the body that the runtime evaluates for each
// iteration. A left-recursive alternative has no first set (ruleFirst gives none for a leader), so it is never
// guarded and always reads the seed; the other alternatives are guarded as in any rule. The generated parsers of the corpus (the leader cases
// of genCorpus: memoized callers, lookaheads, nesting, cuts and captures, with inputs that fail after a seed)
// run as the engine's, with observation, Documents (statistics included) and work controls, and ParseStream
// delivers the same elements and errors; the tests here assert the route.

// TestFlowLeadersPrintFromFlow checks that every leader takes the Flow route in the Node and
// recognizer tables, over the corpus and the ten grammars in both forms (a rule Flow IR does not
// print fails the generation).
func TestFlowLeadersPrintFromFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := append(append(genCorpus(t), grammarCases(t)...), observeShapes...)
	for _, controls := range []bool{false, true} {
		t.Run(fmt.Sprint("controls=", controls), func(t *testing.T) {
			taken := map[string]int{}
			gogen.FramedRouteHook = func(table string, r *compile.Rule) {
				if r.Leader {
					taken[table]++
				}
			}
			defer func() { gogen.FramedRouteHook = nil }()
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
					t.Errorf("no leader takes the Flow route in the %s table (%v)", table, taken)
				}
			}
		})
	}
}
