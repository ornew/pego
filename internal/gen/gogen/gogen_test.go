package gogen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/syntax"
)

// TestGeneratedParsersMatchEngine checks that generated parsers return the same results as the engine:
// the same trees, positions included, and the same errors, and that Recognize returns the errors of
// the engine's recognition.
func TestGeneratedParsersMatchEngine(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedParsersMatchEngine)
}

func testGeneratedParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := gentest.GenCorpus(t)
	// Exactly at and just beyond the default nesting limit (DefaultMaxDepth rule calls). Only
	// here: the iterative VM, which other tests run on the corpus, has a higher default.
	cases = append(cases, gentest.Case{Name: "nesting limit", Src: `rule main = n
rule n = "(" n ")" / "x"`, Inputs: []string{strings.Repeat("(", engine.DefaultMaxDepth-2) + "x" + strings.Repeat(")", engine.DefaultMaxDepth-2),
		strings.Repeat("(", engine.DefaultMaxDepth-1) + "x" + strings.Repeat(")", engine.DefaultMaxDepth-1)}})
	// A direct leaf still counts as a rule call, including when a generic
	// recursive rule invokes it or a rule-table entry reaches its body.
	cases = append(cases, gentest.Case{Name: "direct leaf nesting limit", Src: `rule main = n
rule n = "(" n ")" / leaf
rule leaf = "x"`, Inputs: []string{strings.Repeat("(", engine.DefaultMaxDepth-3) + "x" + strings.Repeat(")", engine.DefaultMaxDepth-3),
		strings.Repeat("(", engine.DefaultMaxDepth-2) + "x" + strings.Repeat(")", engine.DefaultMaxDepth-2)}})
	// Chains of prefix and right-associative operators count against the limit too (a OperatorsRule
	// expression parses them by recursion).
	cases = append(cases, gentest.Case{Name: "operatorsRule nesting limit", Src: `rule main = x
rule x = #operators {
    operand "x"
    level { infix right "^" }
    level { prefix "-" }
}`, Inputs: []string{strings.Repeat("-", 3*engine.DefaultMaxDepth) + "x", strings.Repeat("x^", 3*engine.DefaultMaxDepth) + "x", strings.Repeat("-", engine.DefaultMaxDepth/4) + "x"}})
	t.Run("values", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{}) })
	t.Run("source info", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{SourceInfo: true}) })
}

func TestGenerateErrors(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGenerateErrors)
}

func testGenerateErrors(t *testing.T) {
	g, _ := syntax.Parse("@start\nrule main = \"a\" b\nrule b = \"b\"")
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "nope"}); err == nil || err.Error() != "rule nope is not defined" {
		t.Errorf("got %v", err)
	}
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "b"}); err == nil || err.Error() != "rule b is not a start rule; parsing can begin only at a rule marked @start" {
		t.Errorf("not a start rule: got %v", err)
	}
	if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main"}); err == nil {
		t.Error("expected an error for a missing package name")
	}
	for _, pkg := range []string{"foo-bar", "type", "1p", "_"} {
		if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main"}); err == nil || err.Error() != fmt.Sprintf("invalid package name %q", pkg) {
			t.Errorf("package %q: got %v", pkg, err)
		}
	}
	bad, _ := parseTest(`rule main = x`)
	if _, err := gentest.GenerateCode(t, bad, gen.GenOptions{Package: "p", Start: "main"}); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
}

// TestGeneratedTypes checks ParseAST of parsers generated with Types: it compiles for every
// grammar of the corpus and returns the errors of Parse, and it converts the trees of
// typedGrammar into the expected values.
func TestGeneratedTypes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypes)
}

func testGeneratedTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	gentest.GeneratedTypesCorpus(t, goBin, gentest.GeneratedTypesCases(t), gen.GenOptions{ConvertTypes: true})
}
