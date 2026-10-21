package syntax

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// jsonRoundTripForms hold comments in the places where a declaration, a field or
// a line of an operators rule can own them. S marks a place.
var jsonRoundTripForms = []struct{ name, form string }{
	{"declarations", "S\npackage p S\nS\ntype T = { S\n    A int S\n    S B string S\n} S\nS\nrule main = a:\"a\" => T{A: 1, B: \"x\"} S\nS\nrule other = \"x\" S\n"},
	{"operators rule", "S\npackage p S\nS\nrule ops = operators { S\n    S skip \" \" S\n    S operand \"n\" S\n    S operand \"m\" => 1 S\n    S level { S\n        S infix left \"+\" S\n        S prefix \"-\" S\n    } S\n    S level L { S\n        infix right \"*\" S\n    } S\n} S\n"},
	{"types and variables", "S\npackage p S\nS\ntype A ~ int S\nS\ntype B = { S x int S ; S y string S } S\nS\nvar v = 1 S\nS\nrule r = \"a\" S\n"},
}

// expandLineHoles replaces each S that stands alone between spaces or line
// breaks with a random comment slot.
func expandLineHoles(form string, g *listGen) string {
	var b strings.Builder
	for i := 0; i < len(form); i++ {
		if form[i] == 'S' && (i == 0 || form[i-1] == ' ' || form[i-1] == '\n') && (i+1 == len(form) || form[i+1] == ' ' || form[i+1] == '\n') {
			b.WriteString(g.slot())
			continue
		}
		b.WriteByte(form[i])
	}
	return b.String()
}

// jsonRoundTripProblem formats the grammar that its JSON form decodes to: the
// result must parse to the same JSON, with every comment owned by the same
// node, and formatting it again must change nothing.
func jsonRoundTripProblem(ast *grammar.Grammar) string {
	data, err := grammar.MarshalJSON(ast)
	if err != nil {
		return "marshal: " + err.Error()
	}
	decoded, err := grammar.UnmarshalJSON(data)
	if err != nil {
		return "unmarshal: " + err.Error()
	}
	out := grammar.Format(decoded)
	again, err := Parse(out)
	if err != nil {
		return fmt.Sprintf("the result does not parse: %v\n%s", err, out)
	}
	if a, b := jsonOf(ast), jsonOf(again); a != b {
		return "the result is a different grammar\n" + out
	}
	if diffs := ownerDiffs(ast, again); len(diffs) > 0 {
		return "the owners changed\n" + strings.Join(diffs, "\n") + "\n" + out
	}
	if second := grammar.Format(again); second != out {
		return "formatting is not idempotent\nfirst:\n" + out + "\nsecond:\n" + second
	}
	return ""
}

// TestFormatRandomJSONRoundTrip checks that comments survive JSON: the owned
// comments a decoded grammar carries are written back to the same owners.
func TestFormatRandomJSONRoundTrip(t *testing.T) {
	const seeds = 800
	for _, f := range jsonRoundTripForms {
		t.Run(f.name, func(t *testing.T) {
			failures, parsed := 0, 0
			for seed := uint64(0); seed < seeds && failures < 2; seed++ {
				g := &listGen{r: rand.New(rand.NewPCG(seed, 77))}
				src := expandLineHoles(f.form, g)
				ast, err := Parse(src)
				if err != nil {
					continue
				}
				parsed++
				if problem := jsonRoundTripProblem(ast); problem != "" {
					failures++
					t.Errorf("seed %d\nsource:\n%s\n%s", seed, src, problem)
				}
			}
			if failures == 0 && parsed < seeds/10 {
				t.Errorf("only %d of %d random sources parse", parsed, seeds)
			}
		})
	}
}
