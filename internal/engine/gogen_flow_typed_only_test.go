package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
)

// A typed-only parser has no Node family unless a typed entry point converts the Node result of a rule; then
// it keeps the Node rule functions (keepNodes). Those rules print from Flow IR as in any other Node build.
// The route test forces keepNodes, so that the grammars of the corpus print their Node table in a typed-only
// parser, and checks that the rules and Operators Rule lines take the Flow route; the behavior tests compare
// the typed entry points of grammars whose entries really convert Nodes with those of the full parser.

// TestFlowTypedOnlyNodeRulesPrintFromFlow generates the Node rules of a typed-only parser, in the ordinary
// and the work-controlled forms, over the corpus, and checks that every rule and Operators Rule line takes
// the Flow route (a rule Flow IR does not print fails the generation).
func TestFlowTypedOnlyNodeRulesPrintFromFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := grammarCases(t)
	for _, form := range []struct {
		name     string
		controls bool
	}{{"ordinary", false}, {"controls", true}} {
		t.Run(form.name, func(t *testing.T) {
			taken, framed, operators, generated := 0, 0, 0, 0
			gogen.LeanRouteHook = func(table string, r *compile.Rule) {
				if table == "rules" {
					taken++
				}
			}
			gogen.FramedRouteHook = func(table string, r *compile.Rule) { framed++ }
			gogen.OperatorsRouteHook = func(table string, r *compile.Rule) { operators++ }
			defer func() { gogen.LeanRouteHook, gogen.FramedRouteHook, gogen.OperatorsRouteHook = nil, nil, nil }()
			for _, c := range cases {
				g, err := parseTest(c.src)
				if err != nil {
					continue
				}
				_, err = Generate(g, gen.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: gen.RuntimeImport,
					Types: true, TypedOnly: true, KeepNodes: true, WorkControls: form.controls})
				if err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
				generated++
			}
			if generated < 10 || taken == 0 || framed == 0 || operators == 0 {
				t.Errorf("generated %d grammars; %d direct, %d framed and %d Operators Rule lines took the Flow route", generated, taken, framed, operators)
			}
		})
	}
}

// typedOnlyConverted are grammars whose typed entry points convert Nodes (an action reads a field of a struct), so
// that a typed-only parser keeps the Node rules and its entry points run them: a plain sequence, recovery, a
// fold, a left-recursive leader, a cut with an environment and an Operators Rule.
var typedOnlyConverted = []genCase{
	{"converted sequence", ordinaryOnlyConverted, ordinaryOnlyConvertedInputs},
	{"recovery", `type Item = { Name node[string] }
type Box = { Items node[_]; F Item | error; FT string }
type Out = { Box Box; Start int; End int; Text string; FNil bool }
rule main Out = b:box => Out{Box: $b, Start: $b.Items.Start, End: $b.Items.End, Text: text($b.Items), FNil: $b.F == nil}
rule box Box = "(" xs:item* ")" fw:(f:fitem "") => Box{Items: $xs, F: $f, FT: text($fw)}
rule item Item = " "* i:item2 => $i
rule item2 Item = n:"x" #atomic => Item{Name: $n}
rule fitem Item | error = yitem #recover(skip=[a-z]+)
rule yitem Item = n:"y" #atomic => Item{Name: $n}`,
		[]string{"(x x)y", "(xx)y", "( x )q", "(x)", "(x x", "(x x)zzy", "", "(é)y"}},
	{"fold", `type Op = { Left Node; Op node[string]; Right Node }
type Node ~ Op | node[string]
type Args = { Items []node[string]; N int; S string }
rule main Args = e:(expr #atomic) ";" a:args => Args{Items: concat($a.Items, $a.Items), N: len($a.Items) * 2 - 1, S: text($e) + "!"}
rule expr Node = l:num rest:(op:("+" / "-") #atomic r:num)* => foldl($l, $rest, (acc, i) => Op{Left: acc, Op: i.$op, Right: i.$r})
rule args Args = first:num rest:(-"," x:num)* => Args{Items: concat(list($first), map($rest, (r) => r.$x)), N: len($rest)}
rule num = [0-9]+ #atomic`,
		[]string{"1+2-3;4,5,6", "1;2", "1-;2", "", "1+2;", "12+3;4,", ";"}},
	{"left recursion", `type E = { Items []node[string]; N int }
rule main E = e:expr "!" => E{Items: $e.Items, N: len($e.Items)}
rule expr E = (l:expr "," x:item => E{Items: concat($l.Items, list($x)), N: 0}) / (x:item => E{Items: list($x), N: 0})
rule item = [a-z]+ #atomic`,
		[]string{"a!", "a,b,c!", "a,b", ",a!", "a,,b!", "", "a,b,!", "ab,cd!x"}},
	{"cut and variables", `type R = { Names node[_]; N int }
var n int = 0
rule main R = i:item "!" => R{Names: $i.Names, N: n}
rule item R = ("a" #cut {n := n + 1} xs:name+ => R{Names: $xs, N: 0}) / ("b" {n := n + 2} xs:name* => R{Names: $xs, N: 0})
rule name = [a-z] #atomic ","?`,
		[]string{"ab,c,!", "b!", "bx!", "a!", "a", "c!", "bxy,z!", ""}},
	{"operators", `type It = { L []string }
rule main It = e:expr $$ => It{L: concat($e.L, list("!"))}
rule expr It = #operators {
    skip " "*
    operand x:num => $x
    level { infix left o:("+" / "-") => It{L: concat($lhs.L, list(text($o)), $rhs.L)} }
    level { infix right o:("*" / "/") => It{L: concat(list("["), $lhs.L, list(text($o)), $rhs.L, list("]"))} }
}
rule num It = d:[0-9]+ #atomic => It{L: list(text($d))}`,
		[]string{"1", "1 + 2", "1 - 2 * 3", "1 * 2 / 3 + 4", "1 +", "", " 1  *  2", "1 + 2 +"}},
}

