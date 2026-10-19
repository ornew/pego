package engine

import (
	"strings"
	"testing"
)

func TestGeneratedTSWorkControlScalarStrings(t *testing.T) {
	key := strings.Repeat("a", 1600)
	cases := []struct{ name, action string }{
		{"length", `Box{Ok:len("` + key + `")==1600,N:len("` + key + `"),S:""}`},
		{"concatenation", `Box{Ok:true,N:0,S:"` + key + `"+"` + key + `"}`},
		{"ordering", `Box{Ok:"` + key + `x"<"` + key + `y",N:0,S:""}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			src := `type Box = { Ok bool; N int; S string }
 rule main Box = x:item {$x.Ok} => $x
 rule item Box = "z" => ` + tt.action + `
rule fast = "z"`
			consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes])for(const input of ["z",new Uint8Array([122])]) {
 const ordinary=p.parse(input,unit),typed=p.parseAST(input,unit);
 check(ordinary.error===null&&typed.error===null&&typed.value?.Ok===true,"ordinary scalar value");
 const active=p.parseWithOptions(input,{unit,signal:{aborted:false}}),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(active.error===null&&converted.error===null&&stable(active.node)===stable(ordinary.node)&&stable(converted.value)===stable(typed.value),"controlled scalar value parity");
 check(p.parseRuleWithOptions("fast",input,{unit,maxSteps:128}).error===null,"matching-only work fits");
 for(const error of [p.parseWithOptions(input,{unit,maxSteps:128}).error,p.parseASTWithOptions(input,{unit,maxSteps:128}).error,p.recognizeWithOptions(input,{unit,maxSteps:128})])check(error instanceof p.ParseInterruptionError&&error.kind==="step-work-limit","scalar visits consume cumulative step budget");
 check(p.parseWithOptions(input,{unit,maxInputWork:128}).error===null,"action strings do not consume input-work budget");
 let polls=0;const cause={marker:"scalar"};
 const canceled=p.parseWithOptions(input,{unit,signal:{get aborted(){return ++polls>=4},reason:cause}});
 check(canceled.node===null&&canceled.error instanceof p.ParseInterruptionError&&canceled.error.kind==="canceled"&&canceled.error.cause===cause,"scalar cancellation retains cause and no partial result");
 check(p.parseWithOptions(input,{unit,maxSteps:0}).error===null,"fresh scalar retry");
}
`
			// Recognition needs the scalar value through the predicate dependency. The
			// concatenation case checks its string field in the predicate as well.
			if tt.name == "concatenation" {
				src = strings.Replace(src, "{$x.Ok}", "{$x.S != \"\"}", 1)
			}
			runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true, Recognize: true}, "")
		})
	}
	t.Run("source-text", func(t *testing.T) {
		src := `type Box = {
    Ok bool
    N  int
    S  string
}

rule main Box = x:item {$x.S != ""} => $x

rule item Box = x:source => Box{Ok: true, N: 0, S: text($x)}

rule fast = x:source => $x

rule source = "a"*`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="a".repeat(1200),normal=p.parse(input,unit),typed=p.parseAST(input,unit);
 const active=p.parseWithOptions(input,{unit,signal:{aborted:false}}),converted=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(normal.error===null&&typed.error===null&&active.error===null&&converted.error===null&&stable(active.node)===stable(normal.node)&&stable(converted.value)===stable(typed.value)&&converted.value?.S===input,"source text value parity");
 function fits(n:number):boolean{return p.parseRuleWithOptions("fast",input,{unit,maxSteps:n}).error===null}
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<1000000,"finite matching budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 const limited=p.parseWithOptions(input,{unit,maxSteps:hi+128});
 check(limited.node===null&&limited.error instanceof p.ParseInterruptionError&&limited.error.kind==="step-work-limit","source materialization exceeds fixed wrapper overhead after matching");
 check(p.parseWithOptions(input,{unit,maxSteps:0}).error===null,"source materialization retry");
}
`
		runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true, Recognize: true}, "")
	})
	t.Run("unicode-and-byte-order", func(t *testing.T) {
		src := `type Box = {
    Ok    bool
    Order bool
    N     int
    S     string
}

rule main Box = x:item {$x.Ok && ($x.Order || !$x.Order) && $x.N > 0 && $x.S != ""} => $x

rule item Box = x:.+ => Box{Ok: "𐀀" > "\u{ffff}" && "é" <= "é" && "é" >= "é", Order: text($x) > "é𐀀𐀀", N: len(text($x)), S: text($x) + "!"}`
		consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input=new Uint8Array([0xc3,0xa9,0xf0,0x90,0x80,0x80,0xff]);
 const ordinary=p.parseAST(input,unit),active=p.parseASTWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.error===null&&active.error===null&&stable(active.value)===stable(ordinary.value),"scalar Unicode and escaped-byte parity");
 check(active.value?.Ok===true&&active.value.N===(unit===p.Bytes?7:3),"UTF-8 order and byte-escape length");
 check(active.value?.Order===(unit===p.Bytes),"raw invalid byte versus replacement scalar ordering");
 check(active.value?.S===(unit===p.Bytes?"é𐀀\udcff!":"é𐀀�!"),"concatenation preserves decoding semantics");
 check(p.recognizeWithOptions(input,{unit,signal:{aborted:false}})===null,"recognition uses required scalar actions");
}
`
		runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true, Recognize: true}, "")
	})
}

func TestGeneratedTSScalarIteratorNamespace(t *testing.T) {
	src := `type Generator = { Text string }

rule main Generator = t:("z") #atomic => Generator{Text: text($t)}`
	consumer := `import {parseAST} from "./parser.ts";
 import type {Generator} from "./parser.ts";
` + tsTypedAssert + `
 const value: Generator|null=parseAST("z").value;
 check(value?.type==="Generator"&&stable(value).includes("Generator"),"existing Generator export remains usable");`
	runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Types: true}, "")
}
