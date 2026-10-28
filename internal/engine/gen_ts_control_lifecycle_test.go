package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGeneratedTSControlGCTest(t *testing.T, consumer string, hooks string) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated TypeScript reachability consumers")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	g, err := parseTest(`type Text = { Text string }

rule main Text = t:([a]* #atomic) => Text{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	opts := GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}
	code, err := generateTSBundle(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := generateTSBundle(g, opts)
	if err != nil || string(code) != string(again) {
		t.Fatal("generation is not deterministic", err)
	}
	dir := t.TempDir()
	for name, data := range map[string]string{"parser.ts": string(code) + hooks, "consumer.ts": consumer, "package.json": `{"type":"module"}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(tsc, "--target", "es2022", "--module", "es2022", "--lib", "es2022", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--declaration", "--emitDeclarationOnly", "--outDir", "declarations", "parser.ts", "consumer.ts")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict GC consumer: %v\n%s", err, out)
	}
	cmd = exec.Command(node, "--expose-gc", "consumer.ts")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated control reachability: %v\n%s", err, out)
	}
}

const tsControlGCPrelude = `import * as p from "./parser.ts";
` + tsTypedAssert + `
const host=globalThis as typeof globalThis & {gc?:()=>void;setTimeout:(fn:()=>void,delay:number)=>unknown};
check(typeof host.gc==="function","Node forced GC is enabled");
async function turn():Promise<void>{await new Promise<void>(resolve=>host.setTimeout(resolve,0))}
async function collect(refs:readonly WeakRef<object>[],why:string):Promise<void>{
 for(let i=0;i<12;i++){
  // WeakRef.deref pins a live target until this job finishes. Enter a new job
  // before every collection attempt, including after an unsuccessful deref.
  await turn();host.gc!();host.gc!();
  if(refs.every(ref=>ref.deref()===undefined))return;
 }
 throw new Error(why+"; live targets="+refs.map((ref,index)=>ref.deref()===undefined?"":String(index)).filter(x=>x!=="").join(","));
}
const marker={value:"caller reason or thrown value"};
`

func TestGeneratedTSWorkControlPublicOwnersAfterGC(t *testing.T) {
	consumer := tsControlGCPrelude + `
type Mode="success"|"syntax"|"limit"|"input-limit"|"canceled"|"getter-panic"|"pre-canceled"|"pre-getter-panic";
class OwnedSignal implements p.CancellationSignal {
 readonly payload=new Uint8Array(32768);
 readonly reason=marker;
 private polls=0;
 private readonly mode:Mode;
 constructor(mode:Mode){this.mode=mode;this.payload[0]=42}
 get aborted():boolean {
  check(this.payload[0]===42,"live signal payload");
  this.polls++;
  if(this.mode==="pre-getter-panic"||(this.mode==="getter-panic"&&this.polls>=3))throw marker;
  return this.mode==="pre-canceled"||(this.mode==="canceled"&&this.polls>=3);
 }
}
type Result={value:unknown;error:Error|null};
type Route=(input:Uint8Array,options:p.ParseOptions)=>Result;
const routes:readonly Route[]=[
 (input,options)=>{const r=p.parseWithOptions(input,options);return {value:r.node,error:r.error}},
 (input,options)=>{const r=p.parseRuleWithOptions("main",input,options);return {value:r.node,error:r.error}},
 (input,options)=>p.parseASTWithOptions(input,options),
 (input,options)=>({value:null,error:p.recognizeWithOptions(input,options)}),
];
function makeCase(route:Route,unit:p.Unit,mode:Mode,index:number){
 const signal=new OwnedSignal(mode),input=new Uint8Array(2048).fill(mode==="syntax"?98:97);
 const refs:readonly WeakRef<object>[]=[new WeakRef(signal),new WeakRef(signal.payload),new WeakRef(input)];
 let retained:Result={value:null,error:null},threw=false,panic:unknown;
 try {retained=route(input,{unit,signal,maxSteps:mode==="limit"?32:0,maxInputWork:mode==="input-limit"?32:0})}catch(x){threw=true;panic=x}
 if(mode==="getter-panic"||mode==="pre-getter-panic")check(threw&&panic===marker&&retained.value===null,"exact getter panic is rethrown");
 else {
  check(!threw,"ordinary route does not throw");
  if(mode==="success"){
   check(retained.error===null,"successful public parse");
   if(index===3)check(retained.value===null,"recognition intentionally returns no tree");
   else {
    check(retained.value!==null,"successful output is retained during GC");
    // The generic node of a text struct reads its text through field("Text"), the typed value
    // through the property.
    const value=retained.value as {type:string;Text?:string;field?:(name:string)=>unknown;start:number;end:number};
    const text=index===2?value.Text:value.field?.("Text");
    check(value.type==="Text"&&text==="a".repeat(2048)&&value.start===0&&value.end===2048,"successful retained Text shape and span");
   }
  }
  if(mode==="syntax")check(retained.value===null&&retained.error instanceof p.SyntaxError,"ordinary syntax failure");
  if(mode==="limit"||mode==="input-limit")check(retained.value===null&&retained.error instanceof p.ParseInterruptionError&&retained.error.kind===(mode==="limit"?"step-work-limit":"input-work-limit"),"budget failure publishes no partial value");
  if(mode==="canceled"||mode==="pre-canceled")check(retained.value===null&&retained.error instanceof p.ParseInterruptionError&&retained.error.kind==="canceled"&&retained.error.cause===marker,"cancellation retains its reason only");
 }
 // Deliberate strong roots prove the weak references work with these objects.
 return {refs,retained,panic,strong:{signal,input,payload:signal.payload},snapshot:stable(retained.value),message:retained.error?.message};
}
for(const unit of [p.CodePoints,p.Bytes])for(const [index,route] of routes.entries())for(const mode of ["success","syntax","limit","input-limit","canceled","getter-panic","pre-canceled","pre-getter-panic"] as const){
 const held=makeCase(route,unit,mode,index);
 await turn();host.gc!();
 check(held.refs[0]!.deref()===held.strong.signal&&held.refs[1]!.deref()===held.strong.payload&&held.refs[2]!.deref()===held.strong.input,"strong-root GC positive control");
 // Clear only fixture-owned roots. Retain the public output/error/panic token
 // while checking that it has no path back to the signal or caller input.
 const retained={value:held.retained,panic:held.panic,snapshot:held.snapshot,message:held.message};
 const refs=held.refs;
 // Native lazy stacks may retain call frames. Materialize them in caller code
 // to isolate parser-owned references without changing runtime error semantics.
 if(held.retained.error!==null)void held.retained.error.stack;
 Object.assign(held,{strong:null});
 await collect(refs,"public route retains a signal, signal payload or raw input: "+index+"/"+unit+"/"+mode);
 check(stable(retained.value.value)===retained.snapshot&&retained.value.error?.message===retained.message,"retained public output survives owner collection");
 if(mode==="getter-panic"||mode==="pre-getter-panic")check(retained.panic===marker,"retained panic token stays intact");
 const retry=route(new Uint8Array([97,97]),{unit,signal:{aborted:false}});
 check(retry.error===null,"fresh retry after collection");
}
`
	runGeneratedTSControlGCTest(t, consumer, "")
}

// The hook exists only in the generated fixture. Holding the controller makes
// release omissions observable; public calls normally keep no controller root.
const tsControlFinalizerHook = `
export function retainControlForTest(signal:CancellationSignal,mode:string,arm:()=>void,marker:unknown) {
 const options=resolveOptions(G,{signal,maxSteps:mode==="limit"?1:0,maxInputWork:mode==="input-limit"?1:0});
 if(options instanceof Error)throw options;
 const held:{control:WorkControl|null}={control:null};
 const release=WorkControl.prototype.release;
 WorkControl.prototype.release=function(){held.control=this;release.call(this)};
 let retained:unknown=null,threw=false,panic:unknown;
 try {
  retained=withWorkControl<unknown>(options,control=>{
   held.control=control;
   if(control===null)throw new Error("active controller missing");
   arm();
   if(mode==="limit")control.charge(0,2);
   if(mode==="input-limit")control.charge(2,0);
   if(mode==="canceled"||mode==="getter-panic")control.check();
   if(mode==="body-panic")throw marker;
   if(mode==="syntax")return new SyntaxError(0,1,1,["z"],[]);
   return {complete:true};
  },error=>error);
 }catch(x){threw=true;panic=x}finally{WorkControl.prototype.release=release}
 return {options,held,retained,threw,panic};
}
`

func TestGeneratedTSWorkControlFinalizerAfterGC(t *testing.T) {
	consumer := tsControlGCPrelude + `
type Mode="success"|"syntax"|"limit"|"input-limit"|"canceled"|"getter-panic"|"body-panic"|"pre-canceled"|"pre-getter-panic";
function makeCase(mode:Mode){
 let armed=false,polls=0;
 const payload=new Uint8Array(32768);payload[0]=42;
 const signal:p.CancellationSignal={
  get aborted(){
   check(payload[0]===42,"finalizer fixture owns live payload");polls++;
   if(mode==="pre-getter-panic"||(armed&&mode==="getter-panic"))throw marker;
   return mode==="pre-canceled"||(armed&&mode==="canceled");
  },reason:marker,
 };
 const refs:readonly WeakRef<object>[]=[new WeakRef(signal),new WeakRef(payload)];
 const result=p.retainControlForTest(signal,mode,()=>{armed=true},marker);
 check(result.held.control!==null&&result.options.signal===null,"retained controller and cleared resolved options");
 if(mode==="getter-panic"||mode==="body-panic"||mode==="pre-getter-panic")check(result.threw&&result.panic===marker,"finalizer preserves caller panic identity");
 else {
  check(!result.threw,"finalizer route returns normally");
  if(mode==="syntax")check(result.retained instanceof p.SyntaxError,"retained syntax error");
  if(mode==="limit"||mode==="input-limit")check(result.retained instanceof p.ParseInterruptionError&&result.retained.kind===(mode==="limit"?"step-work-limit":"input-work-limit"),"retained work limit");
  if(mode==="canceled"||mode==="pre-canceled")check(result.retained instanceof p.ParseInterruptionError&&result.retained.kind==="canceled"&&result.retained.cause===marker,"retained cancellation reason");
 }
 // Checking the saved controller after teardown must not consult the signal.
 const before=polls;result.held.control.check();check(polls===before,"teardown removes caller getter access");
 return {result,refs,strong:{signal,payload}};
}
for(const mode of ["success","syntax","limit","input-limit","canceled","getter-panic","body-panic","pre-canceled","pre-getter-panic"] as const){
 const held=makeCase(mode);await turn();host.gc!();
 check(held.refs[0]!.deref()===held.strong.signal&&held.refs[1]!.deref()===held.strong.payload,"finalizer strong-root GC positive control");
 const roots=held.result,refs=held.refs,snapshot=stable(held.result.retained);
 // Stack materialization is fixture-owned work, outside parser finalization.
 if(roots.retained instanceof Error)void roots.retained.stack;
 Object.assign(held,{strong:null});
 await collect(refs,"retained controller/options keep their signal after "+mode);
 check(roots.held.control!==null&&roots.options.signal===null&&stable(roots.retained)===snapshot,"controller/options retained independently of released owner");
 if(mode==="canceled"||mode==="pre-canceled")check(roots.retained instanceof p.ParseInterruptionError&&roots.retained.cause===marker,"intentional reason ownership survives GC");
}
`
	runGeneratedTSControlGCTest(t, consumer, tsControlFinalizerHook)
}
