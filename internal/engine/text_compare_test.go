package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// textCompareGrammar compares string fields of structs with each other, with literals and in
// predicates.
const textCompareGrammar = `type Name = { Text string }
type Tag = { Text string; N int }
type Ops = { Eq bool; Ne bool; Lt bool; Le bool; Gt bool; Ge bool }
type Out = { Items node[_] }
rule main = items:(-" "* item)* $$ => Out{Items: $items}
rule item = cmp / mixed / pred
rule cmp = a:name "," b:name => Ops{Eq: $a.Text == $b.Text, Ne: $a.Text != $b.Text, Lt: $a.Text < $b.Text, Le: $a.Text <= $b.Text, Gt: $a.Text > $b.Text, Ge: $a.Text >= $b.Text}
rule mixed = "%" a:name ":" t:tag => Ops{Eq: $a.Text == $t.Text, Ne: $t.Text != "x", Lt: "m" < $a.Text, Le: $a.Text <= "m", Gt: $t.Text > $a.Text, Ge: $t.N >= 1}
rule pred = "?" a:name {$a.Text != "no"} {"a" <= $a.Text} => $a
rule tag = "<" t:([a-z]+ #atomic) ">" => Tag{Text: text($t), N: 1}
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}`

var textCompareInputs = []string{"", "a,b", "b,a", "ab,ab", "%ab:<ab>", "%z:<a>", "%a:<x>", "?no", "?yes", "a,b %c:<d> ?x", "?", "a,"}

// TestTextFieldComparisons checks that the backends agree on comparisons of string fields, with
// and without work controls.
func TestTextFieldComparisons(t *testing.T) {
	prog := compileBare(t, textCompareGrammar, Options{NoTypeCheck: true})
	limits := WorkLimits{MaxInputWork: 1 << 40, MaxSteps: 1 << 40}
	for _, controlled := range []bool{false, true} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range textCompareInputs {
				opts := ParseOptions{Backend: Closure, Unit: unit}
				if controlled {
					opts.WorkLimits = limits
				}
				want := resultJSON(prog.ParseWith("main", input, opts))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					opts.Backend = backend
					if got := resultJSON(prog.ParseWith("main", input, opts)); got != want {
						t.Errorf("controlled=%t %s/%s %q:\n got %s\nwant %s", controlled, backend, unit, input, got, want)
					}
				}
			}
		}
	}
}

