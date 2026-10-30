package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestLiteralTextPredicateDispatchGeneration(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testLiteralTextPredicateDispatchGeneration)
}

func testLiteralTextPredicateDispatchGeneration(t *testing.T) {
	const source = `rule main = w:[a-z]+ #atomic {text($w) == "a" || "b" == text($w)} $$`
	g, err := parseTest(source)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "dispatch", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(code), `switch c.TextStr(`) || !strings.Contains(string(code), `case "a", "b":`) {
		t.Fatal("typed Go generator did not emit literal text dispatch")
	}
	baseline, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "dispatch", Start: "main", Types: true, DisableLiteralTextDispatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(baseline), `switch c.TextStr(`) || strings.Contains(string(code), "disableLiteralTextDispatch") {
		t.Fatal("generation-only control did not preserve the general baseline")
	}
	ts, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ts), `switch (c.textStr(`) || !strings.Contains(string(ts), `case "a": case "b":`) {
		t.Fatal("TypeScript generator did not emit literal text dispatch")
	}
	tsBaseline, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", DisableLiteralTextDispatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tsBaseline), `switch (c.textStr(`) {
		t.Fatal("TypeScript generation-only control did not preserve the general baseline")
	}
}

// TestGeneratedLiteralDispatchWorkload preserves paired generated parsers for
// separate runtime measurements when PEGO_LITERAL_DISPATCH_DIR is set.
func TestGeneratedLiteralDispatchWorkload(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("PEGO_LITERAL_DISPATCH_DIR")
	if dir == "" {
		t.Skip("set PEGO_LITERAL_DISPATCH_DIR to write standalone workload fixtures")
	}
	workloads := []literalDispatchWorkload{
		{name: "small", source: `rule main = w:[^!]+ #atomic {text($w) == "alpha" || "β" == text($w)} $$`, inputs: []literalDispatchInput{{Key: "hit-first", Value: "alpha"}, {Key: "hit-last-unicode", Value: "β"}, {Key: "miss", Value: "miss"}, {Key: "unicode-miss", Value: "éclair"}}},
		{name: "large", source: `rule main = w:[^!]+ #atomic {text($w) == "alpha" || text($w) == "bravo" || text($w) == "charlie" || text($w) == "delta" || text($w) == "éclair" || text($w) == "😀" || text($w) == "日本語" || text($w) == "λambda"} $$`, inputs: []literalDispatchInput{{Key: "hit-first", Value: "alpha"}, {Key: "hit-middle", Value: "delta"}, {Key: "hit-last", Value: "λambda"}, {Key: "hit-unicode", Value: "日本語"}, {Key: "miss", Value: "miss"}}},
		{name: "control", source: `rule main = w:[^!]+ #atomic {text($w) == "alpha" || len($w) == 1} $$`, inputs: []literalDispatchInput{{Key: "hit-first", Value: "alpha"}, {Key: "hit-length", Value: "x"}, {Key: "miss", Value: "miss"}, {Key: "unicode-miss", Value: "日本語"}}},
	}
	manifest := literalDispatchManifest{Schema: 1, GoVersion: "1.27.1"}
	for _, workload := range workloads {
		g, err := parseTest(workload.source)
		if err != nil {
			t.Fatalf("%s: %v", workload.name, err)
		}
		prog, err := Compile(g, compile.Options{})
		if err != nil {
			t.Fatalf("%s compile engine reference: %v", workload.name, err)
		}
		for i := range workload.inputs {
			input := &workload.inputs[i]
			for _, unit := range []struct {
				unit PositionUnit
				key  string
			}{{CodePoints, "codePoints"}, {Bytes, "bytes"}} {
				_, parseErr := prog.ParseWith("main", input.Value, ParseOptions{Unit: unit.unit})
				input.Expected = append(input.Expected, literalDispatchExpected{Unit: unit.key, Accepted: parseErr == nil})
			}
		}
		for _, mode := range []struct {
			name  string
			types bool
		}{{"go-node", false}, {"go-typed", true}} {
			for _, variant := range []struct {
				name     string
				disabled bool
			}{{"candidate", false}, {"reference", true}} {
				packageName := "dispatch"
				code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: packageName, Start: "main", Types: mode.types, Recognize: true, DisableLiteralTextDispatch: variant.disabled})
				if err != nil {
					t.Fatalf("%s/%s/%s: %v", workload.name, mode.name, variant.name, err)
				}
				out := filepath.Join(dir, workload.name, mode.name, variant.name)
				if err := os.MkdirAll(out, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, "parser.go"), code, 0o644); err != nil {
					t.Fatal(err)
				}
				fingerprint := sha256.Sum256(code)
				api := "variadic-unit"
				if strings.Contains(string(code), "func Parse(input string, opts ...pegort.ParseOption)") {
					api = "functional-options"
				}
				if mode.types {
					if !strings.Contains(string(code), "func ParseAST(") || !strings.Contains(string(code), "func Recognize(") {
						t.Fatalf("%s/%s/%s omitted typed parse or recognition", workload.name, mode.name, variant.name)
					}
				} else if !strings.Contains(string(code), "func Recognize(") {
					t.Fatalf("%s/%s/%s omitted recognition", workload.name, mode.name, variant.name)
				}
				manifest.Parsers = append(manifest.Parsers, literalDispatchParser{Workload: workload.name, Mode: mode.name, Variant: variant.name, SHA256: hex.EncodeToString(fingerprint[:]), UnitAPI: api})
				adapter := literalDispatchGoAdapter(mode.types, api)
				if err := os.WriteFile(filepath.Join(out, "fixture.go"), []byte(adapter), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		workloadDir := filepath.Join(dir, workload.name)
		if err := os.MkdirAll(workloadDir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(workload.inputs, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workloadDir, "inputs.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		inputHash := sha256.Sum256(data)
		manifest.Workloads = append(manifest.Workloads, literalDispatchWorkloadManifest{Name: workload.name, Source: workload.source, Inputs: workload.inputs, InputsSHA256: hex.EncodeToString(inputHash[:])})
		if err := writeLiteralDispatchTypeScript(t, workloadDir, workload, &manifest); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workloadDir, "driver_test.go"), []byte(literalDispatchGoDriverFor(workload.name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("literaldispatch")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mustJSONIndent(t, manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

type literalDispatchInput struct {
	Key      string                    `json:"key"`
	Value    string                    `json:"value"`
	Expected []literalDispatchExpected `json:"expected"`
}

type literalDispatchExpected struct {
	Unit     string `json:"unit"`
	Accepted bool   `json:"accepted"`
}

type literalDispatchWorkload struct {
	name   string
	source string
	inputs []literalDispatchInput
}

type literalDispatchParser struct {
	Workload string `json:"workload"`
	Mode     string `json:"mode"`
	Variant  string `json:"variant"`
	SHA256   string `json:"sha256"`
	UnitAPI  string `json:"unitAPI"`
}

type literalDispatchWorkloadManifest struct {
	Name         string                 `json:"name"`
	Source       string                 `json:"source"`
	Inputs       []literalDispatchInput `json:"inputs"`
	InputsSHA256 string                 `json:"inputsSHA256"`
}

type literalDispatchManifest struct {
	Schema    int                               `json:"schema"`
	GoVersion string                            `json:"goVersion"`
	Parsers   []literalDispatchParser           `json:"parsers"`
	Workloads []literalDispatchWorkloadManifest `json:"workloads"`
}

func mustJSONIndent(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func literalDispatchGoAdapter(typed bool, unitAPI string) string {
	unitExpr := `u`
	if unitAPI == "functional-options" {
		unitExpr = `WithUnit(u)`
	}
	parseCall := "Parse(input, " + unitExpr + ")"
	recognizeCall := "Recognize(input, " + unitExpr + ")"
	var b strings.Builder
	b.WriteString("package dispatch\n\nfunc fixtureUnit(n int) PositionUnit { if n == 1 { return Bytes }; return CodePoints }\n")
	b.WriteString("func FixtureParse(input string, unit int) bool { u := fixtureUnit(unit); _, err := " + parseCall + "; return err == nil }\n")
	b.WriteString("func FixtureRecognize(input string, unit int) bool { u := fixtureUnit(unit); return " + recognizeCall + " == nil }\n")
	if typed {
		typedCall := "ParseAST(input, " + unitExpr + ")"
		b.WriteString("func FixtureTyped(input string, unit int) bool { u := fixtureUnit(unit); _, err := " + typedCall + "; return err == nil }\n")
	}
	return b.String()
}

func writeLiteralDispatchTypeScript(t *testing.T, dir string, workload literalDispatchWorkload, manifest *literalDispatchManifest) error {
	t.Helper()
	g, err := parseTest(workload.source)
	if err != nil {
		return err
	}
	candidate, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		return err
	}
	reference, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", DisableLiteralTextDispatch: true})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "candidate.ts"), candidate, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "reference.ts"), reference, 0o644); err != nil {
		return err
	}
	for _, generated := range []struct {
		variant string
		source  []byte
	}{{"candidate", candidate}, {"reference", reference}} {
		sum := sha256.Sum256(generated.source)
		manifest.Parsers = append(manifest.Parsers, literalDispatchParser{Workload: workload.name, Mode: "typescript", Variant: generated.variant, SHA256: hex.EncodeToString(sum[:]), UnitAPI: "unit-argument"})
	}
	type tsInput struct {
		Key      string                    `json:"key"`
		Kind     string                    `json:"kind"`
		Value    string                    `json:"value"`
		Bytes    []int                     `json:"bytes"`
		Expected []literalDispatchExpected `json:"expected"`
	}
	inputs := make([]tsInput, 0, len(workload.inputs)*2)
	for _, input := range workload.inputs {
		for _, kind := range []string{"string", "bytes"} {
			inputs = append(inputs, tsInput{Key: input.Key, Kind: kind, Value: input.Value, Bytes: literalDispatchBytes(input.Value), Expected: input.Expected})
		}
	}
	inputJSON, err := json.MarshalIndent(inputs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ts-inputs.json"), inputJSON, 0o644); err != nil {
		return err
	}
	const driver = `
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { performance } from "node:perf_hooks";
import * as candidate from "./candidate.ts";
import * as reference from "./reference.ts";

const inputs = JSON.parse(readFileSync(new URL("./ts-inputs.json", import.meta.url), "utf8"));
const unitEntries = [["codePoints", candidate.CodePoints], ["bytes", candidate.Bytes]];
const accepts = (parser, item, unit) => {
  const input = item.kind === "bytes" ? new Uint8Array(item.bytes) : item.value;
  return parser.parse(input, unit).error === null;
};
const expected = (item, unitName) => item.expected.find((entry) => entry.unit === unitName).accepted;
const mode = process.argv[2] ?? "--check";
if (mode === "--check") {
  for (const item of inputs) {
    for (const [unitName, unit] of unitEntries) {
      const got = accepts(candidate, item, unit);
      const ref = accepts(reference, item, unit);
      const label = item.key + "/" + item.kind + "/" + unitName;
      assert.equal(got, ref, label + ": candidate/reference mismatch");
      assert.equal(got, expected(item, unitName), label + ": engine oracle mismatch");
    }
  }
  process.stdout.write("checked " + inputs.length * unitEntries.length + " cases\n");
} else if (mode === "--bench") {
  const iterations = Number(process.argv[3] ?? 100000);
  for (const [label, parser] of [["candidate", candidate], ["reference", reference]]) {
    for (const item of inputs) {
      for (const [unitName, unit] of unitEntries) {
        const input = item.kind === "bytes" ? new Uint8Array(item.bytes) : item.value;
        const start = performance.now();
        let accepted = false;
        for (let i = 0; i < iterations; i++) accepted = parser.parse(input, unit).error === null;
        const elapsedMs = performance.now() - start;
        process.stdout.write(JSON.stringify({ label, key: item.key, kind: item.kind, unit: unitName, iterations, elapsedMs, accepted }) + "\n");
      }
    }
  }
} else {
  throw new Error("usage: node bench.mjs [--check|--bench [iterations]]");
}
`
	if err := os.WriteFile(filepath.Join(dir, "bench.mjs"), []byte(driver), 0o644); err != nil {
		return err
	}
	return nil
}

func literalDispatchBytes(s string) []int {
	raw := []byte(s)
	bytes := make([]int, len(raw))
	for i, b := range raw {
		bytes[i] = int(b)
	}
	return bytes
}

const literalDispatchGoDriver = `package dispatch_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	nc "literaldispatch/$WORKLOAD/go-node/candidate"
	nr "literaldispatch/$WORKLOAD/go-node/reference"
	tc "literaldispatch/$WORKLOAD/go-typed/candidate"
	tr "literaldispatch/$WORKLOAD/go-typed/reference"
)

type fixtureInput struct { Key string; Value string; Expected []struct { Unit string; Accepted bool } }
type backend struct { name string; parse func(string,int) bool; recognize func(string,int) bool; typed func(string,int) bool }
var backends = []backend{
	{"go-node/candidate", nc.FixtureParse, nc.FixtureRecognize, nil},
	{"go-node/reference", nr.FixtureParse, nr.FixtureRecognize, nil},
	{"go-typed/candidate", tc.FixtureParse, tc.FixtureRecognize, tc.FixtureTyped},
	{"go-typed/reference", tr.FixtureParse, tr.FixtureRecognize, tr.FixtureTyped},
}
func loadInputs(b testing.TB, workload string) []fixtureInput {
	b.Helper(); data, err := os.ReadFile(workload); if err != nil { b.Fatal(err) }
	var inputs []fixtureInput; if err := json.Unmarshal(data, &inputs); err != nil { b.Fatal(err) }; return inputs
}

func TestGeneratedFixtureParity(t *testing.T) {
	const workload = "$WORKLOAD"
	for _, input := range loadInputs(t, "inputs.json") {
		for unitIndex, unitName := range []string{"codePoints", "bytes"} {
			want := input.Expected[unitIndex].Accepted
			for _, backend := range backends {
				t.Run(fmt.Sprintf("%s/%s/%s/%s", workload, input.Key, backend.name, unitName), func(t *testing.T) {
					if got := backend.parse(input.Value, unitIndex); got != want { t.Errorf("Parse accepted=%v, want %v", got, want) }
					if got := backend.recognize(input.Value, unitIndex); got != want { t.Errorf("Recognize accepted=%v, want %v", got, want) }
					if backend.typed != nil { if got := backend.typed(input.Value, unitIndex); got != want { t.Errorf("ParseAST accepted=%v, want %v", got, want) } }
				})
			}
		}
	}
}

func BenchmarkGeneratedFixture(b *testing.B) {
	const workload = "$WORKLOAD"
	for _, input := range loadInputs(b, "inputs.json") {
		for unitIndex, unitName := range []string{"codePoints", "bytes"} {
			for _, backend := range backends {
				for _, operation := range []struct { name string; fn func(string,int) bool }{{"Parse", backend.parse}, {"Recognize", backend.recognize}, {"ParseAST", backend.typed}} {
					if operation.fn == nil { continue }
					name := fmt.Sprintf("%s/%s/%s/%s/%s", operation.name, backend.name, workload, input.Key, unitName)
					b.Run(name, func(b *testing.B) { b.ReportAllocs(); for b.Loop() { operation.fn(input.Value, unitIndex) } })
				}
			}
		}
	}
}
`

func literalDispatchGoDriverFor(workload string) string {
	return strings.ReplaceAll(literalDispatchGoDriver, "$WORKLOAD", workload)
}
