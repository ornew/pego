package engine

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/internal/syntax"
)

// tsHarness runs the generated TypeScript parsers g<i>.ts on the inputs in inputs.json and writes
// one JSON line per input, unit and form of the input (bytes, and a string when the input is valid
// UTF-8) to out.jsonl. It parses in a worker thread with a large stack, so that nesting reaches the
// limit of 100,000 rule calls (Node's default stack holds far fewer).
const tsHarness = `import { Worker, isMainThread, workerData } from "node:worker_threads";
import { closeSync, openSync, readFileSync, writeSync } from "node:fs";

if (isMainThread) {
  const w = new Worker(new URL(import.meta.url), {
    workerData: { inputs: process.argv[2], out: process.argv[3] },
    resourceLimits: { stackSizeMb: 1024 },
  });
  w.on("error", (e) => {
    console.error(e);
    process.exit(1);
  });
  w.on("exit", (code) => process.exit(code));
} else {
  // The bytes of a string in base64, with the surrogates that stand for invalid bytes (in text
  // of byte mode) as those bytes, so that strings with them can be compared with Go's.
  const bytesOf = (s) => {
    const out = [];
    for (const ch of s) {
      const c = ch.codePointAt(0);
      if (c >= 0xdc80 && c <= 0xdcff) {
        out.push(c & 0xff);
      } else {
        out.push(...Buffer.from(ch, "utf8"));
      }
    }
    return Buffer.from(out).toString("base64");
  };
  const inputs = JSON.parse(readFileSync(workerData.inputs, "utf8"));
  const out = openSync(workerData.out, "w");
  for (let i = 0; i < inputs.length; i++) {
    const m = await import("./g" + i + ".ts");
    for (const input of inputs[i]) {
      const bytes = new Uint8Array(Buffer.from(input.b64, "base64"));
      for (const unit of [m.CodePoints, m.Bytes]) {
        const forms = [bytes];
        if (input.valid) {
          forms.push(Buffer.from(bytes).toString("utf8"));
        }
        for (const x of forms) {
          const r = m.parse(x, unit);
          const rec = m.recognize(x, unit);
          writeSync(out, JSON.stringify({
            node: m.marshal(r.node),
            err: r.error === null ? null : r.error.message,
            str: r.node === null ? "" : bytesOf(r.node.toString()),
            rec: rec === null ? "<nil>" : rec.message,
          }) + "\n");
        }
      }
    }
  }
  closeSync(out);
}
`

// tsVariantLen is the length up to which TestGeneratedTSParsersMatchEngine also parses variants of
// an input.
var tsVariantLen = 40

