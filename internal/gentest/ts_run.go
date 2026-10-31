package gentest

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/compiletest"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gen/tsgen"
	"github.com/ornew/zeroparse/internal/value"
)

// NodeTSArgs are the arguments that run a .ts script of a directory written by writeTSFiles.
func NodeTSArgs(script string, args ...string) []string {
	return append([]string{script}, args...)
}

// TSTypedDAGTypes are the types of a typed result that is a DAG.
const TSTypedDAGTypes = `type Leaf = { Text string }
type Tree ~ Branch | Leaf
type Branch = { Left Tree; Right Tree }
rule leaf Leaf = t:("x") #atomic => Leaf{Text: text($t)}
`

var (
	TSBundleImport = regexp.MustCompile(`(?m)^(?:import|export) (?:type )?(?:\*\s+as \w+|\{[^}]*\}(?: as \w+)?)(?: from "[^"]*")?;\n`)
	TSBundleHeader = regexp.MustCompile(`(?s)\A(?://[^\n]*\n|\n|/\* eslint-disable \*/\n)*`)
)

// GenerateTSBundle generates a TypeScript parser as one module, the way these tests have always run
// it: the three files in one scope (runtime, types, parser), without the imports between them. Test
// hooks append code that reaches the runtime's internals and parsers' own names this way. The tests
// of the real file split (tsgen_split_test.go) compile and run the files as they are written.
func GenerateTSBundle(g *grammar.Grammar, opts gen.GenOptions) ([]byte, error) {
	files, err := GenerateTS(g, opts)
	if err != nil {
		return nil, err
	}
	return bundleTS(files), nil
}

// GenerateTSBundleRaw is GenerateTSBundle with the options as given.
func GenerateTSBundleRaw(g *grammar.Grammar, opts gen.GenOptions) ([]byte, error) {
	files, err := GenerateTSRaw(g, opts)
	if err != nil {
		return nil, err
	}
	return bundleTS(files), nil
}

// bundleTS joins the generated files into one module.
func bundleTS(files []gen.GeneratedFile) []byte {
	contents := map[string]string{}
	for _, f := range files {
		contents[f.Path] = string(f.Content)
	}
	var b strings.Builder
	b.WriteString(tsgen.TsGeneratedHeader)
	b.WriteString("/* eslint-disable */\n\n")
	for _, name := range []string{"runtime.ts", "types.ts", "parser.ts"} {
		src, ok := contents[name]
		if !ok {
			continue // types.ts exists only with opts.Types
		}
		src = TSBundleHeader.ReplaceAllString(src, "")
		src = TSBundleImport.ReplaceAllString(src, "")
		switch name {
		case "types.ts":
			src = regexp.MustCompile(`\bRuntimeNode\b`).ReplaceAllString(src, "Node")
		case "parser.ts":
			src = regexp.MustCompile(`\bTypes\.`).ReplaceAllString(src, "")
			src = regexp.MustCompile(`(?m)^export type \* from [^\n]*\n`).ReplaceAllString(src, "")
		}
		b.WriteString("\n// --- " + name + " ---\n\n")
		b.WriteString(src)
	}
	return []byte(b.String())
}

