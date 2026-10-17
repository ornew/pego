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

func TestGeneratedTSProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("runs standalone TypeScript profile consumers")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	var files []string
	var harness strings.Builder
	for _, f := range []struct{ name, grammar, input, checks string }{
		{"plain", "type W terminal\nrule main W = part part\nrule part W = \"a\"", "aa", `check(s.entered===3&&s.calls===3&&s.evals===3&&s.skipped===0,"exact plain totals");check(s.rules.find(r=>r.rule==="part")!.consumed===2,"exact consumption");`},
		{"dispatch", "type W terminal\nrule main W = first / second\nrule first W = \"a\"\nrule second W = \"b\"", "b", `check(s.skipped===1&&s.entered===2,"exact dispatch totals");`},
		{"memo", "type W terminal\nrule main W = &part &part &part part\nrule part W = letter\nrule letter W = \"a\"", "a", `check(s.memoHits>0,"memo counters");`},
		{"leftrec", "type W terminal\nrule main W = expr\nrule expr W = expr \"a\" / \"a\"", "aaa", `check(s.evals>s.calls-s.memoHits,"growth counters");`},
		{"repeat", "type W terminal\nrule main W = (&part \"x\" / &part) p:part => $p\nrule part W = [a-z]", "b", `check(s.rules.some(r=>r.repeats>0),"repeated position evaluations");`},
		{"operators", "type W terminal\ntype B = { Left Expr; Right Expr }\ntype Expr ~ W | B\nrule main Expr = l:level(expr, product) \"+\" r:expr => B{Left:$l,Right:$r}\nrule expr Expr = operators {operand num;level sum {infix left \"+\" => B{Left:$lhs,Right:$rhs}};level product {infix left \"*\" => B{Left:$lhs,Right:$rhs}}}\nrule num W = [0-9]+", "1+2*3", ``},
		{"recover", "type W terminal\nrule main W = (first / second) #recover(skip=\"!\")\nrule first W = \"a\"\nrule second W = \"b\"", "!", `check(s.skipped===2,"recovery skips");`},
		{"unicode", "type W terminal\nrule main W = part part\nrule part W = \"é\"", "éé", `check(s.rules.find(r=>r.rule==="main")!.consumed===(unit===p.Bytes?4:2),"position units");`},
		{"nilvalue", "type W terminal\nrule main *W = part => nil\nrule part W = \"a\"", "a", ``},
		{"collision", "type Profile terminal\ntype ProfileSnapshot terminal\ntype RuleProfile terminal\ntype ProfileLocation terminal\ntype SkipReasonCount terminal\ntype ProfileObservation terminal\ntype ProfileFrame terminal\ntype ProfileData terminal\ntype WeakMap terminal\nrule main Profile = part\nrule part Profile = \"a\"", "a", ``},
	} {
		g, err := syntax.Parse(f.grammar)
		if err != nil {
			t.Fatal(f.name, err)
		}
		for _, typed := range []bool{false, true} {
			name := f.name
			if typed {
				name += "_typed"
			}
			options := GenOptions{Start: "main", Types: typed, Recognize: true}
			reference, err := GenerateTS(g, options)
			if err != nil {
				t.Fatal(name, err)
			}
			options.Observability = true
			code, err := GenerateTS(g, options)
			if err != nil {
				t.Fatal(name, err)
			}
			again, err := GenerateTS(g, options)
			if err != nil || string(again) != string(code) {
				t.Fatal(name, "unstable source", err)
			}
			if strings.Count(string(code), "function resolveOptions(") != 1 {
				t.Fatal("option probe anchor changed")
			}
			write(name+".ts", strings.Replace(string(code), "function resolveOptions(", "function resolveOptionsBody(", 1)+tsTraceTestHooks+tsProfileTestHooks)
			write(name+"_reference.ts", string(reference))
			typedRoute := ""
			if typed {
				typedRoute = `routes.push((input,o)=>{const a=p.parseASTWithOptions(input,o),b=reference.parseASTWithOptions(input,o);check(stable(a.value)===stable(b.value),"typed value parity");return {value:a.value,error:a.error,referenceError:b.error}});`
			}
			consumer := strings.NewReplacer("PARSER", name, "INPUT", fmt.Sprintf("%q", f.input), "CHECKS", f.checks, "TYPEDROUTE", typedRoute, "PLAIN", fmt.Sprint(f.name == "plain")).Replace(tsProfileConsumer)
			write(name+"_consumer.ts", consumer)
			files = append(files, name+".ts", name+"_reference.ts", name+"_consumer.ts")
			fmt.Fprintf(&harness, "await import(%q);\n", "./"+name+"_consumer.ts")
		}
	}
	write("harness.mjs", harness.String())
	args := []string{"--target", "es2020", "--lib", "es2020", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--declaration", "--emitDeclarationOnly", "--outDir", "declarations"}
	cmd := exec.Command(tsc, append(args, files...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict profile consumers: %v\n%s", err, out)
	}
	cmd = exec.Command(node, "harness.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("profile consumers: %v\n%s", err, out)
	}
}

const tsProfileTestHooks = `
const profileProbes:ProfileObservation[]=[];
const profileOptions:ResolvedParseOptions[]=[];
const originalProfileCall=Parser.prototype.call;
Parser.prototype.call=function(r:Rule,min:number):R {
 if(this.observation instanceof ProfileObservation)profileProbes.push(this.observation);
 return originalProfileCall.call(this,r,min);
};
function resolveOptions(options?:ParseOptions):ResolvedParseOptions|Error {
 const resolved=resolveOptionsBody(options);
 if(!(resolved instanceof Error))profileOptions.push(resolved);
 return resolved;
}
export function testProfileReleased():boolean {
 const clean=profileProbes.every(o=>{const x=o as unknown as {data:unknown;clock:unknown;profileScope:unknown;frames:unknown[];seen:Map<unknown,unknown>;open:Map<unknown,unknown>};return x.data===null&&x.clock===null&&x.profileScope===null&&x.frames.length===0&&x.seen.size===0&&x.open.size===0})&&profileOptions.every(o=>o.profile===null&&o.profileClock===null);
 profileProbes.length=0;profileOptions.length=0;return clean&&testReleased();
}
`

const tsProfileConsumer = `import * as p from "./PARSER.ts";
import * as reference from "./PARSER_reference.ts";
` + tsTypedAssert + `
function errorView(e:Error|null):unknown{return e===null?null:{name:e.name,message:e.message,data:JSON.parse(stable(e))}}
function counts(s:p.ProfileSnapshot):unknown{return {...s,time:0,rules:s.rules.map(r=>({...r,time:0,selfTime:0}))}}
function checkProfile(s:p.ProfileSnapshot,events:readonly p.TraceEvent[]):void{
 const exits=events.filter(e=>e.kind==="exit"),entries=events.filter(e=>e.kind==="enter"),skips=events.filter(e=>e.kind==="skipped");
 check(s.parses===1&&s.entered===entries.length&&s.calls===exits.length&&s.evals===exits.reduce((a,e)=>a+e.evals,0)&&s.memoHits===exits.filter(e=>e.memo).length&&s.skipped===skips.length,"event totals");
 check(Number.isFinite(s.time)&&s.time>=0,"root time");
 check(stable(s.rules.map(r=>r.rule))===stable(Array.from(new Set(events.map(e=>e.rule))).sort()),"sorted complete rows");
 for(const r of s.rules){
  const calls=exits.filter(e=>e.rule===r.rule),seen=new Map<string,number>();let repeats=0,max=0,at={pos:0,level:0};
  for(const e of calls)if(!e.memo&&e.evals>0){const key=e.pos+":"+e.level,n=(seen.get(key)??0)+1;seen.set(key,n);if(n>1)repeats++;if(n>max){max=n;at={pos:e.pos,level:e.level}}}
  check(r.entered===entries.filter(e=>e.rule===r.rule).length&&r.calls===calls.length&&r.evals===calls.reduce((a,e)=>a+e.evals,0)&&r.memoHits===calls.filter(e=>e.memo).length,"row counters");
  check(r.matched===calls.filter(e=>e.matched).length&&r.failed===calls.filter(e=>!e.matched).length&&r.consumed===calls.reduce((a,e)=>a+(e.matched?e.end-e.pos:0),0),"outcome counters");
  check(r.skipped===skips.filter(e=>e.rule===r.rule).length&&r.skipReasons.reduce((a,e)=>a+e.count,0)===r.skipped,"skip counters");
  check(r.repeats===repeats&&r.maxEvals===max&&stable(r.maxEvalsAt)===stable(at),"position-level history");
  check(!r.examinedKnown&&!r.wastedKnown&&r.examined===0&&r.wasted===0,"unknown metrics");
  check(Number.isFinite(r.time)&&Number.isFinite(r.selfTime)&&r.selfTime>=0&&r.time>=r.selfTime,"row timing");
 }
}
type Result={value:unknown;error:Error|null;referenceError:Error|null};
type Route=(input:string|Uint8Array,o:p.ParseOptions)=>Result;
const routes:Route[]=[
 (input,o)=>{const a=p.parseWithOptions(input,o),b=reference.parseWithOptions(input,o);check(stable(p.marshal(a.node))===stable(reference.marshal(b.node)),"Node parity");return {value:a.node,error:a.error,referenceError:b.error}},
 (input,o)=>{const a=p.parseRuleWithOptions("main",input,o),b=reference.parseRuleWithOptions("main",input,o);check(stable(p.marshal(a.node))===stable(reference.marshal(b.node)),"named parity");return {value:a.node,error:a.error,referenceError:b.error}},
 (input,o)=>({value:null,error:p.recognizeWithOptions(input,o),referenceError:reference.recognizeWithOptions(input,o)}),
];
TYPEDROUTE
const source=INPUT;
check(stable(new p.Profile().snapshot())===stable({parses:0,entered:0,calls:0,evals:0,memoHits:0,skipped:0,time:0,rules:[]}),"empty profile");
for(const route of routes)for(const unit of [p.CodePoints,p.Bytes])for(const controlled of [false,true]){
 const options={unit,maxSteps:controlled?100000:0};
 const profile=new p.Profile();let events:p.TraceEvent[]=[];
 const result=route(source,{...options,profile,trace:e=>events.push(e)}),s=profile.snapshot();
 check(stable(errorView(result.error))===stable(errorView(result.referenceError)),"error parity");checkProfile(s,events);CHECKS
 for(const filter of [{},{traceRules:["missing"]},{traceMaxDepth:1}]){
  const other=new p.Profile();route(source,{...options,...filter,profile:other,trace:()=>{}});check(stable(counts(other.snapshot()))===stable(counts(s)),"filters independent");
 }
 const alone=new p.Profile();route(source,{...options,profile:alone});check(stable(counts(alone.snapshot()))===stable(counts(s)),"profile without trace");
 const inside=new p.Profile();let pending=false;route(source,{...options,profile:inside,trace:e=>{if(e.kind==="exit"&&e.depth===1){pending=true;check(inside.snapshot().calls===profile.snapshot().calls-1,"exit update pending within callback")}}});check(pending,"root exit callback");
 const saved=stable(s);s.rules[0]!.rule="mutated";s.rules[0]!.maxEvalsAt.pos=-1;for(const row of s.rules)for(const reason of row.skipReasons)reason.count=-1;s.rules.length=0;
 check(stable(profile.snapshot())===saved,"deep owned snapshot");
 const prior=profile.snapshot();route(source,{...options,profile});const twice=profile.snapshot();check(twice.parses===2&&twice.calls===prior.calls*2&&twice.evals===prior.evals*2,"accumulating profile");
 const reentrant=new p.Profile();let entered=false;route(source,{...options,profile:reentrant,trace:()=>{if(!entered){entered=true;route(source,{profile:reentrant})}}});check(reentrant.snapshot().parses===2&&reentrant.snapshot().calls===prior.calls*2,"reentrant accounting");
 for(const input of ["",source+"?",new Uint8Array([0xff]),new Uint8Array(Array.from(source).flatMap(ch=>{const c=ch.codePointAt(0)!;return c<128?[c]:[0xc0|(c>>6),0x80|(c&63)]}))]){
  const profile=new p.Profile();events=[];const r=route(input,{...options,profile,trace:e=>events.push(e)});check(stable(errorView(r.error))===stable(errorView(r.referenceError)),"input/error parity");checkProfile(profile.snapshot(),events);
 }
 for(const value of p.testThrownValues())for(const kind of ["enter","exit"]){
  const aborted=new p.Profile();let threw=false;try{route(source,{...options,profile:aborted,trace:e=>{if(e.kind===kind)throw value}})}catch(x){threw=true;check(x===value,"callback identity")}
  check(threw,"callback throw");const a=aborted.snapshot();check(kind==="enter"?a.entered===1&&a.calls===0&&a.time===0:a.calls===1,"aborted counters");
  route(source,{...options,profile:aborted});check(aborted.snapshot().calls===prior.calls+a.calls,"next invocation isolated");
 }
 const canceled=new p.Profile();route(source,{profile:canceled,signal:{aborted:true}});check(canceled.snapshot().entered===0,"pre-cancel has no entry");
 const limited=new p.Profile();const r=route(source,{profile:limited,maxSteps:1});check(r.value===null&&r.error instanceof p.ParseInterruptionError,"interruption withholding");
 check(p.testProfileReleased(),"profile teardown");
}
// Host clock behavior and child exit callback attribution use a deterministic clock.
const host=globalThis as unknown as {performance:unknown};
const descriptor=Object.getOwnPropertyDescriptor(host,"performance")!;
function clock(value:unknown):void{Object.defineProperty(host,"performance",{configurable:true,value})}
try{
 const hostRoutes=[(o:p.ParseOptions)=>p.parseWithOptions(source,o).error,(o:p.ParseOptions)=>p.parseRuleWithOptions("main",source,o).error,(o:p.ParseOptions)=>p.recognizeWithOptions(source,o)];
 for(const bad of [undefined,{}, {now:0}]){clock(bad);for(const run of hostRoutes){check(run({profile:new p.Profile()})?.message==="profiling requires host performance.now()","missing clock result");run({trace:()=>{}})}}
 for(const value of p.testThrownValues()){
  Object.defineProperty(host,"performance",{configurable:true,get(){throw value}});let threw=false;try{p.parseWithOptions(source,{profile:new p.Profile()})}catch(x){threw=true;check(x===value,"clock getter identity")}check(threw,"clock getter threw");p.parseWithOptions(source,{trace:()=>{}});p.parse(source);
  for(const route of routes)for(const controlled of [false,true]){clock({now(){throw value}});threw=false;try{route(source,{maxSteps:controlled?100000:0,profile:new p.Profile()})}catch(x){threw=true;check(x===value,"clock call identity")}check(threw,"clock call threw")}
 }
 clock({now(){return 5}});const coarse=new p.Profile();p.parseWithOptions(source,{profile:coarse});check(coarse.snapshot().time===0&&coarse.snapshot().rules.every(r=>r.time===0&&r.selfTime===0),"coarse clock accepted");
 for(const invalid of [NaN,Infinity]){clock({now(){return invalid}});let threw=false;try{p.parseWithOptions(source,{profile:new p.Profile()})}catch(x){threw=true;check(x instanceof Error&&x.message.includes("finite"),"invalid clock diagnostic")}check(threw,"invalid clock refused")}
 let ticks=3;clock({now(){return ticks--}});let threw=false;try{p.parseWithOptions(source,{profile:new p.Profile()})}catch(x){threw=true;check(x instanceof Error&&x.message.includes("monotonic"),"backward clock diagnostic")}check(threw,"backward clock refused");
 if(PLAIN){let ticks=0;clock({now(){return ticks}});const timed=new p.Profile();p.parseWithOptions(source,{profile:timed,trace:e=>{if(e.rule==="part"&&e.kind==="exit")ticks+=10}});const s=timed.snapshot();check(s.rules.find(r=>r.rule==="part")!.time===20&&s.rules.find(r=>r.rule==="main")!.selfTime===0&&s.time===20,"child exit callback timing")}
 const original={marker:true};let secondary=false,secondaryCalls=0;clock({now(){if(secondary){secondaryCalls++;throw new Error("secondary clock")}return 0}});threw=false;try{p.parseWithOptions(source,{profile:new p.Profile(),trace:e=>{if(e.kind==="exit"){secondary=true;throw original}}})}catch(x){threw=true;check(x===original,"callback wins secondary host error")}check(threw&&secondaryCalls===1,"exit throw and secondary clock failure occurred");
}finally{Object.defineProperty(host,"performance",descriptor)}
check(p.testProfileReleased(),"host failure cleanup");
`
