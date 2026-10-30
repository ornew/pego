package tsgen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

const tsDiagnosticMinimum = `function minimum(fits:(n:number)=>boolean):number {
 let lo=0,hi=1;while(!fits(hi)){hi*=2;check(hi<10000000,"finite diagnostic budget")}
 while(lo+1<hi){const mid=Math.floor((lo+hi)/2);if(fits(mid))hi=mid;else lo=mid}
 return hi;
}
`

func TestGeneratedTSBudgetControlDiagnosticLineScan(t *testing.T) {
	t.Parallel()
	src := `rule main = "z" #repeat(2048) "!"

rule early = "!"`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const input="z".repeat(2048),ordinary=p.parse(input,unit),active=p.parseWithOptions(input,{unit,signal:{aborted:false}});
 check(ordinary.node===null&&ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&stable(active.error)===stable(ordinary.error)&&active.error.message===ordinary.error.message&&active.error.line===1&&active.error.col===2049,"diagnostic line scan parity");
 // A failure at the start of the input costs at least one unit per character: the preparation of the
 // input and the line scan together; TestGeneratedTSBudgetControlDiagnosticLineScanPerRoute isolates the scan.
 const failing=(text:string)=>minimum(n=>p.parseRuleWithOptions("early",text,{unit,budget:n}).error instanceof p.SyntaxError);
 check(failing(input)>=failing("z")+input.length-1,"diagnostic line reads consume budget beyond matching");
 const stopped=p.parseRuleWithOptions("early",input,{unit,budget:failing(input)-1}).error;
 check(stopped instanceof p.ParseInterruptionError&&stopped.kind==="budget-limit","the line scan stops one unit short of its budget");
 const retry=p.parseWithOptions(input,{unit,budget:0});check(retry.node===null&&retry.error instanceof p.SyntaxError&&retry.error.message===ordinary.error.message,"fresh line diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlDiagnosticMessage(t *testing.T) {
	t.Parallel()
	message := strings.Repeat("details λ; ", 256)
	src := fmt.Sprintf(`rule main = "z" @error(message=%q)
 rule fast = "z" @error(message="short")`, message)
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + tsDiagnosticMinimum + `
for(const unit of [p.CodePoints,p.Bytes]) {
 const ordinary=p.parse("x",unit),active=p.parseWithOptions("x",{unit,signal:{aborted:false}});
 check(ordinary.error instanceof p.SyntaxError&&active.error instanceof p.SyntaxError&&active.error.message===ordinary.error.message&&stable(active.error)===stable(ordinary.error),"long custom diagnostic parity");
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","x",{unit,budget:n}).error instanceof p.SyntaxError),limit=baseline+128;
 for(const error of [p.parseWithOptions("x",{unit,budget:limit}).error,p.parseASTWithOptions("x",{unit,budget:limit}).error,p.recognizeWithOptions("x",{unit,budget:limit})])check(error instanceof p.ParseInterruptionError&&error.kind==="budget-limit","custom diagnostic text consumes cumulative spend");
 const retry=p.parseWithOptions("x",{unit,budget:0});check(retry.error instanceof p.SyntaxError&&retry.error.message===ordinary.error.message,"fresh custom diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlDiagnosticSort(t *testing.T) {
	t.Parallel()
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
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","",{unit,budget:n}).error instanceof p.SyntaxError);
 // Matching and expected-ID deduplication are shared. Allow the complete
 // expected text and list movement, but not all common-prefix comparisons.
 const limit=baseline+64*2100+128;
 for(const error of [p.parseWithOptions("",{unit,budget:limit}).error,p.parseASTWithOptions("",{unit,budget:limit}).error,p.recognizeWithOptions("",{unit,budget:limit})])check(error instanceof p.ParseInterruptionError&&error.kind==="budget-limit","expected-label comparisons consume spend beyond rendering");
 check(p.parseWithOptions("",{unit,budget:0}).error instanceof p.SyntaxError,"fresh sorted diagnostic retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlRecoveredMessage(t *testing.T) {
	t.Parallel()
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
 // message, while retaining the aggregate Error's second copy as spend.
 const baseline=minimum(n=>p.parseRuleWithOptions("fast","!",{unit,budget:n}).error instanceof p.SyntaxErrors),limit=baseline+retained.message.length+128;
 const stopped=p.parseWithOptions("!",{unit,budget:limit});check(stopped.node===null&&stopped.error instanceof p.ParseInterruptionError&&stopped.error.kind==="budget-limit","aggregate recovered text consumes spend after individual errors");
 const typed=p.parseASTWithOptions("!",{unit,budget:limit});check(typed.value===null&&typed.error instanceof p.ParseInterruptionError,"interrupted recovery exposes no typed result");
 const retry=p.parseWithOptions("!",{unit,budget:0});check(retry.node!==null&&retry.error instanceof p.SyntaxErrors&&retry.error.message===ordinary.error.message&&retained.message===ordinary.error.errors[0]!.message,"retained recovery and fresh retry");
}
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true}, "")
}

func TestGeneratedTSBudgetControlDiagnosticParity(t *testing.T) {
	t.Parallel()
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
			runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true}, "")
		})
	}
}

// On each route the diagnostic of a failure reads the whole input once, to find the line and column of
// the error. The spend charged inside makeError, measured per route, is at least the length of the input
// and grows with it, whatever else the route charges, and a budget one unit short of the whole failure
// interrupts it.
func TestGeneratedTSBudgetControlDiagnosticLineScanPerRoute(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = "z"+ "!"`)
	if err != nil {
		t.Fatal(err)
	}
	out := gentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
type Hooked = { makeError(...a: unknown[]): unknown; control: { budgetUsed: number } | null };
const proto = (p as unknown as { Parser: { prototype: Hooked } }).Parser.prototype;
const original = proto.makeError;
let scanned = -1;
proto.makeError = function (this: Hooked, ...a: unknown[]) {
  const before = this.control?.budgetUsed ?? 0;
  try { return original.apply(this, a); } finally { scanned = (this.control?.budgetUsed ?? 0) - before; }
};
const routes: [string, (text: string, n: number) => Error | null][] = [
  ["parse", (text, n) => p.parseWithOptions(text, { budget: n }).error],
  ["parseAST", (text, n) => p.parseASTWithOptions(text, { budget: n }).error],
  ["recognize", (text, n) => p.recognizeWithOptions(text, { budget: n })],
];
function fail(why: string): never { throw new Error(why); }
for (const [name, run] of routes) {
  const charged = (len: number): number => {
    scanned = -1;
    if (!(run("z".repeat(len), Number.MAX_SAFE_INTEGER) instanceof p.SyntaxError)) fail(name + " does not fail at the end");
    return scanned;
  };
  const short = charged(256), long = charged(2048);
  if (short < 256 || long < 2048 || long - short < 2048 - 256) fail(name + " charges " + short + " then " + long + " for the line scan");
  const input = "z".repeat(512);
  let lo = 0, hi = 1;
  while (run(input, hi) instanceof p.ParseInterruptionError) { lo = hi; hi *= 2; }
  while (lo + 1 < hi) { const mid = Math.floor((lo + hi) / 2); if (run(input, mid) instanceof p.ParseInterruptionError) lo = mid; else hi = mid; }
  const stopped = run(input, hi - 1);
  if (!(stopped instanceof p.ParseInterruptionError) || stopped.kind !== "budget-limit") fail(name + " does not stop one unit short of its budget");
  if (hi < 512) fail(name + " needs only " + hi + " for 512 characters");
}
console.log("ok");
`, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true})
	if out != "ok\n" {
		t.Fatalf("got %q", out)
	}
}
