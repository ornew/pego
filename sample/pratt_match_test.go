package sample

import (
	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"testing"
)

func TestPrattStopSelectionStatus(t *testing.T) {
	for _, c := range []struct {
		name, levels, text string
		min                int
		closed, final      bool
		want               status
	}{
		{"longer ineligible", `level { postfix "!?" } level { postfix "!" }`, "!?a", 1, false, true, failed},
		{"incomplete winner", `level { postfix "!?" } level { postfix "!" }`, "!", 1, false, false, more},
		{"complete shorter", `level { postfix "!?" } level { postfix "!" }`, "!", 1, false, true, matched},
		{"postfix first tie", `level { postfix "!" infix none "!" }`, "!a", 0, true, true, matched},
		{"closed infix first tie", `level { infix none "!" postfix "!" }`, "!a", 0, true, true, failed},
		{"part before RHS", `level { infix left "!" } level { postfix "!" }`, "!", 1, false, true, failed},
		{"empty postfix only with trivia", `level { postfix "" }`, " ", 0, false, true, failed},
		{"empty postfix", `level { postfix "" infix left "" }`, "a", 0, false, true, matched},
		{"unsupported part", `level { postfix "!?" -- } level { postfix "!" }`, "!?a", 1, false, true, unknown},
	} {
		t.Run(c.name, func(t *testing.T) {
			ast, err := pego.ParseGrammar(`def main=e $$ def e=pratt { skip " "* operand "a" ` + c.levels + ` }`)
			if err != nil {
				t.Fatal(err)
			}
			in := analyze(ast, "main")
			g := newGen(in, &config{})
			ri := in.rules["e"]
			lastNone := -1
			if c.closed {
				lastNone = 0
			}
			check := g.prattStopCheck(ri, newPrattScope(c.min, lastNone, nil))
			if check == nil {
				t.Fatal("expected a possible continuation")
			}
			m := matcher{in: in}
			st, end := m.runPrattStop(check, []byte(c.text), 0, c.final)
			if c.name == "empty postfix" && end != 1 {
				t.Fatalf("empty infix must reach its operand: end=%d", end)
			}
			if st != c.want {
				t.Fatalf("status=%v, want %v", st, c.want)
			}
		})
	}
}

func TestPrattStopSharedWorkBudget(t *testing.T) {
	op := &grammar.PrattOperator{Kind: grammar.Postfix, Expr: &grammar.Literal{Value: "!"}}
	s := &prattStop{pr: &grammar.Pratt{}}
	for range matchSteps + 1 {
		s.ops = append(s.ops, prattOp{op: op})
	}
	m := matcher{}
	st, _ := m.runPrattStop(&prattCheck{stop: s, scope: newPrattScope(0, -1, nil)}, []byte("?"), 0, true)
	if st != unknown || m.steps != matchSteps+1 {
		t.Fatalf("status=%v, shared steps=%d", st, m.steps)
	}
}
