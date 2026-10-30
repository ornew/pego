package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
)

// TestTextFieldComparisons checks that the backends agree on comparisons of string fields, with
// and without budget controls.
func TestTextFieldComparisons(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, gentest.TextCompareGrammar, compile.Options{})
	limits := limits.Limits{Budget: 1 << 40}
	for _, controlled := range []bool{false, true} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range gentest.TextCompareInputs {
				opts := ParseOptions{Backend: Closure, Unit: unit}
				if controlled {
					opts.Limits = limits
				}
				want := gentest.ResultJSON(prog.ParseWith("main", input, opts))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					opts.Backend = backend
					if got := gentest.ResultJSON(prog.ParseWith("main", input, opts)); got != want {
						t.Errorf("controlled=%t %s/%s %q:\n got %s\nwant %s", controlled, backend, unit, input, got, want)
					}
				}
			}
		}
	}
}

// parseSteps parses input as ParseWith does, under budget controls, and returns the steps used and
// the result.
func parseSteps(t *testing.T, prog *Program, backend Backend, unit PositionUnit, input string) (uint64, string) {
	t.Helper()
	o := ParseOptions{Backend: backend, Unit: unit, Limits: limits.Limits{Budget: 1 << 40}}
	active, _, control, err := prog.admitParse("main", o, false, core.InputUnits(input, o.Unit))
	if err != nil {
		t.Fatal(err)
	}
	p := core.AcquirePooledParser(&active.sharedProgram)
	defer p.ReleaseBatchScratch()
	p.Input = core.NewControlledTextInput(input, o.Unit, true, p.Scratch, control)
	p.MaxDepth = core.DepthLimit(o, o.Backend)
	p.DeferMemo = true
	n, err := active.Run(p, o.Backend, "main")
	return control.Used, gentest.ResultJSON(n, err)
}

// textCompareEdgeGrammar compares fields in actions that fail, read the built-in members of a
// node, or nest comparisons and other operations in the operands of a comparison. The comparisons
// the checker rejects are in textCompareRejected.
const textCompareEdgeGrammar = `type Name = { Text string }
type Ops = { Eq bool; Ne bool; Lt bool; Le bool; Gt bool; Ge bool }
rule main = absent / builtin / listed / mixed / nested
rule absent = "?" a:name? "!" => Ops{Eq: $a.(Name).Text == "x"}
rule builtin = "^" a:word => Ops{Eq: $a.Start == 1, Ne: $a.End != 2, Lt: $a.Start < $a.End}
rule listed = "~" a:word => Ops{Lt: text($a) < "m"}
rule mixed = "=" a:name => Ops{Ge: $a.Text >= "b"}
rule nested = "&" a:name "," b:name => Ops{Eq: ($a.Text == "x") == ($b.Text != "y"), Ne: $a.Text != "" && $b.Text != "", Lt: $a.Text < $b.Text + "z", Le: $a.Text <= $b.Text || $a.Text == "", Gt: $a.Text > $b.Text, Ge: $b.Text == $a.Text}
rule word = [a-z]+
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}`

// TestTextFieldComparisonEdges checks that the backends agree on the comparisons of
// textCompareEdgeGrammar, including the errors, with and without budget controls.
func TestTextFieldComparisonEdges(t *testing.T) {
	t.Parallel()
	inputs := []string{"?!", "?a!", "#a", "^ab", "^m", "~ab", "=a", "&x,y", "&a,b", "&b,a", "&ab,ab"}
	prog := compileBare(t, textCompareEdgeGrammar, compile.Options{})
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, input := range inputs {
			// The reference build has no budget controls to compare against.
			var wantResult string
			if core.BudgetControlsEnabled {
				_, wantResult = parseSteps(t, prog, Closure, unit, input)
			}
			wantUncontrolled := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit}))
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				if core.BudgetControlsEnabled {
					if _, gotResult := parseSteps(t, prog, backend, unit, input); gotResult != wantResult {
						t.Errorf("%s/%s %q: %s\nwant %s", backend, unit, input, gotResult, wantResult)
					}
				}
				if got := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit})); got != wantUncontrolled {
					t.Errorf("%s/%s %q uncontrolled:\n got %s\nwant %s", backend, unit, input, got, wantUncontrolled)
				}
			}
		}
	}
}
