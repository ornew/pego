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
	"github.com/ornew/pego/internal/gentest"
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
	t.Parallel()
	if testing.Short() {
		t.Skip("generates the corpus")
	}
	cases := gentest.GrammarCases(t)
	for _, form := range []struct {
		name     string
		controls bool
	}{{"ordinary", false}, {"controls", true}} {
		t.Run(form.name, func(t *testing.T) {
			taken, framed, operators, generated := 0, 0, 0, 0
			gh := &gogen.Hooks{}
			gentest.SetGenHooks(t, gh)
			gh.LeanRoute = func(table string, r *compile.Rule) {
				if table == "rules" {
					taken++
				}
			}
			gh.FramedRoute = func(table string, r *compile.Rule) { framed++ }
			gh.OperatorsRoute = func(table string, r *compile.Rule) { operators++ }
			for _, c := range cases {
				g, err := parseTest(c.Src)
				if err != nil {
					continue
				}
				_, err = gentest.Generate(t, g, gen.GenOptions{Package: "x", Module: "example.com/x", Start: "main", Runtime: gen.RuntimeImport,
					Types: true, TypedOnly: true, KeepNodes: true, WorkControls: form.controls})
				if err != nil {
					t.Fatalf("%s: %v", c.Name, err)
				}
				generated++
			}
			if generated < 10 || taken == 0 || framed == 0 || operators == 0 {
				t.Errorf("generated %d grammars; %d direct, %d framed and %d Operators Rule lines took the Flow route", generated, taken, framed, operators)
			}
		})
	}
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
	t.Parallel()
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
			gh := &gogen.Hooks{}
			gentest.SetGenHooks(t, gh)
			gh.LeanRoute = func(table string, r *compile.Rule) {
				if table == "rules" {
					taken++
				}
			}
			gh.FramedRoute = func(table string, r *compile.Rule) { taken++ }
			gh.OperatorsRoute = func(table string, r *compile.Rule) { taken++ }
			var pkgs []string
			for i, c := range gentest.TypedOnlyConverted {
				base := gen.GenOptions{Types: true, WorkControls: form.controls}
				full, flowOnly, mutated := fmt.Sprintf("full%d", i), fmt.Sprintf("flow%d", i), fmt.Sprintf("mut%d", i)
				before := taken
				only := base
				only.TypedOnly = true
				ordinaryOnlyPackage(t, dir, flowOnly, c.Src, only, typedOnlyTranscript, c.Inputs)
				if taken == before {
					t.Errorf("%s: no Node rule of the typed-only parser takes the Flow route", c.Name)
				}
				if src, err := os.ReadFile(filepath.Join(dir, flowOnly, "parser.go")); err != nil || !strings.Contains(string(src), "RunParseConverted(") {
					t.Errorf("%s: the entries of the typed-only parser do not convert Nodes (%v)", c.Name, err)
				}
				full1 := base
				full1.Recognize = true
				ordinaryOnlyPackage(t, dir, full, c.Src, full1, typedOnlyTranscript, c.Inputs)
				mut := strings.Replace(c.Src, `"!"`, `"?"`, 1)
				if mut == c.Src {
					mut = strings.Replace(c.Src, `"x"`, `"w"`, 1)
				}
				if mut == c.Src {
					mut = strings.Replace(c.Src, `[a-z]`, `[a-y]`, 1)
				}
				if mut == c.Src {
					mut = strings.Replace(c.Src, `[0-9]`, `[0-8]`, 1)
				}
				mo := base
				mo.TypedOnly = true
				ordinaryOnlyPackage(t, dir, mutated, mut, mo, typedOnlyTranscript, c.Inputs)
				pkgs = append(pkgs, full, flowOnly, mutated)
			}
			gh.LeanRoute, gh.FramedRoute, gh.OperatorsRoute = nil, nil, nil
			out := runTranscripts(t, dir, pkgs...)
			for i, c := range gentest.TypedOnlyConverted {
				full, flowOnly, mutated := out[fmt.Sprintf("full%d", i)], out[fmt.Sprintf("flow%d", i)], out[fmt.Sprintf("mut%d", i)]
				if flowOnly != full {
					t.Errorf("%s: the typed-only parser differs from the full one:\n%s", c.Name, firstTranscriptDifference(full, flowOnly))
				}
				if mutated == flowOnly {
					t.Errorf("%s: the transcripts of a mutated grammar are the same: the comparison cannot fail", c.Name)
				}
				if !strings.Contains(flowOnly, "syntax error") {
					t.Errorf("%s: no input fails in the transcript", c.Name)
				}
			}
		})
	}
}

func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	if err := writeFile(filepath.Join(dir, "go.mod"), gentest.GoMod("ordtest")); err != nil {
		t.Fatal(err)
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }
