package tsgen_test

import (
	"os/exec"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestCastGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	gentest.TSParsersCorpus(t, node, gentest.CastCases(), gen.GenOptions{})
	gentest.TSParsersCorpus(t, node, gentest.CastCases(), gen.GenOptions{SourceInfo: true})
}
