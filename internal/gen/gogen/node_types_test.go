package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestNodeTypesGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testNodeTypesGeneratedGoParity)
}

func testNodeTypesGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.NodeTypeCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.NodeTypeCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.NodeTypeCases(), gen.GenOptions{ConvertTypes: true})
}
