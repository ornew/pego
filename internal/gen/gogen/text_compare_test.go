package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestGeneratedTextFieldComparisons checks that generated parsers give the same results for
// comparisons of string fields with and without budget controls, in the node and typed runtimes,
// and that the typed runtime's unboxed comparisons need the steps of comparisons that box the
// strings: the least step limit that each parse succeeds with is the same.
func TestGeneratedTextFieldComparisons(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	g, err := parseTest(gentest.TextCompareGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("comparefixture")), 0644); err != nil {
		t.Fatal(err)
	}
	inputs := fmt.Sprintf("%#v", gentest.TextCompareInputs)
	pkg := func(convert, boxed bool) string { return fmt.Sprintf("compare_%v_%v", convert, boxed) }
	for _, convert := range []bool{false, true} {
		for _, boxed := range []bool{false, true} {
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg(convert, boxed), Start: "main", Types: true, ConvertTypes: convert, BoxedComparisons: boxed})
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
 for lo<hi {mid:=lo+(hi-lo)/2;if _,err:=f(WithLimits(Limits{Budget:mid}));errors.Is(err,ErrLimit){lo=mid+1}else{hi=mid}}
 return lo
}
func TestComparisons(t *testing.T){
 limits:=WithLimits(Limits{Budget: 1<<40})
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
