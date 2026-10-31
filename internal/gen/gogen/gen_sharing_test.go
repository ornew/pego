package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedUnsharedParsersMatchEngine(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedUnsharedParsersMatchEngine)
}

func testGeneratedUnsharedParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	opts := gen.GenOptions{DisableMethodSharing: true, DisableLiteralSharing: true}
	gentest.GeneratedParsersCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), gentest.GenCorpus(t), opts)
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), gentest.GenCorpus(t), opts)
}
