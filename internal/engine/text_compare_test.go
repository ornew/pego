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

// textCompareGrammar compares the text of text structs with each other, with string fields of
// other structs, with literals and in predicates.
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

// TestTextFieldComparisons checks that comparisons reading the unboxed text of a text struct give
// the results of comparisons reading the field list of the struct the action makes, on every
// backend, with and without work controls.
func TestTextFieldComparisons(t *testing.T) {
	ref := compileBare(t, textCompareGrammar, Options{NoTypeCheck: true, noTextStructs: true})
	prog := compileBare(t, textCompareGrammar, Options{NoTypeCheck: true})
	limits := WorkLimits{MaxInputWork: 1 << 40, MaxSteps: 1 << 40}
	for _, controlled := range []bool{false, true} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range textCompareInputs {
				opts := ParseOptions{Backend: Closure, Unit: unit}
				if controlled {
					opts.WorkLimits = limits
				}
				want := resultJSON(ref.ParseWith("main", input, opts))
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					opts.Backend = backend
					if got := resultJSON(prog.ParseWith("main", input, opts)); got != want {
						t.Errorf("controlled=%t %s/%s %q:\n got %s\nwant %s", controlled, backend, unit, input, got, want)
					}
				}
			}
		}
	}
}

// TestTextFieldComparisonWork checks that the unboxed reads and comparisons charge the work of the
// reads and comparisons they replace.
func TestTextFieldComparisonWork(t *testing.T) {
	limits := WorkLimits{MaxInputWork: 1 << 40, MaxSteps: 1 << 40}
	for _, name := range []string{"Text", "Tex", "Texts", "Name", "text"} {
		p := actionWorkParser(nil, limits, Bytes)
		q := actionWorkParser(nil, limits, Bytes)
		got := p.textFieldName(name)
		_, want := q.valueField(Fields{{Name: "Text", Value: "abc"}}, name)
		if got != want || p.control.stepsUsed != q.control.stepsUsed || p.control.inputUsed != q.control.inputUsed {
			t.Errorf("textFieldName(%q) = %t with %d steps, want %t with %d steps", name, got, p.control.stepsUsed, want, q.control.stepsUsed)
		}
	}
	long := string(make([]byte, 3*valueWorkChunk))
	pairs := [][2]string{{"", ""}, {"a", "b"}, {"b", "a"}, {"ab", "abc"}, {long, long}, {long, long + "x"}, {long + "a", long + "b"}}
	for _, op := range []string{"==", "!=", "<", "<=", ">", ">="} {
		for _, pair := range pairs {
			p := actionWorkParser(nil, limits, Bytes)
			q := actionWorkParser(nil, limits, Bytes)
			got, ok := p.compareStrings(op, pair[0], pair[1])
			want, err := q.valueBinary(op, pair[0], pair[1])
			if !ok || err != nil || got != want || p.control.stepsUsed != q.control.stepsUsed || p.control.inputUsed != q.control.inputUsed {
				t.Errorf("compareStrings(%q, %d bytes, %d bytes) = %t with %d steps, want %v with %d steps", op, len(pair[0]), len(pair[1]), got, p.control.stepsUsed, want, q.control.stepsUsed)
			}
		}
	}
	// A comparison of a text with a value of another type is left to valueBinary.
	p := actionWorkParser(nil, limits, Bytes)
	if v, err := p.compareOperands("==", operand{str: "1", isStr: true}, operand{v: 1}); err != nil || v != false {
		t.Errorf("text == 1: %v, %v", v, err)
	}
	if _, err := p.compareOperands("<", operand{str: "1", isStr: true}, operand{v: 1}); err == nil {
		t.Error("text < 1: no error")
	}
	if _, ok := p.compareStrings("+", "a", "b"); ok {
		t.Error("compareStrings accepted +")
	}
}