// TSHarness runs the generated TypeScript parsers g<i>.ts on the inputs in inputs.json and writes
// one JSON line per input, unit and form of the input (bytes, and a string when the input is valid
// UTF-8) to out.jsonl. It parses in a worker thread with a large stack, so that nesting reaches the
// limit of 100,000 rule calls (Node's default stack holds far fewer).
const TSHarness = `import { Worker, isMainThread, workerData } from "node:worker_threads";
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

// DeepResultJSON returns gentest.ResultJSON(n, err), also for trees nested too deeply for encoding/json
// (which fails beyond 10,000 levels, and then resultJSON returns ""): it writes the node with
// WriteJSON, which keeps its own stack, and checks that it agrees with resultJSON where that
// succeeds.
func DeepResultJSON(t *testing.T, n *value.Node, err error) string {
	var b strings.Builder
	if err != nil {
		e, _ := json.Marshal(err.Error())
		b.WriteString(`{"err":` + string(e) + `,"node":`)
	} else {
		b.WriteString(`{"node":`)
	}
	if err := n.WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	b.WriteByte('}')
	if std := ResultJSON(n, err); std != "" && std != b.String() {
		t.Fatalf("DeepResultJSON differs from resultJSON:\n %s\n %s", b.String(), std)
	}
	return b.String()
}

// Clip shortens long results for error messages: s around offset at, where it differs from the
// other result.
func Clip(s string, at int) string {
	if len(s) <= 300 {
		return s
	}
	from := max(0, at-100)
	to := min(len(s), at+200)
	return fmt.Sprintf("...(%d bytes)...%s...(%d bytes)", from, s[from:to], len(s)-to)
}

// DiffAt returns the offset of the first difference between a and b.
func DiffAt(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}

// RunTSGrammarScript generates the TypeScript parser of g, runs script in Node with it and returns the output.
func RunTSGrammarScript(t *testing.T, g *grammar.Grammar, script string) string {
	t.Helper()
	return RunTSGrammarScriptWith(t, g, script, gen.GenOptions{Start: "main"})
}

// RunTSGrammarScriptWith is RunTSGrammarScript with the generator options opts.
func RunTSGrammarScriptWith(t *testing.T, g *grammar.Grammar, script string, opts gen.GenOptions) string {
	t.Helper()
	return runTSGrammarScript(t, g, script, opts, GenerateTSBundle)
}

// RunTSGrammarScriptRaw is RunTSGrammarScriptWith with the options as given: the parser records
// expectations only with opts.Expectations.
func RunTSGrammarScriptRaw(t *testing.T, g *grammar.Grammar, script string, opts gen.GenOptions) string {
	t.Helper()
	return runTSGrammarScript(t, g, script, opts, GenerateTSBundleRaw)
}

func runTSGrammarScript(t *testing.T, g *grammar.Grammar, script string, opts gen.GenOptions, bundle func(*grammar.Grammar, gen.GenOptions) ([]byte, error)) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	code, err := bundle(g, opts)
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
		t.Fatalf("node: %v\n%s", err, Clip(stderr.String(), 0))
	}
	return string(out)
}

// TSParsersCorpus generates a TypeScript parser for each case with opts, runs them with the Node
// executable node (and checks their types with tsc if there is one), and compares the results on the
// inputs of the case, in positions of code points and of bytes, with those of the engine.
func TSParsersCorpus(t *testing.T, node string, cases []Case, opts gen.GenOptions) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type": "module"}`+"\n")
	write("harness.mjs", TSHarness)
	type input struct {
		B64   string `json:"b64"`
		Valid bool   `json:"valid"`
	}
	type want struct{ out, str, rec string }
	var inputs [][]input
	var wants []want
	var files []string
	for i, c := range cases {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts.Start, opts.Recognize = "main", true
		code, err := GenerateTSBundle(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		name := fmt.Sprintf("g%d.ts", i)
		write(name, string(code))
		files = append(files, name)
		var in []input
		// The generated code projects (internal/compile/project.go), and records source information as asked.
		prog := refProgram(t, c.Src, compile.Options{NoProjections: true, SourceInfo: opts.SourceInfo})
		for _, s := range c.Inputs {
			in = append(in, input{base64.StdEncoding.EncodeToString([]byte(s)), utf8.ValidString(s)})
			for _, bytes := range []bool{false, true} {
				n, err := prog(s, bytes, false)
				w := want{out: DeepResultJSON(t, n, err)}
				if n != nil {
					w.str = n.String()
				}
				_, err = prog(s, bytes, true)
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
		for _, s := range c.Inputs {
			for _, unit := range []string{"codepoints", "bytes"} {
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
						at := DiffAt(res, wants[w].out)
						t.Errorf("%s (%v, %s): input %q\n generated %s\n engine    %s", c.Name, unit, form, in, Clip(res, at), Clip(wants[w].out, at))
					}
					str, err := base64.StdEncoding.DecodeString(got.Str)
					if err != nil {
						t.Fatal(err)
					}
					if string(str) != wants[w].str {
						at := DiffAt(string(str), wants[w].str)
						t.Errorf("%s (%v, %s): input %q: String\n generated %s\n engine    %s", c.Name, unit, form, in, Clip(string(str), at), Clip(wants[w].str, at))
					}
					if got.Rec != wants[w].rec {
						t.Errorf("%s (%v, %s): input %q: recognize\n generated %s\n engine    %s", c.Name, unit, form, in, got.Rec, wants[w].rec)
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
