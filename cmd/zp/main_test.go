package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCLI(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := run(args, strings.NewReader(stdin), &out)
	return out.String(), err
}

// runGenGo runs gen for Go into a new directory and returns the text of the generated parser.go and
// api.go.
func runGenGo(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	out, err := runCLI(t, "", append([]string{"gen", "-module", "example.com/gen", "-o", dir}, args...)...)
	if err != nil || out != "" {
		return out, err
	}
	var text strings.Builder
	for _, name := range []string{"parser.go", "api.go", filepath.Join("types", "types.go")} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) && name == filepath.Join("types", "types.go") {
			continue // written with -types only
		}
		if err != nil {
			return "", err
		}
		text.Write(data)
	}
	return text.String(), nil
}

const kv = "@start\nrule main = k:[a-z]+ #atomic \"=\" v:[0-9]+ #atomic"

// TestParseSExprNamedNodeTypes checks that -f sexpr prints the name of a named node type before its
// brackets, for a node over a string, a tuple and a list, and prints a plain node[string] bare.
func TestParseSExprNamedNodeTypes(t *testing.T) {
	g := writeFile(t, "named.zp", `type Ident = node[string]
type Pair = node[(Ident, node[string])]
type Items = node[[]Ident]
type Out = { Id Ident; P Pair; Is Items; Plain node[string] }
@start
rule main Out = id:ident "," p:pair "," is:items "," plain:digits $$ => Out{Id: $id, P: $p, Is: $is, Plain: $plain}
rule ident Ident = [a-z]+ #atomic
rule pair Pair = "<" t:(ident -"=" digits) ">" => Pair{$t}
rule items Items = "[" xs:ident* "]" => Items{$xs}
rule digits = [0-9]+ #atomic
`)
	out, err := runCLI(t, "", "parse", "-g", g, "-i", "ab,<c=1>,[d],7", "-f", "sexpr")
	if err != nil {
		t.Fatal(err)
	}
	if want := `Out{Id: Ident("ab"), Is: Items[Ident("d")], P: Pair(Ident("c") "1"), Plain: "7"}` + "\n"; out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
	if out, err = runCLI(t, "", "parse", "-g", g, "-i", "ab,<c=1>,[d],7"); err != nil || !strings.Contains(out, `"type": "Ident"`) {
		t.Errorf("json: %q, %v", out, err)
	}
}

