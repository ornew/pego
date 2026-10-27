package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
)

// coreRecognizeCase builds the Core IR of the recognizer of the parser dec generated with
// Recognize, with the passes that apply to its rule table recRules (the generator writes no
// terminal programs and no class runs there), verifying the module after each pass.
func coreRecognizeCase(name string, dec *genDecisions) (*coreGenCase, error) {
	g := dec.gen
	rec, err := g.prog.recognizer()
	if err != nil {
		return nil, err
	}
	c := &coreGenCase{name: name, table: "recRules", dec: dec, origin: map[core.Node]grammar.Expr{}}
	c.rules = append(append([]*rule(nil), rec.rules...), rec.twins...)
	m, err := buildCoreFrom(rec, c.origin)
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
	core.RewriteTerms(m, core.TermOptions{TextDispatch: !g.disableLiteralTextDispatch, FieldCompare: !g.boxedComparisons})
	check("terms")
	core.Guards(m, g.lean && !g.disableGuardedClasses)
	check("guards")
	repeats := followRepeats(m, c.origin)
	core.Scans(m)
	for _, f := range repeats {
		f()
	}
	check("scans")
	c.m = m
	return c, nil
}

// recognizeAspects are the aspects compared in recognizers. The terms are not: the recognizer's
// table writes actions without projections, where Core IR projects as in the other tables.
var recognizeAspects = []string{"verify", "generator", "terminal", "guard", "class runs", "scan", "scope"}

// recognizeDifferences returns the differences of the Core IR of c with the generated recognizer
// in recognizeAspects, and the number of guarded classes of uncaptured classes it leaves out
// (capturedGuardedClasses).
func recognizeDifferences(c *coreGenCase, n *coreDecisionCounts) ([]string, int) {
	excused := capturedGuardedClasses(c)
	var out []string
	diverged := 0
	for _, err := range compareCoreDecisions(c, n) {
		if excused[err] {
			diverged++
			continue
		}
		for _, a := range recognizeAspects {
			if strings.HasPrefix(err, a+": ") {
				out = append(out, err)
				break
			}
		}
	}
	return out, diverged
}

// TestCoreMatchesRecognizer compares the Core IR of the recognizers of the corpus with the decisions
// that the generator recorded for its recognizers, frozen in testdata/recognizer_decisions.golden
// (recognizeAspects). The one known difference is in the guards: a capture
// that no predicate reads builds nothing in Core IR, so the class it captures can be a guarded
// class, where the generator's guard sees a capture and the class tests the character again. The other
// is in the rules that recover: the generator does not guard an alternative that begins with a class there,
// where Core IR does (and Flow IR prints that guard). Only that case is excused, and the test requires
// the guarded class in Core IR for the grammars of recognizerKnownDifferences.
func TestCoreMatchesRecognizer(t *testing.T) {
	for _, mode := range recognizerGenModes {
		t.Run(mode.name, func(t *testing.T) {
			// The grammars are those of the golden file in this mode, so a grammar whose decisions
			// are not recorded cannot drop out of the comparison silently.
			want := recognizerGolden.grammarsOf(t, mode.name)
			var n coreDecisionCounts
			cases := 0
			divergedBy := map[string]int{}
			for _, gc := range recognizerCorpus(t) {
				if !want[gc.name] {
					continue
				}
				delete(want, gc.name)
				dec, ok := coreGenerate(gc.src, mode.opts)
				if !ok {
					t.Fatalf("%s: the grammar does not generate", gc.name)
				}
				recognizerGolden.load(t, recognizerRules(dec), mode.name, gc.name, dec)
				c, err := coreRecognizeCase(gc.name, dec)
				if err != nil {
					t.Errorf("%s: %v", gc.name, err)
					continue
				}
				cases++
				errs, diverged := recognizeDifferences(c, &n)
				for _, err := range errs {
					t.Errorf("%s: %s", c.name, err)
				}
				divergedBy[gc.name] = diverged
				if diverged > 0 {
					t.Logf("%s: %d guarded classes of uncaptured classes", c.name, diverged)
				}
			}
			if len(want) != 0 {
				t.Fatalf("grammars of %s are not in the corpus: %v", recognizerDecisionsGolden, want)
			}
			lean := !mode.opts.WorkControls
			if cases < 100 || n.guards == 0 || n.scans == 0 || n.scopes == 0 || (n.guardedClasses > 0) != lean {
				t.Errorf("%d recognizers, counts %+v", cases, n)
			}
			// The known differences, on grammars that have them: classes are guarded only in lean
			// parsers.
			wantDiverged := 0
			if lean {
				wantDiverged = 1
			}
			for _, name := range []string{"uncaptured class", "recovering rule"} {
				got, ok := divergedBy[name]
				if !ok || got != wantDiverged {
					t.Errorf("%s: %d guarded classes of the generator's unguarded alternatives, want %d", name, got, wantDiverged)
				}
			}
		})
	}
}

// capturedGuardedClasses returns the differences compareCoreDecisions reports for the guarded
// classes of c whose class the grammar captures: alternatives that begin with a capture of a
// class (through sequences).
func capturedGuardedClasses(c *coreGenCase) map[string]bool {
	out := map[string]bool{}
	for _, cr := range c.m.Rules {
		r := c.rules[cr.ID]
		core.Walk(cr.Body, func(x core.Node) {
			ch, ok := x.(*core.Choice)
			if !ok {
				return
			}
			for i, a := range ch.Alts {
				e, _ := c.altOrigin(ch, i)
				if !a.GuardedClass || !(capturesFirst(e) || (hasRecovery(r.bodyExpr()) && guardedFirst(e) != nil)) {
					continue
				}
				out[fmt.Sprintf("guard: rule %s: alternative %s: guarded class true, generator false", r.name, grammar.FormatExpr(e))] = true
			}
		})
	}
	return out
}

// capturesFirst reports whether the first expression e matches is a capture of a class.
func capturesFirst(e grammar.Expr) bool {
	for {
		switch x := e.(type) {
		case *grammar.Seq:
			e = x.Items[0]
		case *grammar.Capture:
			_, ok := x.Expr.(*grammar.CharClass)
			return ok
		default:
			return false
		}
	}
}
