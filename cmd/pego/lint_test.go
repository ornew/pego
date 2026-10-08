package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const lintGrammar = `def main = "a" / "ab" / x
def x = k:"x" v:"y" -> $k
`

func TestLint(t *testing.T) {
	g := writeFile(t, "g.pego", lintGrammar)
	out, err := runCLI(t, "", "lint", "-g", g)
	want := g + `:1:18: error: alternative 2 (` + "`\"ab\"`" + `) can never match: alternative 1 (` + "`\"a\"`" + ` at 1:12) matches first wherever it could [shadowed-alternative]
	fix: move it before alternative 1
` + g + `:2:15: warning: capture v is never used: the action does not refer to it [unused-capture]
	fix: remove v:, or use $v
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if err == nil || err.Error() != "lint: 1 error, 1 warning, 0 hints" {
		t.Errorf("an error fails: %v", err)
	}

	// Warnings fail only with -strict.
	if _, err := runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative"); err != nil {
		t.Errorf("warnings: %v", err)
	}
	if _, err := runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative", "-strict"); err == nil {
		t.Error("warnings with -strict do not fail")
	}
	out, err = runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative,unused-capture")
	if out != "" || err != nil {
		t.Errorf("all disabled: %q %v", out, err)
	}
	out, err = runCLI(t, "", "lint", "-g", g, "-disable", "shadowed-alternative", "-disable", "unused-capture")
	if out != "" || err != nil {
		t.Errorf("repeated -disable: %q %v", out, err)
	}
	if _, err := runCLI(t, "", "lint", "-g", g, "-disable", "shadowing"); err == nil || !strings.Contains(err.Error(), `unknown check "shadowing"`) {
		t.Errorf("unknown check: %v", err)
	}

	// The start rule decides what is unreachable.
	out, _ = runCLI(t, "", "lint", "-g", g, "-s", "x")
	if !strings.Contains(out, ":1:1: warning: rule main is never used: x does not call it") {
		t.Errorf("-s x:\n%s", out)
	}
}

func TestLintJSON(t *testing.T) {
	g := writeFile(t, "g.pego", lintGrammar)
	out, err := runCLI(t, "", "lint", "-g", g, "-f", "json")
	if err == nil {
		t.Error("an error does not fail with -f json")
	}
	var doc lintOutput
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if doc.Grammar != g || doc.Start != "main" || len(doc.Findings) != 2 {
		t.Fatalf("got %+v", doc)
	}
	if !strings.Contains(out, `"severity": "error"`) || !strings.Contains(out, `"check": "shadowed-alternative"`) ||
		!strings.Contains(out, `"line": 1`) || !strings.Contains(out, `"col": 18`) || !strings.Contains(out, `"rule": "main"`) ||
		!strings.Contains(out, `"fix": "move it before alternative 1"`) {
		t.Errorf("got\n%s", out)
	}

	clean := writeFile(t, "clean.pego", `def main = "a"`)
	out, err = runCLI(t, "", "lint", "-g", clean, "-f", "json")
	if err != nil || !strings.Contains(out, `"findings": []`) {
		t.Errorf("no findings: %v\n%s", err, out)
	}
}

func TestLintOtherGrammars(t *testing.T) {
	// A grammar in JSON has no positions: findings are reported with the file name only.
	g := writeFile(t, "g.pego", "def main = \"a\"\ndef x = \"b\"")
	j := filepath.Join(t.TempDir(), "g.json")
	if _, err := runCLI(t, "", "convert", "-o", j, g); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "", "lint", "-g", j)
	if err != nil {
		t.Fatal(err)
	}
	if want := j + ": warning: rule x is never used: main does not call it, directly or indirectly [unreachable-rule]\n"; !strings.HasPrefix(out, want) {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	// A compiled grammar is linted from its AST, with the start rule saved in it.
	c := filepath.Join(t.TempDir(), "g.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-s", "x", "-o", c); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "", "lint", "-g", c)
	if err != nil || !strings.Contains(out, "rule main is never used: x does not call it") {
		t.Errorf("compiled: %v\n%s", err, out)
	}

	// A grammar that does not compile is an error, with the compile errors.
	bad := writeFile(t, "bad.pego", "def main = y")
	if _, err := runCLI(t, "", "lint", "-g", bad); err == nil || !strings.Contains(err.Error(), bad+":1:12: undefined rule y") {
		t.Errorf("bad grammar: %v", err)
	}
	if _, err := runCLI(t, "", "lint"); err == nil || !strings.Contains(err.Error(), "-g is required") {
		t.Errorf("no grammar: %v", err)
	}
	if _, err := runCLI(t, "", "lint", "-g", g, "-f", "xml"); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestLintList(t *testing.T) {
	out, err := runCLI(t, "", "lint", "-list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shadowed-alternative  error", "unused-capture", "right-recursion", "hint"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}
