package engine

import (
	"strings"
	"testing"
)

func TestGeneratedTSWorkControlFoundation(t *testing.T) {
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
	runTypedTSFixture(t, src, tsWorkControlFoundationConsumer, GenOptions{Start: "main", Types: true, Recognize: true}, "")
}

const tsWorkControlFoundationConsumer = `import * as p from "./parser.ts";
import type { CancellationSignal, CancellationSignal_, ParseInterruptionError_, WorkControl_, ResolvedParseOptions_ } from "./parser.ts";
` + tsTypedAssert + `
const collisions: [CancellationSignal_|null, ParseInterruptionError_|null, WorkControl_|null, ResolvedParseOptions_|null] = [null,null,null,null];
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
	src := `type Word = { Text string }

rule main Word = t:("a") #atomic => Word{Text: text($t)}`
	for _, lib := range []string{"es2022", "es2022,dom"} {
		t.Run(lib, func(t *testing.T) {
			consumer := tsWorkControlErrorLibraryConsumer
			if lib == "es2022,dom" {
				consumer += tsWorkControlDOMSignalConsumer
			}
			runTypedTSFixtureWithLib(t, src, consumer, GenOptions{Start: "main", Types: true}, "", lib)
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
	runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true}, "")
}

func TestGeneratedTSWorkControlMatcherBudgets(t *testing.T) {
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
   const n=threshold(input,unit,key),options:p.ParseOptions={unit,[key]:n};
   const kind=key==="maxInputWork"?"input-work-limit":"step-work-limit";
   const routes=[p.parseWithOptions(input,options).error,p.parseRuleWithOptions("reread",input,options).error,p.parseASTWithOptions(input,options).error,p.recognizeWithOptions(input,options)];
   for(const error of routes)check(error instanceof p.ParseInterruptionError&&error.kind===kind,"lookahead rereads and recognizer scans consume cumulative work; recovery cannot swallow limits");
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
	runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlWideClassSteps(t *testing.T) {
	// A one-character input keeps preparation small while the class performs
	// many failed comparisons. Both recognition and Node matching must stop.
	var class strings.Builder
	for i := 0; i < 800; i++ {
		class.WriteRune(rune(0x1000 + i*2))
	}
	src := "rule main = [" + class.String() + "] / \"z\""
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 check(p.parse("z",unit).error===null&&p.recognize("z",unit)===null,"wide class fallback succeeds without controls");
 check(stable(p.parseWithOptions("z",{unit,signal:{aborted:false}}).node)===stable(p.parse("z",unit).node),"controlled class fallback preserves result");
 for(const error of [p.parseWithOptions("z",{unit,maxSteps:100}).error,p.recognizeWithOptions("z",{unit,maxSteps:100})])check(error instanceof p.ParseInterruptionError&&error.kind==="step-work-limit","each short-circuit class comparison consumes work");
 const cause={marker:"class cancellation"};let polls=0;
 const result=p.parseWithOptions("z",{unit,signal:{get aborted(){return ++polls>=2},reason:cause}});
 check(result.node===null&&result.error instanceof p.ParseInterruptionError&&result.error.kind==="canceled"&&result.error.cause===cause,"wide class checks cancellation inside comparison chain");
}
`
	runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Recognize: true}, "")
}

func TestGeneratedTSWorkControlMemoKeys(t *testing.T) {
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
		runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true, Recognize: true}, "")
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
		runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true, Recognize: true}, "")
	})
}
