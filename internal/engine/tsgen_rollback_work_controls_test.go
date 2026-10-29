package engine

import (
	"encoding/json"
	"fmt"
	"github.com/ornew/pego/internal/gentest"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestGeneratedTSWorkControlCaptureRollback(t *testing.T) {
	t.Parallel()
	const width = 512
	captures := make([]string, width)
	for i := range captures {
		captures[i] = fmt.Sprintf(`c%d:"z"`, i)
	}
	sequence := strings.Join(captures, " ")
	// Two child frames in fast account for the root allocation and empty-frame
	// inspection in main. Only main also restores both failed capture trails.
	src := `rule main = ({true} ` + sequence + ` "!") / ({1 == 1} ` + sequence + ` "?") / "z" #repeat(512)
 rule fast = first / second / "z" #repeat(512)
 rule first = {true} ` + sequence + ` "!"
 rule second = {1 == 1} ` + sequence + ` "?"`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite rollback budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="z".repeat(512),ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.error===null&&active.error===null&&stable(active.node)===stable(ordinary.node)&&active.node?.fields.length===0,"failed captures are absent from successful fallback");
 const typed=p.parseAST(input,unit),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(typed.error===null&&converted.error===null&&stable(converted.value)===stable(typed.value),"typed rollback output parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast",input,{unit,maxSteps:n}).error===null),limit=baseline+128;
 const stopped=p.parseWithOptions(input,{unit,maxSteps:limit});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="step-work-limit","capture restoration consumes work beyond equivalent fresh frames");
 check(p.parseASTWithOptions(input,{unit,maxSteps:limit}).value===null,"typed rollback interruption exposes no partial value");
 check(p.parseWithOptions(input,{unit,maxSteps:0}).error===null,"fresh retry after interrupted capture restoration");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlOperatorsRuleCapturePreservation(t *testing.T) {
	t.Parallel()
	const width = 512
	captures := make([]string, width)
	for i := range captures {
		captures[i] = fmt.Sprintf(`c%d:"-"`, i)
	}
	sequence := strings.Join(captures, " ")
	src := `type Word = { Text string }
 type Op = { Token node[string] }
 type Box = { Token node[string]; Rhs Expr }
 type Expr ~ Word | Box
 rule main Expr = #operators {
  operand word
  level { prefix ` + sequence + ` => Box{Token:$c0,Rhs:$rhs} }
 }
 rule fast Box = o:operator operator? x:word => Box{Token:$o.Token,Rhs:$x}
 rule operator Op = ` + sequence + ` => Op{Token:$c0}
 rule word Word = t:("z") #atomic => Word{Text: text($t)}`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite Operators Rule budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="-".repeat(512)+"z",ordinary=p.parse(input,unit),typed=p.parseAST(input,unit);
 const active=p.parseWithOptions(input,{unit,signal:{aborted:false}}),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.error===null&&typed.error===null&&active.error===null&&converted.error===null&&stable(active.node)===stable(ordinary.node)&&stable(converted.value)===stable(typed.value),"Operators Rule capture preservation parity");
 check(converted.value?.type==="Box"&&converted.value.Token?.value==="-"&&converted.value.Rhs?.type==="Word","successful operator retains its captured token across candidate reset");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast",input,{unit,maxSteps:n}).error===null);
 // Allow the successful trail reset, operator span materialization and
 // either one preservation pass over both candidate frames. Completing
 // both passes must exceed this limit.
 const limit=baseline+4*512+128;
 const stopped=p.parseWithOptions(input,{unit,maxSteps:limit});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="step-work-limit","Operators Rule preservation visits consume cumulative work");
 check(p.parseASTWithOptions(input,{unit,maxSteps:limit}).value===null,"typed Operators Rule interruption exposes no partial value");
 check(p.parseASTWithOptions(input,{unit,maxSteps:0}).error===null,"fresh Operators Rule retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlRecoveredMemoReplay(t *testing.T) {
	t.Parallel()
	// Prewarm an empty-error memo so slot allocation is common to both routes.
	src := `rule main = &seed &seed &recovered &recovered &recovered .* #atomic $$

rule fast = &seed &seed &recovered &recovered .* #atomic $$

rule fresh = &seed &seed &recovered &other .* #atomic $$

rule other = bad #repeat(512)

rule recovered = bad #repeat(512)

rule bad = ("a" "!") #recover(skip="a" "?")

rule seed = bit

rule bit = ""`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite recovered replay budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="a?".repeat(512),ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.node!==null&&active.node!==null&&ordinary.error instanceof p.SyntaxErrors&&active.error instanceof p.SyntaxErrors&&active.error.errors.length===1536&&stable(active.node)===stable(ordinary.node)&&active.error.message===ordinary.error.message,"same-silent memo hit preserves recovered errors and output");
 for(let i=0;i<1536;i++)check(active.error.errors[i]?.pos===(i%512)*2,"memo replay preserves error positions and order");
 const freshBudget=minimum(n=>{const r=p.parseRuleWithOptions("fresh",input,{unit,maxSteps:n});return r.node!==null&&r.error instanceof p.SyntaxErrors&&r.error.errors.length===1024});
 const snapshot=p.parseRuleWithOptions("fast",input,{unit,maxSteps:freshBudget+128});
 check(snapshot.node===null&&snapshot.error instanceof p.ParseInterruptionError&&snapshot.error.kind==="step-work-limit","recovered memo snapshots consume work beyond unretained errors");
 const baseline=minimum(n=>{const r=p.parseRuleWithOptions("fast",input,{unit,maxSteps:n});return r.node!==null&&r.error instanceof p.SyntaxErrors&&r.error.errors.length===1024});
 const stopped=p.parseWithOptions(input,{unit,maxSteps:baseline+128});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="step-work-limit","recovered memo payload visits consume budget beyond stored entry");
 const retry=p.parseWithOptions(input,{unit,maxSteps:0});
 check(retry.node!==null&&retry.error instanceof p.SyntaxErrors&&retry.error.errors.length===1536,"fresh recovered memo retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlRecoveryParity(t *testing.T) {
	t.Parallel()
	cases := append(append([]gentest.Case(nil), gentest.TypedOperatorsRuleBodyCases...), gentest.TypedLRBodyCases...)
	for _, tt := range cases {
		if tt.Name != "typed/OperatorsRule_recovery" && tt.Name != "typed/LR_recovery" {
			continue
		}
		t.Run(tt.Name, func(t *testing.T) {
			inputs, err := json.Marshal(tt.Inputs)
			if err != nil {
				t.Fatal(err)
			}
			consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes])for(const input of ` + string(inputs) + `) {
 const ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(stable(active.node)===stable(ordinary.node)&&active.error?.message===ordinary.error?.message,"Operators Rule/LR recovered snapshot and replay parity");
 const typed=p.parseAST(input,unit),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(stable(converted.value)===stable(typed.value)&&converted.error?.message===typed.error?.message,"typed Operators Rule/LR recovered parity");
 check(p.recognizeWithOptions(input,{unit,signal:{aborted:false}})?.message===p.recognize(input,unit)?.message,"recognizer recovered parity");
}
`
			runTypedTSFixture(t, tt.Src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
		})
	}
}

func TestGeneratedTSWorkControlOperatorsRuleRecoveredTransfer(t *testing.T) {
	t.Parallel()
	src := `type Word = { Text string }

type Box = {
    Token node[string]
    Rhs   Expr
}

type Expr ~ Word | Box

rule main Expr = #operators {
    operand word
    level {
        prefix (bad #repeat(512) #atomic) => Box{Token: $op, Rhs: $rhs}
    }
}

rule fast Box = t:(bad #repeat(512) #atomic) (-bad #repeat(512))? x:word => Box{Token: $t, Rhs: $x}

rule bad = ("a" "!") #recover(skip="a" "?")

rule word Word = t:("z") #atomic => Word{Text: text($t)}`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite Operators Rule recovered budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="a?".repeat(512)+"z",ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.node!==null&&active.node!==null&&ordinary.error instanceof p.SyntaxErrors&&active.error instanceof p.SyntaxErrors&&active.error.errors.length===512&&stable(active.node)===stable(ordinary.node)&&active.error.message===ordinary.error.message,"Operators Rule recovered snapshot and apply parity");
 const typed=p.parseAST(input,unit),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(converted.error instanceof p.SyntaxErrors&&stable(converted.value)===stable(typed.value)&&converted.value?.type==="Box"&&converted.value.Token?.value==="a?".repeat(512),"typed Operators Rule recovered output");
 const baseline=minimum(n=>{const r=p.parseRuleWithOptions("fast",input,{unit,maxSteps:n});return r.node!==null&&r.error instanceof p.SyntaxErrors&&r.error.errors.length===512});
 const stopped=p.parseWithOptions(input,{unit,maxSteps:baseline+512+128});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="step-work-limit","Operators Rule recovered snapshots and apply both consume work");
 const retry=p.parseWithOptions(input,{unit,maxSteps:0});
 check(retry.node!==null&&retry.error instanceof p.SyntaxErrors&&retry.error.errors.length===512,"fresh Operators Rule recovered retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}
