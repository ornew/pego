package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
)

// TestTextFieldComparisons checks that the backends agree on comparisons of string fields, with
// and without work controls.
func TestTextFieldComparisons(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, gentest.TextCompareGrammar, compile.Options{})
	limits := limits.WorkLimits{MaxInputWork: 1 << 40, MaxSteps: 1 << 40}
	for _, controlled := range []bool{false, true} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range gentest.TextCompareInputs {
				opts := ParseOptions{Backend: Closure, Unit: unit}
				if controlled {
					opts.WorkLimits = limits
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

// parseSteps parses input as ParseWith does, under work controls, and returns the steps used and
// the result.
func parseSteps(t *testing.T, prog *Program, backend Backend, unit PositionUnit, input string) (uint64, string) {
	t.Helper()
	o := ParseOptions{Backend: backend, Unit: unit, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 40}}
	active, _, control, err := prog.admitParse("main", o, false)
	if err != nil {
		t.Fatal(err)
	}
	p := core.AcquirePooledParser(&active.sharedProgram)
	defer p.ReleaseBatchScratch()
	p.Input = core.NewControlledTextInput(input, o.Unit, true, p.Scratch, control)
	p.MaxDepth = core.DepthLimit(o, o.Backend)
	p.DeferMemo = true
	n, err := active.Run(p, o.Backend, "main")
	return control.StepsUsed, gentest.ResultJSON(n, err)
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

// TestTextFieldComparisonsRejected checks the comparisons the checker rejects.
func TestTextFieldComparisonsRejected(t *testing.T) {
	t.Parallel()
	const prelude = "type Name = { Text string }\ntype Ops = { Eq bool; Ne bool; Lt bool; Le bool; Gt bool; Ge bool }\n" +
		"rule word = [a-z]+\nrule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}\n"
	for _, tc := range []struct{ rule, want string }{
		{`"?" a:name? "!" => Ops{Eq: $a.Text == "x"}`, "cannot access .Text of *Name: the value may be nil"},
		{`"#" a:name => Ops{Eq: $a.Missing == "x"}`, "Name has no field Missing"},
		{`"~" a:word => Ops{Lt: $a.Value < "m"}`, "invalid operands []node[string] and string for <"},
		{`"=" a:name => Ops{Ge: $a.Text >= 1}`, "invalid operands string and int for >="},
		{`"&" a:name b:name => Ops{Gt: $a.Text > text($b)}`, "text: invalid argument"},
	} {
		if got := compileMessage(t, prelude+"rule main = "+tc.rule); !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want an error containing %q", tc.rule, got, tc.want)
		}
	}
}

// TestTextFieldComparisonEdges checks that the backends agree on the comparisons of
// textCompareEdgeGrammar, including the errors, with and without work controls.
func TestTextFieldComparisonEdges(t *testing.T) {
	t.Parallel()
	inputs := []string{"?!", "?a!", "#a", "^ab", "^m", "~ab", "=a", "&x,y", "&a,b", "&b,a", "&ab,ab"}
	prog := compileBare(t, textCompareEdgeGrammar, compile.Options{})
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, input := range inputs {
			_, wantResult := parseSteps(t, prog, Closure, unit, input)
			wantUncontrolled := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit}))
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				if _, gotResult := parseSteps(t, prog, backend, unit, input); gotResult != wantResult {
					t.Errorf("%s/%s %q: %s\nwant %s", backend, unit, input, gotResult, wantResult)
				}
				if got := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit})); got != wantUncontrolled {
					t.Errorf("%s/%s %q uncontrolled:\n got %s\nwant %s", backend, unit, input, got, wantUncontrolled)
				}
			}
		}
	}
}
