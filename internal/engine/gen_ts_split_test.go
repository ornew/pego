package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/tsgen"
)

// tsStrictFlags are the strictest checks there are, so that generated code compiles in any project;
// ES2020 is the oldest target it supports (bigint).
var tsStrictFlags = []string{"--target", "es2020", "--lib", "es2020", "--strict",
	"--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch",
	"--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess",
	"--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax"}

// writeTSFiles writes the generated files into dir, with the package.json that runs them.
func writeTSFiles(t *testing.T, dir string, files []gen.GeneratedFile) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		path := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"package.json": `{"type":"module"}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// nodeTSArgs are the arguments that run a .ts script of a directory written by writeTSFiles.
func nodeTSArgs(script string, args ...string) []string {
	return append([]string{script}, args...)
}

// TestGeneratedTSSplitFiles compiles the files as generate writes them (runtime.ts, types.ts and
// parser.ts) with the strictest checks, and parses through them with Node.js, in each runtime mode.
// Most other tests join the files into one module; this one proves that the files fit together.
func TestGeneratedTSSplitFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs tsc and node")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	const src = `type Pair = { Key string; Values []int }
rule main Pair = k:key "=" vs:num+ => Pair{Key: $k, Values: $vs.Value}
rule key string = t:([a-z]+) #atomic => text($t)
rule num int = t:([0-9]+ ","?) #atomic => 1`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	const consumer = `import {parse, parseAST, parseWithOptions, SyntaxError, CodePoints, Node} from "./parser.ts";
import type {Pair} from "./parser.ts";
declare const process: {stdout: {write(s: string): void}};
const n = parse("ab=1,2,3");
if (n.error !== null || !(n.node instanceof Node)) throw new Error("parse failed");
const r = parseAST("ab=1,2,3");
const v: Pair | null = r.value;
if (r.error !== null || v === null || v.Values?.length !== 3) throw new Error("parseAST failed " + JSON.stringify(r.error));
const bad = parseAST("1=");
if (!(bad.error instanceof SyntaxError) || bad.value !== null) throw new Error("expected a syntax error");
const o = parseWithOptions("ab=1", {unit: CodePoints, trace() {}});
if (o.error === null || !String(o.error.message).includes("observability")) throw new Error("trace must be rejected: " + o.error?.message);
process.stdout.write("ok\n");
`
	check := func(t *testing.T, dir string, entries ...string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(consumer), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(tsc, append(append([]string{"--noEmit", "--allowImportingTsExtensions"}, tsStrictFlags...), append(entries, "consumer.ts")...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc: %v\n%s", err, out)
		}
		// Node.js runs the files as generated, by type stripping, with no resolve hook.
		cmd = exec.Command(node, nodeTSArgs("consumer.ts")...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "ok" {
			t.Fatalf("node: %v\n%s", err, out)
		}
		// A project that compiles to JavaScript emits .js specifiers with the documented settings.
		cmd = exec.Command(tsc, append(append([]string{"--rewriteRelativeImportExtensions", "--outDir", "out"}, tsStrictFlags...), append(entries, "consumer.ts")...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc emit: %v\n%s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, "out", "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command(node, "consumer.js")
		cmd.Dir = filepath.Join(dir, "out")
		if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "ok" {
			t.Fatalf("node on the emitted JavaScript: %v\n%s", err, out)
		}
	}
	t.Run("embed", func(t *testing.T) {
		files, err := GenerateTS(g, gen.GenOptions{Start: "main", Types: true, Recognize: true})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, f := range files {
			names = append(names, f.Path)
		}
		if got := strings.Join(names, " "); got != "runtime.ts parser.ts types.ts" {
			t.Fatalf("files %q", got)
		}
		dir := t.TempDir()
		writeTSFiles(t, dir, files)
		check(t, dir, "runtime.ts", "types.ts", "parser.ts")
	})
	t.Run("import", func(t *testing.T) {
		// The runtime lives in a directory of its own, as a project that shares one runtime keeps it.
		files, err := GenerateTS(g, gen.GenOptions{Start: "main", Types: true, Runtime: gen.RuntimeImport, RuntimeModule: "./shared/rt.ts"})
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTSFiles(t, dir, files)
		writeTSFiles(t, filepath.Join(dir, "shared"), []gen.GeneratedFile{{Path: "rt.ts", Content: tsgen.TsRuntimeFiles()[0].Content}})
		if _, err := os.Stat(filepath.Join(dir, "runtime.ts")); err == nil {
			t.Fatal("import mode wrote runtime.ts")
		}
		check(t, dir, "shared/rt.ts", "types.ts", "parser.ts")
	})
	t.Run("only", func(t *testing.T) {
		// A runtime alone compiles, and equals the runtime that embedded mode writes for any grammar.
		only := tsgen.TsRuntimeFiles()
		if len(only) != 1 || only[0].Path != "runtime.ts" {
			t.Fatalf("only wrote %v", only)
		}
		g2, err := parseTest(`rule main = "x"`)
		if err != nil {
			t.Fatal(err)
		}
		for _, gr := range []struct {
			g    *grammar.Grammar
			opts gen.GenOptions
		}{{g, gen.GenOptions{Start: "main", Types: true}}, {g2, gen.GenOptions{Start: "main", Observability: true}}} {
			files, err := GenerateTS(gr.g, gr.opts)
			if err != nil {
				t.Fatal(err)
			}
			if string(files[0].Content) != string(only[0].Content) {
				t.Error("runtime.ts depends on the grammar or its options")
			}
		}
		dir := t.TempDir()
		writeTSFiles(t, dir, only)
		cmd := exec.Command(tsc, append(append([]string{"--noEmit"}, tsStrictFlags...), "runtime.ts")...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc: %v\n%s", err, out)
		}
	})
}

// TestGeneratedTSReservedTypes checks that a grammar type named like a declaration of types.ts
// is rejected, instead of silently renamed or shadowing it.
func TestGeneratedTSReservedTypes(t *testing.T) {
	for _, name := range tsgen.TsReservedTypes {
		g, err := parseTest("type " + name + " = { Text string }\nrule main " + name + " = t:(\"x\") #atomic => " + name + "{Text: text($t)}")
		if err != nil {
			t.Errorf("type %s: %v", name, err)
			continue
		}
		_, err = GenerateTS(g, gen.GenOptions{Start: "main", Types: true})
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("type %s: error %v", name, err)
		}
	}
}

// TestTSRuntimePrintRuns checks the table of printable code points in the runtime against the one
// the generator computes from strconv.IsPrint.
func TestTSRuntimePrintRuns(t *testing.T) {
	want := `export const printRuns = "` + tsgen.TsPrintRuns() + `";`
	if !strings.Contains(tsgen.TsRuntimeText(), want) {
		t.Error("printRuns in runtime.ts differs from tsPrintRuns()")
	}
}

// runTSSplit generates the files of a grammar, compiles them with a consumer under the strictest
// checks, and runs the consumer with Node.js. The consumer prints "ok" when its checks pass.
func runTSSplit(t *testing.T, src string, opts gen.GenOptions, consumer string) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs tsc and node")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTSFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tsc, append(append([]string{"--noEmit", "--allowImportingTsExtensions"}, tsStrictFlags...), "parser.ts", "consumer.ts")...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
	cmd = exec.Command(node, nodeTSArgs("consumer.ts")...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("node: %v\n%s", err, out)
	}
}

// TestGeneratedTSTraceNameCollisions parses with grammar types that are named like the runtime's
// tracing declarations: types.ts is a module of its own, so the names do not collide.
func TestGeneratedTSTraceNameCollisions(t *testing.T) {
	runTSSplit(t, `type TraceKind = { Text string }
