package sample_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

const sampleRHSLevels = `rule e = #operators {
    operand "a"
    level { prefix "~"; infix left "!" }
}`

func TestSampleOperatorsRuleIncompleteRHS(t *testing.T) {
	for _, c := range []struct {
		name, body, rules, input string
		valid, pruned            bool
	}{
		{"caller incomplete prefix", `e "!~"`, sampleRHSLevels, "a!~", true, false},
		{"nested incomplete prefix", `e "!~~"`, sampleRHSLevels, "a!~~", true, false},
		{"complete prefix", "e", sampleRHSLevels, "a!~a", true, false},
		{"nested complete prefix", "e", sampleRHSLevels, "a!~~a", true, false},
		{"fallback operand", "e", `rule e = #operators {
    operand "a" / "~"
    level { prefix "~"; infix left "!" }
}`, "a!~", true, false},
		{"longest prefix", "e", `rule e = #operators {
    operand "a"
    level { prefix "~"; prefix "~~"; infix left "!" }
}`, "a!~~a", true, false},
		{"longest failed prefix", `e "!~~x"`, `rule e = #operators {
    operand "a" / "~x"
    level { prefix "~"; prefix "~~"; infix left "!" }
}`, "a!~~x", true, false},
		{"named entry incomplete", `level(e, tight) "!~"`, `rule e = #operators {
    operand "a"
    level { prefix "~" }
    level tight { infix left "!" }
}`, "a!~", true, false},
		{"named entry complete", "level(e, tight)", `rule e = #operators {
    operand "a"
    level { prefix "~" }
    level tight { infix left "!" }
}`, "a!~a", true, false},
		{"Unicode trivia", "e", `rule e = #operators {
    skip " "*
    operand "α"
    level { prefix "¬"; infix left "!" }
}`, "α ! ¬ α", true, false},
		{"nullable operand", "e", `rule e = #operators {
    operand "a" / ""
    level { prefix "~"; infix left "!" }
}`, "a!~", true, false},
		{"consuming nullable prefix", `e "!~"`, `rule e = #operators {
    operand "a"
    level { prefix "~"?; infix left "!" }
}`, "a!~", true, false},
		{"empty infix", "e", `rule e = #operators {
    operand ""
    level { infix left "" }
}`, "", true, false},
		{"later tighter cut", `e "!a*x"`, `rule e = #operators {
    operand "a"
    level { infix left "!" }
    level { infix left "*" #cut "b" }
}`, "a!a*x", true, false},
		{"cut first tie caller", `e "!~"`, `rule e = #operators {
    operand "a" / "~"
    level { prefix "~" #cut; prefix "~"; infix left "!" }
}`, "a!~", true, false},
		{"plain first tie fallback", "e", `rule e = #operators {
    operand "a" / "~"
    level { prefix "~"; prefix "~" #cut; infix left "!" }
}`, "a!~", true, false},
		{"outer cut cannot stop", `e "!~"`, `rule e = #operators {
    operand "a"
    level { prefix "~"; infix left "!" #cut }
}`, "a!~", false, false},
		{"complete RHS cannot stop", `e "!~a"`, sampleRHSLevels, "a!~a", false, true},
		{"nullable complete cannot stop", `e "!~"`, `rule e = #operators {
    operand "a" / ""
    level { prefix "~"; infix left "!" }
}`, "a!~", false, true},
		{"cut first tie rejects fallback", "e", `rule e = #operators {
    operand "a" / "~"
    level { prefix "~" #cut; prefix "~"; infix left "!" }
}`, "a!~", false, false},
		{"plain first tie consumes RHS", `e "!~"`, `rule e = #operators {
    operand "a" / "~"
    level { prefix "~"; prefix "~" #cut; infix left "!" }
}`, "a!~", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, "rule main = &("+strconv.Quote(c.input)+" $$) "+c.body+" $$\n"+c.rules)
			for _, unit := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					_, err := p.Parse(c.input, pego.WithUnit(unit), pego.WithBackend(backend))
					if (err == nil) != c.valid {
						t.Fatalf("unit%v backend%v: %v expectedvalid%t", unit, backend, err, c.valid)
					}
				}
			}
			for _, coverage := range []bool{false, true} {
				t.Run(strconv.FormatBool(coverage), func(t *testing.T) {
					opts := []sample.Option{sample.WithSeed(1), sample.WithMaxRepeat(4), sample.WithBudget(4000), sample.WithAttempts(8)}
					if coverage {
						opts = append(opts, sample.WithCoverage())
					}
					a, err := sample.New(p, opts...)
					if err != nil {
						t.Fatal(err)
					}
					b, err := sample.New(p, opts...)
					if err != nil {
						t.Fatal(err)
					}
					x, err := a.Generate(1)
					y, other := b.Generate(1)
					if !reflect.DeepEqual(x, y) || fmt.Sprint(err) != fmt.Sprint(other) || a.Stats() != b.Stats() || !reflect.DeepEqual(a.Coverage(), b.Coverage()) {
						t.Fatalf("seed diverged: %q/%v %q/%v", x, err, y, other)
					}
					if c.valid {
						if err != nil || !slices.Equal(x, []string{c.input}) {
							t.Fatalf("%q %v %+v", x, err, a.Stats())
						}
					} else {
						if !errors.Is(err, sample.ErrNoInput) {
							t.Fatalf("invalid caller succeeded: %q %v", x, err)
						}
						if c.pruned && (a.Stats().Candidates != 0 || a.Stats().Failed != 8) {
							t.Fatalf("true continuation was not pruned: %+v", a.Stats())
						}
					}
				})
			}
		})
	}
}