// TestGeneratedTextFieldComparisons checks that generated parsers give the same results for
// comparisons of string fields with and without work controls, in the node and typed runtimes,
// and that the typed runtime's unboxed comparisons need the steps of comparisons that box the
// strings: the least step limit that each parse succeeds with is the same.
func TestGeneratedTextFieldComparisons(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	g, err := syntax.Parse(textCompareGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("comparefixture")), 0644); err != nil {
		t.Fatal(err)
	}
	inputs := fmt.Sprintf("%#v", textCompareInputs)
	pkg := func(convert, boxed bool) string { return fmt.Sprintf("compare_%v_%v", convert, boxed) }
	for _, convert := range []bool{false, true} {
		for _, boxed := range []bool{false, true} {
			code, err := generateCode(g, GenOptions{Package: pkg(convert, boxed), Start: "main", Types: true, convertTypes: convert, boxedComparisons: boxed})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, pkg(convert, boxed))
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			consumer := strings.NewReplacer("PACKAGE", pkg(convert, boxed), "INPUTS", inputs).Replace(generatedTextCompareConsumer)
			for name, data := range map[string][]byte{"parser.go": code, "compare_test.go": []byte(consumer)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated text comparisons: %v\n%s", err, out)
	}
	for _, convert := range []bool{false, true} {
		got, err := os.ReadFile(filepath.Join(dir, pkg(convert, false), "steps.txt"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(dir, pkg(convert, true), "steps.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("convert=%t: steps\n%s\nwant\n%s", convert, got, want)
		}
	}
}

const generatedTextCompareConsumer = `package PACKAGE
import("encoding/json";"errors";"fmt";"os";"strings";"testing")
func show(v any, err error) string {b,_:=json.Marshal(v);return fmt.Sprintf("%s %v",b,err)}
func least(f func(...ParseOption)(any,error)) uint64 {
 lo,hi:=uint64(1),uint64(1<<20)
 for lo<hi {mid:=lo+(hi-lo)/2;if _,err:=f(WithWorkLimits(WorkLimits{MaxSteps:mid}));errors.Is(err,ErrWorkLimit){lo=mid+1}else{hi=mid}}
 return lo
}
func TestComparisons(t *testing.T){
 limits:=WithWorkLimits(WorkLimits{MaxSteps:1<<40,MaxInputWork:1<<40})
 var steps strings.Builder
 for _,in:=range INPUTS{
  for _,unit:=range []PositionUnit{CodePoints,Bytes}{
   if got,want:=show(Parse(in,WithUnit(unit),limits)),show(Parse(in,WithUnit(unit)));got!=want{t.Errorf("Parse %q: %s, want %s",in,got,want)}
   if got,want:=show(ParseAST(in,WithUnit(unit),limits)),show(ParseAST(in,WithUnit(unit)));got!=want{t.Errorf("ParseAST %q: %s, want %s",in,got,want)}
   node:=least(func(o ...ParseOption)(any,error){return Parse(in,append(o,WithUnit(unit))...)})
   ast:=least(func(o ...ParseOption)(any,error){return ParseAST(in,append(o,WithUnit(unit))...)})
   fmt.Fprintf(&steps,"%q %d: %d %d\n",in,unit,node,ast)
  }
 }
 if err:=os.WriteFile("steps.txt",[]byte(steps.String()),0644);err!=nil{t.Fatal(err)}
}
`

// parseSteps parses input as ParseWith does, under work controls, and returns the steps used and
// the result.
func parseSteps(t *testing.T, prog *Program, backend Backend, unit PositionUnit, input string) (uint64, string) {
	t.Helper()
	o := ParseOptions{Backend: backend, Unit: unit, WorkLimits: WorkLimits{MaxSteps: 1 << 40}}
	active, _, control, err := prog.admitParse("main", o, false)
	if err != nil {
		t.Fatal(err)
	}
	p := acquirePooledParser(active)
	defer p.releaseBatchScratch()
	p.input = newControlledTextInput(input, o.Unit, true, p.scratch, control)
	p.maxDepth = o.maxDepth(o.Backend)
	p.deferMemo = true
	n, err := active.run(p, o.Backend, "main")
	return control.stepsUsed, resultJSON(n, err)
}

// textCompareEdgeGrammar compares fields in actions that fail, read the built-in members of a
// node, or nest comparisons and other operations in the operands of a comparison.
const textCompareEdgeGrammar = `type Name = { Text string }
type Ops = { Eq bool; Ne bool; Lt bool; Le bool; Gt bool; Ge bool }
rule main = absent / undeclared / builtin / listed / mixed / nested
rule absent = "?" a:name? "!" => Ops{Eq: $a.Text == "x"}
rule undeclared = "#" a:name => Ops{Eq: $a.Missing == "x"}
rule builtin = "^" a:word => Ops{Eq: $a.Start == 1, Ne: $a.End != 2, Lt: $a.Start < $a.End}
rule listed = "~" a:word => Ops{Lt: $a.Value < "m"}
rule mixed = "=" a:name => Ops{Ge: $a.Text >= 1}
rule nested = "&" a:name "," b:name => Ops{Eq: ($a.Text == "x") == ($b.Text != "y"), Ne: $a.Text != "" && $b.Text != "", Lt: $a.Text < $b.Text + "z", Le: $a.Text <= $b.Text || $a.Text == "", Gt: $a.Text > text($b), Ge: $b.Text == $a.Text}
rule word = [a-z]+
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}`

// TestTextFieldComparisonEdges checks that the backends agree on the comparisons of
// textCompareEdgeGrammar, including the errors, with and without work controls.
func TestTextFieldComparisonEdges(t *testing.T) {
	inputs := []string{"?!", "?a!", "#a", "^ab", "^m", "~ab", "=a", "&x,y", "&a,b", "&b,a", "&ab,ab"}
	prog := compileBare(t, textCompareEdgeGrammar, Options{NoTypeCheck: true})
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, input := range inputs {
			_, wantResult := parseSteps(t, prog, Closure, unit, input)
			wantUncontrolled := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit}))
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				if _, gotResult := parseSteps(t, prog, backend, unit, input); gotResult != wantResult {
					t.Errorf("%s/%s %q: %s\nwant %s", backend, unit, input, gotResult, wantResult)
				}
				if got := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit})); got != wantUncontrolled {
					t.Errorf("%s/%s %q uncontrolled:\n got %s\nwant %s", backend, unit, input, got, wantUncontrolled)
				}
			}
		}
	}
}
