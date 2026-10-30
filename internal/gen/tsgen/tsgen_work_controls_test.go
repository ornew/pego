package tsgen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestGeneratedTSWorkControlFoundation(t *testing.T) {
	t.Parallel()
	src := `type Word = { Text string }

type Bag = {
    Items []Word
}

rule main Bag = x:word* #cut => Bag{Items: $x.Value}

rule word Word = t:("a" / "é" / "😀" / "�") #atomic => Word{Text: text($t)}

type CancellationSignal = { Text string }

type ParseInterruptionError = { Text string }

type WorkControl = { Text string }

type ResolvedParseOptions = { Text string }`
	runTypedTSFixture(t, src, tsWorkControlFoundationConsumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

const tsWorkControlFoundationConsumer = `import * as p from "./parser.ts";
import type { CancellationSignal } from "./parser.ts";
import type { CancellationSignal as GrammarCancellationSignal, ParseInterruptionError as GrammarParseInterruptionError, WorkControl, ResolvedParseOptions } from "./types.ts";
` + tsTypedAssert + `
const collisions: [GrammarCancellationSignal|null, GrammarParseInterruptionError|null, WorkControl|null, ResolvedParseOptions|null] = [null,null,null,null];
check(collisions.length===4,"control declarations remain reserved");
const reason = { marker: "cancellation reason" };
const canceled: CancellationSignal = Object.freeze({aborted:true,reason});
function limits(r: {error: Error|null},kind: p.ParseInterruptionKind,n:number): void {
 check(r.error instanceof p.ParseInterruptionError,"identifiable interruption");
 check(r.error.kind===kind && r.error.limit===n && r.error.used>=0 && r.error.used<=n && r.error.requested>n-r.error.used,"atomic reservation accounting");
}
for (const unit of [p.CodePoints,p.Bytes]) {
 const routes = [
  {name:"Node",run:(s:string|Uint8Array,o:p.ParseOptions)=>{const r=p.parseWithOptions(s,o);return {value:r.node,error:r.error}}},
  {name:"named Node",run:(s:string|Uint8Array,o:p.ParseOptions)=>{const r=p.parseRuleWithOptions("main",s,o);return {value:r.node,error:r.error}}},
  {name:"typed",run:(s:string|Uint8Array,o:p.ParseOptions)=>p.parseASTWithOptions(s,o)},
  {name:"recognize",run:(s:string|Uint8Array,o:p.ParseOptions)=>({value:null,error:p.recognizeWithOptions(s,o)})},
 ];
 for (const route of routes) {
  let units=0,depths=0,signals=0,inputs=0,steps=0;
  const o={get unit(){units++;return unit},get maxDepth(){depths++;return 0},get signal(){signals++;return null},get maxInputWork(){inputs++;return 0},get maxSteps(){steps++;return 0}};
  const baseline=route.run("aaé",o);
  check(baseline.error===null&&[units,depths,signals,inputs,steps].every(n=>n===1),route.name+" options read once");
  for (const input of ["aaé",new Uint8Array([97,97,195,169])]) {
   const r=route.run(input,{unit,signal:canceled});
   check(r.value===null&&r.error instanceof p.ParseInterruptionError&&r.error.kind==="canceled"&&r.error.cause===reason,route.name+" pre-cancellation");
   for (const key of ["maxInputWork","maxSteps"] as const) {
    for (const n of [-1,NaN,Infinity,-Infinity,0.5,Number.MAX_SAFE_INTEGER+1]) {
     const r=route.run(input,{unit,[key]:n,signal:canceled});
     check(r.value===null&&r.error!==null&&!(r.error instanceof p.ParseInterruptionError),"invalid limits before cancellation");
    }
    let boundary=0;
    for (let n=1;n<2048;n++){if(route.run(input,{unit,[key]:n}).error===null){boundary=n;break}}
    check(boundary>1,"finite exact boundary");
    const failed=route.run(input,{unit,[key]:boundary-1});
    check(failed.value===null,"interruption publishes no partial value");
    limits(failed,key==="maxInputWork"?"input-work-limit":"step-work-limit",boundary-1);
    check(route.run(input,{unit,[key]:boundary}).error===null,"exact boundary accepted");
   }
   const active=route.run(input,{unit,maxInputWork:Number.MAX_SAFE_INTEGER,maxSteps:Number.MAX_SAFE_INTEGER});
   check(active.error===null&&stable(active.value)===stable(route.run(input,{unit}).value),"unlimited-equivalent finite limits");
  }
  for (const marker of [new RangeError("Maximum call stack size exceeded"),reason,null,undefined]) {
   for (const getter of ["aborted","reason"]) {
    const signal: CancellationSignal=getter==="aborted"?{get aborted():boolean{throw marker}}:{aborted:true,get reason():unknown{throw marker}};
    let thrown=false;try{route.run("aaé",{unit,signal})}catch(x){thrown=true;check(x===marker,"caller getter exception identity")}
    check(thrown,"getter exception propagates");
   }
  }
  let polls=0;const changing: CancellationSignal={get aborted(){return ++polls>=3},reason};
  const stopped=route.run("a".repeat(2000),{unit,signal:changing});
  check(stopped.value===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="canceled","preparation cancellation");
  check(route.run("aaé",{unit}).error===null,"fresh retry");
 }
 const paritySignal: CancellationSignal={aborted:false};
 for(const input of ["\uDC80","\uDCFF","a\uDCFFé","😀\uDCFF",new Uint8Array([255]),new Uint8Array([195]),new Uint8Array([240,159]),new Uint8Array([237,160,128])]){
  const normal=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:paritySignal,maxInputWork:Number.MAX_SAFE_INTEGER,maxSteps:Number.MAX_SAFE_INTEGER});
  check(stable(active.node)===stable(normal.node)&&active.error?.message===normal.error?.message,"active malformed/surrogate Node parity");
  const typed=p.parseAST(input,unit),controlled=p.parseASTWithOptions(input,{unit,signal:paritySignal,maxInputWork:Number.MAX_SAFE_INTEGER,maxSteps:Number.MAX_SAFE_INTEGER});
  check(stable(controlled.value)===stable(typed.value)&&controlled.error?.message===typed.error?.message,"active malformed/surrogate typed parity");
 }
 let checks=0;
 const signal: CancellationSignal={get aborted(){checks++;p.parseRule("word",new Uint8Array([240,159,152,128]),p.Bytes);return false}};
 for(const input of ["a".repeat(300)+"é😀",new Uint8Array([...Array<number>(300).fill(97),195,169,240,159,152,128])]){
  const a=p.parseWithOptions(input,{unit,signal}),b=p.parse(input,unit);
  check(stable(a.node)===stable(b.node)&&a.error?.message===b.error?.message&&checks>2,"reentrant decoder preserves outer text/spans");
  const c=p.parseASTWithOptions(input,{unit,signal}),d=p.parseAST(input,unit);
  check(stable(c.value)===stable(d.value)&&c.error?.message===d.error?.message,"reentrant typed input");
 }
 const named=p.parseRuleWithOptions("missing","aaé",{unit,signal:{get aborted():boolean{throw reason}}});
 check(named.error!==null&&named.error.message==="rule missing is not defined","rule validation before signal admission");
 let parseSteps=0;for(let n=1;n<2048;n++){if(p.parseWithOptions("aaé",{unit,maxSteps:n}).error===null){parseSteps=n;break}}
 check(parseSteps>0,"Node budget boundary found");
 const converted=p.parseASTWithOptions("aaé",{unit,maxSteps:parseSteps});
 check(converted.value===null,"conversion does not reset the parse budget");limits(converted,"step-work-limit",parseSteps);
}
`

func TestGeneratedTSWorkControlErrorLibraries(t *testing.T) {
	t.Parallel()
	src := `type Word = { Text string }

rule main Word = t:("a") #atomic => Word{Text: text($t)}`
	for _, lib := range []string{"es2022", "es2022,dom"} {
		t.Run(lib, func(t *testing.T) {
			consumer := tsWorkControlErrorLibraryConsumer
			if lib == "es2022,dom" {
				consumer += tsWorkControlDOMSignalConsumer
			}
			runTypedTSFixtureWithLib(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true}, "", lib)
		})
	}
}

const tsWorkControlErrorLibraryConsumer = `import { parseASTWithOptions, ParseInterruptionError } from "./parser.ts";
import type { CancellationSignal } from "./parser.ts";
` + tsTypedAssert + `
const reason={marker:"cause"};const signal:CancellationSignal={aborted:true,reason};
const result=parseASTWithOptions("a",{signal});
check(result.value===null&&result.error instanceof ParseInterruptionError&&result.error.cause===reason&&stable(result.error.cause)===stable(reason),"native Error cause declaration compatibility");
const inherited:Error=result.error;check(inherited.cause===reason,"assignable native Error cause");
`

const tsWorkControlDOMSignalConsumer = `
const controller=new AbortController();controller.abort(reason);
const native:CancellationSignal=controller.signal;
const canceled=parseASTWithOptions("a",{signal:native});
check(canceled.value===null&&canceled.error instanceof ParseInterruptionError&&canceled.error.cause===controller.signal.reason,"native DOM signal compatibility");
`

func TestGeneratedTSWorkControlMalformedText(t *testing.T) {
	t.Parallel()
	src := `type Text = { Text string }

rule main Text = t:(.*) #atomic => Text{Text: text($t)}`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const options:p.ParseOptions={unit,signal:{aborted:false},maxInputWork:Number.MAX_SAFE_INTEGER,maxSteps:Number.MAX_SAFE_INTEGER};
 for(const input of ["\uDC80","\uDCFF","a\uDCFFé","😀\uDCFF",new Uint8Array([255]),new Uint8Array([195]),new Uint8Array([240,159]),new Uint8Array([237,160,128])]) {
  const normal=p.parse(input,unit),active=p.parseWithOptions(input,options);
  check(normal.error===null&&normal.node!==null&&active.error===null&&active.node!==null,"permissive malformed parse succeeds");
  check(String(normal.node.field("Text")).length>0&&normal.node.end>normal.node.start,"malformed text struct produces text and span");
  check(stable(active.node)===stable(normal.node)&&active.node.field("Text")===normal.node.field("Text")&&active.node.start===normal.node.start&&active.node.end===normal.node.end,"controlled malformed text struct text/span parity");
  const typed=p.parseAST(input,unit),controlled=p.parseASTWithOptions(input,options);
  check(typed.error===null&&typed.value!==null&&controlled.error===null&&controlled.value!==null,"permissive malformed typed parse succeeds");
  check(stable(controlled.value)===stable(typed.value),"controlled malformed typed value parity");
 }
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true}, "")
}

