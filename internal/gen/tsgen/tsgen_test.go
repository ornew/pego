package tsgen_test

import (
	"encoding/json"
	"github.com/ornew/pego/internal/syntax"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// tsVariantLen is the length up to which TestGeneratedTSParsersMatchEngine also parses variants of
// an input.
var tsVariantLen = 40

// TestGeneratedTSParsersMatchEngine checks that generated TypeScript parsers return the same results
// as the engine, as TestGeneratedParsersMatchEngine does for Go: the same trees as JSON (byte for
// byte, from marshal), the same strings of the trees, the same errors, and recognize returns the
// errors of the engine's recognition. Each input is parsed as bytes and, when it is valid UTF-8, as
// a string. It also checks that the generated code type-checks, when tsc is available.
func TestGeneratedTSParsersMatchEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	cases := gentest.GenCorpus(t)
	// Exactly at and just beyond the nesting limit, as in TestGeneratedParsersMatchEngine; the
	// harness gives the parser a stack large enough to reach it.
	cases = append(cases, gentest.Case{Name: "nesting limit", Src: `rule main = n
rule n = "(" n ")" / "x"`, Inputs: []string{strings.Repeat("(", engine.DefaultMaxDepth-2) + "x" + strings.Repeat(")", engine.DefaultMaxDepth-2),
		strings.Repeat("(", engine.DefaultMaxDepth-1) + "x" + strings.Repeat(")", engine.DefaultMaxDepth-1)}})
	// Values that JavaScript represents differently from Go: 64-bit ints that wrap around (the
	// runtime switches to bigint beyond the safe integers), negative division, -0, strings
	// compared and measured by code points or UTF-8 bytes (JavaScript strings are UTF-16), and
	// variables holding big ints as memo keys.
	cases = append(cases, gentest.Case{Name: "ints and strings", Src: `type R = { A int; B int; C int; D int; E int; F int; G bool; H bool; I int; J string; K int; L bool; M int }
var big int
rule main = x:[a-z]* #atomic ","? y:[^!]* #atomic {big := 9007199254740992 + len($x)} z:big? => R{A: 9223372036854775807 + len($x), B: -9223372036854775807 - 1 - len($x), C: 4611686018427387904 * (len($x) + 1), D: -7 / 2 * len($x), E: -7 % 2 + 0 * -1, F: (9007199254740993 + len($y)) / 3, G: text($y) < "😀", H: "！" < "😀", I: len($y), J: text($y) + "!", K: -(9223372036854775807 - len($x) + 1), L: big == 9007199254740992 + len($x), M: len($z)}
rule big = {big > 9007199254740992} "!"+ #atomic`, Inputs: []string{"", "a", "ab,c😀", ",！", "z,日本", "abc,😀x", "a,!!"}})
	// Variants of the short inputs: every prefix, and the input without each byte. They fail in many
	// places (expectations, recovery, cuts, memoized failures), and some are not valid UTF-8.
	for i := range cases {
		seen := map[string]bool{}
		for _, s := range cases[i].Inputs {
			seen[s] = true
		}
		for _, s := range cases[i].Inputs {
			if len(s) > tsVariantLen {
				continue
			}
			for k := 0; k < len(s); k++ {
				for _, v := range []string{s[:k], s[:k] + s[k+1:]} {
					if !seen[v] {
						seen[v] = true
						cases[i].Inputs = append(cases[i].Inputs, v)
					}
				}
			}
		}
	}
	t.Run("values", func(t *testing.T) { gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{}) })
	t.Run("source info", func(t *testing.T) { gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{SourceInfo: true}) })
	t.Run("budget controls", func(t *testing.T) { gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{BudgetControls: true}) })
}

func TestGenerateTSErrors(t *testing.T) {
	t.Parallel()
	g, _ := syntax.Parse("@start\nrule main = \"a\" b\nrule b = \"b\"")
	if _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "nope"}); err == nil || err.Error() != "rule nope is not defined" {
		t.Errorf("got %v", err)
	}
	if _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "b"}); err == nil || err.Error() != "rule b is not a start rule; parsing can begin only at a rule marked @start" {
		t.Errorf("not a start rule: got %v", err)
	}
	if code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Types: true}); err != nil || !strings.Contains(string(code), "export function parseAST(") {
		t.Errorf("Types: %v", err)
	}
	bad, _ := parseTest(`rule main = x`)
	if _, err := gentest.GenerateTSBundle(bad, gen.GenOptions{Start: "main"}); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
	// Generating is deterministic.
	src, _ := os.ReadFile("../../../parsers/json/json.pego")
	jg, err := parseTest(string(src))
	if err != nil {
		t.Fatal(err)
	}
	a, err := gentest.GenerateTSBundle(jg, gen.GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := gentest.GenerateTSBundle(jg, gen.GenOptions{Start: "main", Recognize: true})
	if string(a) != string(b) {
		t.Error("generating twice gives different code")
	}
}

// runTSScript generates a TypeScript parser for the grammar src as parser.ts and runs script, a
// module that imports it, with node on the main thread (with Node's default stack). It returns
// the standard output.
func runTSScript(t *testing.T, src, script string) string {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	return gentest.RunTSGrammarScript(t, g, script)
}

