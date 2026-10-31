package gen_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// Generated Go parsers, plain and typed, and generated TypeScript parsers read a scalar operator
// local and the folded value as the engine does.
func TestOperatorsRuleLocalsGenerated(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testOperatorsRuleLocalsGenerated)
}

func testOperatorsRuleLocalsGenerated(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := gentest.OperatorsRuleLocalCases
	t.Run("go", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{}) })
	t.Run("go source info", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{SourceInfo: true}) })
	t.Run("typed go", func(t *testing.T) {
		gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, len(cases))
	})
	t.Run("typescript", func(t *testing.T) {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node not found")
		}
		gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{})
		gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{SourceInfo: true})
	})
}
