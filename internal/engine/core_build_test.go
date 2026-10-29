package engine

import (
	"slices"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	irbuild "github.com/ornew/pego/internal/ir/build"
	"github.com/ornew/pego/internal/ir/core"
)

// coreCorpusProgram compiles a corpus grammar for the Core IR tests, or reports false for a
// grammar that does not compile or that generated parsers refuse (#missing).
func coreCorpusProgram(t *testing.T, c genCase) (*Program, bool) {
	t.Helper()
	g, err := parseTest(c.src)
	if err != nil {
		return nil, false
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil || prog.HasMissing {
		return nil, false
	}
	return prog, true
}

// TestCoreBuildCorpus builds, analyzes and verifies the Core IR of every grammar of the corpus
// and of the parsers directory, and checks that the scope of every rule is the frame layout the
// generated Node rules use, which the typed rules share.
func TestCoreBuildCorpus(t *testing.T) {
	built := 0
	for _, c := range schemaCorpus(t) {
		prog, ok := coreCorpusProgram(t, c)
		if !ok {
			continue
		}
		m, err := irbuild.CoreFrom(prog.Static(), nil)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		core.Analyze(m)
		if err := core.Verify(m); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		g, ok := coreGenerate(c.src, gen.GenOptions{})
		if !ok {
			continue
		}
		for _, r := range typedRules(g) {
			if _, ok := m.Rules[r.ID].Body.(*core.OperatorsRule); ok {
				continue
			}
			if got, want := core.SlotNames(m.Rules[r.ID].Scope), g.NodeScopes[r]; !slices.Equal(got, want) {
				t.Errorf("%s: rule %s (%d): scope %v, generated Node rule %v", c.name, r.Name, r.ID, got, want)
			}
		}
		built++
	}
	if built < 100 {
		t.Fatalf("built %d grammars", built)
	}
}
