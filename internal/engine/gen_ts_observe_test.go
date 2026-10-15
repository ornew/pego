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

func TestGeneratedTSTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("runs standalone TypeScript trace consumers")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	fixtures := []struct{ name, grammar, input, checks string }{
		{"plain", `type W terminal
rule main W = part
rule part W = "a"`, "a", `check(stable(events.map(e=>[e.kind,e.rule,e.depth,e.evals,e.end,e.matched]))===stable([["enter","main",1,0,0,false],["enter","part",2,0,0,false],["exit","part",2,1,1,true],["exit","main",1,1,1,true]]),"exact plain calls");`},
		{"lean", `type W terminal
rule main W = -space part
rule space = " "
rule part W = "a"`, " a", `check(count("space","enter")===1&&count("part","enter")===1,"lean calls");`},
		{"memo", `type W terminal
rule main W = &part &part &part p:part => $p
rule part W = letter
rule letter = "a"`, "a", `check(events.some(e=>e.memo&&e.evals===0),"memo hit");`},
		{"framed", `type W terminal
type N struct { Value W }
rule main N = p:part => N{Value:$p}
rule part W = "a"`, "a", `check(count("part","enter")===1,"framed calls");`},
		{"predicate", `type W terminal
rule main W = {x:=1} -- p:part {x==1} => $p
rule part W = "a"`, "a", `check(count("part","enter")===1,"predicate calls");`},
		{"leftrec", `type W terminal
rule main W = expr
rule expr W = expr "a" / "a"`, "aaa", `check(events.some(e=>e.rule==="expr"&&e.kind==="exit"&&e.evals>1),"growth evaluations");`},
		{"dispatch", `type W terminal
rule main W = first / second
rule first W = "a"
rule second W = "b"`, "b", `check(count("first","enter")===0&&count("first","skipped")===1,"dispatch skip");`},
		{"recovery", `type W terminal
rule main W = part #recover(skip="!")
rule part W = "a"`, "!", `check(count("part","enter")===1,"recovery calls");`},
		{"operators", `type W terminal
type B struct { Left Expr; Right Expr }
type Expr = W | B
rule main Expr = l:level(expr, product) "+" r:expr => B{Left:$l,Right:$r}
rule expr Expr = operators {operand num;level sum {infix left "+" => B{Left:$lhs,Right:$rhs}};level product {infix left "*" => B{Left:$lhs,Right:$rhs}}}
rule num W = [0-9]+`, "1+2*3", `check(events.some(e=>e.rule==="expr"&&e.kind==="enter"&&e.level>0),"named level");`},
		{"unicode", `type W terminal
rule main W = part part
rule part W = "é"`, "éé", `check(events[events.length-1]!.end===(unit===p.Bytes?4:2),"position units");`},
		{"nilvalue", `type W terminal
rule main *W = part => nil
rule part W = "a"`, "a", `check(count("part","enter")===1,"nil value call");`},
		{"anchors", `type W terminal
rule main W = part
rule part W = "input, o.unit, o.maxDepth, control)"`, "input, o.unit, o.maxDepth, control)", `check(count("part","enter")===1,"runtime-like literal remains unchanged");`},
		{"collision", `type TraceKind terminal
type TraceEvent struct { Value Observation }
type Observation terminal
type ObservationCall terminal
type TraceFailure terminal
rule main TraceEvent = p:part => TraceEvent{Value:$p}
rule part Observation = "a"`, "a", `check(count("part","enter")===1,"collision calls");`},
	}
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	files := []string{}
	var harness strings.Builder
	for _, fixture := range fixtures {
		g, err := syntax.Parse(fixture.grammar)
		if err != nil {
			t.Fatal(fixture.name, err)
		}
		for _, typed := range []bool{false, true} {
			name := fixture.name
			if typed {
				name += "_typed"
			}
			opts := GenOptions{Start: "main", Types: typed, Recognize: true}
			reference, err := GenerateTS(g, opts)
			if err != nil {
				t.Fatal(name, err)
			}
			opts.Observability = true
			code, err := GenerateTS(g, opts)
			if err != nil {
				t.Fatal(name, err)
			}
			again, err := GenerateTS(g, opts)
			if err != nil || string(again) != string(code) {
				t.Fatal(name, "nondeterministic trace source", err)
			}
			write(name+"_reference.ts", string(reference))
			write(name+".ts", string(code)+tsTraceTestHooks)
			var names []string
			for _, r := range g.Rules() {
				names = append(names, fmt.Sprintf("%q", r.Name))
			}
			typedConsumer := ""
			typedRoute := ""
			if typed {
				typedConsumer = tsTraceTypedConsumer
				typedRoute = `routes.push((input,o)=>{const r=p.parseASTWithOptions(input,o);return {value:r.value,error:r.error}});`
			}
			consumer := strings.NewReplacer("PARSER", name, "INPUT", fmt.Sprintf("%q", fixture.input), "RULES", strings.Join(names, ","), "CHECKS", fixture.checks, "TYPEDCHECKS", typedConsumer, "TYPEDROUTE", typedRoute).Replace(tsTraceConsumer)
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
		t.Fatalf("strict trace consumers: %v\n%s", err, out)
	}
	cmd = exec.Command(node, "harness.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("trace consumers: %v\n%s", err, out)
	}
}