func TestParse(t *testing.T) {
	g := writeFile(t, "kv.zp", kv)
	out, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-f", "sexpr")
	if err != nil {
		t.Fatal(err)
	}
	if want := `("a" "=" "1")` + "\n"; out != want {
		t.Errorf("got %q", out)
	}
	out, err = runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-f", "sexpr", "-source-info")
	if want := `(k:"a" "=" v:"1")@main` + "\n"; err != nil || out != want {
		t.Errorf("got %q, %v", out, err)
	}
	out, err = runCLI(t, "b=2", "parse", "-g", g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"rule"`) || !strings.Contains(out, `"value": "b"`) {
		t.Errorf("got %s", out)
	}
	out, err = runCLI(t, "b=2", "parse", "-g", g, "-source-info")
	if err != nil || !strings.Contains(out, `"rule": "main"`) || !strings.Contains(out, `"captures"`) {
		t.Errorf("got %s, %v", out, err)
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "a="); err == nil || !strings.Contains(err.Error(), "1:3: syntax error") {
		t.Errorf("got %v", err)
	}
}

const messy = `// key-value pairs
package kv

@start
rule  main=k:[a-z]+ #atomic  "=" v:[0-9]+ #atomic // pair
`

const tidy = `// key-value pairs
package kv

@start
rule main = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic // pair
`

func TestFmt(t *testing.T) {
	g := writeFile(t, "kv.zp", messy)
	out, err := runCLI(t, "", "fmt", g)
	if err != nil || out != tidy {
		t.Errorf("got %q, %v", out, err)
	}
	// Without files, fmt formats standard input.
	if out, err := runCLI(t, messy, "fmt"); err != nil || out != tidy {
		t.Errorf("stdin: got %q, %v", out, err)
	}
	if out, err := runCLI(t, messy, "fmt", "-l"); err != nil || out != "<standard input>\n" {
		t.Errorf("stdin -l: got %q, %v", out, err)
	}
	if _, err := runCLI(t, messy, "fmt", "-w"); err == nil {
		t.Error("expected an error for -w with standard input")
	}
	// -l lists the files that need formatting; flags may follow files.
	ok := writeFile(t, "ok.zp", tidy)
	if out, err := runCLI(t, "", "fmt", ok, g, "-l"); err != nil || out != g+"\n" {
		t.Errorf("-l: got %q, %v", out, err)
	}
	// -w rewrites the files in place.
	if out, err := runCLI(t, "", "fmt", "-w", g); err != nil || out != "" {
		t.Errorf("-w: got %q, %v", out, err)
	}
	if data, err := os.ReadFile(g); err != nil || string(data) != tidy {
		t.Errorf("rewritten file: got %q, %v", data, err)
	}
	if out, err := runCLI(t, "", "fmt", "-l", g); err != nil || out != "" {
		t.Errorf("-l after -w: got %q, %v", out, err)
	}
}

// TestFmtCheck checks the exit status of fmt -check: 0 when every file is formatted, 1 when some
// differ (listing them, as -l does), and 2 when one cannot be formatted.
func TestFmtCheck(t *testing.T) {
	status := func(err error) int {
		var c exitCoder
		switch {
		case err == nil:
			return 0
		case errors.As(err, &c):
			return c.ExitCode()
		}
		return 1
	}
	clean := writeFile(t, "clean.zp", tidy)
	dirty := writeFile(t, "dirty.zp", messy)
	broken := writeFile(t, "broken.zp", "rule main = (")
	for _, tc := range []struct {
		name   string
		stdin  string
		args   []string
		out    string
		status int
	}{
		{"clean", "", []string{clean}, "", 0},
		{"dirty", "", []string{clean, dirty}, dirty + "\n", 1},
		{"broken", "", []string{dirty, broken}, dirty + "\n", 2},
		{"missing", "", []string{clean, broken + ".missing.zp"}, "", 2},
		{"clean stdin", tidy, nil, "", 0},
		{"dirty stdin", messy, nil, "<standard input>\n", 1},
		{"with -l", "", []string{"-l", dirty}, dirty + "\n", 1},
	} {
		out, err := runCLI(t, tc.stdin, append([]string{"fmt", "-check"}, tc.args...)...)
		if out != tc.out || status(err) != tc.status {
			t.Errorf("%s: got %q, status %d (%v), want %q, status %d", tc.name, out, status(err), err, tc.out, tc.status)
		}
	}
	if data, err := os.ReadFile(dirty); err != nil || string(data) != messy {
		t.Errorf("-check rewrote a file: %q, %v", data, err)
	}
	for _, args := range [][]string{{"-check", "-w", dirty}, {"-check", "-syntax", dirty}} {
		if _, err := runCLI(t, "", append([]string{"fmt"}, args...)...); err == nil || status(err) != 1 {
			t.Errorf("%v: got %v, want a usage error", args, err)
		}
	}
	// The plain modes keep exit status 1 for every error, and -l status 0 for a listing.
	if _, err := runCLI(t, "", "fmt", "-l", dirty); err != nil {
		t.Errorf("-l: %v", err)
	}
	if _, err := runCLI(t, "", "fmt", "-l", broken); status(err) != 1 {
		t.Errorf("-l with a broken file: status %d", status(err))
	}
}

func TestFmtRejectsOtherFormats(t *testing.T) {
	g := writeFile(t, "kv.zp", kv)
	js, err := runCLI(t, "", "convert", g)
	if err != nil {
		t.Fatal(err)
	}
	jg := writeFile(t, "kv.json", js)
	pc := filepath.Join(t.TempDir(), "kv.zpc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-o", pc); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{jg, pc} {
		if _, err := runCLI(t, "", "fmt", path); err == nil || !strings.Contains(err.Error(), "zp convert") {
			t.Errorf("%s: got %v", path, err)
		}
	}
	if _, err := runCLI(t, js, "fmt"); err == nil || !strings.Contains(err.Error(), "zp convert") {
		t.Errorf("JSON on stdin: got %v", err)
	}
	data, err := os.ReadFile(pc)
	if err != nil {
		t.Fatal(err)
	}
	// A compiled grammar holds no source, so fmt points to the source instead of convert.
	if _, err := runCLI(t, string(data), "fmt"); err == nil || !strings.Contains(err.Error(), "grammar source it was compiled from") || strings.Contains(err.Error(), "convert") {
		t.Errorf("compiled grammar on stdin: got %v", err)
	}
	// The old json command is gone.
	if _, err := runCLI(t, "", "json", g); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("json: got %v", err)
	}
}

func TestConvert(t *testing.T) {
	g := writeFile(t, "kv.zp", messy)
	js, err := runCLI(t, "", "convert", g)
	if err != nil || !strings.Contains(js, `"name": "kv"`) {
		t.Fatalf("got %q, %v", js, err)
	}
	if !strings.Contains(js, `"leadingComments": " key-value pairs\n"`) || strings.Contains(js, "//") {
		t.Errorf("normalized comments missing or raw markers serialized:\n%s", js)
	}
	jg := writeFile(t, "kv.json", js)
	// JSON converts to ZeroParse source by default, and to JSON with -to json.
	if out, err := runCLI(t, "", "convert", jg); err != nil || out != "// key-value pairs\npackage kv\n\n@start\nrule main = k:[a-z]+ #atomic \"=\" v:[0-9]+ #atomic // pair\n" {
		t.Errorf("got %q, %v", out, err)
	}
	if out, err := runCLI(t, "", "convert", "-to", "json", jg); err != nil || out != js {
		t.Errorf("got %q, %v", out, err)
	}
	// ZeroParse source converts to itself with -to zp, keeping comments.
	if out, err := runCLI(t, "", "convert", g, "-to", "zp"); err != nil || out != tidy {
		t.Errorf("got %q, %v", out, err)
	}
	if out, err := runCLI(t, "", "parse", "-g", jg, "-i", "x=9", "-f", "sexpr", "-source-info"); err != nil || !strings.Contains(out, `k:"x"`) {
		t.Errorf("got %q, %v", out, err)
	}
	// -o writes a file.
	path := filepath.Join(t.TempDir(), "out.json")
	if out, err := runCLI(t, "", "convert", "-o", path, g); err != nil || out != "" {
		t.Fatalf("got %q, %v", out, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != js {
		t.Errorf("file differs from stdout: %v", err)
	}
	if _, err := runCLI(t, "", "convert", "-to", "yaml", g); err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, "", "convert"); err == nil {
		t.Error("expected an error without a file")
	}
}

func TestErrors(t *testing.T) {
	bad := writeFile(t, "bad.zp", "rule main = (")
	if _, err := runCLI(t, "", "fmt", bad); err == nil || !strings.Contains(err.Error(), "bad.zp:1:14") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, "", "convert", bad); err == nil || !strings.Contains(err.Error(), "bad.zp:1:14") {
		t.Errorf("got %v", err)
	}
	g := writeFile(t, "kv.zp", kv)
	if _, err := runCLI(t, "", "parse", "-g", g, "-s", "nope"); err == nil || !strings.HasSuffix(err.Error(), ".zp: rule nope is not defined") {
		t.Errorf("got %v", err)
	}
	// Only a rule marked @start can be selected.
	two := writeFile(t, "two.zp", "@start\nrule main = \"a\"\nrule other = \"b\"")
	if _, err := runCLI(t, "", "parse", "-g", two, "-s", "other", "-i", "b"); err == nil || !strings.HasSuffix(err.Error(), ".zp: rule other is not a start rule; parsing can begin only at a rule marked @start") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, ""); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("got %v", err)
	}
}

func TestParseRecovered(t *testing.T) {
	g := writeFile(t, "r.zp", `@start
rule main = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")*`)
	out, err := runCLI(t, "", "parse", "-g", g, "-i", "a;1;b;", "-f", "sexpr")
	if !strings.Contains(out, `error("1:3: syntax error: expected [a-z]")`) || err == nil || !strings.Contains(err.Error(), "1:3: syntax error") {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestParseStream(t *testing.T) {
	g := writeFile(t, "s.zp", `@start
rule main = ([a-z]+ #atomic -"\n")* #stream`)
	out, err := runCLI(t, "ab\ncd\n", "parse", "-g", g, "-stream", "-f", "sexpr")
	if err != nil || out != "(\"ab\")\n(\"cd\")\n" {
		t.Errorf("got %q, %v", out, err)
	}
	out, err = runCLI(t, "ab\n", "parse", "-g", g, "-stream")
	if err != nil || !strings.HasPrefix(out, `{"start":0,"end":3,"value":`) || strings.Count(out, "\n") != 1 {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestGen(t *testing.T) {
	g := writeFile(t, "kv.zp", kv)
	out, err := runGenGo(t, "-g", g, "-pkg", "kv")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"// Code generated by zp. DO NOT EDIT.", "package kv", "func Parse(input string, opts ...rt.ParseOption) (*rt.Node, error)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	dir := filepath.Join(t.TempDir(), "kv")
	if _, err := runCLI(t, "", "gen", "-g", g, "-module", "example.com/kv", "-o", dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"parser.go", "api.go", filepath.Join("internal", "runtime", "runtime.go")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("embedded runtime mode did not write %s: %v", name, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "parser.go")); err != nil || !strings.HasPrefix(string(data), "// Code generated by zp. DO NOT EDIT.") {
		t.Errorf("parser.go: %v", err)
	}
	if _, err := runCLI(t, "", "gen", "-g", g, "-module", "example.com/kv"); err == nil {
		t.Error("expected an error without -o")
	}
	if _, err := runCLI(t, "", "gen", "-g", g); err == nil {
		t.Error("expected an error without -pkg")
	}
	if strings.Contains(out, "func ParseAST(") {
		t.Error("ParseAST generated without -types")
	}
	typed, err := runGenGo(t, "-g", g, "-pkg", "kv", "-types")
	if err != nil || !strings.Contains(typed, "func ParseAST(") {
		t.Errorf("-types: no ParseAST: %v", err)
	}
	if strings.Contains(out, "func Recognize(") {
		t.Error("Recognize generated without -recognize")
	}
	rec, err := runGenGo(t, "-g", g, "-pkg", "kv", "-recognize")
	if err != nil || !strings.Contains(rec, "func Recognize(") {
		t.Errorf("-recognize: no Recognize: %v", err)
	}
	if !strings.Contains(out, "// Package kv is a parser generated from a ZeroParse grammar.") {
		t.Error("no package comment")
	}
	nodoc, err := runGenGo(t, "-g", g, "-pkg", "kv", "-nodoc")
	if err != nil || strings.Contains(nodoc, "// Package kv") || !strings.Contains(nodoc, "package kv") {
		t.Errorf("-nodoc: package comment still there: %v", err)
	}
}

func TestGenDocument(t *testing.T) {
	g := writeFile(t, "node.zp", `@start
rule main = "a"`)
	out, err := runGenGo(t, "-g", g, "-pkg", "node", "-document")
	if err != nil || !strings.Contains(out, "func NewDocument(") {
		t.Fatalf("-document: missing constructor: %v", err)
	}
	typed, err := runGenGo(t, "-g", g, "-pkg", "node", "-document", "-types")
	if err != nil || !strings.Contains(typed, "func (d *Document) ParseAST(") {
		t.Fatalf("-document -types: %v", err)
	}
	if _, err := runCLI(t, "", "gen", "-lang", "ts", "-g", g, "-document"); err == nil || !strings.Contains(err.Error(), "-document") {
		t.Fatalf("-lang ts -document: %v", err)
	}
}

// runGenTS runs gen -lang ts into a new directory and returns the directory.
func runGenTS(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	_, err := runCLI(t, "", append([]string{"gen", "-lang", "ts", "-o", dir}, args...)...)
	return dir, err
}

// readTS returns the contents of the named files of dir, failing if one is missing.
func readTS(t *testing.T, dir string, names ...string) string {
	t.Helper()
	var all strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	return all.String()
}

func TestGenTypeScriptBudgetControls(t *testing.T) {
	g := writeFile(t, "kv.zp", kv)
	plainDir, err := runGenTS(t, "-g", g)
	if err != nil {
		t.Fatal(err)
	}
	controlledDir, err := runGenTS(t, "-g", g, "-budget-controls")
	if err != nil {
		t.Fatalf("-lang ts -budget-controls: %v", err)
	}
	plain, controlled := readTS(t, plainDir, "parser.ts"), readTS(t, controlledDir, "parser.ts")
	if strings.Contains(plain, "budget") || strings.Contains(plain, "CancellationSignal") || !strings.Contains(plain, "PlainParseOptions") {
		t.Error("a parser generated without -budget-controls has budget controls")
	}
	if !strings.Contains(controlled, "CancellationSignal") || strings.Contains(controlled, "PlainParseOptions") {
		t.Error("a parser generated with -budget-controls has no budget controls")
	}
	if _, err := runGenTS(t, "-runtime", "only", "-budget-controls"); err == nil || !strings.Contains(err.Error(), "-budget-controls cannot be combined with -runtime only") {
		t.Errorf("-runtime only -budget-controls: %v", err)
	}
}

func TestGenTypeScript(t *testing.T) {
	g := writeFile(t, "kv.zp", kv)
	dir, err := runGenTS(t, "-g", g)
	if err != nil {
		t.Fatal(err)
	}
	out := readTS(t, dir, "parser.ts")
	for _, want := range []string{"// Code generated by zp. DO NOT EDIT.", "export function parse(input: string | Uint8Array, unit?: Unit): ParseResult", `from "./runtime.ts"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "export function recognize(") {
		t.Error("recognize generated without -recognize")
	}
	if rt := readTS(t, dir, "runtime.ts"); !strings.HasPrefix(rt, "// Code generated by zp.") || !strings.Contains(rt, "export function mkGrammar(") {
		t.Error("runtime.ts is not the runtime")
	}
	if _, err := os.Stat(filepath.Join(dir, "types.ts")); err == nil {
		t.Error("types.ts generated without -types")
	}
	dir, err = runGenTS(t, "-g", g, "-recognize")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readTS(t, dir, "parser.ts"), "export function recognize(") {
		t.Error("-recognize: no recognize")
	}
	dir, err = runGenTS(t, "-g", g, "-types")
	typed := ""
	if err == nil {
		typed = readTS(t, dir, "parser.ts", "types.ts")
	}
	if err != nil || !strings.Contains(typed, "export function parseAST(") || !strings.Contains(typed, "export function parseASTWithOptions(") || !strings.Contains(typed, "export interface TypedParseResult") {
		t.Errorf("TS -types: missing typed entry points: %v", err)
	}
	for _, args := range [][]string{
		{"gen", "-lang", "ts", "-g", g},
		{"gen", "-lang", "ts", "-o", t.TempDir()},
		{"gen", "-lang", "ts", "-g", g, "-o", t.TempDir(), "-pkg", "kv"},
		{"gen", "-lang", "ts", "-g", g, "-o", t.TempDir(), "-nodoc"},
		{"gen", "-lang", "rust", "-g", g, "-pkg", "kv"},
	} {
		if _, err := runCLI(t, "", args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestCompile(t *testing.T) {
	g := writeFile(t, "kv.zp", strings.Replace(kv, "@start", "@start(default)", 1)+"\n@start\nrule other = \"x\"")
	out := filepath.Join(t.TempDir(), "kv.zpc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-source-info", "-o", out); err != nil {
		t.Fatal(err)
	}
	got, err := runCLI(t, "", "parse", "-g", out, "-i", "a=1", "-f", "sexpr")
	if err != nil || got != `(k:"a" "=" v:"1")@main`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := runCLI(t, "", "parse", "-g", out, "-s", "other", "-i", "x", "-f", "sexpr"); err != nil || got != `"x"@other`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	// The default mode is parse.
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := zp.LoadParser(data); err != nil {
		t.Fatal(err)
	} else if mode, ok := p.CompiledMode(); !ok || mode != zp.CompiledParse {
		t.Errorf("default mode %v %v, want parse", mode, ok)
	}
	// A compiled grammar holds no grammar: it does not convert, with or without -to.
	for _, args := range [][]string{{"convert", "-to", "zp", out}, {"convert", "-to", "json", out}, {"convert", out}} {
		if _, err := runCLI(t, "", args...); err == nil || !strings.Contains(err.Error(), "needs the grammar source") {
			t.Errorf("%v: got %v", args, err)
		}
	}
	if _, err := runCLI(t, "", "compile", "-g", g); err == nil {
		t.Error("expected an error without -o")
	}
	if _, err := runCLI(t, "", "compile", "-g", g, "-mode", "ast", "-o", out); err == nil || !strings.Contains(err.Error(), "unknown compiled grammar mode") {
		t.Errorf("unknown mode: %v", err)
	}
	// A file runs on the bytecode backends, for the parses of its mode.
	bare := filepath.Join(t.TempDir(), "kv-bare.zpc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-o", bare); err != nil {
		t.Fatal(err)
	}
	if got, err := runCLI(t, "", "parse", "-g", bare, "-i", "a=1", "-f", "sexpr"); err != nil || got != `("a" "=" "1")`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	// Source information is compiled in: a file without it cannot provide it.
	if _, err := runCLI(t, "", "parse", "-g", bare, "-i", "a=1", "-source-info"); err == nil || !strings.Contains(err.Error(), "compile it with -source-info") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, "", "parse", "-g", bare, "-i", "a=1", "-backend", "closure"); err == nil || !strings.Contains(err.Error(), "closure backend needs the grammar source") {
		t.Errorf("closure backend on a compiled grammar: %v", err)
	}
	if _, err := runCLI(t, "", "parse", "-g", bare, "-i", "a=1", "-check"); err == nil || !strings.Contains(err.Error(), "needs recognize mode") {
		t.Errorf("recognition on a parse-mode file: %v", err)
	}
	for _, cmd := range []string{"trace", "profile", "explain"} {
		if _, err := runCLI(t, "", cmd, "-g", bare, "-i", "a=1"); err == nil || !strings.Contains(err.Error(), "needs document mode") {
			t.Errorf("%s on a parse-mode file: %v", cmd, err)
		}
	}
	for _, mode := range []string{"recognize", "document"} {
		path := filepath.Join(t.TempDir(), mode+".zpc")
		if _, err := runCLI(t, "", "compile", "-g", g, "-mode", mode, "-o", path); err != nil {
			t.Fatal(err)
		}
		if _, err := runCLI(t, "", "parse", "-g", path, "-i", "a=1"); err == nil || !strings.Contains(err.Error(), "the compiled grammar is in "+mode+" mode; parsing needs parse mode") {
			t.Errorf("%s mode parse: %v", mode, err)
		}
		for _, cmd := range []string{"trace", "profile", "explain"} {
			_, err := runCLI(t, "", cmd, "-g", path, "-i", "a=1")
			if served := mode == "document"; served != (err == nil) {
				t.Errorf("%s on a %s-mode file: %v", cmd, mode, err)
			}
		}
		if mode == "document" {
			want, err := runCLI(t, "", "explain", "-g", g, "-i", "a=")
			if err != nil {
				t.Fatal(err)
			}
			if got, err := runCLI(t, "", "explain", "-g", path, "-i", "a="); err != nil || got != want || !strings.Contains(got, "expected") {
				t.Errorf("explain on a document-mode file: %v\n%s\nwant\n%s", err, got, want)
			}
		}
		// A file is saved again in its own mode only.
		copyPath := filepath.Join(t.TempDir(), "copy.zpc")
		if _, err := runCLI(t, "", "compile", "-g", path, "-o", copyPath); err == nil || !strings.Contains(err.Error(), "pass -mode "+mode) {
			t.Errorf("%s mode resaved as parse: %v", mode, err)
		}
		if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
			t.Errorf("compile wrote output after rejecting the mode: stat error %v", err)
		}
		if _, err := runCLI(t, "", "compile", "-g", path, "-mode", mode, "-o", copyPath); err != nil {
			t.Fatal(err)
		}
		a, _ := os.ReadFile(path)
		b, _ := os.ReadFile(copyPath)
		if !bytes.Equal(a, b) {
			t.Errorf("%s mode: saving again changed the file", mode)
		}
	}
	if got, err := runCLI(t, "", "parse", "-g", filepath.Join(t.TempDir(), "missing.zpc"), "-i", "a=1"); err == nil {
		t.Errorf("missing file: %q", got)
	}
	// Without -s, the start rule saved by compile -s is used.
	other := filepath.Join(t.TempDir(), "other.zpc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-s", "other", "-source-info", "-o", other); err != nil {
		t.Fatal(err)
	}
	if got, err := runCLI(t, "", "parse", "-g", other, "-i", "x", "-f", "sexpr"); err != nil || got != `"x"@other`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := runCLI(t, "", "parse", "-g", other, "-s", "main", "-i", "a=1", "-f", "sexpr"); err != nil || got != `(k:"a" "=" v:"1")@main`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	// Generating code needs the grammar source.
	if _, err := runGenGo(t, "-g", other, "-pkg", "p"); err == nil || !strings.Contains(err.Error(), "gen needs the grammar source") {
		t.Errorf("gen from a .zpc: %v", err)
	}
	if _, err := runGenGo(t, "-g", g, "-pkg", "p", "-s", "other"); err != nil {
		t.Fatal(err)
	}
	broken := writeFile(t, "broken.zpc", "ZPC\x00\x01garbage")
	if _, err := runCLI(t, "", "parse", "-g", broken, "-i", "a=1"); err == nil || !strings.Contains(err.Error(), "broken.zpc") {
		t.Errorf("got %v", err)
	}
}

func TestParseUnit(t *testing.T) {
	g := writeFile(t, "u.zp", `@start
rule main = "é" "a"`)
	_, err := runCLI(t, "", "parse", "-g", g, "-i", "éb", "-unit", "bytes")
	if err == nil || !strings.Contains(err.Error(), "1:3: syntax error") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "éa", "-unit", "nope"); err == nil {
		t.Error("expected an error")
	}
}

func TestParseBackend(t *testing.T) {
	g := writeFile(t, "kv.zp", kv)
	for _, b := range []string{"closure", "bytecode", "bytecode-iterative"} {
		got, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-f", "sexpr", "-backend", b)
		if err != nil || got != `("a" "=" "1")`+"\n" {
			t.Errorf("%s: got %q, %v", b, got, err)
		}
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-backend", "nope"); err == nil {
		t.Error("expected an error")
	}
}

func TestParseCheck(t *testing.T) {
	g := writeFile(t, "kv.zp", kv)
	if got, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-check"); err != nil || got != "ok\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "a=", "-check"); err == nil {
		t.Error("expected an error")
	}
}

func TestGenMaxDepth(t *testing.T) {
	grammar := writeFile(t, "depth.zp", `@start
rule main = "x"`)
	for _, lang := range []string{"go", "ts"} {
		args := []string{"-g", grammar, "-lang", lang}
		run := func(args ...string) (string, error) {
			if lang == "go" {
				return runGenGo(t, args...)
			}
			dir := t.TempDir()
			if _, err := runCLI(t, "", append([]string{"gen", "-o", dir}, args...)...); err != nil {
				return "", err
			}
			return readTS(t, dir, "parser.ts"), nil
		}
		base, err := run(args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, depth := range []string{"0", "100000"} {
			got, err := run(append(args, "-max-depth", depth)...)
			if err != nil || got != base {
				t.Fatalf("%s default %s differs: %v", lang, depth, err)
			}
		}
		got, err := run(append(args, "-max-depth", "12")...)
		want := "var zpGrammar = rt.NewGrammar(12)"
		if lang == "ts" {
			want = "const G = mkGrammar(12, "
		}
		if err != nil || !strings.Contains(got, want) {
			t.Fatalf("%s custom default: %v", lang, err)
		}
		output := filepath.Join(t.TempDir(), "parser")
		if err := os.WriteFile(output, []byte("preserved"), 0644); err != nil {
			t.Fatal(err)
		}
		for _, depth := range []string{"-1", "99999999999999999999999999999"} {
			module := []string{"-module", "example.com/depth"}
			if lang == "ts" {
				module = nil
			}
			got, err := runCLI(t, "", append(append([]string{"gen"}, module...), append(args, "-max-depth", depth, "-o", output)...)...)
			if err == nil || got != "" {
				t.Fatalf("%s invalid %s wrote output: %v", lang, depth, err)
			}
			b, err := os.ReadFile(output)
			if err != nil || string(b) != "preserved" {
				t.Fatalf("invalid configuration changed output: %v", err)
			}
		}
	}
}

func TestGenObservability(t *testing.T) {
	path := writeFile(t, "trace.zp", `@start
rule main = "a"`)
	out, err := runGenGo(t, "-g", path, "-pkg", "fixture", "-observability", "-recognize")
	if err != nil || !strings.Contains(out, "func WithTrace(") || !strings.Contains(out, "func Recognize(") {
		t.Fatalf("trace generation: %v\n%s", err, out)
	}
	if out, err := runGenGo(t, "-g", path, "-pkg", "fixture", "-observability", "-types"); err != nil || !strings.Contains(out, "func ParseAST(") || !strings.Contains(out, "func WithTrace(") {
		t.Fatalf("typed trace generation: %v\n%s", err, out)
	}
	dir, err := runGenTS(t, "-g", path, "-observability", "-types", "-recognize")
	if err != nil {
		t.Fatalf("TypeScript trace generation: %v", err)
	}
	if out := readTS(t, dir, "runtime.ts", "parser.ts"); !strings.Contains(out, "export interface TraceEvent") || !strings.Contains(out, "export function parseASTWithOptions(") || !strings.Contains(out, "mkGrammar(100000, false, true, false)") {
		t.Fatalf("TypeScript trace generation: missing trace declarations\n%s", out)
	}
	for _, flags := range [][]string{{"-document"}, {"-types", "-document"}} {
		args := []string{"-g", path, "-observability", "-pkg", "fixture"}
		args = append(args, flags...)
		if out, err := runGenGo(t, args...); err != nil || !strings.Contains(out, "func NewDocument(") || !strings.Contains(out, "func WithProfile(") {
			t.Fatalf("Document observation combination %v: %v\n%s", flags, err, out)
		}
	}
}
