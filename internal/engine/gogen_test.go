package engine

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/syntax"
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
rule n = "(" n ")" / "x"`, Inputs: []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	// A direct leaf still counts as a rule call, including when a generic
	// recursive rule invokes it or a rule-table entry reaches its body.
	cases = append(cases, gentest.Case{Name: "direct leaf nesting limit", Src: `rule main = n
rule n = "(" n ")" / leaf
rule leaf = "x"`, Inputs: []string{strings.Repeat("(", DefaultMaxDepth-3) + "x" + strings.Repeat(")", DefaultMaxDepth-3),
		strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2)}})
	// Chains of prefix and right-associative operators count against the limit too (a OperatorsRule
	// expression parses them by recursion).
	cases = append(cases, gentest.Case{Name: "operatorsRule nesting limit", Src: `rule main = x
rule x = #operators {
    operand "x"
    level { infix right "^" }
    level { prefix "-" }
}`, Inputs: []string{strings.Repeat("-", 3*DefaultMaxDepth) + "x", strings.Repeat("x^", 3*DefaultMaxDepth) + "x", strings.Repeat("-", DefaultMaxDepth/4) + "x"}})
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

// mutants returns n random edits of each input (a character inserted, deleted or replaced, or two
// characters swapped, up to three times), with characters from the inputs, reproducibly for a seed.
func mutants(inputs []string, n int, seed uint64) []string {
	rng := rand.New(rand.NewPCG(seed, 1))
	var pool []rune
	for _, in := range inputs {
		pool = append(pool, []rune(in)...)
	}
	if len(pool) == 0 {
		return nil
	}
	var out []string
	for _, in := range inputs {
		for range n {
			rs := []rune(in)
			for range 1 + rng.IntN(3) {
				i := rng.IntN(len(rs) + 1)
				switch c := pool[rng.IntN(len(pool))]; rng.IntN(4) {
				case 0:
					rs = append(rs[:i], append([]rune{c}, rs[i:]...)...)
				case 1:
					if i < len(rs) {
						rs = append(rs[:i], rs[i+1:]...)
					}
				case 2:
					if i < len(rs) {
						rs[i] = c
					}
				default:
					if i+1 < len(rs) {
						rs[i], rs[i+1] = rs[i+1], rs[i]
					}
				}
			}
			out = append(out, string(rs))
		}
	}
	return out
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
	gentest.GeneratedTypesCorpus(t, goBin, generatedTypesCases(t), gen.GenOptions{ConvertTypes: true})
}

// generatedTypesCases returns the grammars of TestGeneratedTypes: the corpus, a nesting limit and
// the typed grammar, with mutated inputs for the grammars written for direct rules.
func generatedTypesCases(t *testing.T) []gentest.Case {
	// Exactly at and just beyond the nesting limit, through direct rules (as in
	// TestGeneratedParsersMatchEngine).
	cases := append(gentest.GenCorpus(t), gentest.Case{Name: "nesting limit", Src: `type R = { T node[string] }
rule main R = t:n #atomic => R{T: $t}
rule n = "(" n ")" / "x"`, Inputs: []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	cases = append(cases, typedLeaderValueCases...)
	cases = append(cases, gentest.Case{Name: "typed", Src: gentest.TypedGrammar, Inputs: []string{"f(1,x)!?;[3];zz", "f(1,(;g();", "f(a);"}})
	// The grammars written for direct rules also get mutated inputs, which reach other partial
	// matches and failures than the written ones.
	for i := range cases {
		if strings.Contains(cases[i].Name, "typed/direct_") {
			cases[i].Inputs = append(cases[i].Inputs, mutants(cases[i].Inputs, 8, uint64(i))...)
		}
	}
	return cases
}
