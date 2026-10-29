package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
)

// The rules of the Node table are printed from Flow IR (the lean value-free rules by the same route
// as the recognizer's, gen.Generator.StartRecFlow, and the others as framed bodies). The tests
// below check the routes the rules take and run the parsers against the engine; the generated text
// of the ten checked-in grammars is frozen in parsers/testdata/output_manifest.txt.

// TestGeneratedFlowNodeParity runs the generated Node parsers and recognizers of the generated-corpus
// harness with the lean rules of the Node table printed from Flow IR, against the engine, and with the
// recognizers printed from Flow IR as well. The route must be taken for some rule of every table, so that
// the test cannot pass without exercising it.
func TestGeneratedFlowNodeParity(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedFlowNodeParity)
}

func testGeneratedFlowNodeParity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := genCorpus(t)
	taken := map[string]int{}
	gh := &gogen.Hooks{}
	setGenHooks(t, gh)
	gh.LeanRoute = func(table string, r *compile.Rule) { taken[table]++ }
	framed, operators := map[string]int{}, map[string]int{}
	gh.FramedRoute = func(table string, r *compile.Rule) { framed[table]++ }
	gh.OperatorsRoute = func(table string, r *compile.Rule) { operators[table]++ }
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := generateCode(t, g, gen.GenOptions{Start: "main", Package: "gen", Recognize: true}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	gh.LeanRoute, gh.FramedRoute, gh.OperatorsRoute = nil, nil, nil
	for _, table := range []string{"rules", "recRules"} {
		if operators[table] == 0 {
			t.Errorf("no Operators Rule line of the %s table is printed from Flow IR: the parity test does not cover them", table)
		}
		if framed[table] == 0 {
			t.Errorf("no framed rule of the %s table is printed from Flow IR: the parity test does not cover them", table)
		}
	}
	if taken["rules"] == 0 {
		t.Fatal("no rule of the Node table takes a direct route: the parity test exercises nothing")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	t.Run("flow node rules", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{}) })
}

// operatorGrammars are the checked-in grammars with an Operators Rule.
var operatorGrammars = map[string]bool{"cel": true, "cue": true, "duckdb": true, "golang": true, "python": true, "typescript": true}

// TestFlowNodeRulesPrintFromFlow generates the Node parsers of the checked-in grammars in each form
// and checks that the rules of the Node table and the lines of their Operators Rules take the Flow route
// (a rule or line Flow IR does not print fails the generation), so that the generated text that the output manifest freezes is Flow's. In
// the forms with observability it also checks that the matchers write the reports of skipped alternatives.
func TestFlowNodeRulesPrintFromFlow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generates the large parsers")
	}
	names := []string{"json", "cel", "cue", "duckdb", "golang", "python", "xml", "yaml", "csv", "typescript"}
	sort.Strings(names)
	for _, form := range []struct {
		name              string
		controls, observe bool
		document          bool
	}{{"ordinary", false, false, false}, {"controls", true, false, false}, {"observability", false, true, false}, {"observability+controls", true, true, false},
		{"document", false, false, true}, {"document+controls", true, false, true}} {
		for _, name := range names {
			t.Run(name+"/"+form.name, func(t *testing.T) {
				src, err := os.ReadFile(filepath.Join("..", "..", "parsers", name, name+".pego"))
				if err != nil {
					t.Fatal(err)
				}
				g, err := parseTest(string(src))
				if err != nil {
					t.Fatal(err)
				}
				o := gen.GenOptions{Package: "x", Module: "example.com/x", Runtime: gen.RuntimeImport, Recognize: true, WorkControls: form.controls, Observability: form.observe, Document: form.document}
				taken, framed, operators := 0, 0, 0
				gh := &gogen.Hooks{}
				setGenHooks(t, gh)
				gh.LeanRoute = func(table string, r *compile.Rule) {
					if table == "rules" {
						taken++
					}
				}
				gh.FramedRoute = func(table string, r *compile.Rule) { framed++ }
				gh.OperatorsRoute = func(table string, r *compile.Rule) { operators++ }
				files, err := Generate(t, g, o)
				if err != nil {
					t.Fatal(err)
				}
				flow := mergeGenerated(files)
				if taken == 0 {
					t.Errorf("no rule of the Node table of %s takes the Flow route", name)
				}
				if operators == 0 && operatorGrammars[name] {
					t.Errorf("no Operators Rule line of %s is printed from Flow IR", name)
				}
				if framed == 0 {
					t.Errorf("no framed rule of the Node table of %s takes the Flow route", name)
				}
				if form.observe {
					// The reports of skipped alternatives are the observed events the matchers write.
					if fs := bytes.Count(flow, []byte("p.Observation.Skipped(")); fs == 0 && name != "csv" && name != "json" {
						t.Errorf("%s: no skipped-alternative reports from Flow IR", name)
					}
				}
			})
		}
	}
}

// TestFlowNodeRulesAcceptStreamInWrappedSequence generates, without work controls, parsers whose start
// rule has its #stream repetition in a sequence that an attribute or a group wraps. Core IR is built
// for the whole Node program, so Verify must accept the stream there, as the grammar and the engine do.
func TestFlowNodeRulesAcceptStreamInWrappedSequence(t *testing.T) {
	t.Parallel()
	pinOrdinary(t)
	for name, src := range map[string]string{
		"attributed sequence": `rule main = (([a-z] #atomic)* #stream "!") @diagnostic(name="x") $$`,
		"attributed group": `rule main = (item* #stream) @diagnostic(name="items") "." $$
rule item = [a-z] #atomic`,
		"grouped sequence": `rule main = ((item* #stream) ".") $$
rule item = [a-z] #atomic`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, stream := range []bool{false, true} {
			opts := gen.GenOptions{Start: "main", Package: "gen", Stream: stream}
			if _, err := generateCode(t, g, opts); err != nil {
				t.Errorf("%s (stream %v): %v", name, stream, err)
			}
		}
	}
}

// TestFlowNodeRecognizersOnGrammars runs the generated recognizers and parsers of the ten checked-in
// grammars on the .txt inputs of their test data (the valid ones and the erroneous ones), against the
// engine, with the rules of both tables printed from Flow IR. A recognizer runs the actions of the rules
// that predicates make value-needed, so the repetitions of its Flow-printed rules must gather what those
// actions read.
func TestFlowNodeRecognizersOnGrammars(t *testing.T) {
	t.Parallel()
	eachMode(t, testFlowNodeRecognizersOnGrammars)
}

func testFlowNodeRecognizersOnGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	var cases []genCase
	for _, name := range []string{"json", "cel", "cue", "duckdb", "golang", "python", "xml", "yaml", "csv", "typescript"} {
		dir := filepath.Join("..", "..", "parsers", name)
		src, err := os.ReadFile(filepath.Join(dir, name+".pego"))
		if err != nil {
			t.Fatal(err)
		}
		gc := genCase{name: name, src: string(src)}
		files, err := filepath.Glob(filepath.Join(dir, "testdata", "*.txt"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no test data inputs (%v)", name, err)
		}
		for _, f := range files {
			in, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			gc.inputs = append(gc.inputs, string(in))
		}
		cases = append(cases, gc)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
}
