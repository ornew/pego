package engine

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

type predicateBlockCase struct {
	name, block, adjacent string
	inputs                []string
}

func predicateBlockCases() []predicateBlockCase {
	return []predicateBlockCase{
		{"whole body", `rule main = {true; true}`, `rule main = {true} {true}`, []string{"", "a"}},
		{"captured block", `rule main = x:{n := 1; n == 1} "a" $$ => $x`, `rule main = x:({n := 1} {n == 1}) "a" $$ => $x`, []string{"a", "", "aa"}},
		{"choice blocks", `rule main = ({false; true} / {true; true}) $$`, `rule main = (({false} {true}) / ({true} {true})) $$`, []string{"", "a"}},
		{"nullable block repetition", `rule main = {n := 0} ({n := n + 1; true})* {n == 1} $$`, `rule main = {n := 0} ({n := n + 1} {true})* {n == 1} $$`, []string{"", "a"}},
		{"assignments", `rule main = {x := 1; y := x + 1; y == 2} "é" $$`, `rule main = {x := 1} {y := x + 1} {y == 2} "é" $$`, []string{"é", "", "a", "éé"}},
		{"capture", `rule main = c:"é" {s := text($c); s == "é"} $$ => $c`, `rule main = c:"é" {s := text($c)} {s == "é"} $$ => $c`, []string{"é", "a", "", "éx"}},
		{"positional values", `rule main = v1:"a" {x := 1; x == 1} v2:"b" $$ => list($v1, $v2)`, `rule main = v1:"a" {x := 1} {x == 1} v2:"b" $$ => list($v1, $v2)`, []string{"ab", "a", "ac", ""}},
		{"choice rollback", `rule main = {x := 0} ("a" {x := 1; x == 1} "!" / "a" {x == 0; true} "?") $$`, `rule main = {x := 0} ("a" {x := 1} {x == 1} "!" / "a" {x == 0} {true} "?") $$`, []string{"a!", "a?", "a", "b"}},
		{"optional rollback", `rule main = {x := 0} ("a" {x := 1; false})? {x == 0; true} "a" $$`, `rule main = {x := 0} ("a" {x := 1} {false})? {x == 0} {true} "a" $$`, []string{"a", "", "aa"}},
		{"predicate failure", `rule main = "a" {1 / 0; true} "b" $$`, `rule main = "a" {1 / 0} {true} "b" $$`, []string{"ab", "a", ""}},
		{"short circuit", `rule main = "a" {false; 1 / 0} "b" $$`, `rule main = "a" {false} {1 / 0} "b" $$`, []string{"ab", "a", ""}},
		{"rule environment", `rule main = {x := 1; true} (n "!" / n "?") $$
rule n = {x == 1; true} "é"`, `rule main = {x := 1} {true} (n "!" / n "?") $$
rule n = {x == 1} {true} "é"`, []string{"é!", "é?", "é", "a"}},
		{"repetition", `rule main = {x := 0} ("a" {x := x + 1; true})* {x == 2; true} $$`, `rule main = {x := 0} ("a" {x := x + 1} {true})* {x == 2} {true} $$`, []string{"aa", "a", "aaa", ""}},
		{"lookahead", `rule main = {x := 0} &("a" {x := 1; true}) {x == 0; true} "a" $$`, `rule main = {x := 0} &("a" {x := 1} {true}) {x == 0} {true} "a" $$`, []string{"a", "", "b"}},
		{"cut", `rule main = "a" #cut {x := 1; x == 1} "!" $$ / "a" "?" $$`, `rule main = "a" #cut {x := 1} {x == 1} "!" $$ / "a" "?" $$`, []string{"a!", "a?", "a", ""}},
		{"recovery", `rule main = ("a" {x := 1; false} ";") #recover(skip="?") $$`, `rule main = ("a" {x := 1} {false} ";") #recover(skip="?") $$`, []string{"?", "a?", "a;", ""}},
	}
}

func TestPredicateBlockAssignmentComparison(t *testing.T) {
	program := compile(t, `rule a = {x:=1; x<10}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			if _, err := program.ParseWith("a", "", ParseOptions{Backend: backend, Unit: unit}); err != nil {
				t.Fatalf("%s/%s: %v", backend, unit, err)
			}
		}
	}
}

func TestPredicateBlocksAdjacentParity(t *testing.T) {
	for _, fixture := range predicateBlockCases() {
		t.Run(fixture.name, func(t *testing.T) {
			block := compile(t, fixture.block)
			adjacent := compile(t, fixture.adjacent)
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
	const source = "rule main = {\n // first\n x := (1 +\n 1)\n // second\n x == 2;\n} \"é\" $$"
	g, err := syntax.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	formattedGrammar, err := syntax.Parse(formatted)
	if err != nil || grammar.Format(formattedGrammar) != formatted {
		t.Fatalf("formatter round trip: %v", err)
	}
	for _, original := range []*grammar.Grammar{g, jsonActionBlockGrammar(t, g), formattedGrammar} {
		program, err := Compile(original, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, options := range []MarshalOptions{{}, {OmitAST: true}} {
			data, err := program.MarshalBinaryWith("main", options)
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if options.OmitAST && backend == Closure {
					continue
				}
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, input := range []string{"é", "", "a", "éé"} {
						parseOptions := ParseOptions{Backend: backend, Unit: unit}
						got := resultJSON(loaded.ParseWith("main", input, parseOptions))
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
	for _, source := range []string{
		`rule main = {x := 1; len(true)} "a"`,
		`rule main = {true; $1 == nil} "a"`,
		`rule main = {true; text($missing) == "a"} "a"`,
	} {
		// ParseLegacy reads $1, which still reaches the checker from JSON grammars.
		g, err := syntax.ParseLegacy(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); err == nil {
			t.Fatalf("accepted invalid statement: %s", source)
		}
	}
	for _, source := range []string{`rule main = {}`, `rule main = "a" => {true; true}`} {
		g, err := syntax.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); err == nil || !strings.Contains(err.Error(), "action statement blocks are not implemented") {
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
	testGeneratedParsersCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), predicateBlockGeneratedCases(), GenOptions{})
}

func TestPredicateBlocksGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, predicateBlockGeneratedCases(), GenOptions{})
}
