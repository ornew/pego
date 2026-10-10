package sample_test

import (
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

func TestSamplePrattLongestCallerStops(t *testing.T) {
	for _, c := range []struct{ name, body, rules, input string }{
		{"postfix", `e(tight) "!?" "a"`, `def e=pratt { operand "a" level { postfix "!?" } level tight { postfix "!" } }`, "a!?a"},
		{"infix", `e(tight) "!?" "a"`, `def e=pratt { operand "a" level { infix left "!?" } level tight { infix left "!" } }`, "a!?a"},
		{"mixed postfix winner", `e(tight) "!?" "a"`, `def e=pratt { operand "a" level { postfix "!?" } level tight { infix left "!" } }`, "a!?a"},
		{"mixed infix winner", `e(tight) "!?" "a"`, `def e=pratt { operand "a" level { infix left "!?" } level tight { postfix "!" } }`, "a!?a"},
		{"first declaration tie", `e(tight) "!" "a"`, `def e=pratt { operand "a" level { postfix "!" } level tight { postfix "!" } }`, "a!a"},
		{"closed none winner", `e "<=" "a"`, `def e=pratt { operand "a" / "=a" level { infix left "<" } level { infix none "<=" } }`, "a<=a<=a"},
		{"Unicode skip", `e(tight) " ¬? α"`, `def e=pratt { skip " "* operand "α" level { postfix "¬?" } level tight { postfix "¬" } }`, "α ¬? α"},
		{"empty postfix", `e(tight) "!"`, `def e=pratt { operand "a" level { postfix "" } level tight {} }`, "a!"},
		{"uncertain cut part", `e(tight) "!?" "a"`, `def e=pratt { operand "a" level { postfix "!?" -- } level tight { postfix "!" } }`, "a!?a"},
		{"true postfix continuation", `e(tight)`, `def e=pratt { operand "a" level { postfix "!?" } level tight { postfix "!" } }`, "a!"},
		{"true infix continuation", `e(tight)`, `def e=pratt { operand "a" level { infix left "!?" } level tight { infix left "!" } }`, "a!a"},
		{"infix prefix RHS", `e(tight)`, `def e=pratt { operand "a" level { prefix "~" } level tight { infix left "!" } }`, "a!~a"},
		{"infix without RHS", `e(tight) "!"`, `def e=pratt { operand "a" level { infix left "!?" } level tight { infix left "!" } }`, "a!"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, "def main = &("+strconv.Quote(c.input)+" $$) "+c.body+" $$\n"+c.rules)
			for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					if _, err := p.Parse(c.input, pego.WithUnit(unit), pego.WithBackend(backend)); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, coverage := range []bool{false, true} {
				options := []sample.Option{sample.WithSeed(1), sample.WithMaxRepeat(4), sample.WithBudget(4000), sample.WithAttempts(8)}
				if coverage {
					options = append(options, sample.WithCoverage())
				}
				a, err := sample.New(p, options...)
				if err != nil {
					t.Fatal(err)
				}
				b, err := sample.New(p, options...)
				if err != nil {
					t.Fatal(err)
				}
				x, err := a.Generate(1)
				y, other := b.Generate(1)
				if err != nil || other != nil || !slices.Equal(x, []string{c.input}) || !slices.Equal(x, y) || a.Stats() != b.Stats() || !reflect.DeepEqual(a.Coverage(), b.Coverage()) {
					t.Fatalf("coverage=%t: %q/%v vs %q/%v; stats=%+v", coverage, x, err, y, other, a.Stats())
				}
			}
		})
	}
}
