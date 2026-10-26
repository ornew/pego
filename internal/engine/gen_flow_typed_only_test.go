package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A typed-only parser has no Node family unless a typed entry point converts the Node result of a rule; then
// it keeps the Node rule functions (keepNodes). Those rules print from Flow IR as in any other Node build.
// The text test forces keepNodes, so that the ten checked-in grammars print their Node table in a typed-only
// parser, and compares it with dgen's (the grammars with an Operators Rule only by route; the corpus has
// dead alternatives that dgen prints and Flow IR drops, so it is not a text oracle); the behavior tests compare the typed entry points of grammars whose
// entries really convert Nodes.

// TestFlowTypedOnlyNodeRulesMatchDirect compares the Node rules of a typed-only parser, in the ordinary and the
// work-controlled forms, with the output of dgen (directNodeRules): equal beyond blank assignments of unread
// variables.
func TestFlowTypedOnlyNodeRulesMatchDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the corpus twice")
	}
	cases := grammarCases(t)
	for _, form := range []struct {
		name     string
		controls bool
	}{{"ordinary", false}, {"controls", true}} {
		t.Run(form.name, func(t *testing.T) {
			taken, framed, operators, compared := 0, 0, 0, 0
			for _, c := range cases {
				g, err := parseTest(c.src)
				if err != nil {
					continue
				}
				gen := func(direct bool) []byte {
					files, err := Generate(g, GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: RuntimeImport,
						Types: true, TypedOnly: true, keepNodes: true, WorkControls: form.controls, directNodeRules: direct})
					if err != nil {
						t.Fatalf("%s: %v", c.name, err)
					}
					return mergeGenerated(files)
				}
				tk, fr, op := 0, 0, 0
				directRouteHook = func(table string, r *rule) {
					if table == "rules" {
						tk++
					}
				}
				framedRouteHook = func(table string, r *rule) { fr++ }
				operatorsRouteHook = func(table string, r *rule) { op++ }
				flow := gen(false)
				directRouteHook, framedRouteHook, operatorsRouteHook = nil, nil, nil
				taken, framed, operators = taken+tk, framed+fr, operators+op
				direct := gen(true)
				compared++
				fb, db := unreadBlank.ReplaceAll(flow, nil), unreadBlank.ReplaceAll(direct, nil)
				if operatorGrammars[c.name] {
					// The numbering of the shared literals and shapers of the typed and Node lines differs between the
					// printers, so the text outside the lines is not equal; the behavior tests cover them.
					continue
				}
				if string(fb) != string(db) {
					t.Errorf("%s: the Flow Node rules of a typed-only parser differ from dgen beyond blank assignments (%d and %d bytes)", c.name, len(flow), len(direct))
				}
			}
			if compared < 10 || taken == 0 || framed == 0 || operators == 0 {
				t.Errorf("compared %d grammars; %d direct, %d framed and %d Operators Rule lines took the Flow route", compared, taken, framed, operators)
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

// TestFlowTypedOnlyEntriesMatchDirect runs the typed entry points of typed-only parsers whose Node rules are
// printed from Flow IR, and compares their transcripts (values, errors with positions, depth errors) with those of
// the same parsers printed by dgen and of a parser with every entry point. A mutated grammar must change the
// transcript, and the entries must convert Nodes, which the Flow route takes.
func TestFlowTypedOnlyEntriesMatchDirect(t *testing.T) {
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
			directRouteHook = func(table string, r *rule) {
				if table == "rules" {
					taken++
				}
			}
			framedRouteHook = func(table string, r *rule) { taken++ }
			operatorsRouteHook = func(table string, r *rule) { taken++ }
			var pkgs []string
			for i, c := range typedOnlyConverted {
				base := GenOptions{Types: true, WorkControls: form.controls}
				full, flowOnly, directOnly, mutated := fmt.Sprintf("full%d", i), fmt.Sprintf("flow%d", i), fmt.Sprintf("direct%d", i), fmt.Sprintf("mut%d", i)
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
				only.directNodeRules = true
				ordinaryOnlyPackage(t, dir, directOnly, c.src, only, typedOnlyTranscript, c.inputs)
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
				pkgs = append(pkgs, full, flowOnly, directOnly, mutated)
			}
			directRouteHook, framedRouteHook, operatorsRouteHook = nil, nil, nil
			out := runTranscripts(t, dir, pkgs...)
			for i, c := range typedOnlyConverted {
				full, flowOnly, directOnly, mutated := out[fmt.Sprintf("full%d", i)], out[fmt.Sprintf("flow%d", i)], out[fmt.Sprintf("direct%d", i)], out[fmt.Sprintf("mut%d", i)]
				if flowOnly != directOnly {
					t.Errorf("%s: the Flow typed-only parser differs from the dgen one:\n%s", c.name, firstTranscriptDifference(directOnly, flowOnly))
				}
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
