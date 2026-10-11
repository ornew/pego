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

// TestGeneratedDepthBenchmarkWorkloads validates benchmarks without timing them.
// PEGO_DEPTH_BENCH_DIR preserves the standalone workloads for serialized,
// paired measurements; generation and compilation remain outside parse timing.
func TestGeneratedDepthBenchmarkWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone benchmark controls")
	}
	dir := os.Getenv("PEGO_DEPTH_BENCH_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path string, b []byte) {
		t.Helper()
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte("module depthbench\n\ngo 1.27.1\n"))
	g, err := syntax.Parse(`type Word terminal
def main: Word = n+
def n: Word = "(" n ")" / "é"`)
	if err != nil {
		t.Fatal(err)
	}
	goSizes := make(map[string]int)
	for _, convert := range []bool{false, true} {
		pkg := "direct"
		if convert {
			pkg = "convert"
		}
		code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, convertTypes: convert})
		if err != nil {
			t.Fatal(err)
		}
		goSizes[pkg] = len(code)
		write(pkg+"/parser.go", code)
		adapter := `func depthParsers(unit Unit, limit *int) depthRoutes {
 if limit != nil { panic("baseline has no invocation override") }
 if unit == Bytes { return depthRoutes{
  node:func(s string)(*Node,error){return Parse(s,Bytes)},
  ast:func(s string)(*Word,error){return ParseAST(s,Bytes)},
  rec:func(s string)error{return Recognize(s,Bytes)},
 } }
 return depthRoutes{
  node:func(s string)(*Node,error){return Parse(s)},
  ast:func(s string)(*Word,error){return ParseAST(s)},
  rec:func(s string)error{return Recognize(s)},
 }
}`
		controls := ""
		hasOptions := strings.Contains(string(code), "type ParseOption ")
		if hasOptions {
			adapter = `func depthParsers(unit Unit, limit *int) depthRoutes {
 if limit==nil {
  if unit==Bytes {return depthRoutes{
   node:func(s string)(*Node,error){return Parse(s,WithUnit(Bytes))},
   ast:func(s string)(*Word,error){return ParseAST(s,WithUnit(Bytes))},
   rec:func(s string)error{return Recognize(s,WithUnit(Bytes))},
  }}
  return depthRoutes{
   node:func(s string)(*Node,error){return Parse(s)},
   ast:func(s string)(*Word,error){return ParseAST(s)},
   rec:func(s string)error{return Recognize(s)},
  }
 }
 opts:=[]ParseOption{WithMaxDepth(*limit)}
 if unit==Bytes {opts=append(opts,WithUnit(Bytes))}
 return depthRoutes{
  node:func(s string)(*Node,error){return Parse(s,opts...)},
  ast:func(s string)(*Word,error){return ParseAST(s,opts...)},
  rec:func(s string)error{return Recognize(s,opts...)},
 }
}`
			controls = `for _,limit:=range []struct{name string;n int}{{"Zero",0},{"Equivalent",100000},{"Override",600000}} {
 for _,input:=range []struct{name,text string}{{"Small",depthInput(8)},{"Large",strings.Repeat(depthInput(8),1024)},{"Deep",depthInput(8192)}} {
  n:=limit.n;cases=append(cases,depthCase{input.name+"/"+limit.name,input.text,&n})
 }
}`
		}
		bench := `package ` + pkg + `
import("strings";"testing")
var depthNode *Node
var depthValue *Word
var depthError error
func depthInput(n int)string{return strings.Repeat("(",n)+"é"+strings.Repeat(")",n)}
type depthRoutes struct{node func(string)(*Node,error);ast func(string)(*Word,error);rec func(string)error}
type depthCase struct{name,input string;limit *int}
` + adapter + `
func depthCases() []depthCase {
 cases:=[]depthCase{{"Small/Default",depthInput(8),nil},{"Large/Default",strings.Repeat(depthInput(8),1024),nil},{"Deep/Default",depthInput(8192),nil}}
` + controls + `
 return cases
}
func TestWorkloads(t *testing.T){
 for _,unit:=range []Unit{CodePoints,Bytes}{for _,c:=range depthCases(){
  routes:=depthParsers(unit,c.limit)
  if _,err:=routes.node(c.input);err!=nil{t.Fatal(err)}
  if _,err:=routes.ast(c.input);err!=nil{t.Fatal(err)}
  if err:=routes.rec(c.input);err!=nil{t.Fatal(err)}
 }}
}
func BenchmarkDepthOptions(b *testing.B){
 for _,unit:=range []Unit{CodePoints,Bytes}{
  unitName:="CodePoints";if unit==Bytes{unitName="Bytes"}
  for _,route:=range []string{"Node","Recognize","AST"}{for _,c:=range depthCases(){
   routes:=depthParsers(unit,c.limit)
   b.Run(unitName+"/"+route+"/"+c.name,func(b *testing.B){
    b.ReportAllocs();b.SetBytes(int64(len(c.input)))
    for b.Loop(){switch route{
     case "Node":depthNode,depthError=routes.node(c.input)
     case "Recognize":depthError=routes.rec(c.input)
     case "AST":depthValue,depthError=routes.ast(c.input)
    };if depthError!=nil{b.Fatal(depthError)}}
   })
  }}
 }
}
`
		// Direct entry-point controls avoid indirect-call adapter specialization.
		directBench := `func BenchmarkDepthDirect(b *testing.B){
for _,unit:=range []Unit{CodePoints,Bytes}{unitName:="CodePoints";if unit==Bytes{unitName="Bytes"}
for _,route:=range []string{"Node","Recognize","AST"}{for _,c:=range depthCases(){if c.limit!=nil{continue};b.Run(unitName+"/"+route+"/"+c.name,func(b *testing.B){b.ReportAllocs();b.SetBytes(int64(len(c.input)))
if unit==CodePoints{for b.Loop(){switch route{case "Node":depthNode,depthError=Parse(c.input);case "Recognize":depthError=Recognize(c.input);case "AST":depthValue,depthError=ParseAST(c.input)};if depthError!=nil{b.Fatal(depthError)}}}else{for b.Loop(){switch route{case "Node":depthNode,depthError=Parse(c.input,WithUnit(Bytes));case "Recognize":depthError=Recognize(c.input,WithUnit(Bytes));case "AST":depthValue,depthError=ParseAST(c.input,WithUnit(Bytes))};if depthError!=nil{b.Fatal(depthError)}}}
})}}}
}
`
		if !hasOptions {
			directBench = strings.ReplaceAll(directBench, "WithUnit(Bytes)", "Bytes")
		}
		bench += directBench
		write(pkg+"/depth_bench_test.go", []byte(bench))
	}
	code, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	write("ts/parser.ts", code)
	script := `import assert from "node:assert/strict";
import { Worker, isMainThread } from "node:worker_threads";
import * as p from "./parser.ts";
if(isMainThread){
 const worker=new Worker(new URL(import.meta.url),{resourceLimits:{stackSizeMb:128}});
 worker.on("error",e=>{throw e});worker.on("exit",code=>{process.exitCode=code});
}else{
 const verify=process.argv.includes("--verify");
 const ordinary="(".repeat(8)+"é"+")".repeat(8),deep="(".repeat(8192)+"é"+")".repeat(8192);
 const large=ordinary.repeat(1024);
 const cases=[
  {name:"Legacy",input:ordinary,options:null},{name:"Default",input:ordinary,options:{}},
  {name:"Zero",input:ordinary,options:{maxDepth:0}},{name:"Equivalent",input:ordinary,options:{maxDepth:100000}},
  {name:"Override",input:ordinary,options:{maxDepth:600000}},
  {name:"LargeLegacy",input:large,options:null},{name:"LargeEquivalent",input:large,options:{maxDepth:100000}},
  {name:"LargeOverride",input:large,options:{maxDepth:600000}},
  {name:"DeepLegacy",input:deep,options:null},{name:"DeepEquivalent",input:deep,options:{maxDepth:100000}},
  {name:"DeepOverride",input:deep,options:{maxDepth:600000}},
 ];
 const hasOptions=typeof p.parseWithOptions === "function";
 for(const unit of [p.CodePoints,p.Bytes])for(const route of ["Node","Recognize"])for(const c of cases){
  if(!hasOptions && c.options!==null)continue;
  const options=c.options===null?null:{...c.options,unit};
  const parse=()=>route==="Node"?(options===null?p.parse(c.input,unit):p.parseWithOptions(c.input,options)).error:
    options===null?p.recognize(c.input,unit):p.recognizeWithOptions(c.input,options);
  assert.equal(parse(),null);
  if(verify)continue;
  const iterations=c.name.startsWith("Deep")?20:c.name.startsWith("Large")?100:20000;
  for(let i=0;i<Math.min(1000,iterations);i++)assert.equal(parse(),null);
  const start=performance.now();for(let i=0;i<iterations;i++)assert.equal(parse(),null);
  console.log(JSON.stringify({unit,route,setting:c.name,iterations,nsPerOp:(performance.now()-start)*1e6/iterations}));
 }
}
`
	write("ts/bench.mjs", []byte(script))
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("depth workload controls: %v\n%s", err, out)
	}
	if node, err := exec.LookPath("node"); err == nil {
		cmd := exec.Command(node, "bench.mjs", "--verify")
		cmd.Dir = filepath.Join(dir, "ts")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("TypeScript depth workload controls: %v\n%s", err, out)
		}
	}
	// Preserve a small manifest for measurements and source-size comparisons.
	manifest := fmt.Sprintf("Generated depth workload controls\nRoutes: direct and conversion Go Node/Recognize/AST; TypeScript legacy/options Node/Recognize\nInputs: small 8 parentheses, large 1024 repeated small inputs, deep 8192 parentheses\nOptions: default, zero, equivalent 100000, override 600000\nGenerated defaults remain 100000.\nGo direct source bytes: %d\nGo conversion source bytes: %d\nTypeScript source bytes: %d\n", goSizes["direct"], goSizes["convert"], len(code))
	write("README.txt", []byte(strings.TrimSpace(manifest)+"\n"))
}
