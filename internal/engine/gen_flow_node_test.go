package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"testing"
)

// The lean value-free rules of the Node table are printed from Flow IR in ordinary builds (the same
// route as the recognizer's, startRecFlow). The private directNodeRules option prints them with dgen
// instead; the tests below use it as the oracle of the Flow route.

// TestGeneratedFlowNodeParity runs the generated Node parsers and recognizers of the generated-corpus
// harness with the lean rules of the Node table printed from Flow IR, against the engine, and with the
// recognizers printed from Flow IR as well. The route must be taken for some rule of every table, so that
// the test cannot pass without exercising it.
func TestGeneratedFlowNodeParity(t *testing.T) { eachMode(t, testGeneratedFlowNodeParity) }

func testGeneratedFlowNodeParity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := genCorpus(t)
	taken := map[string]int{}
	directRouteHook = func(table string, r *rule) { taken[table]++ }
	framed, operators := map[string]int{}, map[string]int{}
	framedRouteHook = func(table string, r *rule) { framed[table]++ }
	operatorsRouteHook = func(table string, r *rule) { operators[table]++ }
	t.Cleanup(func() { directRouteHook, framedRouteHook, operatorsRouteHook = nil, nil, nil })
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := generateCode(g, GenOptions{Start: "main", Package: "gen", Recognize: true}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	directRouteHook, framedRouteHook, operatorsRouteHook = nil, nil, nil
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
	t.Run("direct node rules", func(t *testing.T) {
		testGeneratedParsersCorpus(t, goBin, cases, GenOptions{directNodeRules: true, directRecognizer: true})
	})
	t.Run("flow node rules", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{}) })
}

// unreadBlank matches the line that dgen writes for the variables of a rule that its body never reads.
// Flow IR declares a variable only for what its function reads, so the Flow form has no such line.
var unreadBlank = regexp.MustCompile(`(?m)^\t+_(, _)* = x\d+(, x\d+)*\n`)

// operatorGrammars are the checked-in grammars with an Operators Rule.
var operatorGrammars = map[string]bool{"cel": true, "cue": true, "duckdb": true, "golang": true, "python": true, "typescript": true}

// TestFlowNodeRulesMatchDirect generates the Node parsers of the checked-in grammars with the rules of
// both tables printed from Flow IR and with dgen (directNodeRules and directRecognizer), and checks that
// the output is equal except for the blank assignments of unread variables, which only dgen writes. The
// bodies agree with dgen's projections of the Node table's actions, and so do the actions themselves.
// It runs for parsers without and with work controls; the text of a work-control build equals dgen's as
// well outside the Operators Rule lines (lineResidue), because the rules print alike from the same Attrs.Steps.
func TestFlowNodeRulesMatchDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the large parsers twice")
	}
	names := []string{"json", "cel", "cue", "duckdb", "golang", "python", "xml", "yaml", "csv", "typescript"}
	sort.Strings(names)
	for _, form := range []struct {
		name              string
		controls, observe bool
	}{{"ordinary", false, false}, {"controls", true, false}, {"observability", false, true}, {"observability+controls", true, true}} {
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
				gen := func(o GenOptions) []byte {
					o.Package, o.Module, o.Runtime, o.Recognize, o.WorkControls, o.Observability = "x", "example.com/x", RuntimeImport, true, form.controls, form.observe
					files, err := Generate(g, o)
					if err != nil {
						t.Fatal(err)
					}
					return mergeGenerated(files)
				}
				taken, framed, operators := 0, 0, 0
				directRouteHook = func(table string, r *rule) {
					if table == "rules" {
						taken++
					}
				}
				framedRouteHook = func(table string, r *rule) { framed++ }
				operatorsRouteHook = func(table string, r *rule) { operators++ }
				flow := gen(GenOptions{})
				directRouteHook, framedRouteHook, operatorsRouteHook = nil, nil, nil
				if taken == 0 {
					t.Errorf("no rule of the Node table of %s takes the Flow route", name)
				}
				if operators == 0 && operatorGrammars[name] {
					t.Errorf("no Operators Rule line of %s is printed from Flow IR", name)
				}
				if framed == 0 {
					t.Errorf("no framed rule of the Node table of %s takes the Flow route", name)
				}
				direct := gen(GenOptions{directNodeRules: true, directRecognizer: true})
				if form.observe {
					// The reports of skipped alternatives are the observed events the matchers write. Both printers
					// write them at the same sites, except that Flow IR repeats the matcher of a line that dgen
					// shares among lines (the observation tests compare the events themselves), so the counts are
					// equal for the grammars without an Operators Rule.
					fs, ds := bytes.Count(flow, []byte("p.Observation.Skipped(")), bytes.Count(direct, []byte("p.Observation.Skipped("))
					if fs == 0 && name != "csv" && name != "json" || ds == 0 && fs != 0 || !operatorGrammars[name] && fs != ds {
						t.Errorf("%s: %d skipped-alternative reports from Flow IR, %d from dgen", name, fs, ds)
					}
				}
				if operatorGrammars[name] {
					// The old printers do not print the lines as Flow IR does: compare outside them (lineResidue).
					do, fo := lineResidue(unreadBlank.ReplaceAll(flow, nil), unreadBlank.ReplaceAll(direct, nil))
					if len(do) > 0 || len(fo) > 0 {
						t.Errorf("outside the Operators Rule lines, the Flow output of %s has %d chunks that dgen's lacks (first: %s) and dgen's has %d that Flow's lacks (first: %s)", name, len(fo), summary(fo), len(do), summary(do))
					}
					return
				}
				if string(unreadBlank.ReplaceAll(flow, nil)) != string(unreadBlank.ReplaceAll(direct, nil)) {
					t.Errorf("the Flow Node rules of %s differ from dgen beyond blank assignments of unread variables (%d and %d bytes)", name, len(flow), len(direct))
				}
			})
		}
	}
}

// TestFlowNodeRulesAcceptStreamInWrappedSequence generates, without work controls, parsers whose start
// rule has its #stream repetition in a sequence that an attribute or a group wraps. Core IR is built
// for the whole Node program, so Verify must accept the stream there, as the grammar and the engine do.
func TestFlowNodeRulesAcceptStreamInWrappedSequence(t *testing.T) {
	t.Setenv("PEGO_TEST_ORDINARY", "1")
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
			for _, direct := range []bool{false, true} {
				opts := GenOptions{Start: "main", Package: "gen", Stream: stream, directNodeRules: direct}
				if _, err := generateCode(g, opts); err != nil {
					t.Errorf("%s (stream %v, direct node rules %v): %v", name, stream, direct, err)
				}
			}
		}
	}
}

// TestFlowNodeRecognizersOnGrammars runs the generated recognizers and parsers of the ten checked-in
// grammars on the .txt inputs of their test data (the valid ones and the erroneous ones), against the
// engine, with the rules of both tables printed from Flow IR. A recognizer runs the actions of the rules
// that predicates make value-needed, so the repetitions of its Flow-printed rules must gather what those
// actions read.
func TestFlowNodeRecognizersOnGrammars(t *testing.T) { eachMode(t, testFlowNodeRecognizersOnGrammars) }

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
	testGeneratedParsersCorpus(t, goBin, cases, GenOptions{})
}
