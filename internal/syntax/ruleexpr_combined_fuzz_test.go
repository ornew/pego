package syntax

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// ruleExprCombinedForms combine the kinds of tokens that a rule expression
// has, with a comment place between any two of them.
var ruleExprCombinedForms = []struct{ name, form string }{
	{"label then suffix", `rule r = x: S "a" S * S "b"`},
	{"not lookahead", `rule r = ! S "a" S "b"`},
	{"discard group", `rule r = - S ( S "a" S ) S "b"`},
	{"attribute arguments", `rule r = "a" @custom( S 1 S ) S "b"`},
	{"two attributes", `rule r = "a" S @x S @y S "b"`},
	{"pragma then suffix", `rule r = ( "a" ) S #atomic S + S "b"`},
	{"choice", `rule r = "a" S / S "b" S / S "c"`},
	{"nested group", `rule r = ( S ( S "a" S ) S ) S`},
	{"predicate after label", `rule r = x: S "a" S { S x == "a" S } S "b"`},
	{"statements of a block", `rule r = "a" { S x := 1 S ; S y := 2 S ; S x == y S } S "b"`},
	{"lambda", `rule r = "a" => ( S x S ) S => S x S + S 1`},
	{"binary operators", `rule r = "a" => 1 S + S 2 S * S 3`},
	{"unary operators", `rule r = "a" => - S 1 S + S ! S true`},
	{"repeat pragma", `rule r = "a" S #repeat( S 2 S , S 3 S ) S "b"`},
	{"after equals", `rule r = S "a" S "b"`},
	{"label, suffix and action", `rule r = x: S "a" S * S => S x`},
}

// TestFormatRandomRuleExprCombined formats the forms of ruleExprCombinedForms
// with block comments, and inside a group with comments of every kind, and
// checks the grammar, the comments, their owners and the stability.
func TestFormatRandomRuleExprCombined(t *testing.T) {
	const seeds = 400
	for _, grouped := range []bool{false, true} {
		for _, inline := range []bool{true, false} {
			if !grouped && !inline {
				continue // a rule body outside delimiters stays on one line
			}
			for _, f := range ruleExprCombinedForms {
				form := f.form
				name := f.name
				if grouped {
					form = "rule r = ( " + strings.TrimPrefix(form, "rule r = ") + " )"
					name += " in a group"
				}
				if !inline {
					name += " with lines"
				}
				t.Run(name, func(t *testing.T) {
					failures, parsed := 0, 0
					for seed := uint64(0); seed < seeds && failures < 2; seed++ {
						g := &listGen{r: rand.New(rand.NewPCG(seed, 23)), inline: inline}
						src := expandHoles(form, g) + "\n"
						ast, err := Parse(src)
						if err != nil {
							continue
						}
						parsed++
						if problem := checkRandomFormatOwners(src, ast, true); problem != "" {
							failures++
							t.Errorf("seed %d\nsource:\n%s\n%s", seed, src, problem)
						}
					}
					if failures == 0 && parsed < 5 {
						t.Errorf("only %d of %d random sources parse", parsed, seeds)
					}
				})
			}
		}
	}
}
