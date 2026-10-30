package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestFilterGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testFilterGeneratedGoParity)
}

func testFilterGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.FilterCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.FilterCases(), gen.GenOptions{})
}
