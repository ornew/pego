package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestValueResultsGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testValueResultsGeneratedGoParity)
}

func testValueResultsGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.ValueResultCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.ValueResultCases(), gen.GenOptions{ConvertTypes: true})
}
