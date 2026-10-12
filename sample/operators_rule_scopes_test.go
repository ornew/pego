package sample_test

import (
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

const scopeNoneLevels = `rule e = operators {
    operand "a"
    level { infix none "<" }
    level { infix none "=" }
}`

func TestSampleOperatorsRuleRHSScope(t *testing.T) {
	for _, c := range []struct {
		name, body, rules, input string
		valid                    bool
	}{
		{"nested then enclosing", "e", scopeNoneLevels, "a<a=a=a", true},
		{"short control", "e", scopeNoneLevels, "a<a=a", true},
		{"three levels", "e", scopeNoneLevels[:len(scopeNoneLevels)-1] + ` level { infix none ":" } }`, "a<a=a:a:a", true},
		{"prefix RHS", "e", `rule e = operators {
    operand "a"
    level { prefix "~"; infix none "<" }
    level { infix none "=" }
}`, "~a=a=a", true},
		{"nested prefixes", "e", `rule e = operators {
    operand "a"
    level { prefix "~" }
    level { prefix "@" }
    level { infix none ":" }
}`, "~@a:a:a:a", true},
		{"restricted prefix", "level(e, tight)", `rule e = operators {
    operand "a"
    level { prefix "~"; infix none "<" }
    level tight { infix none "=" }
}`, "~a=a=a", true},
		{"explicit inner call", "e", `rule e = operators {
    operand "(" inner ")" / "a"
    level { infix none "<" }
    level { infix none "=" }
}
rule inner = e`, "a<a=(a=a)", true},
		{"right RHS same level", "e", `rule e = operators {
    operand "a"
    level { infix right "^"; infix none "=" }
}`, "a^a=a=a", true},
		{"left resets same frame", "e", `rule e = operators {
    operand "a"
    level { infix none "="; infix left "+" }
}`, "a=a+a=a", true},
		{"right resets same frame", "e", `rule e = operators {
    operand "a"
    level { infix none "="; infix right "+" }
}`, "a=a+a=a", true},
		{"loose reset", "e", `rule e = operators {
    operand "a"
    level { infix left "|" }
    level { infix none "<" }
    level { infix none "=" }
}`, "a<a=a|a<a=a", true},
		{"inner postfix preserves parent", "e", `rule e = operators {
    operand "a"
    level { infix none "<" }
    level { infix none "="; postfix "!" }
}`, "a<a=a!<a", false},
		{"postfix reset", "e", `rule e = operators {
    operand "a"
    level { infix none "<" }
    level { infix none "="; postfix "!" }
}`, "a<a=a=a!=a", true},
		{"caller boundary", `e "=" e`, scopeNoneLevels, "a=a=a", true},
		{"Unicode trivia", "e", `rule e = operators {
    skip " "*
    operand "α"
    level { infix none "<" }
    level { infix none "=" }
}`, " α < α = α = α", true},
		{"past64 levels", "e", `rule e = operators { operand "a"; ` + strings.Repeat("level {}; ", 69) + `level { infix none "<" }; level { infix none "=" } }`, "a<a=a=a", true},
		{"nested repeated root none", "e", scopeNoneLevels, "a<a=a=a=a", false},
		{"standalone none", "e", scopeNoneLevels, "a=a=a", false},
		{"three level repeated", "e", scopeNoneLevels[:len(scopeNoneLevels)-1] + ` level { infix none ":" } }`, "a:a:a", false},
		{"prefix repeated outer", "e", `rule e = operators {
    operand "a"
    level { prefix "~" }
    level { infix none "=" }
}`, "~a=a=a=a", false},
		{"invalid explicit inner", "e", `rule e = operators {
    operand "(" inner ")" / "a"
    level { infix none "<" }
    level { infix none "=" }
}
rule inner = e`, "a<a=(a=a=a)", false},
		{"caller repeated", "e \"=\" e", scopeNoneLevels, "a=a=a=a=a", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, "rule main = &("+strconv.Quote(c.input)+" $$) "+c.body+" $$\n"+c.rules)
			for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					_, err := p.Parse(c.input, pego.WithUnit(unit), pego.WithBackend(backend))
					if (err == nil) != c.valid {
						t.Fatalf("unit%v backend%v: runtime=%v expectedvalid%t", unit, backend, err, c.valid)
					}
				}
			}
			for _, coverage := range []bool{false, true} {
				opts := []sample.Option{sample.WithSeed(1), sample.WithMaxRepeat(8), sample.WithBudget(10000), sample.WithAttempts(8), sample.WithMaxDepth(2), sample.WithMaxLen(64)}
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
				if !reflect.DeepEqual(x, y) || !errors.Is(err, other) || a.Stats() != b.Stats() || !reflect.DeepEqual(a.Coverage(), b.Coverage()) {
					t.Fatalf("seed diverged: %q/%v %q/%v", x, err, y, other)
				}
				if c.valid {
					if err != nil || !slices.Equal(x, []string{c.input}) {
						t.Fatalf("coverage%t: %q %v %+v", coverage, x, err, a.Stats())
					}
				} else if !errors.Is(err, sample.ErrNoInput) || a.Stats().Candidates != 0 || a.Stats().Failed != 8 {
					t.Fatalf("invalid chain: %v %+v", err, a.Stats())
				}
			}
		})
	}
}

func TestSampleOperatorsRuleScopesLimits(t *testing.T) {
	p := compile(t, `rule main = &("a<a=a=a" $$) e $$`+"\n"+scopeNoneLevels)
	for _, opts := range [][]sample.Option{
		{sample.WithBudget(1)}, {sample.WithMaxRepeat(2)},
	} {
		opts = append(opts, sample.WithAttempts(2))
		g, err := sample.New(p, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Next(); !errors.Is(err, sample.ErrNoInput) || g.Stats().Failed != 2 {
			t.Fatalf("bounded scope search: %v %+v", err, g.Stats())
		}
	}
	g, err := sample.New(p, sample.WithSeed(1), sample.WithMaxRepeat(4), sample.WithMaxDepth(0), sample.WithMaxLen(0), sample.WithBudget(4000), sample.WithAttempts(8))
	if err != nil {
		t.Fatal(err)
	}
	if x, err := g.Generate(1); err != nil || !slices.Equal(x, []string{"a<a=a=a"}) {
		t.Fatalf("soft limits: %q %v", x, err)
	}
}
