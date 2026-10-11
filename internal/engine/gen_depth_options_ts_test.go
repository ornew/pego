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

func TestGeneratedTSDepthOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	fixtures := []genCase{
		{"ordinary", `def main = trigger n
def trigger = _ -> $0
def n = "(" n ")" / "é"`, []string{"é", "(é)", "((é))", "((((é))))", "?"}},
		{"left", `def main = n
def n = n "+" "é" / "(" n ")" / "é"`, []string{"é", "é+é+é", "((é))", "?"}},
		{"pratt", `def main = n
def n = pratt {
 operand "(" n ")" / "é"
 level { prefix "-" }
 level { infix right "^" }
}`, []string{"é", "--é", "é^é^é", "((é))", "?"}},
		{"dispatch", `def main = "a" one / "b" two / "c" three
def one = two
def two = three
def three = four
def four = "é"`, []string{"aé", "bé", "cé", "a?", "b?", "c?"}},
		{"recovery", `def main = n
def n = ("(" n ")" / "é") #recover(skip="?")`, []string{"é", "(é)", "((é))", "?", "(?)"}},
	}
	dir := t.TempDir()
	var script strings.Builder
	script.WriteString(`import assert from "node:assert/strict";
`)
	for i, c := range fixtures {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatal(err)
		}
		code, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true, MaxDepth: 4})
		if err != nil {
			t.Fatal(err)
		}
		prog := compile(t, c.src)
		if i == 0 {
			code = append(code, []byte(fmt.Sprintf("\nexport function installTestAction(fn: () => void): void { R%d.action = () => {fn(); return null;}; }\n", prog.byName["trigger"].id))...)
		}
		path := filepath.Join(dir, fmt.Sprintf("g%d.ts", i))
		if err := os.WriteFile(path, code, 0644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&script, "{ const m = await import('./g%d.ts'); const rows = [\n", i)
		for _, name := range []string{"main", prog.rules[len(prog.rules)-1].name} {
			for _, input := range c.inputs {
				for _, unit := range []Unit{CodePoints, Bytes} {
					for _, limit := range []int{1, 2, 3, 4, 5, 6, 8, 20} {
						_, err := prog.ParseWith(name, input, ParseOptions{Unit: unit, MaxDepth: limit})
						want := ""
						if err != nil {
							want = err.Error()
						}
						fmt.Fprintf(&script, "{name:%s,input:%s,unit:%d,maxDepth:%d,want:%s},\n", tsString(name), tsString(input), unit, limit, tsString(want))
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
