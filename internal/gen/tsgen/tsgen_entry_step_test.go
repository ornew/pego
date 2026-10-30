package tsgen_test

import (
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// A rule call costs one step at its entry. A rule that reads no captures and takes callBody's unmemoized
// or first-call branch (the start rule called through p.call, a left-recursive rule, a memoized rule)
// runs its body without a capture frame; the call charges the step there once, and the plain
// evaluation of the body does not charge it again.
func TestGeneratedTSRuleEntryChargesOneStep(t *testing.T) {
	t.Parallel()
	src := `rule main = expr $$
rule expr = expr "+" "a" / "a"`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	out := gentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
const proto = (p as unknown as { Parser: { prototype: { stepWork(n?: number): void } } }).Parser.prototype;
const original = proto.stepWork;
const by = new Map<string, number>();
proto.stepWork = function (this: unknown, n?: number): void {
  const frame = (new Error().stack ?? "").split("\n")[2] ?? "";
  const name = /Parser\.(\w+)/.exec(frame)?.[1] ?? frame.trim();
  by.set(name, (by.get(name) ?? 0) + 1);
  original.call(this, n);
};
const r = p.parseWithOptions("a+a+a", { unit: p.CodePoints, maxWork: 100000 });
if (r.error !== null) throw new Error("parse failed");
console.log((by.get("callBody") ?? 0) > 0 ? by.get("invokePlainBody") ?? 0 : "no call charged");
`, gen.GenOptions{Start: "main", WorkControls: true})
	if out != "0\n" {
		t.Fatalf("the plain evaluation of a rule body charged %q steps under a call that charged its entry", out)
	}
}
