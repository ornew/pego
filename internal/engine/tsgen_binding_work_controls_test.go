package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestGeneratedTSWorkControlBindings(t *testing.T) {
	t.Parallel()
	key := strings.Repeat("a", 1600)
	longName := strings.Repeat("v", 1600)
	cases := []struct {
		name, src, baseline string
		isolated            bool
	}{
		{"head-equal", `type Box = { S string }
 var v string
 rule main Box = {v := "` + key + `"} {v := "` + key + `"} {v != ""} "z" => Box{S:v}
 rule fast = {v := "` + key + `"} "z"`, "fast", false},
		{"head-different", `type Box = { S string }
 var v string
 rule main Box = {v := "` + key + `x"} {v := "` + key + `y"} {v != ""} "z" => Box{S:v}
 rule fast = {v := "` + key + `x"} "z"`, "fast", false},
		{"nonhead-equal", `type Box = { S string }
 var v string
 var other bool
 rule main Box = {v := "` + key + `"} {other := true} {v := "` + key + `"} {v != ""} "z" => Box{S:v}
 rule fast = {v := "` + key + `"} {other := true} "z"`, "fast", false},
		{"nonhead-different", `type Box = { S string }
 var v string
 var other bool
 rule main Box = {v := "` + key + `x"} {other := true} {v := "` + key + `y"} {v != ""} "z" => Box{S:v}
 rule fast = {v := "` + key + `x"} {other := true} "z"`, "fast", false},
		{"identifier-hashing", `var ` + longName + ` int
 rule main = {` + longName + ` := 1} "z"
 rule fast = "z"`, "fast", false},
		{"identifier-lookup", `type Box = { N int }
 var ` + longName + ` int
 rule main Box = {` + longName + ` := 1} {` + longName + ` == 1} "z" => Box{N:` + longName + `}
 rule fast = {` + longName + ` := 1} "z"`, "fast", false},
		// The first helper call skips memoization, and silent memo entries cannot
		// serve the normal call. Both routes therefore perform six lookups.
		{"environment-snapshot", `type Box = { N int }
 var ` + longName + ` int
 rule main Box = {` + longName + ` := 1} &helper &helper x:helper => $x
 rule helper Box = {` + longName + ` == 1} letter => Box{N:` + longName + `}
 rule inline Box = {` + longName + ` := 1} ` + strings.Repeat("{"+longName+" == 1} ", 5) + `letter => Box{N:` + longName + `}
 rule letter = "z"`, "inline", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite binding budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
for(const unit of [p.CodePoints,p.Bytes])for(const input of ["z",new Uint8Array([122])]) {
 const ordinary=p.parse(input,unit),typed=p.parseAST(input,unit);
 const active=p.parseWithOptions(input,{unit,signal:{aborted:false}}),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.error===null&&typed.error===null&&active.error===null&&converted.error===null&&stable(active.node)===stable(ordinary.node)&&stable(converted.value)===stable(typed.value),"binding and environment value parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("` + tt.baseline + `",input,{unit,maxSteps:n}).error===null);
 const limit=baseline+128;
 const stopped=p.parseWithOptions(input,{unit,maxSteps:limit});
 check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="step-work-limit","` + tt.name + ` work exceeds baseline");
 check(p.parseASTWithOptions(input,{unit,maxSteps:limit}).error instanceof p.ParseInterruptionError,"typed routes share binding budget");
 check(p.parseWithOptions(input,{unit,maxInputWork:128}).error===null,"identifier and binding values use step work only");
 check(p.parseASTWithOptions(input,{unit,maxSteps:0}).error===null,"fresh binding retry");
}
`
			if !tt.isolated {
				consumer += `const stop=p.recognizeWithOptions("z",{maxSteps:128});check(stop instanceof p.ParseInterruptionError&&stop.kind==="step-work-limit","predicate-dependent recognition counts binding work");`
			}
			runTypedTSFixture(t, tt.src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
		})
	}

	t.Run("scope-and-rollback", func(t *testing.T) {
		src := `type Box = {
    N int
}

var v int
var flag bool
rule main Box = {v := 1} (changing "x" / {v == 1} "z") => Box{N: v}

rule changing = {v := 2} {flag := true} {v := 3} {v == 3} "z"`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parseAST("z",unit),active=p.parseASTWithOptions("z",{unit,signal:{aborted:false}});
 check(ordinary.error===null&&active.error===null&&stable(ordinary.value)===stable(active.value)&&active.value?.N===1,"callee shadowing and failed-branch rollback preserve caller value");
 let polls=0;const cause={marker:"bindings"};
 const canceled=p.parseWithOptions("z",{unit,signal:{get aborted(){return ++polls>=2},reason:cause}});
 check(canceled.node===null&&canceled.error instanceof p.ParseInterruptionError&&canceled.error.kind==="canceled"&&canceled.error.cause===cause,"binding cancellation does not publish partial node");
 check(p.parseASTWithOptions("z",{unit,maxSteps:0}).value?.N===1,"fresh caller environment after interruption");
}
`
		runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", WorkControls: true, Types: true, Recognize: true}, "")
	})
}
