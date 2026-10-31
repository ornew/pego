package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestTopBottomGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testTopBottomGeneratedGoParity)
}

func testTopBottomGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.TopBottomCases(), gen.GenOptions{})
	// A field of type _, like one of type any, keeps the reference runtime.
	gentest.GeneratedTypesCorpus(t, goBin, gentest.TopBottomCases(), gen.GenOptions{ConvertTypes: true}, 2)
}
