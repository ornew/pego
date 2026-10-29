package engine

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

type predicateBlockCase struct {
	name, block, adjacent string
	inputs                []string
}

func predicateBlockCases() []predicateBlockCase {
	return []predicateBlockCase{
		{"whole body", `rule main = {true; true}`, `rule main = {true} {true}`, []string{"", "a"}},
		{"captured block", `var n int
rule main = x:{n := 1; n == 1} "a" $$ => $x`, `var n int
rule main = x:({n := 1} {n == 1}) "a" $$ => $x`, []string{"a", "", "aa"}},
		{"choice blocks", `rule main = ({false; true} / {true; true}) $$`, `rule main = (({false} {true}) / ({true} {true})) $$`, []string{"", "a"}},
		{"nullable block repetition", `var n int
rule main = {n := 0} ({n := n + 1; true})* {n == 1} $$`, `var n int
rule main = {n := 0} ({n := n + 1} {true})* {n == 1} $$`, []string{"", "a"}},
		{"bindings", `var x int
var y int
rule main = {x := 1; y := x + 1; y == 2} "é" $$`, `var x int
var y int
rule main = {x := 1} {y := x + 1} {y == 2} "é" $$`, []string{"é", "", "a", "éé"}},
		{"capture", `var s string
rule main = c:"é" {s := text($c); s == "é"} $$ => $c`, `var s string
rule main = c:"é" {s := text($c)} {s == "é"} $$ => $c`, []string{"é", "a", "", "éx"}},
		{"positional values", `var x int
rule main = v1:"a" {x := 1; x == 1} v2:"b" $$ => list($v1, $v2)`, `var x int
rule main = v1:"a" {x := 1} {x == 1} v2:"b" $$ => list($v1, $v2)`, []string{"ab", "a", "ac", ""}},
		{"choice rollback", `var x int
rule main = {x := 0} ("a" {x := 1; x == 1} "!" / "a" {x == 0; true} "?") $$`, `var x int
rule main = {x := 0} ("a" {x := 1} {x == 1} "!" / "a" {x == 0} {true} "?") $$`, []string{"a!", "a?", "a", "b"}},
		{"optional rollback", `var x int
rule main = {x := 0} ("a" {x := 1; false})? {x == 0; true} "a" $$`, `var x int
rule main = {x := 0} ("a" {x := 1} {false})? {x == 0} {true} "a" $$`, []string{"a", "", "aa"}},
		{"predicate failure", `rule main = "a" {1 / 0; true} "b" $$`, `rule main = "a" {1 / 0} {true} "b" $$`, []string{"ab", "a", ""}},
		{"short circuit", `rule main = "a" {false; 1 / 0} "b" $$`, `rule main = "a" {false} {1 / 0} "b" $$`, []string{"ab", "a", ""}},
		{"rule environment", `var x int
rule main = {x := 1; true} (n "!" / n "?") $$
rule n = {x == 1; true} "é"`, `var x int
rule main = {x := 1} {true} (n "!" / n "?") $$
rule n = {x == 1} {true} "é"`, []string{"é!", "é?", "é", "a"}},
		{"repetition", `var x int
rule main = {x := 0} ("a" {x := x + 1; true})* {x == 2; true} $$`, `var x int
rule main = {x := 0} ("a" {x := x + 1} {true})* {x == 2} {true} $$`, []string{"aa", "a", "aaa", ""}},
		{"lookahead", `var x int
rule main = {x := 0} &("a" {x := 1; true}) {x == 0; true} "a" $$`, `var x int
rule main = {x := 0} &("a" {x := 1} {true}) {x == 0} {true} "a" $$`, []string{"a", "", "b"}},
		{"cut", `var x int
rule main = "a" #cut {x := 1; x == 1} "!" $$ / "a" "?" $$`, `var x int
rule main = "a" #cut {x := 1} {x == 1} "!" $$ / "a" "?" $$`, []string{"a!", "a?", "a", ""}},
		{"recovery", `var x int
rule main = ("a" {x := 1; false} ";") #recover(skip="?") $$`, `var x int
rule main = ("a" {x := 1} {false} ";") #recover(skip="?") $$`, []string{"?", "a?", "a;", ""}},
	}
}

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
	for _, fixture := range predicateBlockCases() {
		t.Run(fixture.name, func(t *testing.T) {
			block := mustCompile(t, fixture.block)
			adjacent := mustCompile(t, fixture.adjacent)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, recognize := range []bool{false, true} {
						options := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
						for _, input := range fixture.inputs {
							got := resultJSON(block.ParseWith("main", input, options))
							want := resultJSON(adjacent.ParseWith("main", input, options))
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
						got := resultJSON(loaded.ParseWith("main", input, servedBy(mode, parseOptions)))
						want := resultJSON(program.ParseWith("main", input, parseOptions))
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

func predicateBlockGeneratedCases() []genCase {
	var cases []genCase
	for _, fixture := range predicateBlockCases() {
		cases = append(cases, genCase{name: fixture.name, src: fixture.block, inputs: fixture.inputs})
	}
	return cases
}

func TestPredicateBlocksGeneratedGoParity(t *testing.T) {
	t.Parallel()
	eachMode(t, testPredicateBlocksGeneratedGoParity)
}

func testPredicateBlocksGeneratedGoParity(t *testing.T) {
	testGeneratedParsersCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), predicateBlockGeneratedCases(), gen.GenOptions{})
}

func TestPredicateBlocksGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, predicateBlockGeneratedCases(), gen.GenOptions{})
}
