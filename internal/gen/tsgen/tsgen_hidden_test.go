package tsgen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// A hidden rule, and the rule of an inline action, is not an entry point of a TypeScript parser
// either: both rule-by-name functions refuse it, after the options, as the Go ones do, and still
// parse the declared rules.
func TestGeneratedTSHiddenRulesAreNotEntryPoints(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs node")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	g, err := parseTest(`rule main = _a (x:"b" => $x) _c
rule _a = "a"
rule _c = "c"`)
	if err != nil {
		t.Fatal(err)
	}
	files, err := gentest.GenerateTS(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTSFiles(t, dir, files)
	const consumer = `import {parseRule, parseRuleWithOptions} from "./parser.ts";
declare const process: {stdout: {write(s: string): void}};
const fail = (m: string): never => { throw new Error(m); };
for (const [name, input] of [["_a", "a"], ["_c", "c"], ["_main_1", "b"]] as const) {
  for (const r of [parseRule(name, input), parseRuleWithOptions(name, input)]) {
    if (r.node !== null || r.error === null || !r.error.message.includes("hidden") || r.error.message.includes("not defined")) fail(name + ": " + r.error?.message);
  }
}
// The options are validated first, as in the Go parser.
const bad = parseRuleWithOptions("_a", "a", {maxDepth: -1});
if (bad.error === null || bad.error.message.includes("hidden")) fail("options: " + bad.error?.message);
for (const r of [parseRule("main", "abc"), parseRuleWithOptions("main", "abc")]) {
  if (r.error !== null || r.node === null) fail("main: " + r.error?.message);
}
const undef = parseRule("zz", "a");
if (undef.error === null || !undef.error.message.includes("not defined")) fail("undefined: " + undef.error?.message);
process.stdout.write("ok\n");
`
	if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, gentest.NodeTSArgs("consumer.ts")...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("node: %v\n%s", err, out)
	}
}
