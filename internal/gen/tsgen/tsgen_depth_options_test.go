package tsgen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gen/tsgen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedTSDepthOptions(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	fixtures := []gentest.Case{
		{Name: "ordinary", Src: `rule main = trigger n
rule trigger = v1:_ => list($v1)
rule n = "(" n ")" / "é"`, Inputs: []string{"é", "(é)", "((é))", "((((é))))", "?"}},
		{Name: "left", Src: `rule main = n
rule n = n "+" "é" / "(" n ")" / "é"`, Inputs: []string{"é", "é+é+é", "((é))", "?"}},
		{Name: "operatorsRule", Src: `rule main = n
rule n = #operators {
    operand "(" n ")" / "é"
    level { prefix "-" }
    level { infix right "^" }
}`, Inputs: []string{"é", "--é", "é^é^é", "((é))", "?"}},
		{Name: "dispatch", Src: `rule main = "a" one / "b" two / "c" three
rule one = two
rule two = three
rule three = four
rule four = "é"`, Inputs: []string{"aé", "bé", "cé", "a?", "b?", "c?"}},
		{Name: "recursion through helpers", Src: `rule main = n
rule n = "(" n ")" / "a" one / "b" one / "é"
rule one = two
rule two = "é"`, Inputs: []string{"é", "aé", "(aé)", "((bé))", "(((aé)))", "((((é))))", "(a?)", "((b"}},
		{Name: "recovery", Src: `rule main = n
rule n = ("(" n ")" / "é") #recover(skip="?")`, Inputs: []string{"é", "(é)", "((é))", "?", "(?)"}},
	}
	dir := t.TempDir()
	var script strings.Builder
	script.WriteString(`import assert from "node:assert/strict";
`)
	for i, c := range fixtures {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Recognize: true, MaxDepth: 4})
		if err != nil {
			t.Fatal(err)
		}
		prog := mustCompile(t, c.Src)
		if i == 0 {
			code = append(code, []byte(fmt.Sprintf("\nexport function installTestAction(fn: () => void): void { R%d.action = () => {fn(); return null;}; }\n", prog.ByName["trigger"].ID))...)
		}
		path := filepath.Join(dir, fmt.Sprintf("g%d.ts", i))
		if err := os.WriteFile(path, code, 0644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&script, "{ const m = await import('./g%d.ts'); const rows = [\n", i)
		for _, name := range []string{"main", prog.RuleList[len(prog.RuleList)-1].Name} {
			for _, input := range c.Inputs {
				for _, unit := range []engine.PositionUnit{engine.CodePoints, engine.Bytes} {
					for _, limit := range []int{1, 2, 3, 4, 5, 6, 8, 20} {
						_, err := prog.ParseWith(name, input, engine.ParseOptions{Expectations: true, Unit: unit, MaxDepth: limit})
						want := ""
						if err != nil {
							want = err.Error()
						}
						fmt.Fprintf(&script, "{name:%s,input:%s,unit:%d,maxDepth:%d,want:%s},\n", tsgen.TsString(name), tsgen.TsString(input), unit, limit, tsgen.TsString(want))
					}
				}
			}
		}
		script.WriteString(`];
 const message = r => r === null ? "" : r.message;
 for(let repeat=0;repeat<3;repeat++) for(const c of rows) {
  for(const input of [c.input,new Uint8Array(Buffer.from(c.input))]) {
   const options = {unit:c.unit,maxDepth:c.maxDepth};
   assert.equal(message(m.parseRuleWithOptions(c.name,input,options).error),c.want,JSON.stringify(c));
   if(c.name==="main") {
    assert.equal(message(m.parseWithOptions(input,options).error),c.want,JSON.stringify(c));
    assert.equal(message(m.recognizeWithOptions(input,options)),c.want,JSON.stringify(c));
    if(c.maxDepth===4) for(const maxDepth of [undefined,0,4]) {
     assert.equal(message(m.parseWithOptions(input,{unit:c.unit,maxDepth}).error),c.want);
     assert.equal(message(m.recognizeWithOptions(input,{unit:c.unit,maxDepth})),c.want);
     assert.equal(message(m.parse(input,c.unit).error),c.want);
     assert.equal(message(m.recognize(input,c.unit)),c.want);
    }
   }
  }
 }
 for(const depth of [-1,0.5,NaN,Infinity,-Infinity,Number.MAX_SAFE_INTEGER+1]) {
  const options={maxDepth:depth};
  assert.match(m.parseWithOptions("?",options).error.message,/max depth must be a non-negative safe integer/);
  assert.match(m.parseRuleWithOptions("missing","?",options).error.message,/max depth must be a non-negative safe integer/);
  assert.match(m.recognizeWithOptions("?",options).message,/max depth must be a non-negative safe integer/);
 }
 assert.doesNotMatch(message(m.parseWithOptions("?",{maxDepth:Number.MAX_SAFE_INTEGER}).error),/max depth must/);
 const frozen=Object.freeze({unit:m.Bytes,maxDepth:20});m.parseWithOptions("?",frozen);
 let units=0,depths=0;
 m.parseWithOptions("?",{get unit(){units++;return m.Bytes},get maxDepth(){depths++;return 20}});
 assert.equal(units,1);assert.equal(depths,1);
`)
		if i == 0 {
			script.WriteString(`
 const options={unit:m.Bytes,maxDepth:20};let calls=0;
 m.installTestAction(()=>{calls++;options.unit=m.CodePoints;options.maxDepth=1});
 const result=m.parseWithOptions("((((é))))",options);
 assert.equal(result.error,null);assert.equal(result.node.end,10);assert.equal(calls,1);
 m.recognizeWithOptions("é",{maxDepth:20});assert.equal(calls,1);
 assert.match(m.parseWithOptions("é",{maxDepth:-1}).error.message,/max depth must/);assert.equal(calls,1);
 assert.match(m.parseRuleWithOptions("missing","é",{maxDepth:-1}).error.message,/max depth must/);assert.equal(calls,1);
`)
		}
		script.WriteString("}\n")
	}
	path := filepath.Join(dir, "test.mjs")
	os.WriteFile(path, []byte(script.String()), 0644)
	cmd := exec.Command(node, path)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("TypeScript depth options: %v\n%s", err, out)
	}
	if tsc, err := exec.LookPath("tsc"); err == nil {
		cmd := exec.Command(tsc, "--noEmit", "--strict", "--target", "ES2020", "--module", "ESNext", "--skipLibCheck", "g0.ts", "g1.ts", "g2.ts", "g3.ts", "g4.ts")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("TypeScript depth API types: %v\n%s", err, out)
		}
	}
}
