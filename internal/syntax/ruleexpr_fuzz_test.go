package syntax

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// ruleExprForms are rule bodies with a hole, written S, at each place between
// two tokens of a rule expression where a comment can stand.
var ruleExprForms = []struct{ name, form string }{
	{"after an attribute", `rule r = "a" @custom S "b"`},
	{"after an attribute with arguments", `rule r = "a" @custom(1) S "b"`},
	{"before an attribute", `rule r = "a" S @custom "b"`},
	{"after a pragma", `rule r = "a" #atomic S "b"`},
	{"after a repeat pragma", `rule r = "a" #repeat(2) S "b"`},
	{"after star", `rule r = "a"* S "b"`},
	{"after plus", `rule r = "a"+ S "b"`},
	{"after optional", `rule r = "a"? S "b"`},
	{"before a suffix", `rule r = "a" S * "b"`},
	{"around a choice", `rule r = "a" S / S "b"`},
	{"around an action arrow", `rule r = "a" S => S 1`},
	{"after a label", `rule r = x: S "a" "b"`},
	{"after a lookahead", `rule r = & S "a" "b"`},
	{"after a discard", `rule r = - S "a" "b"`},
	{"in a group", `rule r = ( S "a" S "b" S ) S "c"`},
	{"in a predicate", `rule r = "a" { S true S } "b"`},
	{"after a predicate", `rule r = "a" { true } S "b"`},
	{"around the semicolon of a block", `rule r = "a" { x := 1 S ; S x == 1 } "b"`},
	{"after the brace of a block", `rule r = "a" {S x := 1; x == 1 } "b"`},
	{"around a choice in a group", `rule r = ( "a" S / S "b" ) "c"`},
	{"around a binary operator", `rule r = "a" => (1 S + S 2)`},
	{"in a recover pragma", `rule r = "a" #recover(skip= S "b" S ) "c"`},
	{"before the index bracket", `rule r = "a" => x[ 0 S ]`},
	{"after the index bracket", `rule r = "a" => x[ S 0 ]`},
	{"around a binary operator", `rule r = "a" => (1 S + S 2)`},
	{"in a call", `rule r = "a" => f( S 1 S , S 2 S )`},
	{"in an action group", `rule r = "a" => ( S 1 S )`},
	{"in a recover pragma", `rule r = "a" #recover(skip= S "b" S ) "c"`},
	{"before the brace that closes a block", `rule r = "a" { x := 1; x == 1 S } "b"`},
	{"around the statements of a block", `rule r = "a" { S x := 1 S ; S x == 1 S ; S } "b"`},
}

// ruleExprLineForms are the forms inside delimiters, where comments may also
// stand on lines of their own.
var ruleExprLineForms = []struct{ name, form string }{
	{"in a group", `rule r = ( S "a" S "b" S ) S "c"`},
	{"in a predicate", `rule r = "a" { S true S } "b"`},
	{"around the statements of a block", `rule r = "a" { S x := 1 S ; S x == 1 S } "b"`},
	{"after the brace of a block", `rule r = "a" {S x := 1; x == 1 } "b"`},
	{"in a call", `rule r = "a" => f( S 1 S , S 2 S )`},
	{"around a choice in a group", `rule r = ( "a" S / S "b" ) "c"`},
	{"around a binary operator", `rule r = "a" => (1 S + S 2)`},
	{"in a recover pragma", `rule r = "a" #recover(skip= S "b" S ) "c"`},
	{"before the index bracket", `rule r = "a" => x[ 0 S ]`},
	{"after the index bracket", `rule r = "a" => x[ S 0 ]`},
}

// TestFormatRandomRuleExpr formats rule bodies with comments between the
// tokens of their expressions, and checks that formatting parses to the same
// grammar with every comment owned by the same node.
func TestFormatRandomRuleExpr(t *testing.T) {
	const seeds = 600
	for _, f := range ruleExprForms {
		t.Run(f.name, func(t *testing.T) {
			failures, parsed := 0, 0
			for seed := uint64(0); seed < seeds && failures < 2; seed++ {
				g := &listGen{r: rand.New(rand.NewPCG(seed, 23)), inline: true}
				src := expandHoles(f.form, g) + "\n"
				ast, err := Parse(src)
				if err != nil {
					continue
				}
				parsed++
				if problem := checkRandomFormat(src, ast); problem != "" {
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

// TestFormatRandomRuleExprLines is TestFormatRandomRuleExpr with comments on
// lines of their own inside delimiters.
func TestFormatRandomRuleExprLines(t *testing.T) {
	testRuleExprLines(t, ruleExprLineForms, true)
}

func testRuleExprLines(t *testing.T, forms []struct{ name, form string }, owners bool) {
	const seeds = 600
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			failures, parsed := 0, 0
			for seed := uint64(0); seed < seeds && failures < 2; seed++ {
				g := &listGen{r: rand.New(rand.NewPCG(seed, 29))}
				src := expandHoles(f.form, g) + "\n"
				ast, err := Parse(src)
				if err != nil {
					continue
				}
				parsed++
				if problem := checkRandomFormatOwners(src, ast, owners); problem != "" {
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

// expandHoles replaces each hole S, a capital S between spaces or at the edge
// of a bracket, with a random slot.
func expandHoles(form string, g *listGen) string {
	var b strings.Builder
	for i := 0; i < len(form); i++ {
		if form[i] == 'S' && (i == 0 || form[i-1] == ' ' || form[i-1] == '{' || form[i-1] == '=') && (i+1 == len(form) || form[i+1] == ' ' || form[i+1] == ';') {
			b.WriteString(g.slot())
			continue
		}
		b.WriteByte(form[i])
	}
	return b.String()
}
