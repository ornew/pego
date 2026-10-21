package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func runTypedTSFixture(t *testing.T, src, consumer string, opts GenOptions, keep string) {
	runTypedTSFixtureWithLib(t, src, consumer, opts, keep, "es2020")
}

func runTypedTSFixtureWithLib(t *testing.T, src, consumer string, opts GenOptions, keep, lib string) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated TypeScript consumers")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	opts.Types = true
	if opts.Start == "" {
		opts.Start = "main"
	}
	files, err := GenerateTS(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := GenerateTS(g, opts)
	if err != nil || !slices.EqualFunc(files, again, func(a, b GeneratedFile) bool { return a.Path == b.Path && string(a.Content) == string(b.Content) }) {
		t.Fatal("typed generation is not deterministic", err)
	}
	dir := t.TempDir()
	if keep != "" {
		dir = keep
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTSFiles(t, dir, files)
	if keep != "" {
		reference := opts
		reference.Types = false
		plain, err := GenerateTS(g, reference)
		if err != nil {
			t.Fatal(err)
		}
		writeTSFiles(t, filepath.Join(dir, "untyped"), plain)
	}
	if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--target", "es2020", "--lib", lib, "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--declaration", "--emitDeclarationOnly", "--outDir", "declarations", "parser.ts", "consumer.ts"}
	cmd := exec.Command(tsc, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict declaration consumer: %v\n%s", err, out)
	}
	decl, err := os.ReadFile(filepath.Join(dir, "declarations/parser.d.ts"))
	if err != nil || !strings.Contains(string(decl), "export declare function parseAST(") || !strings.Contains(string(decl), "export declare function parseASTWithOptions(") {
		t.Fatalf("typed declaration exports missing: %v", err)
	}
	cmd = exec.Command(node, nodeTSArgs("consumer.ts")...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed Node consumer: %v\n%s", err, out)
	}
}

const tsTypedAssert = `function check(x: unknown, why: string): asserts x { if (!x) throw new Error(why); }
function stable(x: unknown): string { return JSON.stringify(x, (_key, v: unknown) => typeof v === "bigint" ? "int:" + v : v); }
`

func TestGeneratedTSTypedConsumers(t *testing.T) {
	src := `type Word = { Text string }
type W ~ Word
type Words ~ []Word
type Optional ~ *Word
type Integer ~ int
type Text ~ string
type Flag ~ bool
type MaybeInteger ~ *int
type Choice ~ Word | node[string]
type ChoiceAgain ~ node[string] | Word
type Mixed ~ Word | int
type Open ~ _
type Raw ~ node[_]
type R = { W W; Items Words; Missing Words; Empty Words; Optional Optional; I Integer; Big Integer; S Text; B Flag; OpenItems []_; O MaybeInteger; M Mixed; U Choice; Any Open; Raw Raw }
rule main R = w:word xs:word* raw:("!"? "?"?) => R{W: $w, Items: $xs.Value, OpenItems: $xs.Value, Empty: list(), I: -7 / 2, Big: 9223372036854775807, S: $w.Text, B: true, M: 9007199254740992, U: $w, Any: $raw, Raw: $raw}
rule word Word = t:([a-zあ-ん😀]+) #atomic => Word{Text: text($t)}`
	consumer := `import { parse, parseAST, parseASTWithOptions, CodePoints, Bytes, Node } from "./parser.ts";
import type { R, W, Words, Optional, Integer, Text, Flag, MaybeInteger, Choice, ChoiceAgain, Mixed, Open, Raw, TypedParseResult } from "./types.ts";
` + tsTypedAssert + `
const r: TypedParseResult<R | null> = parseAST("😀あ!?");
check(r.error === null && r.value !== null, "parse success");
const v = r.value;
const types: [W, Words, Optional, Integer, Text, Flag, MaybeInteger, Choice, ChoiceAgain, Mixed, Open, Raw] = [v.W, v.Items, v.Optional, v.I, v.S, v.B, v.O, v.U, v.U, v.M, v.Any, v.Raw];
check(types.length === 12 && v.W?.type === "Word" && v.W.Text === "😀あ", "exported types/text struct");
check(v.Items?.length === 0 && v.Empty?.length === 0 && v.Missing === null && v.Optional === null && v.O === null, "nil/empty/optionals");
check(v.I === -3 && v.Big === 9223372036854775807n && v.M === 9007199254740992n && v.S === "😀あ" && v.B, "scalar representation");
check(v.Raw !== null && !(v.Raw instanceof Node) && (v.Raw as {type?: string}).type === "tuple" && v.Any === v.Raw, "CST types convert to shared TNode values");
check(v.Items !== v.OpenItems, "list representations stay distinct");
check(v.start === 0 && v.end === 4 && v.W.end === 2, "code point spans");
const b = parseASTWithOptions("😀あ!?", {unit: Bytes, maxDepth: 0});
check(b.value?.end === 9 && b.value.W?.end === 7 && b.error === null, "byte spans/options");
const before = stable(v);
for (let i = 0; i < 10; i++) parseAST("z", CodePoints);
check(stable(v) === before, "retained result ownership");
check(parse("😀あ!?").node?.field("W") instanceof Node, "legacy Node fields unchanged");
let units = 0, depths = 0;
let nested: ReturnType<typeof parseAST> | null = null;
const o = { get unit() { units++; nested = parseAST("z"); return Bytes; }, get maxDepth() { depths++; return 0; } };
check(parseASTWithOptions("a", o).error === null && units === 1 && depths === 1, "options read once");
check((nested as ReturnType<typeof parseAST> | null)?.value?.W?.Text === "z", "reentrant getter invocation");
for (const d of [-1, 1.5, Number.NaN, Number.MAX_SAFE_INTEGER + 1]) {
  const bad = parseASTWithOptions("a", {maxDepth:d});
  check(bad.value === null && bad.error?.message.includes("non-negative safe integer"), "invalid depth");
}
check(parseAST("1").value === null && parseAST("1").error !== null, "ordinary failure");
// @ts-expect-error Converted grammar fields are readonly.
v.I = 1;
`
	runTypedTSFixture(t, src, consumer, GenOptions{}, "")
}

func TestGeneratedTSTypedNameCollisions(t *testing.T) {
	// Names of the runtime and of the generated parser are free: types.ts is a module of its own.
	// The names types.ts declares are reserved (TestGeneratedTSReservedTypes).
	names := []string{"Actx", "ASTRecipe", "Bytes", "Error", "Map", "Match", "Node", "Parser", "OperatorsRuleLine", "R0", "R0_", "Rule", "ReadonlyMap", "R", "Value", "Grammar", "ParseResult", "TraceKind", "Profile"}
	var src, imports, vals strings.Builder
	for i, name := range names {
		// Match and Error are retired node type names, which grammars cannot declare, so aliases
		// test the other runtime declarations.
		if name == "Match" || name == "Error" {
			continue
		}
		fmt.Fprintf(&src, "type %s = { Text string }\nrule n%d %s = t:(\"x\") #atomic => %s{Text: text($t)}\n", name, i, name, name)
		fmt.Fprintf(&imports, "import type { %s } from \"./types.ts\";\n", name)
		fmt.Fprintf(&vals, "const v%d: %s | null = null; check(v%d === null, %q);\n", i, name, i, name)
	}
	src.WriteString("rule main = n0\n")
	consumer := "import {parseAST} from \"./parser.ts\";\n" + imports.String() + tsTypedAssert + vals.String() + `check(parseAST("x").value?.type === "Actx", "discriminator preserves grammar name"); check(stable(parseAST("x").value).includes("Actx"), "result");`
	runTypedTSFixture(t, src.String(), consumer, GenOptions{}, "")
}

func TestGeneratedTSTypedRecovery(t *testing.T) {
	src := `type Word = { Text string }
type U ~ Word | node[string] | error
type Mixed ~ Word | int | error
type WE ~ Word | error
type R = { Specific WE; U U; M Mixed; A _; Items []WE; OpenItems []_ }
rule main R = w:word #recover(skip=[^;]+ ";") xs:word #recover(skip=[^;]+ ";")* => R{Specific: $w, U: $w, M: $w, A: $w, Items: $xs.Value, OpenItems: $xs.Value}
rule word Word = t:([a-z]+ ";") #atomic => Word{Text: text($t)}`
	consumer := `import {parse, parseAST, SyntaxErrors, Bytes, CodePoints} from "./parser.ts";
` + tsTypedAssert + `
for (const unit of [CodePoints, Bytes]) {
  const r = parseAST("1;abc;2;", unit), n = parse("1;abc;2;", unit);
  check(r.error instanceof SyntaxErrors && n.error instanceof SyntaxErrors && r.error.message === n.error.message, "same recovery diagnostics");
  const v = r.value; check(v !== null, "recovered result");
  check(v.Specific?.type === "error" && v.U?.type === "error", "recovered Error " + stable(v));
  check(typeof v.M === "object" && v.M !== null && !Array.isArray(v.M) && v.M.type === "error", "mixed recovery");
  check(typeof v.A === "object" && v.A !== null && !Array.isArray(v.A) && v.A.type === "error", "any recovery");
  check(v.M === v.A && v.M === v.U && v.Specific === v.U, "equivalent representations share converted Error");
  const first = v.Items?.[0];
  check(v.Items?.length === 2 && first != null && "Text" in first && first.Text === "abc;" && v.Items[1]?.type === "error", "list recovery Error element");
  check(v.OpenItems !== v.Items && v.OpenItems?.[0] === v.Items?.[0], "list recipes/cache sharing");
  const openError = v.OpenItems?.[1]; check(typeof openError === "object" && openError !== null && !Array.isArray(openError) && openError?.type === "error", "open list retains Error");
  const before = stable(v); parseAST("abc;"); check(stable(v) === before, "recovery ownership");
}
`
	runTypedTSFixture(t, src, consumer, GenOptions{}, "")
}

func TestGeneratedTSTypedUnicodeIntegers(t *testing.T) {
	src := `type R = { Text string; Token node[string]; A int; B int; C int; D int; E int; F int; G int; O *int }
rule main R = x:.* #atomic => R{Text: text($x), Token: $x, A: 9007199254740991, B: 9007199254740992, C: -9007199254740992, D: 9223372036854775807 + 1, E: -7 % 2, F: -7 / 2, G: 9223372036854775807}`
	consumer := `import {parse, parseAST, Bytes, CodePoints, Node} from "./parser.ts";
` + tsTypedAssert + `
for (const unit of [Bytes, CodePoints]) for (const input of ["😀é\ud800�", new Uint8Array([0x61,0xff,0xe2,0x82]), new Uint8Array([0xef,0xbf,0xbd])]) {
  const r = parseAST(input, unit), n = parse(input, unit); check(r.error === null && n.error === null && r.value !== null && n.node !== null, "unicode parse");
  const v = r.value, token = n.node.field("Token"); check(token instanceof Node, "Node token");
  check(v.Text === n.node.field("Text") && v.Token?.value === token.text && v.Token.start === token.start && v.Token.end === token.end, "raw text/positions copied exactly");
  check(v.A === 9007199254740991 && v.B === 9007199254740992n && v.C === -9007199254740992n && v.D === -9223372036854775808n && v.E === -1 && v.F === -3 && v.G === 9223372036854775807n && v.O === null, "canonical int64/optional");
  check(stable(v).includes("int:9223372036854775807"), "explicit exact transport");
}
check(parseAST(new Uint8Array([0xff]), Bytes).value?.Text === "\udcff", "byte surrogate escape");
check(parseAST(new Uint8Array([0xff]), CodePoints).value?.Text === "�", "decoded replacement");
`
	runTypedTSFixture(t, src, consumer, GenOptions{}, "")
}

const tsTypedDAGTypes = `type Leaf = { Text string }
type Tree ~ Branch | Leaf
type Branch = { Left Tree; Right Tree }
rule leaf Leaf = t:("x") #atomic => Leaf{Text: text($t)}
`

func TestGeneratedTSTypedSharedDAG(t *testing.T) {
	for name, body := range map[string]string{
		"fold": `rule main Tree = first:leaf rest:leaf* => foldl($first, $rest, (acc, i) => Branch{Left: acc, Right: acc})`,
		"lr": `rule main Tree = branch / leaf
rule branch Branch = prev:main "x" => Branch{Left: $prev, Right: $prev}`,
	} {
		t.Run(name, func(t *testing.T) {
			consumer := `import {parseAST, parseASTWithOptions, Bytes, CodePoints} from "./parser.ts";
` + tsTypedAssert + `
for (const unit of [CodePoints, Bytes]) for (const size of [1, 8, 6000]) {
  const r = parseAST("x".repeat(size), unit); check(r.error === null && r.value !== null, "DAG parse");
  let v = r.value, count = 1;
  while (v.type === "Branch") { check(v.Left !== null && v.Left === v.Right, "shared capture stays shared"); v = v.Left; count++; }
  check(v.type === "Leaf" && count === size, "linear graph/deep iterative conversion");
  check(parseASTWithOptions("x", {maxDepth:0}).error === null, "second invocation");
}
check(stable(parseAST("xx").value).includes("Branch"), "small DAG value");
`
			keep := ""
			if out := os.Getenv("PEGO_TS_TYPED_DIR"); out != "" {
				keep = filepath.Join(out, name)
			}
			runTypedTSFixture(t, tsTypedDAGTypes+body, consumer, GenOptions{Recognize: true}, keep)
			if keep != "" {
				inputs := tsStrings([]string{"x", strings.Repeat("x", 8), strings.Repeat("x", 6000)})
				if err := os.WriteFile(filepath.Join(keep, "inputs.json"), []byte(inputs), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGeneratedTSTypedNilAndDepth(t *testing.T) {
	for name, src := range map[string]string{
		"any":  `rule main _ = "" => nil`,
		"node": `rule main *node[_] = "" => nil`,
		"union": `type T = { Text string }
type U ~ T | node[string]
rule main *U = "" => nil`,
		"list": `rule main *[]node[string] = "" => nil`,
	} {
		t.Run(name, func(t *testing.T) {
			runTypedTSFixture(t, src, `import {parse, parseAST} from "./parser.ts";
`+tsTypedAssert+`const r = parseAST(""); check(r.error === null && r.value === null && parse("").error === null, "nil success"); check(stable(r.value) === "null", "nil transport");`, GenOptions{}, "")
		})
	}
	runTypedTSFixture(t, `type T = { X node[string] }
rule main T = x:n #atomic => T{X: $x}
rule n = "(" n ")" / "x"`, `import {parseAST, parseASTWithOptions} from "./parser.ts";
`+tsTypedAssert+`check(parseAST("(((x)))").error?.message.includes("nesting too deep"), "generated limit"); check(parseASTWithOptions("(((x)))", {maxDepth:20}).value?.X?.value === "(((x)))", "override"); check(stable(parseASTWithOptions("x", {maxDepth:0}).value).includes("T"), "zero default");`, GenOptions{MaxDepth: 3}, "")
	for _, scalar := range []string{"int", "string", "bool"} {
		g, err := syntax.Parse("rule main " + scalar + ` = "" => nil`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := generateTSBundle(g, GenOptions{Start: "main", Types: true}); err == nil {
			t.Errorf("top-level scalar %s unexpectedly accepted", scalar)
		}
	}
}

// Export same-revision typed/untyped sources and small/large public workloads.
// The test checks values only; the coordinator owns time/source/peak measurements.
func TestGeneratedTSTypedWorkloads(t *testing.T) {
	cases := []struct {
		name, src, small, large string
	}{
		{"fields", `type Item = { Key node[string]; Value node[string]; N int; S string; B bool }
rule main []Item = xs:item* => $xs.Value
rule item Item = k:[a-zé😀]+ #atomic "=" v:[0-9]+ #atomic ";" => Item{Key: $k, Value: $v, N: len($v), S: text($k), B: true}`, "a=1;", strings.Repeat("é😀=123;", 4000)},
		{"capture-free", `rule main = [a-zé😀]* #atomic`, "a", strings.Repeat("é😀a", 8000)},
		{"recovery", `type Good = { Token node[string] }
type Item ~ Good | node[string] | error
rule main []Item = xs:item #recover(skip=[^;]+ ";")* => $xs.Value
rule item Good = x:[a-zé😀]+ #atomic ";" => Good{Token: $x}`, "a;1;", strings.Repeat("é😀;1;", 2000)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inputs := tsStrings([]string{c.small, c.large})
			consumer := `import {parse, parseAST, parseASTWithOptions, CodePoints, Bytes} from "./parser.ts";
` + tsTypedAssert + `for (const unit of [CodePoints, Bytes]) for (const input of ` + inputs + `) {
  const r = parseAST(input, unit), n = parse(input, unit);
  check(r.error?.message === n.error?.message && r.value !== null && n.node !== null, "workload parity");
  check(parseASTWithOptions(input, {unit,maxDepth:0}).error?.message === r.error?.message, "options control");
  if (Array.isArray(r.value)) check(r.value.length === n.node.children.length, "list count");
  else check(stable(r.value).includes('"type":"string"'), "string node value");
}`
			keep := ""
			if dir := os.Getenv("PEGO_TS_TYPED_DIR"); dir != "" {
				keep = filepath.Join(dir, c.name)
			}
			runTypedTSFixture(t, c.src, consumer, GenOptions{Recognize: true}, keep)
			if keep != "" {
				if err := os.WriteFile(filepath.Join(keep, "inputs.json"), []byte(inputs), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// Inferred nil/never recipes are Go any-converters: recovery can produce a
// value despite the normal rule having no value. Their advertised type must
// include that converted Error instead of claiming TypedParseResult<null>.
func TestGeneratedTSTypedInferredRecovery(t *testing.T) {
	for name, c := range map[string]struct {
		src, inferred string
		recovered     bool
	}{
		"nil":   {`rule main = (-"a") #recover(skip=.+)`, "node[()] | error", true},
		"never": {`rule main = main`, "_|_", false},
		"nil-rule-recovery": {`rule leaf = "a" => nil
rule main = leaf #recover(skip=.+)`, "*error", true},
		"never-rule-recovery": {`rule leaf = leaf
rule main = leaf #recover(skip=.+)`, "error", true},
	} {
		t.Run(name, func(t *testing.T) {
			prog := compile(t, c.src)
			if got, ok := inferredType(prog, "main"); !ok || got != c.inferred {
				t.Fatalf("inferred start = %s, want %s", got, c.inferred)
			}
			consumer := `import {parseAST, parse, Bytes, CodePoints, Node} from "./parser.ts";
import type {TypedValue, TypedParseResult} from "./types.ts";
` + tsTypedAssert + `for(const unit of [CodePoints,Bytes]) {
 const r: TypedParseResult<TypedValue> = parseAST("!",unit), n = parse("!",unit);
 check(r.error !== null && r.error.message === n.error?.message, "inferred diagnostics");
 // Narrow the actual entry point's type, not just the widened annotation.
 const v = parseAST("!",unit).value;
 if (typeof v === "object" && v !== null && !(v instanceof Node) && !Array.isArray(v)) {
   check(v.type === "error" && v.start === 0 && v.end === 1, "inferred recovered value");
 }
 const bad = parseAST("",unit); check(bad.value === null && bad.error !== null, "unrecoverable empty input");
 check(stable(r.value) === stable(parseAST("!",unit).value), "repeat value");
}`
			if c.recovered {
				consumer += `check(parseAST("!").value !== null, "recovery produces a value");`
			} else {
				consumer += `check(parseAST("!").value === null, "selected Node zero remains null");`
			}
			switch name {
			case "nil":
				// The group of a dropped expression is a node whose value is the empty tuple.
				consumer += `const success = parseAST("a"); check(stable(success.value) === '{"type":"tuple","start":0,"end":1,"value":[]}' && success.error === null, "normal empty tuple success " + stable(success.value));`
			case "nil-rule-recovery":
				consumer += `const success = parseAST("a"); check(success.value === null && success.error === null, "normal inferred nil success");`
			}
			runTypedTSFixture(t, c.src, consumer, GenOptions{}, "")
		})
	}
}

func TestGeneratedTSByteStringSurrogateEncoding(t *testing.T) {
	src := `type Text = { Text string }
rule main Text = t:(.*) #atomic => Text{Text: text($t)}
rule wrapped Text = t:("x�y") #atomic => Text{Text: text($t)}`
	for _, start := range []string{"main", "wrapped"} {
		t.Run(start, func(t *testing.T) {
			consumer := strings.ReplaceAll(tsByteStringSurrogateConsumer, "START_WRAPPED", fmt.Sprint(start == "wrapped"))
			runTypedTSFixture(t, src, consumer, GenOptions{Start: start, Types: true, Recognize: true}, "")
		})
	}
}

const tsByteStringSurrogateConsumer = `import * as p from "./parser.ts";
import type { Text as ParsedText } from "./types.ts";
` + tsTypedAssert + `
const startIsWrapped = START_WRAPPED;
const samples = ["", "x�y", "\uD800", "\uDC00", "\uDC7F", "\uDD00", "\uDFFF", "\uD800\uDC80", "a\uD800\uDC80\uDC80z"];
for (let c=0xDC80;c<=0xDCFF;c++) samples.push(String.fromCharCode(c),"x"+String.fromCharCode(c)+"y");
interface Result { node: { readonly start: number; readonly end: number } | null; error: Error | null }
// The text of a text struct: a field of a generic node, a property of a typed value.
const textOf = (n: { readonly start: number } | null): string | undefined => n === null ? undefined : n instanceof p.Node ? String(n.field("Text")) : (n as ParsedText).Text;
for (const unit of [p.CodePoints,p.Bytes]) {
 const routes: { name: string; run: (s: string | Uint8Array) => Result }[] = [
  {name:"Node",run:s=>p.parse(s,unit)},
  {name:"Node options",run:s=>p.parseWithOptions(s,{unit})},
  {name:"named Node",run:s=>p.parseRule(startIsWrapped?"wrapped":"main",s,unit)},
  {name:"named Node options",run:s=>p.parseRuleWithOptions(startIsWrapped?"wrapped":"main",s,{unit})},
  {name:"typed",run:s=>{const r=p.parseAST(s,unit);return {node:r.value,error:r.error}}},
  {name:"typed options",run:s=>{const r=p.parseASTWithOptions(s,{unit});return {node:r.value,error:r.error}}},
 ];
 for (const s of samples) {
  const normalized=Array.from(s,ch=>{const c=ch.codePointAt(0)!;return c>=0xD800&&c<=0xDFFF?"�":ch}).join("");
  for (const r of routes) {
   const actual=r.run(s),wanted=r.run(normalized);
   check(stable(actual.node)===stable(wanted.node)&&actual.error?.message===wanted.error?.message,r.name+" lone surrogate parity "+unit+" "+JSON.stringify(s));
   if (wanted.error===null) {
    const end=unit===p.Bytes?Array.from(normalized).reduce((n,ch)=>{const c=ch.codePointAt(0)!;return n+(c<0x80?1:c<0x800?2:c<0x10000?3:4)},0):Array.from(normalized).length;
    check(actual.node!==null&&textOf(actual.node)===normalized&&actual.node.start===0&&actual.node.end===end,r.name+" normalized text/span");
   }
  }
  check(p.recognize(s,unit)?.message===p.recognize(normalized,unit)?.message,"recognition parity");
  check(p.recognizeWithOptions(s,{unit})?.message===p.recognizeWithOptions(normalized,{unit})?.message,"recognition options parity");
  const named=p.parseRuleWithOptions("wrapped",s,{unit}),wanted=p.parseRuleWithOptions("wrapped",normalized,{unit});
  check(stable(named.node)===stable(wanted.node)&&named.error?.message===wanted.error?.message,"literal match preserves normalized suffix");
 }
 if (!startIsWrapped) {
  for(const r of routes){const raw=r.run(new Uint8Array([120,255,121]));check(raw.error===null&&raw.node!==null&&textOf(raw.node)===(unit===p.Bytes?"x\uDCFFy":"x�y")&&raw.node.end===3,"raw-byte text unchanged");}
 }
}
if (!startIsWrapped) {
 const typed: ParsedText|null=p.parseAST("\uDCFF",p.Bytes).value;
 check(typed!==null&&typed.Text==="�"&&typed.end===3,"typed public string uses full replacement bytes");
}
`

// TestGeneratedTSDeclaredNodeAny checks that a rule declared node[_] without an action has the
// declared TypeScript type, and not the type of its inferred value.
func TestGeneratedTSDeclaredNodeAny(t *testing.T) {
	src := `rule main node[_] = a b
rule a = "a"
rule b = "b"`
	consumer := `import { parseAST } from "./parser.ts";
import type { TNode, TypedParseResult } from "./types.ts";
` + tsTypedAssert + `
type Eq<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false;
const same: Eq<ReturnType<typeof parseAST>, TypedParseResult<TNode<unknown> | null>> = true;
check(same, "declared type");
const r: TypedParseResult<TNode<unknown> | null> = parseAST("ab");
check(r.error === null && r.value !== null && Array.isArray(r.value.value), "tuple node value");
check(r.value!.start === 0 && r.value!.end === 2 && stable(r.value).length > 0, "span");
`
	runTypedTSFixture(t, src, consumer, GenOptions{}, "")
}
