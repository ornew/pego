package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_SPARSE_MEMO_GENERATED_DIR preserves same-generator fixtures for
// separately built timing and allocation controls.
func TestGeneratedSparseMemoControls(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	src, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PEGO_SPARSE_MEMO_GENERATED_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	adaptiveTests, err := os.ReadFile("genrt/adaptive_memo_test.go")
	if err != nil {
		t.Fatal(err)
	}
	adaptiveTests = []byte(strings.Replace(string(adaptiveTests), "package genrt", "package memofixture", 1))
	var fingerprints [2]string
	for i, name := range []string{"optimized", "reference"} {
		code, err := Generate(g, GenOptions{Package: "memofixture", Start: "main", Types: true,
			Recognize: true, disableSparseMemo: i == 1})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(code), "tparse(trules") {
			t.Fatal("fixture does not exercise the typed runtime")
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		for file, data := range map[string][]byte{
			"go.mod":    []byte("module memofixture\n\ngo 1.24\n"),
			"parser.go": code, "memo_test.go": []byte(sparseMemoGeneratedFixture),
			"adaptive_test.go": adaptiveTests,
		} {
			if err := os.WriteFile(filepath.Join(path, file), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^Test(SparseGenerated|AdaptiveMemoCounters)")
		cmd.Dir = path
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s generated control: %v\n%s", name, err, output)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if at := strings.Index(line, "MEMO_RESULT "); at >= 0 {
				fingerprints[i] += line[at:] + "\n"
			}
		}
	}
	if fingerprints[0] == "" || fingerprints[0] != fingerprints[1] {
		t.Fatalf("generated controls differ:\n%s\n%s", fingerprints[0], fingerprints[1])
	}
}

