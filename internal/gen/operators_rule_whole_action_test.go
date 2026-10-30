package gen_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestOperatorsRuleExplicitDefaultAction(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testOperatorsRuleExplicitDefaultAction)
}

func testOperatorsRuleExplicitDefaultAction(t *testing.T) {
	const source = `rule main = #operators {
    operand "α"
    level { infix left "+" }
}`
	plain := mustCompile(t, source)
	for _, opts := range []compile.Options{{SourceInfo: true}} {
		g, err := parseTest(source + " => ...")
		if err != nil {
			t.Fatal(err)
		}
		if !g.Rules()[0].DefaultAction || g.Rules()[0].Action != nil {
			t.Fatal("explicit default lost its AST representation")
		}
		data, err := grammar.MarshalJSON(g)
		if err != nil {
			t.Fatal(err)
		}
		g, err = grammar.UnmarshalJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		explicit, err := engine.Compile(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			for _, unit := range []engine.PositionUnit{engine.CodePoints, engine.Bytes} {
				for _, input := range []string{"α", "α+α", ""} {
					parseOpts := engine.ParseOptions{Backend: backend, Unit: unit}
					n, err := explicit.ParseWith("main", input, parseOpts)
					if input != "" && (n == nil || err != nil) || input == "" && err == nil {
						t.Fatalf("%s/%s input %q: node=%v error=%v", backend, unit, input, n, err)
					}
					if got, want := gentest.ResultJSON(n, err), gentest.ResultJSON(plain.ParseWith("main", input, parseOpts)); got != want {
						t.Fatalf("%s/%s input %q: explicit %s; omitted %s", backend, unit, input, got, want)
					}
				}
			}
		}
		for name, run := range map[string]func() error{
			"Go generator": func() error {
				_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p"})
				return err
			},
			"typed Go generator": func() error {
				_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p", Types: true})
				return err
			},
			"TS generator": func() error { _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"}); return err },
			"typed TS generator": func() error {
				_, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Types: true})
				return err
			},
		} {
			if err := run(); err != nil {
				t.Fatalf("%s rejected explicit default: %v", name, err)
			}
		}
	}
}

// Generated Go parsers, plain, with source information and typed, and generated TypeScript parsers
// build the same results as the engine for Operators Rules with whole-rule actions.
func TestOperatorsRuleWholeActionGenerated(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testOperatorsRuleWholeActionGenerated)
}

func testOperatorsRuleWholeActionGenerated(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := gentest.OperatorsRuleWholeActionCases
	t.Run("go", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{}) })
	t.Run("go source info", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{SourceInfo: true}) })
	t.Run("typed go", func(t *testing.T) { gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}) })
	t.Run("typescript", func(t *testing.T) {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node not found")
		}
		gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{})
		gentest.TSParsersCorpus(t, node, cases, gen.GenOptions{SourceInfo: true})
	})
}