func TestGeneratedTSWorkControlMatcherBudgets(t *testing.T) {
	t.Parallel()
	src := `type Text = { Text string }

rule main Text = t:(reread #recover(skip=.+)) #atomic => Text{Text: text($t)}

rule fast Text = t:(.* #atomic) => Text{Text: text($t)}

rule reread Text = t:(&.* #atomic fast) #atomic => Text{Text: text($t)}`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function threshold(input:string|Uint8Array,unit:p.Unit,key:"maxInputWork"|"maxSteps"):number {
 function fits(n:number):boolean {return p.parseRuleWithOptions("fast",input,{unit,[key]:n}).error===null}
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"bounded threshold search")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 check(!fits(hi-1),"one-short budget fails");return hi;
}
for(const unit of [p.CodePoints,p.Bytes]) {
 for(const input of ["a".repeat(800)+"é😀",new Uint8Array([...Array<number>(800).fill(97),195,169,240,159,152,128])]) {
  const plain=p.parse(input,unit),ast=p.parseAST(input,unit);
  check(plain.error===null&&plain.node!==null&&ast.error===null&&ast.value!==null&&p.recognize(input,unit)===null,"ordinary matcher paths succeed");
  for(const key of ["maxInputWork","maxSteps"] as const) {
   const n=threshold(input,unit,key);
   const kind=key==="maxInputWork"?"input-work-limit":"step-work-limit";
   const routes=[(o:p.ParseOptions)=>p.parseWithOptions(input,o).error,(o:p.ParseOptions)=>p.parseRuleWithOptions("reread",input,o).error,(o:p.ParseOptions)=>p.parseASTWithOptions(input,o).error,(o:p.ParseOptions)=>p.recognizeWithOptions(input,o)];
   for(const [route,run] of routes.entries()) {
    // Each route stops one unit short of the budget it needs. The lookahead rereads and the scans
    // consume cumulative work, so a route needs more than the single scan of the rule "fast" that
    // sets n, and recovery cannot swallow the limit.
    let lo=0,hi=1;while(run({unit,[key]:hi})!==null){hi*=2;check(hi<10000000,"bounded threshold search")}
    while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(run({unit,[key]:mid})===null)hi=mid;else lo=mid}
    const error=run({unit,[key]:hi-1});
    check(error instanceof p.ParseInterruptionError&&error.kind===kind,"lookahead rereads and recognizer scans consume cumulative work; recovery cannot swallow limits");
    // The steps of the recognizer route come to about those of one scan (4820 against 4824, measured),
    // so only that bound is pinned for them; its input work is still cumulative.
    if(key==="maxSteps"&&route===3)check(hi>=800&&hi>=n/2,"the recognizer needs the steps of about one scan: "+hi+" against "+n);
    else check(hi>n,"a route needs more than one scan: "+key+" route "+route+" needs "+hi+", one scan "+n);
   }
   check(p.parseWithOptions(input,{unit}).error===null,"retry after matcher interruption");
  }
  let polls=0;const observer:p.CancellationSignal={get aborted(){polls++;return false}};
  check(p.parseRuleWithOptions("fast",input,{unit,signal:observer}).error===null,"count deterministic cooperative checks");
  const cancelAt=polls+1;polls=0;
  const canceled=p.parseWithOptions(input,{unit,signal:{get aborted(){return ++polls>=cancelAt}}});
  check(canceled.node===null&&canceled.error instanceof p.ParseInterruptionError&&canceled.error.kind==="canceled","extra matcher work checks cancellation after preparation");
  check(stable(p.parse(input,unit).node)===stable(plain.node),"interruption leaves fresh result unchanged");
 }
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlWideClassSteps(t *testing.T) {
	t.Parallel()
	// A wide class is tested by comparisons that cost no work of their own: a character is charged
	// once, by the peek that reads it, and each repetition by its step. The budgets that parse and
	// recognition need for one character and for a run of them are pinned, so that a long input of
	// wide-class matches still reaches the limits.
	var class strings.Builder
	for i := 0; i < 800; i++ {
		class.WriteRune(rune(0x1000 + i*2))
	}
	src := "rule main = ([" + class.String() + "] / \"z\")+"
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function threshold(run:(n:number)=>Error|null):number {
 let lo=0,hi=1;while(run(hi)!==null){hi*=2;check(hi<10000000,"bounded threshold search")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(run(mid)===null)hi=mid;else lo=mid}
 check(run(hi-1)!==null,"one-short budget fails");return hi;
}
const wide=String.fromCodePoint(0x1000);
// The budgets of recognition and of parsing: [maxInputWork, maxSteps] for "z", fifty "z" and fifty wide characters.
const budgets=new Map<p.Unit,number[][]>([[p.CodePoints,[[4,10],[200,353],[200,303]]],[p.Bytes,[[9,17],[401,507],[752,908]]]]);
const parseBudgets=new Map<p.Unit,number[][]>([[p.CodePoints,[[4,11],[200,403],[200,403]]],[p.Bytes,[[9,18],[401,557],[752,1108]]]]);
for(const unit of [p.CodePoints,p.Bytes]) {
 check(p.parse("z",unit).error===null&&p.recognize("z",unit)===null,"wide class fallback succeeds without controls");
 check(stable(p.parseWithOptions("z",{unit,signal:{aborted:false}}).node)===stable(p.parse("z",unit).node),"controlled class fallback preserves result");
 for(const key of ["maxInputWork","maxSteps"] as const) {
  const kind=key==="maxInputWork"?"input-work-limit":"step-work-limit";
  const inputs=["z","z".repeat(50),wide.repeat(50)];
  for(let i=0;i<inputs.length;i++) {
   const input=inputs[i]!;
   const n=threshold(k=>p.recognizeWithOptions(input,{unit,[key]:k}));
   check(n===budgets.get(unit)![i]![key==="maxInputWork"?0:1],key+" budget of recognition for input "+i);
   const limited=p.recognizeWithOptions(input,{unit,[key]:n-1});
   check(limited instanceof p.ParseInterruptionError&&limited.kind===kind,"recognition stops one unit short of its budget");
   const m=threshold(k=>p.parseWithOptions(input,{unit,[key]:k}).error);
   check(m===parseBudgets.get(unit)![i]![key==="maxInputWork"?0:1],key+" budget of parsing for input "+i);
   const parsed=p.parseWithOptions(input,{unit,[key]:m-1}).error;
   check(parsed instanceof p.ParseInterruptionError&&parsed.kind===kind,"parse stops one unit short of its budget");
  }
 }
 check(threshold(k=>p.recognizeWithOptions(wide.repeat(50),{unit,maxInputWork:k}))>threshold(k=>p.recognizeWithOptions("z",{unit,maxInputWork:k})),"a longer input needs more input work");
 const cause={marker:"class cancellation"};let polls=0;
 const result=p.parseWithOptions("z",{unit,signal:{get aborted(){return ++polls>=2},reason:cause}});
 check(result.node===null&&result.error instanceof p.ParseInterruptionError&&result.error.kind==="canceled"&&result.error.cause===cause,"cancellation is polled while a wide class is tested");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlMemoKeys(t *testing.T) {
	t.Parallel()
	t.Run("rollback", func(t *testing.T) {
		src := `type Word = { Text string }

var v string
rule main Word = t:({v := "a"} &word {v := "b"} word / {v := "b"} word) #atomic => Word{Text: text($t)}

rule word Word = t:(x:letters {text($x) == v}) => Word{Text: text($t)}

rule letters = [a-z]+ #atomic`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes])for(const input of ["a","b","aa","bb",new Uint8Array([98])]) {
 const normal=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(stable(active.node)===stable(normal.node)&&active.error?.message===normal.error?.message,"memo keys and variable rollback preserve values/errors");
 const typed=p.parseAST(input,unit),controlled=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(stable(controlled.value)===stable(typed.value)&&controlled.error?.message===typed.error?.message,"typed memo rollback parity");
 check(p.recognizeWithOptions(input,{unit,signal:{aborted:false}})?.message===p.recognize(input,unit)?.message,"recognizer memo rollback parity");
}
`
		runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
	})
	t.Run("long-string", func(t *testing.T) {
		key := strings.Repeat("a", 1600)
		src := `type Word = { Text string }
 var v string
 rule main Word = {v := "` + key + `"} &word &word &word x:word => $x
 rule fast Word = {v := "` + key + `"} x:word => $x
 rule word Word = {v != ""} t:letter #atomic => Word{Text: text($t)}
 rule letter = "z"`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes])for(const input of ["z",new Uint8Array([122])]) {
 const fast=p.parseRuleWithOptions("fast",input,{unit,maxSteps:128});
 check(fast.error===null&&fast.node!==null,"one call fits before memo string comparison");
 const normal=p.parse(input,unit);
 check(normal.error===null&&normal.node!==null,"ordinary repeated memo call succeeds");
 const errors=[p.parseWithOptions(input,{unit,maxSteps:128}).error,p.parseRuleWithOptions("main",input,{unit,maxSteps:128}).error,p.parseASTWithOptions(input,{unit,maxSteps:128}).error,p.recognizeWithOptions(input,{unit,maxSteps:128})];
 for(const [index,error] of errors.entries())check(error instanceof p.ParseInterruptionError&&error.kind==="step-work-limit"&&error.requested===128,"bounded memo-key string comparison consumes steps "+index+" "+stable(error));
 const unlimited=p.parseWithOptions(input,{unit,maxSteps:0,signal:{aborted:false}});
 check(unlimited.error===null&&stable(unlimited.node)===stable(normal.node),"unlimited memo results preserved");
 let polls=0;const reason={marker:"memo"};
 const interrupted=p.parseWithOptions(input,{unit,signal:{get aborted(){return ++polls>=3},reason}});
 check(interrupted.node===null&&interrupted.error instanceof p.ParseInterruptionError&&interrupted.error.kind==="canceled"&&interrupted.error.cause===reason,"memo key chunks check cancellation");
 check(p.parse(input,unit).error===null,"fresh retry after memo interruption");
}
`
		runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
	})
}