func TestGeneratedTSSparseMemoControls(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	src, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PEGO_SPARSE_MEMO_TS_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	var results [2]string
	for i, name := range []string{"optimized", "reference"} {
		code, err := GenerateTS(g, GenOptions{Start: "main", Recognize: true, disableSparseMemo: i == 1})
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && strings.Contains(string(code), "const first = this.sparse") {
			t.Fatal("reference output retains matching-time representation selector")
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		control := "true"
		if i == 1 {
			control = "false"
		}
		code = append(code, []byte(strings.ReplaceAll(sparseMemoTSBoundaries, "sparseSeen", control))...)
		for file, data := range map[string][]byte{"parser.ts": code, "test.mjs": []byte(sparseMemoTSFixture), "bench.mjs": []byte(sparseMemoTSBenchmark)} {
			if err := os.WriteFile(filepath.Join(path, file), data, 0o644); err != nil {
				t.Fatal(err)
			}
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
    const p = new Parser("", CodePoints, stride);
    for (const pos of [0, 63, 64, 1023, 1024, 1025, 65535]) {
      for (const r of [0, stride - 1]) {
        const mark = () => sparseSeen && stride > 64 ? p.markSparseSeen(pos, r) : p.markSeen(pos, r);
        if (!mark() || mark()) throw new Error("first/repeated mismatch");
      }
    }
  }
  for (const stride of [64, 65, 175]) {
    for (const [input, unit, large] of [
      ["a".repeat(1024), CodePoints, false], ["a".repeat(1025), CodePoints, true],
      ["é".repeat(513), CodePoints, false], ["é".repeat(513), Bytes, true],
    ] as const) {
      const p = new Parser(input, unit, stride);
      if (p.sparse !== (sparseSeen && stride > 64 && large)) throw new Error("input cutoff mismatch");
    }
  }
  for (const sparse of [false, true]) {
    for (const [calls, repeats, repeat, wantCalls, wantRepeats, eager] of [
      [15, 0, true, 16, 1, false], [14, 0, true, 15, 1, true],
      [16, 0, true, 17, 1, false],
      [2 ** 31 - 2, 2 ** 27 - 1, true, 2 ** 31 - 1, 2 ** 27, true],
      [2 ** 30 - 1, 2 ** 26 - 1, true, 2 ** 30, 2 ** 26, false],
      [2 ** 30 - 2, 2 ** 26 - 1, true, 2 ** 30 - 1, 2 ** 26, true],
      [2 ** 31 - 2, 1, false, 2 ** 31 - 1, 1, false],
      [2 ** 31 - 1, 1, false, 2 ** 30, 0, false],
      [2 ** 31 - 1, 3, true, 2 ** 30, 2, false],
      [2 ** 31 - 1, 0, false, 2 ** 30, 0, false],
      [2 ** 31 - 1, 0, true, 2 ** 30, 1, false],
      [2 ** 31 - 1, 2 ** 27 - 3, true, 2 ** 30, 2 ** 26 - 1, false],
      [2 ** 31 - 1, 2 ** 27 - 1, true, 2 ** 30, 2 ** 26, false],
      [2 ** 31 - 1, 2 ** 27, true, 2 ** 30, 2 ** 26 + 1, true],
    ] as const) {
      const p = new Parser("", CodePoints, 1);
      p.sparse = sparseSeen && sparse;
      const r = {seen: 0} as Rule;
      if (repeat) {
        if (p.sparse) p.markSparseSeen(0, 0); else p.markSeen(0, 0);
      }
      p.calls[0] = calls;
      p.repeats[0] = repeats;
      if (p.firstCall(r) !== !repeat || p.calls[0] !== wantCalls ||
          p.repeats[0] !== wantRepeats || (p.eager[0] !== 0) !== eager) {
        throw new Error("adaptive memo threshold or rescaling mismatch");
      }
      if (eager && (p.firstCall(r) || p.calls[0] !== wantCalls || p.repeats[0] !== wantRepeats)) {
        throw new Error("eager mode resumed counting");
      }
    }
    const p = new Parser("", CodePoints, 1);
    p.sparse = sparseSeen && sparse;
    p.calls[0] = 2 ** 31 - 1;
    const r = {seen: 0} as Rule;
    if (!p.firstCall(r) || p.firstCall(r) || p.eager[0] !== 0) {
      throw new Error("fresh then repeated calls became eager after rescaling");
    }
  }
  const p = new Parser("", CodePoints, 175);
  for (const pos of [2 ** 31, 2 ** 32, 2 ** 32 + 1024]) {
    if (!p.markSparseSeen(pos, 174) || p.markSparseSeen(pos, 174)) {
      throw new Error("wide sparse position aliased");
    }
  }
}

// Test-only representative bit-buffer measurement, excluding JS metadata.
export function testMemoStorage(input: string, unit: Unit, rec: boolean): object {
  const p = new Parser(input, unit, rec ? recNseen : nseen);
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

const sparseMemoGeneratedFixture = `package memofixture

import (
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "os"
    "strings"
    "testing"
)

func TestSparseGeneratedResults(t *testing.T) {
    if nseen <= 64 || recNseen <= 64 { t.Fatal("fixture no longer exercises sparse tables") }
    inputs := []string{
        "const 日本語 = \"😀\"; const v = [1, 2].map(x => x + 1);",
        strings.Repeat("const 日本語 = 1 + 2;\n", 1200),
        "function f( { return 1;", "",
    }
    for _, unit := range []Unit{CodePoints, Bytes} {
        for i, input := range inputs {
            for pass := 0; pass < 3; pass++ {
                node, err := Parse(input, unit)
                ast, aerr := ParseAST(input, unit)
                rerr := Recognize(input, unit)
                data, jerr := json.Marshal([]any{node, fmt.Sprint(err), ast, fmt.Sprint(aerr), fmt.Sprint(rerr)})
                if jerr != nil { t.Fatal(jerr) }
                t.Logf("MEMO_RESULT %d/%d/%d %x", unit, i, pass, sha256.Sum256(data))
            }
        }
    }
}

func TestSparseGeneratedInputSelection(t *testing.T) {
    p := &tparser{parser: &parser{}, ext: &tslabs{}}
    for _, input := range []string{
        "/*" + strings.Repeat("a", 1020) + "*/",
        "/*" + strings.Repeat("a", 1021) + "*/",
        "/*" + strings.Repeat("é", 512) + "*/",
        "/*" + strings.Repeat("a", 1020) + "*/",
    } {
        for _, unit := range []Unit{CodePoints, Bytes} {
            _, err := p.run(trules[0], input, []Unit{unit}, &tslabs{})
            if err != nil { t.Fatal(err) }
            want := len([]rune(input)) > 1024
            if unit == Bytes { want = len(input) > 1024 }
            if p.memo.sparse != (sparseSeen && want) { t.Fatal("typed input cutoff mismatch") }
            p.recycle()
        }
    }
}

// An optional real corpus exercises retained scratch across successful and
// failed large parses, with small parses between them, in the same parser.
func TestSparseGeneratedRealCorpusReuse(t *testing.T) {
    path := os.Getenv("PEGO_SPARSE_MEMO_INPUT")
    if path == "" { t.Skip("PEGO_SPARSE_MEMO_INPUT not set") }
    data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
    for _, unit := range []Unit{CodePoints, Bytes} {
        p := &tparser{parser: &parser{}}
        for i, input := range []string{string(data), "const x=1;", string(data)+"\nfunction (", "const x=1;", string(data)} {
            _, err := p.run(trules[0], input, []Unit{unit}, &tslabs{})
            if (err != nil) != (i == 2) { t.Fatalf("unit=%d step=%d err=%v", unit, i, err) }
            p.recycle()
            if p.memo.bitPages.n != 0 { t.Fatal("used pages survived recycle") }
            for _, dir := range p.memo.pages {
                if dir.last != nil || dir.at != 0 { t.Fatal("cached pointer survived recycle") }
                for _, page := range dir.pages { if page != nil { t.Fatal("directory pointer survived recycle") } }
            }
            for _, chunk := range p.memo.bitPages.chunks {
                for _, page := range chunk {
                    for _, word := range page { if word != 0 { t.Fatal("seen bits survived recycle") } }
                }
            }
        }
    }
}

func BenchmarkSparseMemoGenerated(b *testing.B) {
    inputs := []struct{name, text string}{{"synthetic", strings.Repeat("const 日本語 = 1 + 2;\n", 12000)}, {"small", "const 日本語 = 1 + 2;\n"}}
    if path := os.Getenv("PEGO_SPARSE_MEMO_INPUT"); path != "" {
        data, err := os.ReadFile(path); if err != nil { b.Fatal(err) }
        inputs = append(inputs, struct{name, text string}{"compiler", string(data)})
    }
    for _, input := range inputs {
        for _, unit := range []Unit{CodePoints, Bytes} {
            for _, api := range []string{"Node", "AST", "Recognize"} {
                b.Run(fmt.Sprintf("%s/%s/%d", input.name, api, unit), func(b *testing.B) {
                    run := func() error {
                        switch api {
                        case "Node": _, err := Parse(input.text, unit); return err
                        case "AST": _, err := ParseAST(input.text, unit); return err
                        default: return Recognize(input.text, unit)
                        }
                    }
                    if err := run(); err != nil { b.Fatal(err) }
                    b.SetBytes(int64(len(input.text)))
                    b.ReportAllocs()
                    for b.Loop() { if err := run(); err != nil { b.Fatal(err) } }
                })
            }
        }
    }
}
`

// The fixtures preserve this driver so timing controls can be rerun from a
// source commit. Each API invocation creates its own TypeScript parser.
const sparseMemoTSBenchmark = `import {readFileSync} from "node:fs";
import {performance} from "node:perf_hooks";
import {parse, recognize, CodePoints, Bytes, testMemoStorage} from "./parser.ts";
const text = process.env.PEGO_SPARSE_MEMO_INPUT
  ? readFileSync(process.env.PEGO_SPARSE_MEMO_INPUT, "utf8")
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
