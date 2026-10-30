package tsgen_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestLiteralTextPredicateDispatchTSDisabledParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	const source = `rule main = w:[^!]* #atomic {text($w) == "" || text($w) == "a" || text($w) == "b" || text($w) == "é" || text($w) == "😀" || text($w) == "a"} $$`
	g, err := parseTest(source)
	if err != nil {
		t.Fatal(err)
	}
	optimized, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", DisableLiteralTextDispatch: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, code := range map[string][]byte{"optimized.ts": optimized, "reference.ts": reference} {
		if err := os.WriteFile(filepath.Join(dir, name), code, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code := string(optimized)
	if !strings.Contains(code, `case "": case "a": case "b": case "é": case "😀":`) {
		t.Fatal("optimized TypeScript lacks a multi-label dispatch for all distinct literals")
	}
	if strings.Count(code, `case "a":`) != 1 {
		t.Fatal("duplicate literals were not collapsed in TypeScript dispatch")
	}
	if tsc, err := exec.LookPath("tsc"); err == nil {
		cmd := exec.Command(tsc, "--noEmit", "--strict", "--target", "ES2022", "--module", "NodeNext", "--moduleResolution", "NodeNext", "optimized.ts", "reference.ts")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("strict TypeScript check: %v\n%s", err, out)
		}
	} else {
		t.Log("tsc not found; strict TypeScript check skipped")
	}
	inputs := []struct {
		Key   string `json:"key"`
		Kind  string `json:"kind"`
		Value string `json:"value"`
		Bytes []int  `json:"bytes"`
	}{
		{Key: "empty", Kind: "string", Value: ""},
		{Key: "first", Kind: "string", Value: "a"},
		{Key: "middle", Kind: "string", Value: "b"},
		{Key: "unicode", Kind: "string", Value: "é"},
		{Key: "last", Kind: "string", Value: "😀"},
		{Key: "miss", Kind: "string", Value: "miss"},
		{Key: "byte-empty", Kind: "bytes", Bytes: []int{}},
		{Key: "byte-first", Kind: "bytes", Bytes: []int{0x61}},
		{Key: "byte-middle", Kind: "bytes", Bytes: []int{0x62}},
		{Key: "byte-unicode", Kind: "bytes", Bytes: []int{0xc3, 0xa9}},
		{Key: "byte-last", Kind: "bytes", Bytes: []int{0xf0, 0x9f, 0x98, 0x80}},
		{Key: "byte-miss", Kind: "bytes", Bytes: []int{0x6d, 0x69, 0x73, 0x73}},
		{Key: "invalid-bytes", Kind: "bytes", Bytes: []int{0xff}},
	}
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	const script = `
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import * as optimized from "./optimized.ts";
import * as reference from "./reference.ts";
const inputs = JSON.parse(readFileSync(new URL("./inputs.json", import.meta.url), "utf8"));
const out = [];
for (const item of inputs) {
  const input = item.kind === "bytes" ? new Uint8Array(item.bytes) : item.value;
  for (const unit of [optimized.CodePoints, optimized.Bytes]) {
    const a = optimized.parse(input, unit);
    const b = reference.parse(input, unit);
    const ok = a.error === null;
    assert.equal(ok, b.error === null, item.key + ": optimized/reference mismatch");
    out.push({ key: item.key, unit, ok });
  }
}
process.stdout.write(JSON.stringify(out));
`
	path := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, path)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Node disabled-route comparison: %v\n%s", err, out)
	}
	var got []struct {
		Key  string              `json:"key"`
		Unit engine.PositionUnit `json:"unit"`
		OK   bool                `json:"ok"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode Node results: %v\n%s", err, out)
	}
	prog, err := engine.Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantByKey := map[string]string{"empty": "", "first": "a", "middle": "b", "unicode": "é", "last": "😀", "miss": "miss", "byte-empty": "", "byte-first": "a", "byte-middle": "b", "byte-unicode": "é", "byte-last": "😀", "byte-miss": "miss", "invalid-bytes": string([]byte{0xff})}
	for _, result := range got {
		input := wantByKey[result.Key]
		_, parseErr := prog.ParseWith("main", input, engine.ParseOptions{Unit: result.Unit})
		want := parseErr == nil
		if result.OK != want {
			t.Errorf("%s, unit %v: TypeScript accepted=%v, engine accepted=%v (error %v)", result.Key, result.Unit, result.OK, want, parseErr)
		}
	}
}
