package tsgen_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// Small tables cannot select sparse storage, so their default TypeScript
// output must match the static dense control rather than retain a
// matching-time branch.
func TestGeneratedSparseMemoSmallTable(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = "a"*`)
	if err != nil {
		t.Fatal(err)
	}
	for _, recognize := range []bool{false, true} {
		for _, generate := range []func(*grammar.Grammar, gen.GenOptions) ([]byte, error){gentest.GenerateTSBundle} {
			opts := gen.GenOptions{Package: "smallmemo", Start: "main", Recognize: recognize}
			code, err := generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			opts.DisableSparseMemo = true
			control, err := generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(code, control) {
				t.Fatal("small default output differs from static dense control")
			}
		}
	}
}

func TestGeneratedTSSparseMemoControls(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	src, err := os.ReadFile("../../../parsers/typescript/typescript.zp")
	if err != nil {
		t.Fatal(err)
	}
	g, err := parseTest(string(src))
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("ZP_SPARSE_MEMO_TS_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	var results [2]string
	for i, name := range []string{"optimized", "reference"} {
		code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Recognize: true, DisableSparseMemo: i == 1})
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("const G = mkGrammar(100000, %v, ", i == 0); !strings.Contains(string(code), want) {
			t.Fatalf("%s output lacks %q", name, want)
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		code = append(code, []byte(sparseMemoTSBoundaries)...)
		for file, data := range map[string][]byte{"parser.ts": code, "test.mjs": []byte(sparseMemoTSFixture), "bench.mjs": []byte(sparseMemoTSBenchmark)} {
			if err := os.WriteFile(filepath.Join(path, file), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if tsc, err := exec.LookPath("tsc"); err == nil {
			cmd := exec.Command(tsc, "--noEmit", "--target", "es2020", "--lib", "es2020", "--strict",
				"--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch",
				"--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess",
				"--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "parser.ts")
			cmd.Dir = path
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s strict TypeScript control: %v\n%s", name, err, output)
			}
		} else {
			t.Log("tsc not found: not type-checking the generated code")
		}
		cmd := exec.Command(node, "test.mjs")
		cmd.Dir = path
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s TypeScript control: %v\n%s", name, err, output)
		}
		results[i] = string(output)
	}
	if results[0] == "" || results[0] != results[1] {
		t.Fatalf("TypeScript controls differ:\n%s\n%s", results[0], results[1])
	}
}

const sparseMemoTSBoundaries = `
// Test-only access to unexported bit bookkeeping; not emitted in public output.
export function testMemoBoundaries(): void {
  if (nseen <= 64 || recNseen <= 64) throw new Error("small fixture rule table");
  for (const stride of [63, 64, 65, 128, 175]) {
    const p = new Parser(G, "", CodePoints, stride, G.maxDepth);
    for (const pos of [0, 63, 64, 1023, 1024, 1025, 65535]) {
      for (const r of [0, stride - 1]) {
        const mark = () => G.sparse && stride > 64 ? p.markSparseSeen(pos, r) : p.markSeen(pos, r);
        if (!mark() || mark()) throw new Error("first/repeated mismatch");
      }
    }
  }
  for (const stride of [64, 65, 175]) {
    for (const [input, unit, large] of [
      ["a".repeat(1024), CodePoints, false], ["a".repeat(1025), CodePoints, true],
      ["é".repeat(513), CodePoints, false], ["é".repeat(513), Bytes, true],
    ] as const) {
      const p = new Parser(G, input, unit, stride, G.maxDepth);
      if (p.sparse !== (G.sparse && stride > 64 && large)) throw new Error("input cutoff mismatch");
    }
  }
  const p = new Parser(G, "", CodePoints, 175, G.maxDepth);
  for (const pos of [2 ** 31, 2 ** 32, 2 ** 32 + 1024]) {
    if (!p.markSparseSeen(pos, 174) || p.markSparseSeen(pos, 174)) {
      throw new Error("wide sparse position aliased");
    }
  }
}

// Test-only representative bit-buffer measurement, excluding JS metadata.
export function testMemoStorage(input: string, unit: Unit, rec: boolean): object {
  const p = new Parser(G, input, unit, rec ? recNseen : nseen, G.maxDepth);
  p.call(rec ? Q0 : R0, 0);
  return {
    bitBytes: p.seen.byteLength + (p.seenPageChunks?.reduce((n, x) => n + x.byteLength, 0) ?? 0),
    directorySlots: p.seenPages?.reduce((n, x) => n + (x?.length ?? 0), 0) ?? 0,
    pageChunks: p.seenPageChunks?.length ?? 0,
    rules: p.stride,
  };
}
`

const sparseMemoTSFixture = `import {createHash} from "node:crypto";
import {Bytes, CodePoints, parse, recognize, marshal, testMemoBoundaries} from "./parser.ts";
testMemoBoundaries();
for (const unit of [CodePoints, Bytes]) {
  for (const input of ["const 日本語 = \"😀\";", "const 日本語 = 1 + 2;\n".repeat(1200), "function f( { return 1;", ""]) {
    for (const form of [input, new TextEncoder().encode(input)]) {
      const r = parse(form, unit), rec = recognize(form, unit);
      const data = JSON.stringify([marshal(r.node), r.node?.toString(), r.error?.message, rec?.message]);
      console.log(createHash("sha256").update(data).digest("hex"));
    }
  }
}
`

// The fixtures preserve this driver so timing controls can be rerun from a
// source commit. Each API invocation creates its own TypeScript parser.
const sparseMemoTSBenchmark = `import {readFileSync} from "node:fs";
import {performance} from "node:perf_hooks";
import {parse, recognize, CodePoints, Bytes, testMemoStorage} from "./parser.ts";
const text = process.env.ZP_SPARSE_MEMO_INPUT
  ? readFileSync(process.env.ZP_SPARSE_MEMO_INPUT, "utf8")
  : "const 日本語 = 1 + 2;\n".repeat(12000);
const out = [];
for (const unit of [CodePoints, Bytes]) {
  for (const rec of [false, true]) {
    const run = () => {
      const err = rec ? recognize(text, unit) : parse(text, unit).error;
      if (err !== null) throw err;
    };
    for (let i = 0; i < (text.length < 1000 ? 1000 : 3); i++) run();
    global.gc();
    const count = text.length < 1000 ? 10000 : 3;
    const start = performance.now();
    for (let i = 0; i < count; i++) run();
    out.push({unit, recognize: rec, ms: (performance.now() - start) / count,
      storage: testMemoStorage(text, unit, rec)});
  }
}
console.log(JSON.stringify(out));
`
