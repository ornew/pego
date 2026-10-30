package engine

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestPredicateBlockBindingComparison(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, `var x int
rule a = {x:=1; x<10}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			if _, err := program.ParseWith("a", "", ParseOptions{Backend: backend, Unit: unit}); err != nil {
				t.Fatalf("%s/%s: %v", backend, unit, err)
			}
		}
	}
}

func TestPredicateBlocksAdjacentParity(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.PredicateBlockCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			block := mustCompile(t, fixture.Block)
			adjacent := mustCompile(t, fixture.Adjacent)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, recognize := range []bool{false, true} {
						options := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
						for _, input := range fixture.Inputs {
							got := gentest.ResultJSON(block.ParseWith("main", input, options))
							want := gentest.ResultJSON(adjacent.ParseWith("main", input, options))
							if got != want {
								t.Fatalf("%s/%s recognize=%t input=%q: block %s; adjacent %s", backend, unit, recognize, input, got, want)
							}
						}
					}
				}
			}
		})
	}
}

func TestPredicateBlocksSourceAndSavedGrammar(t *testing.T) {
	t.Parallel()
	const source = "var x int\nrule main = {\n // first\n x := (1 +\n 1)\n // second\n x == 2;\n} \"é\" $$"
	g, err := parseTest(source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	formattedGrammar, err := parseTest(formatted)
	if err != nil || grammar.Format(formattedGrammar) != formatted {
		t.Fatalf("formatter round trip: %v", err)
	}
	for _, original := range []*grammar.Grammar{g, jsonActionBlockGrammar(t, g), formattedGrammar} {
		program, err := Compile(original, compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range valueModes {
			loaded := saveLoad(t, program, "main", mode)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if backend == Closure {
					_, err := loaded.ParseWith("main", "", servedBy(mode, ParseOptions{Backend: backend}))
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, input := range []string{"é", "", "a", "éé"} {
						parseOptions := ParseOptions{Backend: backend, Unit: unit}
						got := gentest.ResultJSON(loaded.ParseWith("main", input, servedBy(mode, parseOptions)))
						want := gentest.ResultJSON(program.ParseWith("main", input, parseOptions))
						if got != want {
							t.Fatalf("saved %s/%s input=%q: %s / %s", backend, unit, input, got, want)
						}
					}
				}
			}
		}
	}
	after, err := grammar.MarshalJSON(g)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("compilation mutated source: %v", err)
	}
}

func TestPredicateBlocksStatementValidation(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`var x int
rule main = {x := 1; len(true)} "a"`,
		`rule main = {true; $1 == nil} "a"`,
		`rule main = {true; text($missing) == "a"} "a"`,
	} {
		// ParseLegacy reads $1, which still reaches the checker from JSON grammars.
		g, err := parseLegacyTest(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Fatalf("accepted invalid statement: %s", source)
		}
	}
	for _, source := range []string{`rule main = {}`, `rule main = "a" => {true; true}`} {
		g, err := parseTest(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), "action statement blocks are not implemented") {
			t.Fatalf("unsupported boundary %s: %v", source, err)
		}
	}
}

func TestPredicateBlocksGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, gentest.PredicateBlockGeneratedCases(), gen.GenOptions{})
}
