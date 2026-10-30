package tsgen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestGeneratedTSBudgetControlActions(t *testing.T) {
	t.Parallel()
	t.Run("comparison-and-predicate", func(t *testing.T) {
		key := strings.Repeat("a", 1600)
		src := `type Box = { Flag bool }
 rule main Box = x:item {$x.Flag} => $x
 rule fast = "z"
 rule item Box = x:"z" => Box{Flag:("` + key + `"=="` + key + `") && (true || (1/(len($x)-1)>0))}`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes])for(const input of ["z",new Uint8Array([122])]) {
 const normal=p.parse(input,unit),typed=p.parseAST(input,unit);
 check(normal.error===null&&typed.error===null&&typed.value?.Flag===true,"short-circuit RHS division remains unevaluated");
 const active=p.parseWithOptions(input,{unit,signal:{aborted:false}}),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(active.error===null&&stable(active.node)===stable(normal.node)&&stable(converted.value)===stable(typed.value),"controlled action values match ordinary values");
 check(p.parseRuleWithOptions("fast",input,{unit,budget:128}).error===null,"small match fits before action-string spend");
 for(const error of [p.parseWithOptions(input,{unit,budget:128}).error,p.parseASTWithOptions(input,{unit,budget:128}).error,p.recognizeWithOptions(input,{unit,budget:128})])check(error instanceof p.ParseInterruptionError&&error.kind==="budget-limit"&&error.requested===128,"predicate-dependent action equality consumes bounded chunks");
 let polls=0;const cause={marker:"action"};
 const stopped=p.parseWithOptions(input,{unit,signal:{get aborted(){return ++polls>=3},reason:cause}});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="canceled"&&stopped.error.cause===cause,"cancellation during action equality");
 check(p.parse(input,unit).error===null,"fresh retry after action interruption");
}
`
		runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true}, "")
	})
	t.Run("lists-and-folds", func(t *testing.T) {
		src := `type Box = {
    Items []node[string]
    Left  int
    Right int
}

rule main Box = x:"z"* => Box{Items: concat(map($x, (i) => i), list(), $x), Left: foldl(0, $x, (a, i) => a + 1), Right: foldr(0, $x, (a, i) => a + 1)}

rule fast = x:"z"* => $x`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="z".repeat(1200),normal=p.parse(input,unit),typed=p.parseAST(input,unit);
 check(normal.error===null&&typed.error===null&&typed.value?.Items?.length===2400&&typed.value.Left===1200&&typed.value.Right===1200,"list/map/fold ordinary semantics");
 const active=p.parseWithOptions(input,{unit,signal:{aborted:false}}),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(active.error===null&&converted.error===null&&stable(active.node)===stable(normal.node)&&stable(converted.value)===stable(typed.value),"controlled fused concat/map/folds preserve values");
 function fits(n:number):boolean{return p.parseRuleWithOptions("fast",input,{unit,budget:n}).error===null}
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<1000000,"bounded matching budget search")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 const limited=p.parseWithOptions(input,{unit,budget:hi});
 check(limited.node===null&&limited.error instanceof p.ParseInterruptionError&&limited.error.kind==="budget-limit","collection spend does not reset matching budget");
 let polls=0;check(p.parseRuleWithOptions("fast",input,{unit,signal:{get aborted(){polls++;return false}}}).error===null,"observe deterministic baseline checkpoints");
 const cancelAt=polls+1;polls=0;
 const stopped=p.parseWithOptions(input,{unit,signal:{get aborted(){return ++polls>=cancelAt}}});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="canceled","list operations poll after baseline matching spend");
 check(p.parseWithOptions("zz",{unit}).error===null,"fresh collection retry");
}
`
		runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true}, "")
	})
}
