package engine

import (
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
func TestGeneratedFlowNodeParity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	t.Setenv("PEGO_TEST_ORDINARY", "1")
	cases := genCorpus(t)
	taken := map[string]int{}
	directRouteHook = func(table string, r *rule) { taken[table]++ }
	t.Cleanup(func() { directRouteHook = nil })
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := generateCode(g, GenOptions{Start: "main", Package: "gen", Recognize: true}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	directRouteHook = nil
	if taken["rules"] == 0 {
		t.Fatal("no rule of the Node table takes a direct route: the parity test exercises nothing")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	t.Run("direct node rules", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{directNodeRules: true}) })
	t.Run("flow node rules", func(t *testing.T) { testGeneratedParsersCorpus(t, goBin, cases, GenOptions{}) })
}

// unreadBlank matches the line that dgen writes for the variables of a rule that its body never reads.
// Flow IR declares a variable only for what its function reads, so the Flow form has no such line.
var unreadBlank = regexp.MustCompile(`(?m)^\t_(, _)* = x\d+(, x\d+)*\n`)

// TestFlowNodeRulesMatchDirect generates the Node parsers of the checked-in grammars with the lean rules
// of the Node table printed from Flow IR and with dgen, and checks that the output is equal except for the
// blank assignments of unread variables, which only dgen writes.
func TestFlowNodeRulesMatchDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the large parsers twice")
	}
	names := []string{"json", "cel", "cue", "duckdb", "golang", "python", "xml", "yaml", "csv", "typescript"}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "..", "parsers", name, name+".pego"))
			if err != nil {
				t.Fatal(err)
			}
			g, err := parseTest(string(src))
			if err != nil {
				t.Fatal(err)
			}
			gen := func(o GenOptions) []byte {
				o.Package, o.Module, o.Runtime, o.Recognize = "x", "example.com/x", RuntimeImport, true
				files, err := Generate(g, o)
				if err != nil {
					t.Fatal(err)
				}
				return mergeGenerated(files)
			}
			taken := 0
			directRouteHook = func(table string, r *rule) {
				if table == "rules" {
					taken++
				}
			}
			flow := gen(GenOptions{})
			directRouteHook = nil
			if taken == 0 {
				t.Errorf("no rule of the Node table of %s takes the Flow route", name)
			}
			direct := gen(GenOptions{directNodeRules: true})
			if string(unreadBlank.ReplaceAll(flow, nil)) != string(unreadBlank.ReplaceAll(direct, nil)) {
				t.Errorf("the Flow Node rules of %s differ from dgen beyond blank assignments of unread variables (%d and %d bytes)", name, len(flow), len(direct))
			}
		})
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
