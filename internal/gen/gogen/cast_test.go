package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestCastGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testCastGeneratedGoParity)
}

func testCastGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.CastCases(), gen.GenOptions{})
	gentest.GeneratedParsersCorpus(t, goBin, gentest.CastCases(), gen.GenOptions{SourceInfo: true})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.CastCases(), gen.GenOptions{ConvertTypes: true})
}
