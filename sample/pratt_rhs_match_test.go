package sample

import (
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func rhsCheck(t *testing.T, rules string) (*info, *prattCheck) {
	t.Helper()
	ast, err := pego.ParseGrammar("def main=e $$\n" + rules)
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	g := newGen(in, &config{})
	return in, g.prattStopCheck(in.rules["e"], newPrattScope(0, -1, nil))
}

func TestPrattRHSStatus(t *testing.T) {
	simple := `def e=pratt { operand "a" level { prefix "~" infix left "!" } }`
	for _, c := range []struct {
		name, rules, text string
		final             bool
		want              status
		end               int
	}{
		{"incomplete final", simple, "!~", true, failed, 0},
		{"incomplete partial", simple, "!~", false, more, 0},
		{"complete final", simple, "!~a", true, matched, 3},
		{"stable partial", simple, "!~a", false, matched, 3},
		{"nested complete", simple, "!~~a", true, matched, 4},
		{"prefix own bound", `def e=pratt { operand "a" level { prefix "~" } level { infix left "+" } level { infix left "!" } }`, "!~a+a", true, matched, 5},
		{"nested none returns", `def e=pratt { operand "a" level { infix left "!" } level { infix none "<" } level { infix none "=" } }`, "!a<a=a=a", true, matched, 8},
		{"right none frames", `def e=pratt { operand "a" level { infix left "!" } level { infix right "^" infix none "=" } }`, "!a^a=a=a", true, matched, 8},
		{"postfix resets none", `def e=pratt { operand "a" level { infix left "!" } level { infix none "=" postfix "#" } }`, "!a=a#=a", true, matched, 7},
		{"prefix fallback", `def e=pratt { operand "a" / "~" level { prefix "~" infix left "!" } }`, "!~", true, matched, 2},
		{"longest failed no shorter", `def e=pratt { operand "a" / "~x" level { prefix "~" prefix "~~" infix left "!" } }`, "!~~x", true, failed, 0},
		{"longer prefix incomplete", `def e=pratt { operand "a" level { prefix "~" prefix "~~" infix left "!" } }`, "!~", false, more, 0},
		{"nullable prefix consumes", `def e=pratt { operand "a" level { prefix "~"? infix left "!" } }`, "!~", true, failed, 0},
		{"empty prefix excluded", `def e=pratt { operand "a" level { prefix "" infix left "!" } }`, "!a", true, matched, 2},
		{"empty postfix trivia rollback", `def e=pratt { skip " "* operand "a" level { infix left "!" postfix "" } }`, "!a ", true, matched, 2},
		{"nud skip retained on fallback", `def e=pratt { skip " "* operand "a" / "~" level { prefix "~" infix left "!" } }`, "! ~", true, matched, 3},
		{"tail skip rollback", `def e=pratt { skip " "* operand "a" level { infix left "!" } level { infix left "*" } }`, "!a *x", true, matched, 2},
		{"empty infix nullable progress", `def e=pratt { skip " "* operand "a" / "" level { infix left "!" } level { infix left "" } }`, "! a ", true, matched, 4},
		{"later cut unknown", `def e=pratt { operand "a" level { infix left "!" } level { infix left ("*" -- "b") } }`, "!a*x", true, unknown, 0},
		{"ineligible cut candidate unknown", `def e=pratt { operand "a" level { infix left "!" infix left ("*" -- "b") } }`, "!a*x", true, unknown, 0},
		{"unsafe future tail partial", `def e=pratt { operand "a" level { infix left "!" } level { infix left ("*" -- "b") } }`, "!a", false, more, 0},
		{"unsafe future tail final", `def e=pratt { operand "a" level { infix left "!" } level { infix left ("*" -- "b") } }`, "!a", true, matched, 2},
		{"prefix cut unknown", `def e=pratt { operand "a" level { prefix ("~" --) infix left "!" } }`, "!~", true, unknown, 0},
		{"operand predicate unknown", `def e=pratt { operand "a" [true] level { infix left "!" } }`, "!a", true, unknown, 0},
		{"explicit Pratt ref unknown", `def e=pratt { operand inner level { infix left "!" } } def inner=pratt { operand "a" }`, "!a", true, unknown, 0},
		{"left recursion unknown", `def e=pratt { operand atom level { infix left "!" } } def atom=atom "a" / "a"`, "!a", true, unknown, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			in, check := rhsCheck(t, c.rules)
			m := matcher{in: in}
			st, end := m.runPrattStop(check, []byte(c.text), 0, c.final)
			if st != c.want || end != c.end {
				t.Fatalf("status%v end%d; want%v end%d", st, end, c.want, c.end)
			}
			if len(m.active) != 0 {
				t.Fatal("active references leaked")
			}
		})
	}
}

func TestPrattRHSRecursiveWorkBudget(t *testing.T) {
	for _, c := range []struct{ name, rules, text string }{
		{"prefix", `def e=pratt { operand "a" level { prefix "~" infix left "!" } }`, "!" + strings.Repeat("~", matchSteps) + "a"},
		{"right", `def e=pratt { operand "a" level { infix left "!" } level { infix right "^" } }`, "!" + strings.Repeat("a^", matchSteps) + "a"},
		{"empty right", `def e=pratt { operand "" level { infix left "!" } level { infix right "" } }`, "!"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in, check := rhsCheck(t, c.rules)
			m := matcher{in: in}
			st, _ := m.runPrattStop(check, []byte(c.text), 0, true)
			if st != unknown || m.steps != matchSteps+1 {
				t.Fatalf("status%v steps%d", st, m.steps)
			}
		})
	}
	in, check := rhsCheck(t, `def e=pratt { operand "a" level { prefix "~" infix left "!" } }`)
	m := matcher{in: in, text: []byte("~~a"), final: true, steps: matchSteps - 2}
	st, _ := m.prattExpr(check.stop, 0, 0)
	if st != unknown || m.steps != matchSteps+1 {
		t.Fatalf("RHS reset shared budget: status%v steps%d", st, m.steps)
	}
}

func TestPrattPrefixDeclarationTie(t *testing.T) {
	in, check := rhsCheck(t, `def e=pratt { operand "a" level { prefix "~" infix left "!" } level { prefix "~" } }`)
	m := matcher{in: in, text: []byte("~a"), final: true}
	st, op, end := m.prattLongest(check.stop.prefixes, 0)
	if st != matched || op != &check.stop.prefixes[0] || end != 1 {
		t.Fatalf("prefix tie: status%v op%v end%d", st, op, end)
	}
}

func TestPrattStableNestedWitness(t *testing.T) {
	in, check := rhsCheck(t, `def e=pratt { operand "a" level { prefix "~" infix left "!" } }`)
	m := matcher{in: in, text: []byte("~~a"), final: false}
	st, end := m.prattExpr(check.stop, 0, 0)
	if st != prattStableSuccess || end != 3 {
		t.Fatalf("nested partial witness: status%v end%d", st, end)
	}
}
