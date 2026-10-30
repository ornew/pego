package gogen_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/ir/exec/core"
)

// TestGeneratedCallProtocolMatchesEngine compares the results and errors of node and typed parsers
// whose rules reuse memo entries in lookaheads, attributes, recovery and growth with the engine's.
func TestGeneratedCallProtocolMatchesEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	gentest.PinOrdinary(t)
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.CallProtocolCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.CallProtocolCases(), gen.GenOptions{})
}

// TestGeneratedCallProtocolExecRuntime compares, as TestGeneratedCallProtocolMatchesEngine does, the
// parsers built on the runtime with the printed compositions, in a run without the Exec IR switch
// (with the switch, TestGeneratedCallProtocolMatchesEngine builds them so already).
func TestGeneratedCallProtocolExecRuntime(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	if core.Enabled {
		t.Skip("TestGeneratedCallProtocolMatchesEngine builds on the printed runtime")
	}
	gentest.PinOrdinary(t)
	gentest.UseExecRuntime(t)
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, gentest.CallProtocolCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, gentest.CallProtocolCases(), gen.GenOptions{})
}
