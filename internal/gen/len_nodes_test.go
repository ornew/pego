package gen_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestGeneratedLenOfNodes checks that generated Go parsers, untyped and typed, and generated
// TypeScript parsers give the engine's len of nodes and strings, in both units.
func TestGeneratedLenOfNodes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedLenOfNodes)
}

func testGeneratedLenOfNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	var cases []gentest.Case
	for _, c := range gentest.GenCorpus(t) {
		if c.Name == "len of nodes and strings" {
			cases = append(cases, c)
		}
	}
	if len(cases) != 1 {
		t.Fatalf("%d cases", len(cases))
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, 1)
	if node, err := exec.LookPath("node"); err == nil {
		gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{})
	}
}