const tsTraceTestHooks = `
// Test-only hooks keep private exception and observer teardown checks concrete.
const traceProbes: Observation[]=[];
const originalTraceCall=Parser.prototype.call;
Parser.prototype.call=function(r:Rule,min:number):R {
 if(this.observation!==null)traceProbes.push(this.observation);
 return originalTraceCall.call(this,r,min);
};
export function testReleased():boolean {
 const clean=traceProbes.every(o=>{const x=o as unknown as {callback:unknown;scope:unknown;rules:unknown;stack:unknown[]};return x.callback===null&&x.scope===null&&x.rules===null&&x.stack.length===0});
 traceProbes.length=0;return clean;
}
export function testThrownValues():unknown[] {
 return [null,undefined,17,"marker",{marker:true},new Error("error"),new RangeError("Maximum call stack size exceeded"),new Fatal(new Error("private fatal")),new EvalError("private eval"),new SignalPanic({marker:true}),new TraceFailure({marker:true},{})];
}
`

const tsTraceConsumer = `import * as p from "./PARSER.ts";
import * as reference from "./PARSER_reference.ts";
` + tsTypedAssert + `
function errorView(e:Error|null):unknown{return e===null?null:{name:e.name,message:e.message,data:JSON.parse(stable(e))}}
function balanced(events:readonly p.TraceEvent[]):void {
 const stack:p.TraceEvent[]=[];
 for(const e of events){
  check(!e.examinedKnown,"examined availability");
  if(e.kind==="enter"){check(e.depth===stack.length+1,"enter depth");stack.push(e)}
  else if(e.kind==="exit"){
   const entry=stack.pop();check(entry!==undefined&&entry.rule===e.rule&&entry.pos===e.pos&&entry.level===e.level&&entry.depth===e.depth,"paired exit");
   check(e.memo?e.evals===0:e.evals>0,"body evaluations");
  }else check(e.kind==="skipped"&&e.skipReason==="first-character"&&e.depth===stack.length+1&&e.evals===0&&!e.memo,"truthful skip");
 }
 check(stack.length===0,"complete calls");
}
const source=INPUT;
for(const unit of [p.CodePoints,p.Bytes])for(const controlled of [false,true])for(const bytes of [false,true]){
 const o={unit,maxSteps:controlled?100000:0};
 const forms=bytes?[new Uint8Array(Array.from(source).flatMap(ch=>{const c=ch.codePointAt(0)!;return c<128?[c]:[0xc0|(c>>6),0x80|(c&63)]})),new Uint8Array(),new Uint8Array([0xff])]:[source,"",source+"?"];
 for(const input of forms){
  let events:p.TraceEvent[]=[];
  const got=p.parseWithOptions(input,{...o,trace:e=>events.push(e)}),want=reference.parseWithOptions(input,o);
  check(stable(p.marshal(got.node))===stable(reference.marshal(want.node))&&stable(errorView(got.error))===stable(errorView(want.error)),"Node parity");balanced(events);
  if(input===source){const count=(rule:string,kind:p.TraceKind)=>events.filter(e=>e.rule===rule&&e.kind===kind).length;void count;CHECKS}
  for(const name of [RULES]){
   events=[];const got=p.parseRuleWithOptions(name,input,{...o,trace:e=>events.push(e)}),want=reference.parseRuleWithOptions(name,input,o);
   check(stable(p.marshal(got.node))===stable(reference.marshal(want.node))&&stable(errorView(got.error))===stable(errorView(want.error)),"named Node parity");balanced(events);
  }
  events=[];const rec=p.recognizeWithOptions(input,{...o,trace:e=>events.push(e)}),refRec=reference.recognizeWithOptions(input,o);
  check(stable(errorView(rec))===stable(errorView(refRec)),"recognition parity");balanced(events);
  TYPEDCHECKS
  check(p.testReleased(),"all observer references released");
 }
}
type Result={value:unknown;error:Error|null};
type Route=(input:string,o:p.ParseOptions)=>Result;
const routes:Route[]=[
 (input,o)=>{const r=p.parseWithOptions(input,o);return {value:r.node,error:r.error}},
 (input,o)=>{const r=p.parseRuleWithOptions("main",input,o);return {value:r.node,error:r.error}},
 (input,o)=>({value:null,error:p.recognizeWithOptions(input,o)}),
];
TYPEDROUTE
for(const route of routes)for(const controlled of [false,true]){
 const options={maxSteps:controlled?100000:0};
 let full:p.TraceEvent[]=[];route(source,{...options,trace:e=>full.push(e)});balanced(full);
 for(const value of p.testThrownValues()){
  let threw=false;try{route(source,{...options,signal:{get aborted():boolean{throw value}},trace:()=>{}})}catch(x){threw=true;check(x===value,"signal thrown identity through trace boundary")}
  check(threw,"signal getter actually threw");
 }
 for(const value of p.testThrownValues())for(const at of [1,2]){
  let n=0,threw=false;
  try{route(source,{...options,trace:()=>{if(++n===at)throw value}})}catch(x){threw=true;check(x===value,"callback thrown identity")}
  check(threw,"callback actually threw");check(p.testReleased(),"throw teardown");
  route(source,options);
 }
 let mutationEvents=0;const recorded:p.TraceEvent[]=[];
 route(source,{...options,trace:e=>{mutationEvents++;recorded.push({...e});const x=e as unknown as {kind:string;rule:string;depth:number;evals:number};x.kind="broken";x.rule="broken";x.depth=-1;x.evals=-1}});
 check(mutationEvents===full.length&&stable(recorded)===stable(full),"event mutation does not corrupt later events");
 let events:p.TraceEvent[]=[];route(source,{...options,trace:e=>events.push(e)});check(stable(events)===stable(full),"no retained caller mutation");
 for(const depth of [0,-1,1]){events=[];route(source,{...options,trace:e=>events.push(e),traceMaxDepth:depth});check(stable(events)===stable(full.filter(e=>depth<=0||e.depth<=depth)),"depth filter")}
 events=[];const names=["main"];route(source,{...options,trace:e=>{names[0]="missing";events.push(e)},traceRules:names});check(stable(events)===stable(full),"filter names copied");
 route(source,{...options,trace:()=>{throw new Error("unknown filter delivered")},traceRules:["missing"]});
 const selected=full.find(e=>e.kind==="enter"&&e.depth===2)!.rule;
 const selection:boolean[]=[];const expected=full.filter(e=>{const yes=e.rule===selected||(selection[selection.length-1]??false);if(e.kind==="enter")selection.push(yes);if(e.kind==="exit")return selection.pop()!;return yes});
 events=[];route(source,{...options,trace:e=>events.push(e),traceRules:[selected]});check(stable(events)===stable(expected),"selected descendants");
 let reentered=false;route(source,{...options,trace:()=>{if(!reentered){reentered=true;route(source,{trace:()=>{}})}}});check(reentered,"callback reentry");
 let retained=0;route(source,{...options,trace:()=>retained++});const n=retained;route(source,options);p.parse(source);check(retained===n,"no callback on later parse");
 const canceled=route(source,{signal:{aborted:true},trace:()=>{throw new Error("pre-canceled delivery")}});check(canceled.value===null&&canceled.error instanceof p.ParseInterruptionError,"pre-cancel");
 const limited=route(source,{maxSteps:1,trace:()=>{}});check(limited.value===null&&limited.error instanceof p.ParseInterruptionError,"budget withholding");
 let aborted=false;const late=route(source,{signal:{get aborted(){return aborted}},trace:()=>{aborted=true}});check(late.value===null&&late.error instanceof p.ParseInterruptionError,"callback cancellation");
 const tooDeep=route(source,{maxDepth:1,trace:()=>{}});check(tooDeep.value===null&&tooDeep.error!==null&&tooDeep.error.message.includes("nesting too deep"),"depth error unchanged");
 check(p.testReleased(),"lifecycle observer release");
}
for(const traceMaxDepth of [NaN,Infinity,1.5]){const r=p.parseRuleWithOptions("not-defined",source,{traceMaxDepth});check(r.node===null&&r.error?.message.includes("trace max depth"),"options validated before lookup")}
check(p.parseRuleWithOptions("not-defined",source,{trace:()=>{throw new Error("unknown rule callback")}}).error!==null,"unknown rule has no trace");
`

const tsTraceTypedConsumer = `
 events=[];const typed=p.parseASTWithOptions(input,{...o,trace:e=>events.push(e)}),refTyped=reference.parseASTWithOptions(input,o);
 check(stable(typed.value)===stable(refTyped.value)&&stable(errorView(typed.error))===stable(errorView(refTyped.error)),"typed parity");balanced(events);
`
