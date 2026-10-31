package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestCaptureAccessGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testCaptureAccessGeneratedGoParity)
}

func testCaptureAccessGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.CaptureAccessCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.CaptureAccessCases(), gen.GenOptions{})
}
