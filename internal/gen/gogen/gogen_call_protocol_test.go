package gogen_test

import (
	"maps"
	"path/filepath"
	"runtime"
	"slices"
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

// TestCallProtocolBudget checks the budget totals, polls and results of the call protocol cases with
// budget controls against testdata/budget.golden, on the runtime of the run and, in a run without the
// Exec IR switch, on the runtime with the printed compositions too, which must charge and report the
// same: a composition that loses a charge, a growth step or a restore of the parser state changes
// a total or a result.
func TestCallProtocolBudget(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := gentest.CallProtocolCases()
	got, problems := budgetTotals(t, "call protocol", cases, "main", nil)
	for _, p := range problems {
		t.Error(p)
	}
	budgetGoldenFile.Check(t, "call protocol/", got)
	if core.Enabled {
		return
	}
	t.Run("printed runtime", func(t *testing.T) {
		gentest.UseExecRuntime(t)
		printed, problems := budgetTotals(t, "call protocol", cases, "main", nil)
		for _, p := range problems {
			t.Error(p)
		}
		for _, k := range slices.Sorted(maps.Keys(got)) {
			if printed[k] != got[k] {
				t.Errorf("%s: %q on the printed runtime, %q on the hand-written one", k, printed[k], got[k])
			}
		}
	})
}