type TraceEvent = { Value Observation }
type Observation = { Text string }
type ObservationCall = { Text string }
type TraceFailure = { Text string }
rule main TraceEvent = p:part => TraceEvent{Value:$p}
rule part Observation = t:("a") #atomic => Observation{Text: text($t)}`, gen.GenOptions{Start: "main", Types: true, Recognize: true, Observability: true}, `import {parseWithOptions, parseASTWithOptions} from "./parser.ts";
import type {TraceEvent} from "./parser.ts";
import type {TraceKind, TraceEvent as GrammarTraceEvent, Observation, ObservationCall, TraceFailure} from "./types.ts";
declare const process: {stdout: {write(s: string): void}};
const names: [TraceKind|null, Observation|null, ObservationCall|null, TraceFailure|null] = [null, null, null, null];
const events: TraceEvent[] = [];
const r = parseWithOptions("a", {trace: (e) => { events.push(e); }});
const typed = parseASTWithOptions("a", {trace: () => {}});
const value: GrammarTraceEvent | null = typed.value;
if (names.length !== 4 || r.error !== null || typed.error !== null || value === null) throw new Error("parse failed");
if (events.filter((e) => e.rule === "part" && e.kind === "enter").length !== 1) throw new Error("collision calls");
process.stdout.write("ok\n");
`)
}

// TestGeneratedTSProfileNameCollisions profiles a parser whose grammar types are named like the
// runtime's profiling declarations.
func TestGeneratedTSProfileNameCollisions(t *testing.T) {
	runTSSplit(t, "type Profile = { Text string }\ntype ProfileSnapshot = { Text string }\ntype RuleProfile = { Text string }\ntype ProfileLocation = { Text string }\ntype SkipReasonCount = { Text string }\ntype ProfileObservation = { Text string }\ntype ProfileFrame = { Text string }\ntype ProfileData = { Text string }\ntype WeakMap = { Text string }\nrule main Profile = t:(part) #atomic => Profile{Text: text($t)}\nrule part Profile = t:(\"a\") #atomic => Profile{Text: text($t)}",
		gen.GenOptions{Start: "main", Types: true, Recognize: true, Observability: true}, `import {Profile, parseWithOptions, parseASTWithOptions} from "./parser.ts";
import type {Profile as GrammarProfile, ProfileSnapshot, RuleProfile, ProfileLocation, SkipReasonCount, ProfileObservation, ProfileFrame, ProfileData, WeakMap} from "./types.ts";
declare const process: {stdout: {write(s: string): void}};
const names: [ProfileSnapshot|null, RuleProfile|null, ProfileLocation|null, SkipReasonCount|null, ProfileObservation|null, ProfileFrame|null, ProfileData|null, WeakMap|null] = [null, null, null, null, null, null, null, null];
const profile = new Profile();
const r = parseWithOptions("a", {profile});
const typed: GrammarProfile | null = parseASTWithOptions("a", {profile}).value;
const snapshot = profile.snapshot();
if (names.length !== 8 || r.error !== null || typed === null || snapshot.parses !== 2 || snapshot.entered < 2) throw new Error("profile " + JSON.stringify(snapshot));
process.stdout.write("ok\n");
`)
}
