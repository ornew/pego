package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestGeneratedCallProtocolMatchesEngine compares the results and errors of node and typed parsers
// whose rules reuse memo entries in lookaheads, attributes, recovery and growth with the engine's.
func TestGeneratedCallProtocolMatchesEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	gentest.PinOrdinary(t)
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.CallProtocolCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.CallProtocolCases(), gen.GenOptions{})
}
