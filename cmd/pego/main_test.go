package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

const kv = `def main = k:@(?a-z)+ "=" v:@(?0-9)+`

func TestParse(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	out, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-f", "sexpr")
	if err != nil {
		t.Fatal(err)
	}
	if want := `(Seq "a" "=" "1" k="a" v="1")@main` + "\n"; out != want {
		t.Errorf("got %q", out)
	}
	out, err = runCLI(t, "b=2", "parse", "-g", g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"rule": "main"`) || !strings.Contains(out, `"text": "b"`) {
		t.Errorf("got %s", out)
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "a="); err == nil || !strings.Contains(err.Error(), "1:3: syntax error") {
		t.Errorf("got %v", err)
	}
}

const messy = `// key-value pairs
package kv


def main = k:@(?a-z)+   "=" v:@(?0-9)+ // pair
`

const tidy = `// key-value pairs
package kv

def main = k:@(?a-z)+ "=" v:@(?0-9)+ // pair
`

func TestFmt(t *testing.T) {
	g := writeFile(t, "kv.pego", messy)
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
	ok := writeFile(t, "ok.pego", tidy)
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

func TestFmtRejectsOtherFormats(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	js, err := runCLI(t, "", "convert", g)
	if err != nil {
		t.Fatal(err)
	}
	jg := writeFile(t, "kv.json", js)
	pc := filepath.Join(t.TempDir(), "kv.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-o", pc); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{jg, pc} {
		if _, err := runCLI(t, "", "fmt", path); err == nil || !strings.Contains(err.Error(), "pego convert") {
			t.Errorf("%s: got %v", path, err)
		}
	}
	if _, err := runCLI(t, js, "fmt"); err == nil || !strings.Contains(err.Error(), "pego convert") {
		t.Errorf("JSON on stdin: got %v", err)
	}
	data, err := os.ReadFile(pc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, string(data), "fmt"); err == nil || !strings.Contains(err.Error(), "pego convert") {
		t.Errorf("compiled grammar on stdin: got %v", err)
	}
	// The old json command is gone.
	if _, err := runCLI(t, "", "json", g); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("json: got %v", err)
	}
}

func TestConvert(t *testing.T) {
	g := writeFile(t, "kv.pego", messy)
	js, err := runCLI(t, "", "convert", g)
	if err != nil || !strings.Contains(js, `"package": "kv"`) {
		t.Fatalf("got %q, %v", js, err)
	}
	if strings.Contains(js, "pair") {
		t.Errorf("comments leaked into JSON:\n%s", js)
	}
	jg := writeFile(t, "kv.json", js)
	// JSON converts to PEGO source by default, and to JSON with -to json.
	if out, err := runCLI(t, "", "convert", jg); err != nil || out != "package kv\n\ndef main = k:@(?a-z)+ \"=\" v:@(?0-9)+\n" {
		t.Errorf("got %q, %v", out, err)
	}
	if out, err := runCLI(t, "", "convert", "-to", "json", jg); err != nil || out != js {
		t.Errorf("got %q, %v", out, err)
	}
	// PEGO source converts to itself with -to pego, keeping comments.
	if out, err := runCLI(t, "", "convert", g, "-to", "pego"); err != nil || out != tidy {
		t.Errorf("got %q, %v", out, err)
	}
	if out, err := runCLI(t, "", "parse", "-g", jg, "-i", "x=9", "-f", "sexpr"); err != nil || !strings.Contains(out, `k="x"`) {
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
	bad := writeFile(t, "bad.pego", "def main = (")
	if _, err := runCLI(t, "", "fmt", bad); err == nil || !strings.Contains(err.Error(), "bad.pego:1:13") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, "", "convert", bad); err == nil || !strings.Contains(err.Error(), "bad.pego:1:13") {
		t.Errorf("got %v", err)
	}
	g := writeFile(t, "kv.pego", kv)
	if _, err := runCLI(t, "", "parse", "-g", g, "-s", "nope"); err == nil || !strings.Contains(err.Error(), "start rule nope is not defined") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, ""); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("got %v", err)
	}
}

func TestParseRecovered(t *testing.T) {
	g := writeFile(t, "r.pego", `def main = ((@(?a-z)+ ";") #recover(skip=(?^;)+ ";"))*`)
	out, err := runCLI(t, "", "parse", "-g", g, "-i", "a;1;b;", "-f", "sexpr")
	if !strings.Contains(out, `Error"1;"`) || err == nil || !strings.Contains(err.Error(), "1:3: syntax error") {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestParseStream(t *testing.T) {
	g := writeFile(t, "s.pego", `def main = (@(?a-z)+ -"\n")* #stream`)
	out, err := runCLI(t, "ab\ncd\n", "parse", "-g", g, "-stream", "-f", "sexpr")
	if err != nil || out != "(Seq \"ab\")\n(Seq \"cd\")\n" {
		t.Errorf("got %q, %v", out, err)
	}
	out, err = runCLI(t, "ab\n", "parse", "-g", g, "-stream")
	if err != nil || !strings.HasPrefix(out, `{"type":"Seq"`) || strings.Count(out, "\n") != 1 {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestGen(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	out, err := runCLI(t, "", "gen", "-g", g, "-pkg", "kv")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"// Code generated by pego. DO NOT EDIT.", "package kv", "func Parse(input string, unit ...Unit) (*Node, error)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	path := filepath.Join(t.TempDir(), "p.go")
	if _, err := runCLI(t, "", "gen", "-g", g, "-pkg", "kv", "-o", path); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != out {
		t.Errorf("file differs from stdout: %v", err)
	}
	if _, err := runCLI(t, "", "gen", "-g", g); err == nil {
		t.Error("expected an error without -pkg")
	}
}

func TestCompile(t *testing.T) {
	g := writeFile(t, "kv.pego", kv+"\ndef other = \"x\"")
	out := filepath.Join(t.TempDir(), "kv.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-o", out); err != nil {
		t.Fatal(err)
	}
	got, err := runCLI(t, "", "parse", "-g", out, "-i", "a=1", "-f", "sexpr")
	if err != nil || got != `(Seq "a" "=" "1" k="a" v="1")@main`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := runCLI(t, "", "parse", "-g", out, "-s", "other", "-i", "x", "-f", "sexpr"); err != nil || got != `"x"@other`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	// A compiled grammar with its AST converts to either format, given -to.
	if got, err := runCLI(t, "", "convert", "-to", "pego", out); err != nil || !strings.Contains(got, `def other = "x"`) {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := runCLI(t, "", "convert", "-to", "json", out); err != nil || !strings.Contains(got, `"name": "other"`) {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := runCLI(t, "", "convert", out); err == nil || !strings.Contains(err.Error(), "-to") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, "", "compile", "-g", g); err == nil {
		t.Error("expected an error without -o")
	}
	// A file without the AST runs on the bytecode backend but cannot be
	// converted.
	bare := filepath.Join(t.TempDir(), "kv-bare.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-no-ast", "-o", bare); err != nil {
		t.Fatal(err)
	}
	if got, err := runCLI(t, "", "parse", "-g", bare, "-i", "a=1", "-f", "sexpr"); err != nil || got != `(Seq "a" "=" "1" k="a" v="1")@main`+"\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := runCLI(t, "", "parse", "-g", bare, "-i", "a=1", "-backend", "closure"); err == nil {
		t.Error("expected an error for the closure backend without AST")
	}
	if _, err := runCLI(t, "", "convert", "-to", "pego", bare); err == nil || !strings.Contains(err.Error(), "omits the AST") {
		t.Errorf("got %v", err)
	}
	broken := writeFile(t, "broken.pegoc", "PEGOC\x00\x01garbage")
	if _, err := runCLI(t, "", "parse", "-g", broken, "-i", "a=1"); err == nil || !strings.Contains(err.Error(), "broken.pegoc") {
		t.Errorf("got %v", err)
	}
}

func TestParseUnit(t *testing.T) {
	g := writeFile(t, "u.pego", `def main = "é" "a"`)
	_, err := runCLI(t, "", "parse", "-g", g, "-i", "éb", "-unit", "bytes")
	if err == nil || !strings.Contains(err.Error(), "1:3: syntax error") {
		t.Errorf("got %v", err)
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "éa", "-unit", "nope"); err == nil {
		t.Error("expected an error")
	}
}

func TestParseBackend(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	for _, b := range []string{"closure", "bytecode", "bytecode-iterative"} {
		got, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-f", "sexpr", "-backend", b)
		if err != nil || got != `(Seq "a" "=" "1" k="a" v="1")@main`+"\n" {
			t.Errorf("%s: got %q, %v", b, got, err)
		}
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-backend", "nope"); err == nil {
		t.Error("expected an error")
	}
}

func TestParseCheck(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	if got, err := runCLI(t, "", "parse", "-g", g, "-i", "a=1", "-check"); err != nil || got != "ok\n" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := runCLI(t, "", "parse", "-g", g, "-i", "a=", "-check"); err == nil {
		t.Error("expected an error")
	}
}
