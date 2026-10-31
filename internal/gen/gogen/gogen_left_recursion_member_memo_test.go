package gogen_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/ir/exec/core"
)

const memberMemoSource = `@start
rule main = a $$
rule a = c "+" / "n"
rule c = "(" c ")" "y" / "(" c ")" "z" / a`

// typedMemberMemoSource is memberMemoSource with typed values, so that ParseAST runs the typed
// runtime.
const typedMemberMemoSource = `type D = { N int }
@start
rule main D = e:a $$ => $e
rule a D = sum / zero
rule sum D = x:c "+" => D{N: $x.N}
rule zero D = "n" => D{N: 0}
rule c D = up / uz / a
rule up D = "(" x:c ")" "y" => D{N: $x.N + 1}
rule uz D = "(" x:c ")" "z" => D{N: $x.N + 1}
`

// TestGeneratedLeftRecursionMemberMemoLinear checks that generated Go parsers memoize the
// members of a left-recursion cycle that are not its leader: the Parse, Recognize and typed
// ParseAST of a member reached from two call sites at one position take a number of steps in
// proportion to the input, and give the engine's results.
func TestGeneratedLeftRecursionMemberMemoLinear(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedLeftRecursionMemberMemoLinear)
}

func testGeneratedLeftRecursionMemberMemoLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("gentest")), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := gen.GenOptions{Start: "main", Recognize: true}
	gentest.GeneratePackage(t, dir, "g", mustParse(memberMemoSource), opts)
	opts.Types = true
	if code := gentest.GeneratePackage(t, dir, "tg", mustParse(typedMemberMemoSource), opts); !strings.Contains(code, "TParse(trules") {
		t.Fatal("typed runtime is not exercised")
	}
	prog := compileBare(t, memberMemoSource)
	typed := compileBare(t, typedMemberMemoSource)
	input := func(n int) string { return strings.Repeat("(", n) + "n" + strings.Repeat(")z", n) + "+" }
	// 2^n steps without the memo, far beyond the budget below at n = 300.
	sizes := []int{12, 300}
	var want strings.Builder
	for _, n := range sizes {
		in := input(n)
		node, err := prog.ParseWith("main", in, engine.ParseOptions{})
		v, typedErr := typed.ParseWith("main", in, engine.ParseOptions{})
		fmt.Fprintf(&want, "%s %v | %s\n", gentest.ResultJSON(node, err), typedErr, typedN(v))
	}
	var limit, typedLimit string
	if gentest.FixtureBudgetControls(t) {
		limit = "g.WithLimits(g.Limits{Budget: 1 << 20})"
		typedLimit = "tg.WithLimits(tg.Limits{Budget: 1 << 20})"
	}
	main := `package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"gentest/g"
	"gentest/tg"
)

func main() {
	for _, n := range []int{` + fmt.Sprint(sizes[0], ", ", sizes[1]) + `} {
		in := strings.Repeat("(", n) + "n" + strings.Repeat(")z", n) + "+"
		node, err := g.Parse(in` + comma(limit) + `)
		out := map[string]any{"node": node}
		if err != nil {
			out["err"] = err.Error()
		}
		b, _ := json.Marshal(out)
		if err := g.Recognize(in` + comma(limit) + `); err != nil {
			panic(err)
		}
		v, typedErr := tg.ParseAST(in` + comma(typedLimit) + `)
		fmt.Printf("%s %v | ", b, typedErr)
		if v == nil {
			fmt.Println("<nil>")
		} else {
			fmt.Println(v.N)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without the memo the ordinary form runs for ages at n = 300; fail instead of waiting.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"), "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	if string(out) != want.String() {
		t.Fatalf("generated\n%s\nengine\n%s", out, want.String())
	}
}

// comma returns ", " and the option, or nothing without an option.
func comma(option string) string {
	if option == "" {
		return ""
	}
	return ", " + option
}

// TestGeneratedLeftRecursionMemberMemoStale checks that generated Go parsers (untyped and typed)
// give the engine's results on grammars where a memoized member result could go stale while a
// leader grows. TestLeftRecursionMemberMemoStale in the engine checks that the engine gives the
// results of the grammars compiled without a memo.
func TestGeneratedLeftRecursionMemberMemoStale(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedLeftRecursionMemberMemoStale)
}

func testGeneratedLeftRecursionMemberMemoStale(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := append(gentest.LeftRecursionMemberCases(), gentest.LeftRecursionMemberSeedCases()...)
	t.Run("untyped", func(t *testing.T) {
		t.Parallel()
		gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	})
	t.Run("typed", func(t *testing.T) {
		t.Parallel()
		gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{}, len(cases))
	})
}
