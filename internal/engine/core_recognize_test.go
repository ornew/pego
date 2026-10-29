package engine

import (
	"fmt"
	"github.com/ornew/pego/internal/gentest"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	irbuild "github.com/ornew/pego/internal/ir/build"
	"github.com/ornew/pego/internal/ir/core"
)

// coreRecognizeCase builds the Core IR of the recognizer of the parser generated with Recognize,
// with the passes that apply to its rule table recRules (no terminal programs and no class runs
// there), verifying the module after each pass.
func coreRecognizeCase(name, mode string, g *gogen.Generator) (*coreGenCase, error) {
	rec, err := recognizerOf(g)
	if err != nil {
		return nil, err
	}
	c := &coreGenCase{name: name, mode: mode, table: "recRules", golden: recognizerGolden, gen: g, origin: map[core.Node]grammar.Expr{}}
	c.rules = append(append([]*compile.Rule(nil), rec.RuleList...), rec.Twins...)
	m, err := irbuild.CoreFrom(rec, c.origin)
	if err != nil {
		return nil, err
	}
	check := func(pass string) {
		core.Analyze(m)
		if err := core.Verify(m); err != nil {
			c.passErrs = append(c.passErrs, fmt.Sprintf("after %s: %v", pass, err))
		}
	}
	check("build")
	core.RewriteTerms(m, core.TermOptions{TextDispatch: !g.DisableLiteralTextDispatch, FieldCompare: !g.BoxedComparisons})
	check("terms")
	core.Guards(m, g.Ordinary && !g.DisableGuardedClasses)
	check("guards")
	repeats := irbuild.FollowRepeats(m, c.origin)
	core.Scans(m)
	for _, f := range repeats {
		f()
	}
	check("scans")
	c.m = m
	return c, nil
}

// guardedClassCount returns the number of alternatives of the module whose first class is a guarded class.
func guardedClassCount(m *core.Module) int {
	n := 0
	for _, r := range m.Rules {
		core.Walk(r.Body, func(x core.Node) {
			if ch, ok := x.(*core.Choice); ok {
				for _, a := range ch.Alts {
					if a.GuardedClass {
						n++
					}
				}
			}
		})
	}
	return n
}

// recognizerGenModes are the modes in which the Core IR of the recognizers is frozen.
var recognizerGenModes = []coreGenMode{
	{"lean", gen.GenOptions{Recognize: true}},
	{"controlled", gen.GenOptions{Recognize: true, WorkControls: true}},
}

// recognizerCorpus is the corpus grammars of the recognizer freeze.
func recognizerCorpus(t *testing.T) []gentest.Case {
	return append(append(gentest.CorpusWithParsers(t), coreExtraGrammars...), recognizerKnownDifferences...)
}

// recognizerKnownDifferences are grammars where the Core IR guards differently from what the
// recognizers printed before they were printed from Flow IR: a capture that no predicate reads builds
// nothing in Core IR, so the class it captures can be a guarded class, and a rule that recovers has a
// class guard in Core IR that the printer did not write. Flow IR prints Core's guards, so the
// freeze holds them and TestCoreRecognizerDecisionsFrozen requires the guarded class.
var recognizerKnownDifferences = []gentest.Case{
	{Name: "uncaptured class", Src: "rule main = (x:[a-z] \"1\" / \"b\") $$\n"},
	{Name: "recovering rule", Src: "rule main = x* $$\nrule x = (\"a\" \"b\") #recover(skip=[^;]*) / [0-9]\n"},
}

// TestCoreRecognizerDecisionsFrozen compares the decisions of the Core IR of the recognizers of the
// corpus with those frozen in testdata/recognizer_decisions.golden, in each recognizer mode. The
// grammars of recognizerKnownDifferences must have a guarded class in the lean mode, and none in the
// controlled one. -update rewrites the golden (core_typed_decisions_test.go).
func TestCoreRecognizerDecisionsFrozen(t *testing.T) {
	t.Parallel()
	recognizerGolden.freeze(t, recognizerGenModes, func(t *testing.T, mode coreGenMode) []*coreGenCase {
		var cases []*coreGenCase
		for _, gc := range recognizerCorpus(t) {
			gen, ok := coreGenerate(gc.Src, mode.opts)
			if !ok {
				continue
			}
			if _, err := recognizerOf(gen); err != nil {
				continue
			}
			c, err := coreRecognizeCase(gc.Name, mode.name, gen)
			if err != nil {
				t.Errorf("%s: %v", gc.Name, err)
				continue
			}
			for _, known := range recognizerKnownDifferences {
				if known.Name != gc.Name {
					continue
				}
				want := 0
				if !mode.opts.WorkControls {
					want = 1
				}
				if got := guardedClassCount(c.m); got != want {
					t.Errorf("%s: %d guarded classes, want %d", gc.Name, got, want)
				}
			}
			cases = append(cases, c)
		}
		return cases
	}, func(t *testing.T, mode coreGenMode, n *coreDecisionCounts) {
		lean := !mode.opts.WorkControls
		if n.guards == 0 || n.scans == 0 || n.scopes == 0 || (n.guardedClasses > 0) != lean {
			t.Errorf("counts %+v", n)
		}
	})
}
