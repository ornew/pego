package tsgen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestGeneratedTSWorkControlFields(t *testing.T) {
	t.Parallel()
	const width = 48
	names := make([]string, width)
	declarations := make([]string, width)
	values := make([]string, width)
	captures := make([]string, width)
	for i := range names {
		names[i] = "Field" + strings.Repeat("a", 200) + fmt.Sprintf("%02d", i)
		declarations[i] = names[i] + " int"
		values[i] = names[i] + ":1"
		captures[i] = "capture" + strings.Repeat("a", 200) + fmt.Sprintf("%02d", i) + `:"z"`
	}
	src := `type Box = { ` + strings.Join(declarations, ";") + ` }
 rule main Box = x:item {$x.` + names[width-1] + ` == 1} => $x
 rule item Box = "z" => Box{` + strings.Join(values, ",") + `}
 rule fast = "z"`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite field budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
for(const unit of [p.CodePoints,p.Bytes]) {
 const normal=p.parse("z",unit),typed=p.parseAST("z",unit);
 const active=p.parseWithOptions("z",{unit,signal:{aborted:false}}),converted=p.parseASTWithOptions("z",{unit,signal:{aborted:false}});
 check(normal.error===null&&typed.error===null&&active.error===null&&converted.error===null&&stable(active.node)===stable(normal.node)&&stable(converted.value)===stable(typed.value),"field construction, lookup and typed conversion parity");
 check(p.parseRuleWithOptions("fast","z",{unit,maxSteps:4096}).error===null,"matching fits before field construction");
 const construction=p.parseRuleWithOptions("item","z",{unit,maxSteps:4096});
 check(construction.node===null&&construction.error instanceof p.ParseInterruptionError&&construction.error.kind==="step-work-limit","field construction scans consume budget");
 for(const error of [p.parseWithOptions("z",{unit,maxSteps:4096}).error,p.parseASTWithOptions("z",{unit,maxSteps:4096}).error,p.recognizeWithOptions("z",{unit,maxSteps:4096})])check(error instanceof p.ParseInterruptionError&&error.kind==="step-work-limit","field-dependent parse routes consume budget");
 const itemBudget=minimum(n=>p.parseRuleWithOptions("item","z",{unit,maxSteps:n}).error===null);
 const lookup=p.parseWithOptions("z",{unit,maxSteps:itemBudget+128});
 check(lookup.node===null&&lookup.error instanceof p.ParseInterruptionError&&lookup.error.kind==="step-work-limit","member lookup charges beyond construction");
 const parseBudget=minimum(n=>p.parseWithOptions("z",{unit,maxSteps:n}).error===null);
 check(p.parseWithOptions("z",{unit,maxSteps:parseBudget+1024}).error===null,"same budget finishes parse");
 const conversion=p.parseASTWithOptions("z",{unit,maxSteps:parseBudget+1024});
 check(conversion.value===null&&conversion.error instanceof p.ParseInterruptionError&&conversion.error.kind==="step-work-limit","typed field scans share cumulative budget");
 let polls=0;check(p.parseWithOptions("z",{unit,signal:{get aborted(){polls++;return false}}}).error===null,"observe parse checkpoints");
 const cancelAt=polls+1;polls=0;
 const canceled=p.parseASTWithOptions("z",{unit,signal:{get aborted(){return ++polls>=cancelAt},reason:"fields"}});
 check(canceled.value===null&&canceled.error instanceof p.ParseInterruptionError&&canceled.error.kind==="canceled"&&canceled.error.cause==="fields","typed conversion cancellation exposes no partial value");
 check(p.parseWithOptions("z",{unit,maxInputWork:128}).error===null,"field names consume step work only");
 check(p.parseASTWithOptions("z",{unit,maxSteps:0}).error===null,"fresh retry after field interruption");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")

	t.Run("capture-attachment", func(t *testing.T) {
		grammar := `rule main = ` + strings.Join(captures, " ") + `
rule fast = "z" #repeat(48)`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="z".repeat(48),ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.error===null&&active.error===null&&stable(active.node)===stable(ordinary.node)&&active.node?.fields.length===48,"capture attachment parity");
 check(p.parseRuleWithOptions("fast",input,{unit,maxSteps:4096}).error===null,"matching-only capture baseline fits");
 const stopped=p.parseWithOptions(input,{unit,maxSteps:4096});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="step-work-limit","capture attachment scans charge after matching");
 check(p.parseWithOptions(input,{unit,maxSteps:0}).error===null,"capture attachment fresh retry");
}
`
		runTypedTSFixture(t, grammar, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true}, "")
	})
}