// TestGeneratedTSParsersMatchEngine checks that generated TypeScript parsers return the same results
// as the engine, as TestGeneratedParsersMatchEngine does for Go: the same trees as JSON (byte for
// byte, from marshal), the same strings of the trees, the same errors, and recognize returns the
// errors of the engine's recognition. Each input is parsed as bytes and, when it is valid UTF-8, as
// a string. It also checks that the generated code type-checks, when tsc is available.
func TestGeneratedTSParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	cases := genCorpus(t)
	// Exactly at and just beyond the nesting limit, as in TestGeneratedParsersMatchEngine; the
	// harness gives the parser a stack large enough to reach it.
	cases = append(cases, genCase{"nesting limit", `
def main = n
def n = "(" n ")" / "x"`, []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	// Values that JavaScript represents differently from Go: 64-bit ints that wrap around (the
	// runtime switches to bigint beyond the safe integers), negative division, -0, strings
	// compared and measured by code points or UTF-8 bytes (JavaScript strings are UTF-16), and
	// variables holding big ints as memo keys.
	cases = append(cases, genCase{"ints and strings", `
type R struct { A int, B int, C int, D int, E int, F int, G bool, H bool, I int, J string, K int, L bool, M int }
def main = x:@(?a-z)* ","? y:@(?^!)* [big = 9007199254740992 + len($x)] z:big? -> new R{
    A: 9223372036854775807 + len($x),
    B: -9223372036854775807 - 1 - len($x),
    C: 4611686018427387904 * (len($x) + 1),
    D: -7 / 2 * len($x),
    E: -7 % 2 + 0 * -1,
    F: (9007199254740993 + len($y)) / 3,
    G: text($y) < "\u{1F600}",
    H: "\u{FF01}" < "\u{1F600}",
    I: len($y),
    J: text($y) + "!",
    K: -(9223372036854775807 - len($x) + 1),
    L: big == 9007199254740992 + len($x),
    M: len($z),
}
def big = [big > 9007199254740992] @"!"+`, []string{"", "a", "ab,c😀", ",！", "z,日本", "abc,😀x", "a,!!"}})
	// Variants of the short inputs: every prefix, and the input without each byte. They fail in many
	// places (expectations, recovery, cuts, memoized failures), and some are not valid UTF-8.
	for i := range cases {
		seen := map[string]bool{}
		for _, s := range cases[i].inputs {
			seen[s] = true
		}
		for _, s := range cases[i].inputs {
			if len(s) > tsVariantLen {
				continue
			}
			for k := 0; k < len(s); k++ {
				for _, v := range []string{s[:k], s[:k] + s[k+1:]} {
					if !seen[v] {
						seen[v] = true
						cases[i].inputs = append(cases[i].inputs, v)
					}
				}
			}
		}
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type": "module"}`+"\n")
	write("harness.mjs", tsHarness)
	type input struct {
		B64   string `json:"b64"`
		Valid bool   `json:"valid"`
	}
	type want struct{ out, str, rec string }
	var inputs [][]input
	var wants []want
	var files []string
	for i, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		name := fmt.Sprintf("g%d.ts", i)
		write(name, string(code))
		files = append(files, name)
		var in []input
		prog := compile(t, c.src, Options{noProjections: true}) // the generated code projects (project.go)
		for _, s := range c.inputs {
			in = append(in, input{base64.StdEncoding.EncodeToString([]byte(s)), utf8.ValidString(s)})
			for _, unit := range []Unit{CodePoints, Bytes} {
				n, err := prog.ParseWith("main", s, ParseOptions{Unit: unit})
				w := want{out: deepResultJSON(t, n, err)}
				if n != nil {
					w.str = n.String()
				}
				_, err = prog.ParseWith("main", s, ParseOptions{Unit: unit, Recognize: true})
				w.rec = fmt.Sprint(err)
				wants = append(wants, w)
			}
		}
		inputs = append(inputs, in)
	}
	data, _ := json.Marshal(inputs)
	write("inputs.json", string(data))

	if tsc, err := exec.LookPath("tsc"); err == nil {
		// The strictest checks there are, so that the code compiles in any project; ES2020 is the
		// oldest target it supports (bigint).
		args := append([]string{"--noEmit", "--target", "es2020", "--lib", "es2020", "--strict",
			"--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch",
			"--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess",
			"--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax"}, files...)
		cmd := exec.Command(tsc, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("tsc: %v\n%s", err, out)
		}
	} else {
		t.Log("tsc not found: not type-checking the generated code")
	}

	cmd := exec.Command(node, "harness.mjs", "inputs.json", "out.jsonl")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	f, err := os.Open(filepath.Join(dir, "out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	results := bufio.NewReader(f)
	k, w := 0, 0
	for _, c := range cases {
		for _, s := range c.inputs {
			for _, unit := range []Unit{CodePoints, Bytes} {
				forms := []string{"bytes"}
				if utf8.ValidString(s) {
					forms = append(forms, "string")
				}
				for _, form := range forms {
					line, err := results.ReadString('\n')
					if err != nil {
						t.Fatalf("got %d results, want more: %v", k, err)
					}
					var got struct {
						Node string
						Err  *string
						Str  string
						Rec  string
					}
					if err := json.Unmarshal([]byte(line), &got); err != nil {
						t.Fatalf("%v: %s", err, line)
					}
					k++
					// The result as resultJSON encodes it, with the node as marshal encoded it.
					res := `{"node":` + got.Node + `}`
					if got.Err != nil {
						e, _ := json.Marshal(*got.Err)
						res = `{"err":` + string(e) + `,"node":` + got.Node + `}`
					}
					in := s
					if len(in) > 200 {
						in = in[:200] + "..."
					}
					if res != wants[w].out {
						at := diffAt(res, wants[w].out)
						t.Errorf("%s (%v, %s): input %q\n generated %s\n engine    %s", c.name, unit, form, in, clip(res, at), clip(wants[w].out, at))
					}
					str, err := base64.StdEncoding.DecodeString(got.Str)
					if err != nil {
						t.Fatal(err)
					}
					if string(str) != wants[w].str {
						at := diffAt(string(str), wants[w].str)
						t.Errorf("%s (%v, %s): input %q: String\n generated %s\n engine    %s", c.name, unit, form, in, clip(string(str), at), clip(wants[w].str, at))
					}
					if got.Rec != wants[w].rec {
						t.Errorf("%s (%v, %s): input %q: recognize\n generated %s\n engine    %s", c.name, unit, form, in, got.Rec, wants[w].rec)
					}
				}
				w++
			}
		}
	}
	t.Logf("compared %d results of %d grammars", k, len(cases))
	if rest, _ := results.ReadString('\n'); rest != "" {
		t.Errorf("got more than %d results", k)
	}
}

func TestGenerateTSErrors(t *testing.T) {
	g, _ := syntax.Parse(`def main = "a"`)
	if _, err := GenerateTS(g, GenOptions{Start: "nope"}); err == nil || err.Error() != "start rule nope is not defined" {
		t.Errorf("got %v", err)
	}
	if _, err := GenerateTS(g, GenOptions{Start: "main", Types: true}); err == nil || err.Error() != "typed values are not supported for TypeScript" {
		t.Errorf("got %v", err)
	}
	bad, _ := syntax.Parse(`def main = x`)
	if _, err := GenerateTS(bad, GenOptions{Start: "main"}); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
	// Generating is deterministic.
	src, _ := os.ReadFile("../../parsers/json/json.pego")
	jg, err := syntax.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	a, err := GenerateTS(jg, GenOptions{Start: "value", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateTS(jg, GenOptions{Start: "value", Recognize: true})
	if string(a) != string(b) {
		t.Error("generating twice gives different code")
	}
}

// deepResultJSON returns resultJSON(n, err), also for trees nested too deeply for encoding/json
// (which fails beyond 10,000 levels, and then resultJSON returns ""): it writes the node itself,
// encoding only strings and leaves with encoding/json, and checks that it agrees with resultJSON
// where that succeeds.
func deepResultJSON(t *testing.T, n *Node, err error) string {
	var b strings.Builder
	var value func(v any)
	value = func(v any) {
		x, ok := v.(*Node)
		if !ok || x == nil {
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			b.Write(data)
			return
		}
		str := func(s string) string { data, _ := json.Marshal(s); return string(data) }
		b.WriteString(`{"type":` + str(x.Type()))
		if x.Rule() != "" {
			b.WriteString(`,"rule":` + str(x.Rule()))
		}
		fmt.Fprintf(&b, `,"start":%d,"end":%d`, x.Start, x.End)
		if x.Text != "" {
			b.WriteString(`,"text":` + str(x.Text))
		}
		if len(x.Children) > 0 {
			b.WriteString(`,"children":[`)
			for i, c := range x.Children {
				if i > 0 {
					b.WriteByte(',')
				}
				value(c)
			}
			b.WriteByte(']')
		}
		if len(x.Fields) > 0 {
			b.WriteString(`,"fields":{`)
			for i, f := range x.Fields.sorted() {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(str(f.Name) + ":")
				value(f.Value)
			}
			b.WriteByte('}')
		}
		b.WriteByte('}')
	}
	if err != nil {
		e, _ := json.Marshal(err.Error())
		b.WriteString(`{"err":` + string(e) + `,"node":`)
	} else {
		b.WriteString(`{"node":`)
	}
	value(n)
	b.WriteByte('}')
	if std := resultJSON(n, err); std != "" && std != b.String() {
		t.Fatalf("deepResultJSON differs from resultJSON:\n %s\n %s", b.String(), std)
	}
	return b.String()
}

// clip shortens long results for error messages: s around offset at, where it differs from the
// other result.
func clip(s string, at int) string {
	if len(s) <= 300 {
		return s
	}
	from := max(0, at-100)
	to := min(len(s), at+200)
	return fmt.Sprintf("...(%d bytes)...%s...(%d bytes)", from, s[from:to], len(s)-to)
}

// diffAt returns the offset of the first difference between a and b.
func diffAt(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}

// runTSScript generates a TypeScript parser for the grammar src as parser.ts and runs script, a
// module that imports it, with node on the main thread (with Node's default stack). It returns
// the standard output.
func runTSScript(t *testing.T, src, script string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := GenerateTS(g, GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": string(code), "main.ts": script} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "main.ts")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, clip(stderr.String(), 0))
	}
	return string(out)
}

// TestGeneratedTSLoneSurrogates checks that strings with many lone surrogates, which the parser
// replaces with U+FFFD before parsing, parse like any other input.
func TestGeneratedTSLoneSurrogates(t *testing.T) {
	out := runTSScript(t, `def main = .*`, `import { parse } from "./parser.ts";
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
		t.Errorf("got\n%s\nwant\n%s", clip(out, diffAt(out, want)), clip(want, diffAt(out, want)))
	}
}

// TestGeneratedTSDeepTrees checks that the printers handle trees nested more deeply than the
// JavaScript stack allows recursion: left recursion builds such trees without nesting calls.
func TestGeneratedTSDeepTrees(t *testing.T) {
	src := `
def main = e $$
def e = e "+" n / n
def n = @(?0-9)+`
	input := strings.Repeat("1+", 19999) + "1"
	out := runTSScript(t, src, `import { parse, marshal } from "./parser.ts";
const r = parse("`+input+`");
console.log(r.error === null ? "ok" : r.error.message);
console.log(String(r.node));
console.log(marshal(r.node));
`)
	n, err := compile(t, src).Parse("main", input)
	if err != nil {
		t.Fatal(err)
	}
	want := "ok\n" + n.String() + "\n" + strings.TrimSuffix(strings.TrimPrefix(deepResultJSON(t, n, nil), `{"node":`), "}") + "\n"
	if out != want {
		at := diffAt(out, want)
		t.Errorf("got\n%s\nwant\n%s", clip(out, at), clip(want, at))
	}
}

// TestGeneratedTSQuotesEveryCodePoint checks that toString quotes every code point as the engine's
// Node.String does (with strconv.Quote, whose idea of printable characters follows Go's Unicode
// version, not the JavaScript runtime's).
func TestGeneratedTSQuotesEveryCodePoint(t *testing.T) {
	out := runTSScript(t, `def main = .*`, `import { parse } from "./parser.ts";
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
	n, err := compile(t, `def main = .*`).Parse("main", b.String())
	if err != nil {
		t.Fatal(err)
	}
	if want := n.String(); out != want {
		at := diffAt(out, want)
		t.Errorf("got\n%s\nwant\n%s", clip(out, at), clip(want, at))
	}
}

// TestGeneratedTSFieldNames checks that JSON.stringify writes fields whose names are special in
// JavaScript objects.
func TestGeneratedTSFieldNames(t *testing.T) {
	src := `def main = __proto__:"a" constructor:"b" toString:"c" hasOwnProperty:"d"`
	out := runTSScript(t, src, `import { parse } from "./parser.ts";
console.log(JSON.stringify(parse("abcd").node));
`)
	n, err := compile(t, src).Parse("main", "abcd")
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
	out := runTSScript(t, `def main = .*`, `import { parse } from "./parser.ts";
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
	want := "nesting too deep: the JavaScript stack overflowed at 0 rule calls\n" +
		"nesting too deep: the JavaScript stack overflowed at 0 rule calls\n" +
		"threw TypeError: bad\n" +
		"threw RangeError: Invalid array length\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}