// TestGeneratedTSLoneSurrogates checks that strings with many lone surrogates, which the parser
// replaces with U+FFFD before parsing, parse like any other input.
func TestGeneratedTSLoneSurrogates(t *testing.T) {
	t.Parallel()
	out := runTSScript(t, `rule main = .*`, `import { parse } from "./parser.ts";
for (const s of ["\uDC00".repeat(7000), "x\uDC00".repeat(15000), "\uD800", "a😀\uDE00"]) {
  const r = parse(s);
  if (r.error !== null) {
    console.log("error: " + r.error.message);
    continue;
  }
  console.log(r.node.end, r.node.children.filter((c) => c.text === "�").length, r.node.children.map((c) => c.text.length).join(""));
}
`)
	want := "7000 7000 " + strings.Repeat("1", 7000) + "\n30000 15000 " + strings.Repeat("1", 30000) + "\n1 1 1\n3 1 121\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", gentest.Clip(out, gentest.DiffAt(out, want)), gentest.Clip(want, gentest.DiffAt(out, want)))
	}
}

// TestGeneratedTSDeepTrees checks that the printers handle trees nested more deeply than the
// JavaScript stack allows recursion: left recursion builds such trees without nesting calls.
func TestGeneratedTSDeepTrees(t *testing.T) {
	t.Parallel()
	src := `rule main = e $$
rule e = e "+" n / n
rule n = [0-9]+ #atomic`
	input := strings.Repeat("1+", 19999) + "1"
	out := runTSScript(t, src, `import { parse, marshal } from "./parser.ts";
const r = parse("`+input+`");
console.log(r.error === null ? "ok" : r.error.message);
console.log(String(r.node));
console.log(marshal(r.node));
`)
	n, err := compileBare(t, src).Parse("main", input)
	if err != nil {
		t.Fatal(err)
	}
	want := "ok\n" + n.String() + "\n" + strings.TrimSuffix(strings.TrimPrefix(gentest.DeepResultJSON(t, n, nil), `{"node":`), "}") + "\n"
	if out != want {
		at := gentest.DiffAt(out, want)
		t.Errorf("got\n%s\nwant\n%s", gentest.Clip(out, at), gentest.Clip(want, at))
	}
}

// TestGeneratedTSQuotesEveryCodePoint checks that toString quotes every code point as the engine's
// Node.String does (with strconv.Quote, whose idea of printable characters follows Go's Unicode
// version, not the JavaScript runtime's).
func TestGeneratedTSQuotesEveryCodePoint(t *testing.T) {
	t.Parallel()
	out := runTSScript(t, `rule main = .*`, `import { parse } from "./parser.ts";
const parts = [];
for (let r = 0; r <= 0x10ffff; r++) {
  if (r < 0xd800 || r > 0xdfff) {
    parts.push(String.fromCodePoint(r));
  }
}
const r = parse(parts.join(""));
process.stdout.write(r.error === null ? String(r.node) : r.error.message);
`)
	var b strings.Builder
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r < 0xD800 || r > 0xDFFF {
			b.WriteRune(r)
		}
	}
	n, err := compileBare(t, `rule main = .*`).Parse("main", b.String())
	if err != nil {
		t.Fatal(err)
	}
	if want := n.String(); out != want {
		at := gentest.DiffAt(out, want)
		t.Errorf("got\n%s\nwant\n%s", gentest.Clip(out, at), gentest.Clip(want, at))
	}
}

// TestGeneratedTSFieldNames checks that JSON.stringify writes fields whose names are special in
// JavaScript objects.
func TestGeneratedTSFieldNames(t *testing.T) {
	t.Parallel()
	src := `rule main = prototype:"a" constructor:"b" toString:"c" hasOwnProperty:"d"`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	// Capture names appear in the source information.
	out := gentest.RunTSGrammarScriptWith(t, g, `import { parse } from "./parser.ts";
console.log(JSON.stringify(parse("abcd").node));
`, gen.GenOptions{Start: "main", SourceInfo: true})
	n, err := mustCompile(t, src).Parse("main", "abcd")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(n)
	if out != string(want)+"\n" {
		t.Errorf("got  %s\nwant %s", out, want)
	}
}

// TestGeneratedTSStackOverflowErrors checks that parse returns the stack overflows of JavaScript
// engines as errors (V8 and JavaScriptCore throw a RangeError, SpiderMonkey an InternalError "too
// much recursion"), and throws other exceptions. It simulates them with an input whose bytes throw.
func TestGeneratedTSStackOverflowErrors(t *testing.T) {
	t.Parallel()
	out := runTSScript(t, `rule main = .*`, `import { parse } from "./parser.ts";
const internal = new Error("too much recursion");
internal.name = "InternalError";
for (const e of [internal, new RangeError("Maximum call stack size exceeded"), new TypeError("bad"), new RangeError("Invalid array length")]) {
  const input = new Proxy(new Uint8Array(4), {
    get(target, key) {
      if (key === "2") {
        throw e;
      }
      return Reflect.get(target, key);
    },
  });
  try {
    const r = parse(input);
    console.log(r.error === null ? "ok" : r.error.message);
  } catch (x) {
    console.log("threw " + x.name + ": " + x.message);
  }
}
`)
	want := "nesting too deep: the JavaScript stack overflowed at 0 nested recursive rule calls\n" +
		"nesting too deep: the JavaScript stack overflowed at 0 nested recursive rule calls\n" +
		"threw TypeError: bad\n" +
		"threw RangeError: Invalid array length\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}