// TestGeneratedTextFieldComparisons checks that generated parsers give the same results for
// comparisons of the text of text structs with and without work controls, in the node and typed
// runtimes, and that they need the steps of comparisons that box the text: the least step limit
// that each parse succeeds with is the same.
func TestGeneratedTextFieldComparisons(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	g, err := syntax.Parse(textCompareGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module comparefixture\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	inputs := fmt.Sprintf("%#v", textCompareInputs)
	pkg := func(convert, boxed bool) string { return fmt.Sprintf("compare_%v_%v", convert, boxed) }
	for _, convert := range []bool{false, true} {
		for _, boxed := range []bool{false, true} {
			code, err := Generate(g, GenOptions{Package: pkg(convert, boxed), Start: "main", Types: true, convertTypes: convert, boxedComparisons: boxed})
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

// TestTextFieldComparisonSteps checks that, on every backend, comparisons reading the text of
// text structs unboxed take the steps of comparisons reading it as any other field.
func TestTextFieldComparisonSteps(t *testing.T) {
	prog := compileBare(t, textCompareGrammar, Options{NoTypeCheck: true})
	boxed := compileBare(t, textCompareGrammar, Options{NoTypeCheck: true, boxedComparisons: true})
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, input := range textCompareInputs {
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				got, gotErr := parseSteps(t, prog, backend, unit, input)
				want, wantErr := parseSteps(t, boxed, backend, unit, input)
				if got != want || gotErr != wantErr {
					t.Errorf("%s/%s %q: %d steps (%s), want %d (%s)", backend, unit, input, got, gotErr, want, wantErr)
				}
			}
		}
	}
}

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

// TestTextFieldComparisonEdges checks the comparisons of textCompareEdgeGrammar, including the
// errors, against the comparisons that box the text, with and without work controls.
func TestTextFieldComparisonEdges(t *testing.T) {
	inputs := []string{"?!", "?a!", "#a", "^ab", "^m", "~ab", "=a", "&x,y", "&a,b", "&b,a", "&ab,ab"}
	for _, noTextStructs := range []bool{false, true} {
		prog := compileBare(t, textCompareEdgeGrammar, Options{NoTypeCheck: true, noTextStructs: noTextStructs})
		boxed := compileBare(t, textCompareEdgeGrammar, Options{NoTypeCheck: true, noTextStructs: noTextStructs, boxedComparisons: true})
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range inputs {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					got, gotResult := parseSteps(t, prog, backend, unit, input)
					want, wantResult := parseSteps(t, boxed, backend, unit, input)
					if got != want || gotResult != wantResult {
						t.Errorf("noTextStructs=%t %s/%s %q: %d steps %s\nwant %d steps %s", noTextStructs, backend, unit, input, got, gotResult, want, wantResult)
					}
					opts := ParseOptions{Backend: backend, Unit: unit}
					if got, want := resultJSON(prog.ParseWith("main", input, opts)), resultJSON(boxed.ParseWith("main", input, opts)); got != want {
						t.Errorf("noTextStructs=%t %s/%s %q uncontrolled:\n got %s\nwant %s", noTextStructs, backend, unit, input, got, want)
					}
				}
			}
		}
	}
}

// TestTextComparisonInstructions checks the flag of EMEMBER and EBIN that keeps a text unboxed:
// the compiler sets it on the operands of a comparison, a module keeps it through a round trip,
// the disassembly shows it and a module with another value is rejected.
func TestTextComparisonInstructions(t *testing.T) {
	prog := compile(t, textCompareGrammar)
	module := prog.Module()
	flagged := map[Op]int{}
	for _, in := range module.Exprs {
		if (in.Op == EMember || in.Op == EBin) && in.B == 1 {
			flagged[in.Op]++
		}
	}
	if flagged[EMember] == 0 || flagged[EBin] == 0 {
		t.Fatalf("flagged instructions %v", flagged)
	}
	if !strings.Contains(module.Disassemble(), " text") {
		t.Error("disassembly does not show the flag")
	}
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range textCompareInputs {
		opts := ParseOptions{Backend: Bytecode}
		if got, want := resultJSON(loaded.ParseWith("main", input, opts)), resultJSON(prog.ParseWith("main", input, opts)); got != want {
			t.Errorf("loaded %q: %s, want %s", input, got, want)
		}
	}
	for _, op := range []Op{EMember, EBin} {
		m := prog.Module()
		for i := range m.Exprs {
			if m.Exprs[i].Op == op {
				m.Exprs[i].B = 2
				break
			}
		}
		if err := validateModule(m); err == nil {
			t.Errorf("%v with B 2 accepted", op)
		}
	}
}
