package engine

import "testing"

func TestGeneratedTSConversionControlCheckpoints(t *testing.T) {
	src := `type Word = { Text string }
 type Bag = { Items []Word }
 rule main Bag = x:word* #cut => Bag{Items: $x.Value}
 rule word Word = t:("a") #atomic => Word{Text: text($t)}`
	runTypedTSFixture(t, src, tsConversionControlConsumer, GenOptions{Start: "main", WorkControls: true, Types: true}, "")
}

const tsConversionControlConsumer = `import * as p from "./parser.ts";
` + tsTypedAssert + `
const input="a".repeat(8192);
for(const unit of [p.CodePoints,p.Bytes]) {
 let parsePolls=0;
 const node=p.parseWithOptions(input,{unit,signal:{get aborted(){parsePolls++;return false}}});
 check(node.node!==null&&node.error===null,"Node baseline");
 let typedPolls=0;
 const saved=p.parseASTWithOptions(input,{unit,signal:{get aborted(){typedPolls++;return false}}});
 check(saved.value!==null&&saved.error===null&&saved.value.Items?.length===8192,"typed baseline");
 // Parsing has the same checkpoints. More than one extra typed poll ensures
 // the selected poll is in conversion, before final invocation publication.
 check(typedPolls>parsePolls+1,"conversion has distinct cooperative checkpoints");
 const old=stable(saved.value);
 const reason={phase:"conversion"};
 let polls=0;
 const canceled=p.parseASTWithOptions(input,{unit,signal:{get aborted(){return ++polls>parsePolls},reason}});
 check(canceled.value===null&&canceled.error instanceof p.ParseInterruptionError&&canceled.error.kind==="canceled"&&canceled.error.cause===reason,"conversion cancellation withholds value");
 check(polls===parsePolls+1,"conversion cancellation poll");
 const token={phase:"conversion-getter-panic"};
 polls=0;let threw=false;
 try { p.parseASTWithOptions(input,{unit,signal:{get aborted(){if(++polls>parsePolls)throw token;return false}}}); }
 catch(e){threw=true;check(e===token,"conversion caller exception identity")}
 check(threw&&polls===parsePolls+1,"exception raised inside conversion");
 const retry=p.parseAST(input,unit);
 check(retry.error===null&&stable(retry.value)===old&&stable(saved.value)===old,"retry and retained output ownership");
}
`
