package gogen_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestGeneratedTypedLeaderValues compares the values of typed parsers with the engine's for
// leaders whose values are not nodes, in both position units and on the reference route of
// Node conversion. TestGeneratedTypes covers the cases again within its corpus.
func TestGeneratedTypedLeaderValues(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedLeaderValues)
}

func testGeneratedTypedLeaderValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedLeaderValueCases, gen.GenOptions{ConvertTypes: true}, len(gentest.TypedLeaderValueCases))
}
