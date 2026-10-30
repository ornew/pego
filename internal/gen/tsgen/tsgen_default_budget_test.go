package tsgen_test

import (
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// A budget of zero selects the default of max(2^20, 1024 times the input length) in the position unit,
// for every entry point, and Unlimited disables the limit. The grammar scans the rest of the input from
// every position of a scan from every position, so its cost grows with the cube of the input length.
func TestGeneratedTSDefaultBudget(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = (. &mid)* !.
rule mid = (. &.*)*`)
	if err != nil {
		t.Fatal(err)
	}
	out := gentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
declare const process: { exit(code: number): never };
function fail(why: string): never { console.log(why); return process.exit(0); }
const limitOf = (e: unknown): number => e instanceof p.ParseInterruptionError && e.kind === "budget-limit" ? e.limit : -1;
const want = 1 << 20;
const short = "a".repeat(300);
if (limitOf(p.parse(short).error) !== want) fail("parse default " + p.parse(short).error);
if (limitOf(p.parseRule("", short).error) !== want) fail("parseRule default");
if (limitOf(p.recognize(short)) !== want) fail("recognize default");
if (limitOf(p.parseAST(short).error) !== want) fail("parseAST default");
if (limitOf(p.parseWithOptions(short, {}).error) !== want) fail("parseWithOptions default");
if (limitOf(p.parseWithOptions(short, { budget: 0 }).error) !== want) fail("zero budget default");
if (limitOf(p.parseWithOptions(short, { budget: 1000 }).error) !== 1000) fail("explicit budget");
const long = "a".repeat(5000);
if (limitOf(p.parseWithOptions(long, { unit: p.Bytes }).error) !== 5000 * 1024) fail("default scales with the input: " + limitOf(p.parseWithOptions(long, { unit: p.Bytes }).error));
const wide = "\u00e9".repeat(2000);
if (limitOf(p.parseWithOptions(wide, { unit: p.Bytes }).error) !== 4000 * 1024) fail("bytes of a string");
if (limitOf(p.parseWithOptions(wide, { unit: p.CodePoints }).error) !== 2000 * 1024) fail("code points of a string");
if (limitOf(p.parseWithOptions(new TextEncoder().encode(wide), { unit: p.CodePoints }).error) !== 2000 * 1024) fail("code points of bytes");
if (p.Unlimited !== Number.MAX_SAFE_INTEGER) fail("Unlimited");
console.log("ok");
`, gen.GenOptions{Start: "main", BudgetControls: true, Types: true, Recognize: true})
	if out != "ok\n" {
		t.Fatalf("got %q", out)
	}
}

// Unlimited needs no limit: the parser runs to the end, and a parser generated without budget controls
// accepts it too.
func TestGeneratedTSUnlimitedBudget(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = "a"+ !.`)
	if err != nil {
		t.Fatal(err)
	}
	for _, controls := range []bool{true, false} {
		out := gentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
const options = { budget: Number.MAX_SAFE_INTEGER } as p.ParseOptions;
const r = p.parseWithOptions("a".repeat(100), options);
if (r.error !== null || r.node === null) throw new Error("unlimited parse failed: " + r.error);
console.log("ok");
`, gen.GenOptions{Start: "main", BudgetControls: controls, Types: true, Recognize: true})
		if out != "ok\n" {
			t.Fatalf("controls=%v: got %q", controls, out)
		}
	}
}
