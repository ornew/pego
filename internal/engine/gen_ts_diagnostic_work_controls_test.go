package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

const tsDiagnosticMinimum = `function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite diagnostic budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
`

func TestGeneratedTSWorkControlDiagnosticLineScan(t *testing.T) {
	src := `rule main = "z" #repeat(2048) "!"

rule fast = "z" #repeat(2048)`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="z".repeat(2048),ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.node===null&&ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&stable(active.error)===stable(ordinary.error)&&active.error.message===ordinary.error.message&&active.error.line===1&&active.error.col===2049,"diagnostic line scan parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast",input,{unit,maxInputWork:n}).error===null),limit=baseline+64;
 for(const error of [p.parseWithOptions(input,{unit,maxInputWork:limit}).error,p.parseASTWithOptions(input,{unit,maxInputWork:limit}).error,p.recognizeWithOptions(input,{unit,maxInputWork:limit})])check(error instanceof p.ParseInterruptionError&&error.kind==="input-work-limit","diagnostic line reads consume input budget beyond matching");
 const retry=p.parseWithOptions(input,{unit,maxInputWork:0});check(retry.node===null&&retry.error instanceof p.SyntaxError&&retry.error.message===ordinary.error.message,"fresh line diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlDiagnosticMessage(t *testing.T) {
	message := strings.Repeat("details λ; ", 256)
	src := fmt.Sprintf(`rule main = "z" @error(message=%q)
 rule fast = "z" @error(message="short")`, message)
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parse("x",unit),active=p.parseWithOptions("x",{unit,signal:{aborted:false}});
 check(ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&active.error.message===ordinary.error.message&&stable(active.error)===stable(ordinary.error),"long custom diagnostic parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","x",{unit,maxSteps:n}).error instanceof p.SyntaxError),limit=baseline+128;
 for(const error of [p.parseWithOptions("x",{unit,maxSteps:limit}).error,p.parseASTWithOptions("x",{unit,maxSteps:limit}).error,p.recognizeWithOptions("x",{unit,maxSteps:limit})])check(error instanceof p.ParseInterruptionError&&error.kind==="step-work-limit","custom diagnostic text consumes cumulative work");
 const retry=p.parseWithOptions("x",{unit,maxSteps:0});check(retry.error instanceof p.SyntaxError&&retry.error.message===ordinary.error.message,"fresh custom diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlDiagnosticSort(t *testing.T) {
	const width = 64
	alternatives := make([]string, width)
	for i := range alternatives {
		alternatives[i] = fmt.Sprintf("%q", strings.Repeat("p", 2048)+fmt.Sprintf("%02d", width-i))
	}
	choice := strings.Join(alternatives, " / ")
	src := `rule main = (` + choice + `)
 rule fast = (` + choice + `) @error(message="short")`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parse("",unit),active=p.parseWithOptions("",{unit,signal:{aborted:false}});
 check(ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&active.error.expected.length===64&&active.error.message===ordinary.error.message&&stable(active.error)===stable(ordinary.error),"long-prefix expected sorting parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","",{unit,maxSteps:n}).error instanceof p.SyntaxError);
 // Matching and expected-ID deduplication are shared. Allow the complete
 // expected text and list movement, but not all common-prefix comparisons.
 const limit=baseline+64*2100+128;
 for(const error of [p.parseWithOptions("",{unit,maxSteps:limit}).error,p.parseASTWithOptions("",{unit,maxSteps:limit}).error,p.recognizeWithOptions("",{unit,maxSteps:limit})])check(error instanceof p.ParseInterruptionError&&error.kind==="step-work-limit","expected-label comparisons consume work beyond rendering");
 check(p.parseWithOptions("",{unit,maxSteps:0}).error instanceof p.SyntaxError,"fresh sorted diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlRecoveredMessage(t *testing.T) {
	message := strings.Repeat("details λ; ", 256)
	src := fmt.Sprintf(`rule main = ("z" @error(message=%q)) #recover(skip="!")
 rule fast = ("z" @error(message="short")) #recover(skip="!")`, message)
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parse("!",unit),active=p.parseWithOptions("!",{unit,signal:{aborted:false}});
 check(ordinary.node!==null&&active.node!==null&&ordinary.error instanceof p.SyntaxErrors&&active.error instanceof p.SyntaxErrors&&active.error.errors.length===1&&active.error.message===ordinary.error.message&&stable(active.node)===stable(ordinary.node),"aggregate recovered diagnostic parity");
 const retained=active.error.errors[0]!;
 // The matching-only difference is tiny. Allow one complete long error
 // message, while retaining the aggregate Error's second copy as work.
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","!",{unit,maxSteps:n}).error instanceof p.SyntaxErrors),limit=baseline+retained.message.length+128;
 const stopped=p.parseWithOptions("!",{unit,maxSteps:limit});check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="step-work-limit","aggregate recovered text consumes work after individual errors");
 const typed=p.parseASTWithOptions("!",{unit,maxSteps:limit});check(typed.value===null&&typed.error instanceof p.ParseInterruptionError,"interrupted recovery exposes no typed result");
 const retry=p.parseWithOptions("!",{unit,maxSteps:0});check(retry.node!==null&&retry.error instanceof p.SyntaxErrors&&retry.error.message===ordinary.error.message&&retained.message===ordinary.error.errors[0]!.message,"retained recovery and fresh retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSWorkControlDiagnosticParity(t *testing.T) {
	for _, width := range []int{1, 2, 3, 7, 8, 9, 127, 128, 129} {
		alternatives := make([]string, width)
		for i := range alternatives {
			alternatives[i] = fmt.Sprintf("%q", []string{"é", "𐀀", "\uffff", "a"}[(width-i)%4]+fmt.Sprint(i))
		}
		src := `rule main = "é\n" (` + strings.Join(alternatives, " / ") + `)
 rule custom = "é\n" (("a" @error(message="one")) / ("b" @error(message="two")))`
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes])for(const rule of ["main","custom"]) {
 const ordinary=p.parseRule(rule,"é\n!",unit),active=p.parseRuleWithOptions(rule,"é\n!",{unit,signal:{aborted:false}});
 check(ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&ordinary.error.message===active.error.message&&stable(active.error)===stable(ordinary.error)&&active.error.line===2&&active.error.col===1,"diagnostic odd-width Unicode sort and message order parity");
 if(rule==="custom")check(stable(active.error.messages)===stable(["one","two"]),"custom messages preserve choice order");
}
`
			runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
		})
	}
}
