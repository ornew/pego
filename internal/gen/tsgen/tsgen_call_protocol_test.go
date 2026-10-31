package tsgen_test

import (
	"os/exec"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// TestGeneratedTSCallProtocolMatchesEngine compares the results and errors of TypeScript parsers
// whose rules reuse memo entries in lookaheads, attributes, recovery and growth with the engine's.
func TestGeneratedTSCallProtocolMatchesEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	gentest.TSParsersCorpus(t, node, gentest.CallProtocolCases(), gen.GenOptions{})
}
