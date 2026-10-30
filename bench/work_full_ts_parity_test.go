package bench

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/gen"
	"github.com/ornew/pego/internal/syntax"
)

// TestGeneratedTSFullWorkloadParity compares generated TypeScript against its
// inactive route on the full benchmark inputs. Existing Go full-workload tests
// cover the same grammars and fingerprint the Go engine/generated Go outputs.
func TestGeneratedTSFullWorkloadParity(t *testing.T) {
	if testing.Short() {
		t.Skip("generates and runs full-corpus TypeScript consumers")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	for _, workload := range workloads {
		workload := workload
		t.Run(workload.name, func(t *testing.T) {
			source, err := os.ReadFile(workload.grammar)
			if err != nil {
				t.Fatal(err)
			}
			grammar, err := syntax.Parse(string(source))
			if err != nil {
				t.Fatal(err)
			}
			generated, err := gen.TypeScript{Types: true, Recognize: true, WorkControls: true}.Generate(grammar, "main")
			if err != nil {
				t.Fatal(err)
			}
			input := workload.input(1)
			inputs, err := json.Marshal([]struct {
				Name     string `json:"name"`
				Input    string `json:"input"`
				Recovery bool   `json:"recovery"`
			}{{workload.name, input, workload.partial}})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, f := range generated {
				if err := os.WriteFile(filepath.Join(dir, f.Path), f.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for name, data := range map[string][]byte{
				"package.json": []byte(`{"type":"module"}`),
				"input.json":   inputs,
				"consumer.ts":  []byte(tsFullWorkloadConsumer),
				"runner.mjs": []byte(`import { readFileSync } from "node:fs";
globalThis.__pegoFullWorkloads = JSON.parse(readFileSync(new URL("./input.json", import.meta.url), "utf8"));
await import("./consumer.ts");
`),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"--target", "es2022", "--module", "es2022", "--lib", "es2022", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--declaration", "--emitDeclarationOnly", "--outDir", "declarations", "parser.ts", "consumer.ts"}
			cmd := exec.Command(tsc, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("strict TypeScript consumer: %v\n%s", err, out)
			}
			cmd = exec.Command(node, "runner.mjs")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("full-corpus TypeScript parity: %v\n%s", err, out)
			}
		})
	}
}

const tsFullWorkloadConsumer = `import * as p from "./parser.ts";

type Workload={name:string;input:string;recovery:boolean};
const workloads=(globalThis as typeof globalThis & {__pegoFullWorkloads:Workload[]}).__pegoFullWorkloads;
function check(ok:unknown,why:string):asserts ok {if(!ok)throw new Error(why)}
// Flat iterative frames avoid Node.toJSON and the host stringify depth limit.
function stable(value:unknown):string {
  const frames:unknown[]=[];
  const pending:unknown[]=[value];
  const seen=new Map<object,number>();
  while(pending.length!==0){
    const v=pending.pop();
    if(v===null){frames.push("null");continue}
    if(typeof v!=="object"){
      check(["undefined","boolean","string","number","bigint"].includes(typeof v),"unsupported output value");
      frames.push(typeof v,typeof v==="bigint"?v.toString():v??null);continue;
    }
    if(Array.isArray(v)&&v.length===0){frames.push("empty-array");continue}
    const prior=seen.get(v);
    if(prior!==undefined){frames.push("ref",prior);continue}
    const id=seen.size;seen.set(v,id);
    if(v instanceof p.Node){
      frames.push("node",id,v.type,v.rule,v.start,v.end,v.text,v.terminal,v.children.length,v.fields.length);
      for(let i=v.fields.length-1;i>=0;i--){const f=v.fields[i]!;pending.push(f.value,f.name)}
      for(let i=v.children.length-1;i>=0;i--)pending.push(v.children[i]);
    }else if(Array.isArray(v)){
      frames.push("array",id,v.length);
      for(let i=v.length-1;i>=0;i--)pending.push(v[i]);
    }else{
      const fields=v as Record<string,unknown>;
      const keys=Object.keys(fields).sort();frames.push("object",id,keys.length);
      for(let i=keys.length-1;i>=0;i--){const key=keys[i]!;pending.push(fields[key],key)}
    }
  }
  return JSON.stringify(frames);
}
// Negative fixtures keep the oracle sensitive to scalar types, order and sharing.
check(stable({value:1n})!==stable({value:"int:1"}),"bigint differs from formatted string");
check(stable("\udc80")!==stable("\udcff"),"surrogate text remains lossless");
const shared={value:1};
check(stable({left:shared,right:shared})!==stable({left:{value:1},right:{value:1}}),"shared objects differ from copies");
const cycleA:{next?:unknown}={},cycleB:{next?:unknown}={};cycleA.next=cycleA;cycleB.next=cycleB;
check(stable(cycleA)===stable(cycleB),"equivalent cycles are deterministic");
const ordered=new p.Node("Pair","",0,1,"",[],[{name:"a",value:1n},{name:"b",value:2n}],false,false);
const reversed=new p.Node("Pair","",0,1,"",[],[{name:"b",value:2n},{name:"a",value:1n}],false,false);
check(stable(ordered)!==stable(reversed),"public field order remains visible");
function diagnostic(error:Error|null):string|null {
  if(error===null)return null;
  const e=error as Error&{line?:number;col?:number;errors?:readonly {message:string;line:number;col:number}[]};
  return stable({name:e.name,message:e.message,line:e.line,col:e.col,errors:e.errors?.map(x=>({message:x.message,line:x.line,col:x.col}))});
}
function expectOutcome(w:Workload,route:string,value:unknown,error:Error|null):asserts value is {} {
  check(value!==null&&value!==undefined, w.name+"/"+route+" unexpectedly returned null");
  if(w.recovery){
    check(error!==null&&error.name==="SyntaxErrors",w.name+"/"+route+" lost recovered diagnostics");
    const recovered=(error as Error&{errors?:readonly unknown[]}).errors;
    check(recovered!==undefined&&recovered.length>0,w.name+"/"+route+" returned no recovery details");
  }else check(error===null,w.name+"/"+route+" unexpectedly reported diagnostics: "+error?.message);
}
for(const w of workloads)for(const unit of [p.CodePoints,p.Bytes]){
  const plain=p.parse(w.input,unit);
  expectOutcome(w,"Node",plain.node,plain.error);
  check(Number.isInteger(plain.node.start)&&plain.node.start>=0&&plain.node.end>=plain.node.start,w.name+"/Node missing root span");
  const plainTree=stable(plain.node),plainDiag=diagnostic(plain.error);
  const modes=[
    {name:"unlimited-counters",options:{unit,maxWork:Number.MAX_SAFE_INTEGER}},
    {name:"live-signal",options:{unit,signal:{aborted:false}}},
    {name:"combined",options:{unit,signal:{aborted:false},maxWork:Number.MAX_SAFE_INTEGER}},
  ] as const;
  for(const mode of modes){
    const active=p.parseWithOptions(w.input,mode.options);
    expectOutcome(w,"Node/"+mode.name,active.node,active.error);
    check(stable(active.node)===plainTree&&diagnostic(active.error)===plainDiag,w.name+"/Node/"+mode.name+" changed text, span/tree, or diagnostics");
  }
  const typed=p.parseAST(w.input,unit);
  expectOutcome(w,"typed",typed.value,typed.error);
  const typedTree=stable(typed.value),typedDiag=diagnostic(typed.error);
  const typedActive=p.parseASTWithOptions(w.input,{unit,signal:{aborted:false},maxWork:Number.MAX_SAFE_INTEGER});
  expectOutcome(w,"typed/combined",typedActive.value,typedActive.error);
  check(stable(typedActive.value)===typedTree&&diagnostic(typedActive.error)===typedDiag,w.name+"/typed changed serialized output or diagnostics");
  const recognized=p.recognize(w.input,unit);
  const recognizedActive=p.recognizeWithOptions(w.input,{unit,signal:{aborted:false},maxWork:Number.MAX_SAFE_INTEGER});
  check(diagnostic(recognizedActive)===diagnostic(recognized),w.name+"/recognition changed diagnostics");
  if(w.recovery){
    check(recognized!==null&&recognized.name==="SyntaxErrors",w.name+"/recognition lost recovery diagnostics");
    const recovered=(recognized as Error&{errors?:readonly unknown[]}).errors;
    check(recovered!==undefined&&recovered.length>0,w.name+"/recognition returned no recovery details");
  }else check(recognized===null,w.name+"/recognition failed");
}

// Early failures exercise interruption publication and ensure the same parser
// remains usable for a subsequent full-corpus retry.
for(const w of workloads)if(["JSON","Arith_OperatorsRule","Recovery","Outline"].includes(w.name))for(const unit of [p.CodePoints,p.Bytes]){
  for(const kind of ["work-limit"] as const){
    const options={unit,maxWork:1};
    const stopped=p.parseWithOptions(w.input,options);
    check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind===kind,w.name+"/"+unit+" did not interrupt with "+kind);
    const baseline=p.parse(w.input,unit),retry=p.parse(w.input,unit);
    expectOutcome(w,"retry",retry.node,retry.error);
    check(stable(retry.node)===stable(baseline.node)&&diagnostic(retry.error)===diagnostic(baseline.error),w.name+" retry changed the full result");
  }
}
`
