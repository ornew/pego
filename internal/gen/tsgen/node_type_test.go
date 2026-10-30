package tsgen_test

import (
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedTSTypedNamedNodeTypes(t *testing.T) {
	t.Parallel()
	consumer := `import { parseAST } from "./parser.ts";
import type { Num, Ident, Pair, Nums, Any, Expr } from "./types.ts";
` + tsTypedAssert + `
const r = parseAST("ab 12<1,x>[1]!bc");
check(r.error === null && r.value !== null, "parse success");
const v = r.value;
const items: readonly Expr[] = v.Items ?? [];
const seen: string[] = [];
for (const x of items) {
  if (x === null) continue;
  switch (x.type) {
    case "Num": { const n: Num = x; seen.push("Num " + n.value + " " + n.start + " " + n.end); break; }
    case "Ident": { const i: Ident = x; seen.push("Ident " + i.value + " " + i.start + " " + i.end); break; }
  }
}
check(seen.join(";") === "Ident ab 0 2;Num 12 3 5", "union members: " + seen.join(";"));
const p: Pair | null = v.Pair;
check(p !== null && p.type === "Pair" && p.value?.[0]?.type === "Num" && p.value[1]?.value === "x", "pair " + JSON.stringify(p));
const ns: Nums | null = v.Nums;
check(ns !== null && ns.type === "Nums" && ns.value?.length === 1 && ns.value[0]?.type === "Num", "nums");
const a: Any | null = v.Any;
check(a !== null && a.type === "Any", "any " + stable(a));
`
	runTypedTSFixture(t, gentest.NodeTypeGrammar, consumer, gen.GenOptions{}, "")
}

// TestGeneratedTSTypedNamedNodesAsPlainNodes is TestGeneratedNamedNodesAsPlainNodes for typed
// TypeScript.
func TestGeneratedTSTypedNamedNodesAsPlainNodes(t *testing.T) {
	t.Parallel()
	const src = `type Num = node[string]
type Nums = node[[]Num]
type P = node[(Num, Num)]
type O = { C *node[string]; D []node[string]; E *Nums; F node[(node[string], node[string])]; G []Num; H *P }
rule main O = ns:nums p:pair $$ => O{C: $ns.Value[0], D: $ns.Value, E: $ns, F: $p, G: $ns.Value, H: $p}
rule nums Nums = "[" ns:num* "]" => Nums{$ns}
rule num Num = [0-9] #atomic
rule pair P = "<" t:(num num) ">" => P{$t}
`
	consumer := `import { parseAST } from "./parser.ts";
` + tsTypedAssert + `
const v = parseAST("[12]<34>").value;
check(v !== null, "parse");
const got = [v.C?.value, v.D?.map(d => d?.value).join(","), v.E?.value?.[0]?.value, v.E?.value?.[0] === v.G?.[0],
  v.F?.value?.[0]?.value, v.F?.value?.[1]?.value, v.G?.[1]?.end, v.H?.value?.[0]?.value, v.H?.type].join(" ");
check(got === "1 1,2 1 true 3 4 3 3 P", "got " + got + " " + stable(v));
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{}, "")
}
