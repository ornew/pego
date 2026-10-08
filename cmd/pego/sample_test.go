package main

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

const kvList = `def main = pair ("," pair)*
def pair = k:@(?a-z)+ "=" v:@(?0-9)+`

// captureStderr replaces sampleStderr for the duration of the test.
func captureStderr(t *testing.T) *strings.Builder {
	t.Helper()
	var b strings.Builder
	old := sampleStderr
	sampleStderr = &b
	t.Cleanup(func() { sampleStderr = old })
	return &b
}

// quotedLines decodes the lines format.
func quotedLines(t *testing.T, out string) []string {
	t.Helper()
	var ins []string
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		s, err := strconv.Unquote(l)
		if err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		ins = append(ins, s)
	}
	return ins
}

func TestSample(t *testing.T) {
	g := writeFile(t, "kv.pego", kvList)
	p, err := pego.CompileSource(kvList, "main")
	if err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "", "sample", "-g", g, "-n", "5", "-seed", "3")
	if err != nil {
		t.Fatal(err)
	}
	ins := quotedLines(t, out)
	if len(ins) != 5 {
		t.Errorf("got %d inputs: %q", len(ins), ins)
	}
	for _, in := range ins {
		if _, err := p.Parse(in); err != nil {
			t.Errorf("%q does not parse: %v", in, err)
		}
	}
	// The same seed gives the same inputs, another seed others.
	if again, _ := runCLI(t, "", "sample", "-g", g, "-n", "5", "-seed", "3"); again != out {
		t.Errorf("the same seed gave\n%s\nand\n%s", out, again)
	}
	if other, _ := runCLI(t, "", "sample", "-g", g, "-n", "5", "-seed", "4"); other == out {
		t.Errorf("seeds 3 and 4 gave the same inputs\n%s", out)
	}
}

func TestSampleOptions(t *testing.T) {
	g := writeFile(t, "nest.pego", `def main = "(" main ")" / "x"`)
	// -max-depth 0: the shortest way from the start.
	out, err := runCLI(t, "", "sample", "-g", g, "-n", "3", "-max-depth", "0")
	if err != nil {
		t.Fatal(err)
	}
	if out != "\"x\"\n" {
		t.Errorf("got %q", out)
	}
	stderr := captureStderr(t)
	if _, err := runCLI(t, "", "sample", "-g", g, "-n", "3", "-max-depth", "0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "found only 1 distinct inputs") {
		t.Errorf("stderr: %q", stderr)
	}
	// -s selects the start rule, -max-len bounds the length.
	g = writeFile(t, "kv.pego", kvList)
	out, err = runCLI(t, "", "sample", "-g", g, "-s", "pair", "-n", "3", "-max-len", "4", "-max-repeat", "1")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range quotedLines(t, out) {
		if strings.Contains(in, ",") || len(in) > 12 {
			t.Errorf("start pair, max len 4: %q", in)
		}
	}
	// Errors.
	for _, args := range [][]string{
		{"sample"},
		{"sample", "-g", g, "-f", "xml"},
		{"sample", "-g", g, "-n", "-3"},
		{"sample", "-g", g, "-s", "nope"},
		{"sample", "-g", writeFile(t, "never.pego", `def main = "a"* "a"`), "-n", "1"},
	} {
		if _, err := runCLI(t, "", args...); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}
}

func TestSampleCoverage(t *testing.T) {
	g := writeFile(t, "kv.pego", `def main = item ("," item)*
def item = word / number / "true" / "false"
def word = @(?a-z)+
def number = @(?0-9)+`)
	stderr := captureStderr(t)
	if _, err := runCLI(t, "", "sample", "-g", g, "-n", "1", "-coverage"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stderr.String(), "rules ") || !strings.Contains(stderr.String(), "missed") {
		t.Errorf("stderr: %q", stderr)
	}
	out, err := runCLI(t, "", "sample", "-g", g, "-n", "10", "-coverage", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc sampleOutput
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	// "true" and "false" are never reached: word, which comes first in the ordered choice, matches them.
	c := doc.Coverage
	if doc.Start != "main" || len(doc.Inputs) != 10 || c == nil || c.Rules != 4 || c.RulesCovered != 4 ||
		c.Alternatives != 4 || c.AlternativesCovered != 2 || len(c.MissedAlternatives) != 2 ||
		c.MissedAlternatives[0].Expr != `"true"` || c.MissedAlternatives[1].Expr != `"false"` {
		t.Errorf("got %s", out)
	}
}

func TestSampleCoverageWithoutInputs(t *testing.T) {
	// No input parses: the report still shows what the search exercised, then the command fails.
	g := writeFile(t, "never.pego", `def main = item+ "a"
def item = "a" / "b"`)
	stderr := captureStderr(t)
	out, err := runCLI(t, "", "sample", "-g", g, "-n", "3", "-coverage")
	if err == nil || out != "" || !strings.HasPrefix(stderr.String(), "rules 0/2") {
		t.Errorf("got %q, %v, stderr %q", out, err, stderr)
	}
	out, err = runCLI(t, "", "sample", "-g", g, "-n", "3", "-coverage", "-f", "json")
	var doc sampleOutput
	if err == nil || json.Unmarshal([]byte(out), &doc) != nil || doc.Coverage == nil || doc.Coverage.Rules != 2 {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestSampleInvalid(t *testing.T) {
	g := writeFile(t, "kv.pego", kvList)
	p, err := pego.CompileSource(kvList, "main")
	if err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "", "sample", "-g", g, "-n", "5", "-invalid")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range quotedLines(t, out) {
		if _, err := p.Parse(in); err == nil {
			t.Errorf("invalid input %q parses", in)
		}
	}
	out, err = runCLI(t, "", "sample", "-g", g, "-n", "5", "-invalid", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc sampleOutput
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Invalid) != 5 || len(doc.Inputs) != 0 {
		t.Fatalf("got %s", out)
	}
	for _, inv := range doc.Invalid {
		_, err := p.Parse(inv.Input)
		if err == nil || err.Error() != inv.Error || inv.Mutation == "" {
			t.Errorf("got %+v", inv)
		}
		if _, err := p.Parse(inv.Base); err != nil {
			t.Errorf("base %q does not parse", inv.Base)
		}
	}
}

func TestSampleCompiledGrammar(t *testing.T) {
	g := writeFile(t, "kv.pego", kvList)
	dir := t.TempDir()
	pc := filepath.Join(dir, "kv.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-s", "pair", "-o", pc); err != nil {
		t.Fatal(err)
	}
	// The start rule saved in the compiled grammar is the default.
	out, err := runCLI(t, "", "sample", "-g", pc, "-n", "3")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range quotedLines(t, out) {
		if strings.Contains(in, ",") {
			t.Errorf("start pair: %q", in)
		}
	}
	noAST := filepath.Join(dir, "kv-noast.pegoc")
	if _, err := runCLI(t, "", "compile", "-g", g, "-no-ast", "-o", noAST); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "", "sample", "-g", noAST); err == nil || !strings.Contains(err.Error(), "AST") {
		t.Errorf("got %v", err)
	}
}
