package gen_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedRuleResultChecks(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedRuleResultChecks)
}

func testGeneratedRuleResultChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := gentest.ResultCheckCases()
	gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true})
	if node, err := exec.LookPath("node"); err == nil {
		gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{})
	} else {
		t.Error("Node is required for result-check parity")
	}
}