// typedOnlyTranscript prints the typed results of the entry points of the package PKG on INPUTS in both position
// units and with a nesting limit.
const typedOnlyTranscript = `package PKG

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	e := err.Error()
	var se *SyntaxError
	if errors.As(err, &se) {
		e += fmt.Sprintf(" @%d:%d:%d", se.Line, se.Col, se.Pos)
	}
	return e
}

func show(v any, err error) string {
	j, jerr := json.Marshal(v)
	if jerr != nil {
		return "marshal: " + jerr.Error()
	}
	return string(j) + " | " + errText(err)
}

func TestTranscript(t *testing.T) {
	var b strings.Builder
	for _, in := range INPUTS {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			v, err := ParseAST(in, WithUnit(unit))
			fmt.Fprintf(&b, "ast %q %v: %s\n", in, unit, show(v, err))
			w, err := ParseRuleAST("main", in, WithUnit(unit))
			fmt.Fprintf(&b, "rule main %q %v: %s\n", in, unit, show(w, err))
		}
		v, err := ParseAST(in, WithMaxDepth(3))
		fmt.Fprintf(&b, "depth %q: %s\n", in, show(v, err))
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("TRANSCRIPT_DIR"), "PKG.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
`

// TestFlowTypedOnlyEntriesMatchFull runs the typed entry points of typed-only parsers whose Node rules are
// printed from Flow IR, and compares their transcripts (values, errors with positions, depth errors) with those of
// a parser with every entry point. A mutated grammar must change the
// transcript, and the entries must convert Nodes, which the Flow route takes.
func TestFlowTypedOnlyEntriesMatchFull(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, form := range []struct {
		name     string
		controls bool
	}{{"ordinary", false}, {"controls", true}} {
		t.Run(form.name, func(t *testing.T) {
			dir := t.TempDir()
			writeGoMod(t, dir)
			taken := 0
			gogen.LeanRouteHook = func(table string, r *compile.Rule) {
				if table == "rules" {
					taken++
				}
			}
			gogen.FramedRouteHook = func(table string, r *compile.Rule) { taken++ }
			gogen.OperatorsRouteHook = func(table string, r *compile.Rule) { taken++ }
			var pkgs []string
			for i, c := range typedOnlyConverted {
				base := gen.GenOptions{Types: true, WorkControls: form.controls}
				full, flowOnly, mutated := fmt.Sprintf("full%d", i), fmt.Sprintf("flow%d", i), fmt.Sprintf("mut%d", i)
				before := taken
				only := base
				only.TypedOnly = true
				ordinaryOnlyPackage(t, dir, flowOnly, c.src, only, typedOnlyTranscript, c.inputs)
				if taken == before {
					t.Errorf("%s: no Node rule of the typed-only parser takes the Flow route", c.name)
				}
				if src, err := os.ReadFile(filepath.Join(dir, flowOnly, "parser.go")); err != nil || !strings.Contains(string(src), "RunParseConverted(") {
					t.Errorf("%s: the entries of the typed-only parser do not convert Nodes (%v)", c.name, err)
				}
				full1 := base
				full1.Recognize = true
				ordinaryOnlyPackage(t, dir, full, c.src, full1, typedOnlyTranscript, c.inputs)
				mut := strings.Replace(c.src, `"!"`, `"?"`, 1)
				if mut == c.src {
					mut = strings.Replace(c.src, `"x"`, `"w"`, 1)
				}
				if mut == c.src {
					mut = strings.Replace(c.src, `[a-z]`, `[a-y]`, 1)
				}
				if mut == c.src {
					mut = strings.Replace(c.src, `[0-9]`, `[0-8]`, 1)
				}
				mo := base
				mo.TypedOnly = true
				ordinaryOnlyPackage(t, dir, mutated, mut, mo, typedOnlyTranscript, c.inputs)
				pkgs = append(pkgs, full, flowOnly, mutated)
			}
			gogen.LeanRouteHook, gogen.FramedRouteHook, gogen.OperatorsRouteHook = nil, nil, nil
			out := runTranscripts(t, dir, pkgs...)
			for i, c := range typedOnlyConverted {
				full, flowOnly, mutated := out[fmt.Sprintf("full%d", i)], out[fmt.Sprintf("flow%d", i)], out[fmt.Sprintf("mut%d", i)]
				if flowOnly != full {
					t.Errorf("%s: the typed-only parser differs from the full one:\n%s", c.name, firstTranscriptDifference(full, flowOnly))
				}
				if mutated == flowOnly {
					t.Errorf("%s: the transcripts of a mutated grammar are the same: the comparison cannot fail", c.name)
				}
				if !strings.Contains(flowOnly, "syntax error") {
					t.Errorf("%s: no input fails in the transcript", c.name)
				}
			}
		})
	}
}

func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	if err := writeFile(filepath.Join(dir, "go.mod"), testGoMod("ordtest")); err != nil {
		t.Fatal(err)
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }
