package sample_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

const samplePrefixLevels = `def e = pratt {
    operand "x" / "é"
    level loose { prefix "~" infix left "|" postfix "?" }
    level middle { prefix "-" infix left "+" postfix "!" }
    level tight { prefix "@" infix left "*" }
    level power { prefix "%" infix right "^" }
}`

// Requiring each text with a lookahead distinguishes a reachable derivation from a lucky random
// sample. Fresh parses on every backend and unit validate both the oracle and generated input.
func TestSamplePrattPrefixBindings(t *testing.T) {
	cases := []struct{ name, body, tail, input string }{
		{"lower", "e(tight)", samplePrefixLevels, "~x"},
		{"equal", "e(tight)", samplePrefixLevels, "@x*x"},
		{"higher", "e(tight)", samplePrefixLevels, "%x*x"},
		{"intermediate infix", "e(tight)", samplePrefixLevels, "~é+x"},
		{"intermediate postfix", "e(tight)", samplePrefixLevels, "~é!"},
		{"equal prefix postfix", "e(middle)", samplePrefixLevels, "-é!"},
		{"loose then tight", "e(tight)", samplePrefixLevels, "~@x+x"},
		{"tight then loose", "e(tight)", samplePrefixLevels, "@~x+x"},
		{"repeated", "e(tight)", samplePrefixLevels, "~~x+x"},
		{"left RHS", "e(tight)", samplePrefixLevels, "x*~x+x"},
		{"right RHS", "e(tight)", samplePrefixLevels, "x^~x+x"},
		{"unrestricted", "e", samplePrefixLevels, "~x+x|x"},
		{"caller lower tail", `e(tight) "|" e(tight)`, samplePrefixLevels, "~x+x|x"},
		{"caller after inner tail", `e(tight) ";"`, samplePrefixLevels, "~x+x;"},
		{"explicit inner entry", "e(tight)", `def e = pratt {
    operand "(" e(tight) ")" / "x"
    level { prefix "~" infix left "|" }
    level { infix left "*" }
    level tight { }
}`, "~(~x*x)*x"},
		{"longest prefix", "e(tight)", `def e = pratt {
    operand "x"
    level { prefix "-" prefix "--" }
    level { infix left "*" }
    level tight { }
}`, "--x*x"},
		{"nullable consuming prefix", "e(tight)", `def e = pratt {
    operand "x"
    level { prefix "~"? }
    level { infix left "*" }
    level tight { }
}`, "~x*x"},
		{"Unicode prefix with skip", "e(tight)", `def e = pratt {
    skip " "*
    operand "é"
    level { prefix "¬" }
    level { infix left "*" }
    level tight { }
}`, " ¬ é * é"},
		{"new none scope", "e", `def e = pratt {
    operand "x"
    level { prefix "~" }
    level { infix none "=" }
    level tight { prefix "@" }
}`, "x=~x=x"},
		{"nested none scopes", "e(tight)", `def e = pratt {
    operand "x"
    level { prefix "~" }
    level { infix none "=" }
    level tight { prefix "@" }
}`, "~x=~x=x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, "def main = &("+strconv.Quote(c.input)+" $$) "+c.body+" $$\n"+c.tail)
			for _, coverage := range []bool{false, true} {
				opts := []sample.Option{sample.WithSeed(11), sample.WithMaxRepeat(4), sample.WithMaxDepth(2), sample.WithMaxLen(64), sample.WithBudget(4000), sample.WithAttempts(20)}
				if coverage {
					opts = append(opts, sample.WithCoverage())
				}
				g, err := sample.New(p, opts...)
				if err != nil {
					t.Fatal(err)
				}
				got, err := g.Next()
				if err != nil || got != c.input {
					t.Fatalf("coverage=%t: Next = %q, %v; want %q", coverage, got, err, c.input)
				}
			}
			for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					if _, err := p.Parse(c.input, pego.WithUnit(unit), pego.WithBackend(backend)); err != nil {
						t.Errorf("unit=%v backend=%v: %v", unit, backend, err)
					}
				}
			}
		})
	}
	for _, input := range []string{"x+x", "~x|x", "~x?", "-x+x", "x|x"} {
		p := compile(t, "def main = e(tight) $$\n"+samplePrefixLevels)
		if _, err := p.Parse(input); err == nil {
			t.Errorf("restricted entry unexpectedly accepts %q", input)
		}
	}
	p := compile(t, `def main = &("x=@x=x" $$) e $$
def e = pratt { operand "x" level { prefix "~" } level { infix none "=" } level { prefix "@" } }`)
	if _, err := p.Parse("x=@x=x"); err == nil {
		t.Fatal("a tighter prefix erased an outer nonassociative restriction")
	}
	g, err := sample.New(p, sample.WithAttempts(2), sample.WithBudget(500))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Next(); !errors.Is(err, sample.ErrNoInput) {
		t.Fatalf("invalid nonassociative chain: %v", err)
	}
}

func TestSamplePrattPrefixCoverage(t *testing.T) {
	// The lowest level's tails remain blocked, but the prefix and its RHS-only middle tails,
	// nested alternatives and callees are reachable. A blocked unrestricted self-call must not
	// resurrect its own operator or helper targets.
	p := compile(t, `def main = e(tight) $$
def e = pratt {
    operand atom
    level { prefix prefixPart infix left (blocked / "|" e) postfix "?" }
    level { infix left middle postfix "!" }
    level tight { }
}
def atom = "x"
def prefixPart = "~"
def middle = "+" / "*"
def blocked = "|" / "/"`)
	g, err := sample.New(p, sample.WithSeed(0), sample.WithCoverage())
	if err != nil {
		t.Fatal(err)
	}
	c := g.Coverage()
	if c.Rules != 5 || c.Alternatives != 6 || !slices.Equal(c.Unreachable, []string{"blocked"}) || len(c.Impossible) != 0 {
		t.Fatalf("before generation: %+v", c)
	}
	for _, a := range c.MissedAlternatives {
		if a.Rule == "blocked" || a.Kind == "operator" && (a.Index == 1 || a.Index == 2) || a.Kind == "choice" && strings.Contains(a.Expr, "blocked") {
			t.Errorf("blocked target counted: %+v", a)
		}
	}
	inputs, err := g.Generate(30)
	if err != nil {
		t.Fatal(err)
	}
	checkParse(t, p, inputs)
	if c = g.Coverage(); c.RulesCovered != c.Rules || c.AlternativesCovered != c.Alternatives {
		t.Fatalf("after generation: %s", c.Report())
	}
	// Reachable explicit calls with a looser entry widen the target set, even through a cycle.
	p = compile(t, `def main = e(tight) $$
def e = pratt { operand "x" / "(" helper ")" level { infix left "+" } level tight { infix left "*" } }
def helper = e / "y"`)
	g, err = sample.New(p)
	if err != nil {
		t.Fatal(err)
	}
	if c = g.Coverage(); c.Rules != 3 || c.Alternatives != 7 || len(c.Unreachable) != 0 {
		t.Fatalf("widened entry: %+v", c)
	}
}

func TestSamplePrattPrefixBoundsAndDeterminism(t *testing.T) {
	p := compile(t, "def main = e(tight) $$\n"+samplePrefixLevels)
	makeGen := func() *sample.Generator {
		t.Helper()
		g, err := sample.New(p, sample.WithSeed(17), sample.WithCoverage(), sample.WithMaxRepeat(2), sample.WithMaxDepth(0), sample.WithMaxLen(12), sample.WithBudget(500))
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	a, b := makeGen(), makeGen()
	for range 40 {
		x, err := a.Next()
		y, other := b.Next()
		if x != y || !errors.Is(err, other) || !reflect.DeepEqual(a.Coverage(), b.Coverage()) || a.Stats() != b.Stats() {
			t.Fatalf("same seed diverged: %q/%v versus %q/%v", x, err, y, other)
		}
		if err != nil {
			t.Fatal(err)
		}
		// There are at most two tail operators and two prefixes per primary, including RHSs.
		prefixes, tails := 0, 0
		for _, ch := range x {
			if strings.ContainsRune("~-@%", ch) {
				prefixes++
			} else if strings.ContainsRune("|?+!*^", ch) {
				tails++
			}
		}
		if tails > 2 || prefixes > 2*(tails+1) {
			t.Fatalf("operator limit exceeded: %q", x)
		}
		checkParse(t, p, []string{x})
	}
	for _, c := range []struct {
		name string
		opts []sample.Option
	}{
		{"repeat", []sample.Option{sample.WithMaxRepeat(0)}},
		{"budget", []sample.Option{sample.WithBudget(1)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, `def main = &"~x" e(tight) $$`+"\n"+samplePrefixLevels)
			g, err := sample.New(p, append(c.opts, sample.WithAttempts(2))...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.Next(); !errors.Is(err, sample.ErrNoInput) || g.Stats().Failed != 2 {
				t.Fatalf("bounded attempt: %v, %+v", err, g.Stats())
			}
		})
	}
	// Explicit nested Pratt calls retain the recursion limit independently of flat prefix tails.
	input := strings.Repeat("(", 20) + "~x*x" + strings.Repeat(")", 20)
	p = compile(t, "def main = &("+strconv.Quote(input)+" $$) e(tight) $$\n"+`def e = pratt {
    operand "(" e(tight) ")" / "x"
    level { prefix "~" }
    level { infix left "*" }
    level tight { }
}`)
	for _, depth := range []int{0, 32} {
		g, err := sample.New(p, sample.WithMaxDepth(depth), sample.WithMaxLen(0), sample.WithMaxRepeat(1), sample.WithAttempts(2))
		if err != nil {
			t.Fatal(err)
		}
		got, err := g.Next()
		if depth == 0 {
			if !errors.Is(err, sample.ErrNoInput) {
				t.Fatalf("hard depth limit ignored: %q, %v", got, err)
			}
		} else if err != nil || got != input {
			t.Fatalf("necessary prefixes beyond soft bounds: %q, %v", got, err)
		} else {
			checkParse(t, p, []string{got})
		}
	}
}

const sampleNoneLevels = `def e = pratt {
    operand "a"
    level { prefix "~" infix left "|" }
    level compare { infix none "<" postfix "!" }
    level { prefix "@" infix left "+" }
}`

func TestSamplePrattNoneCallerStops(t *testing.T) {
	const simple = `def e = pratt { operand "a" level { infix none "<" } }`
	cases := []struct{ name, body, rules, input string }{
		{"caller none", `e "<" e`, simple, "a<a<a"},
		{"punctuation", `e ";"`, simple, "a<a;"},
		{"restricted caller", `e(compare) "<" e(compare)`, sampleNoneLevels, "a<a<a"},
		{"tighter tail preserves none", `e "<" e`, sampleNoneLevels, "a<a+a<a"},
		{"looser tail resets none", "e", sampleNoneLevels, "a<a|a<a"},
		{"same-level postfix resets none", "e", sampleNoneLevels, "a<a!<a"},
		{"tighter prefix preserves none", `e "<" e`, sampleNoneLevels, "a<@a<a"},
		{"loose prefix inner none", `e "<" e`, sampleNoneLevels, "a<~a<a<a"},
		{"fresh caller scope", `e "<" e`, sampleNoneLevels, "a<a<a<a"},
		{"same-level left retained", "e", `def e = pratt { operand "a" level { infix none "<" infix left "+" } }`, "a<a+a<a"},
		{"same-level right retained", "e", `def e = pratt { operand "a" level { infix none "<" infix right "+" } }`, "a<a+a<a"},
		{"Unicode skip", `e " < " e`, `def e = pratt {
    skip " "*
    operand "α"
    level { infix none "<" prefix "¬" }
}`, " α < ¬ α < α"},
		{"beyond 64 levels", `e "<" e`, "def e = pratt { operand \"a\" " + strings.Repeat("level {} ", 69) + `level { infix none "<" } }`, "a<a<a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, "def main = &("+strconv.Quote(c.input)+" $$) "+c.body+" $$\n"+c.rules)
			for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					if _, err := p.Parse(c.input, pego.WithUnit(unit), pego.WithBackend(backend)); err != nil {
						t.Fatalf("fresh unit=%v backend=%v: %v", unit, backend, err)
					}
				}
			}
			for _, coverage := range []bool{false, true} {
				opts := []sample.Option{sample.WithSeed(1), sample.WithMaxRepeat(4), sample.WithBudget(4000), sample.WithAttempts(8)}
				if coverage {
					opts = append(opts, sample.WithCoverage())
				}
				g, err := sample.New(p, opts...)
				if err != nil {
					t.Fatal(err)
				}
				got, err := g.Generate(1)
				if err != nil || !slices.Equal(got, []string{c.input}) {
					t.Fatalf("coverage=%t: Generate=%q, %v; stats=%+v", coverage, got, err, g.Stats())
				}
				if c.name == "caller none" {
					if report := g.Coverage(); report.Rules != 2 || report.RulesCovered != 2 || report.Alternatives != 2 || report.AlternativesCovered != 2 {
						t.Fatalf("caller coverage: %+v", report)
					}
				}
			}
		})
	}
	// Other operators at a closed none level, and looser/tighter tails, must still prevent an
	// early stop. These caller decompositions are invalid because the first e consumes the suffix.
	for _, c := range []struct{ name, body, rules, input string }{
		{"bare none chain", "e", simple, "a<a<a"},
		{"remaining postfix", `e "!"`, sampleNoneLevels, "a<a!"},
		{"remaining looser infix", `e "|" e`, sampleNoneLevels, "a<a|a"},
		{"remaining tighter infix", `e "+" e`, sampleNoneLevels, "a<a+a"},
		{"remaining same-level infix", `e "+" e`, `def e = pratt { operand "a" level { infix none "<" infix left "+" } }`, "a<a+a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, "def main = &("+strconv.Quote(c.input)+" $$) "+c.body+" $$\n"+c.rules)
			for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					if _, err := p.Parse(c.input, pego.WithUnit(unit), pego.WithBackend(backend)); err == nil {
						t.Fatalf("invalid caller accepted: unit=%v backend=%v", unit, backend)
					}
				}
			}
			g, err := sample.New(p, sample.WithSeed(1), sample.WithAttempts(2), sample.WithBudget(500))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.Next(); !errors.Is(err, sample.ErrNoInput) || g.Stats().Candidates != 0 {
				t.Fatalf("real continuation was not pruned: %v; %+v", err, g.Stats())
			}
		})
	}
}

func TestSamplePrattNoneStopsDeterminism(t *testing.T) {
	p := compile(t, `def main = e "<" e $$`+"\n"+sampleNoneLevels)
	makeGen := func() *sample.Generator {
		t.Helper()
		g, err := sample.New(p, sample.WithSeed(29), sample.WithCoverage(), sample.WithMaxRepeat(2), sample.WithMaxDepth(0), sample.WithMaxLen(8), sample.WithBudget(1000))
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	a, b := makeGen(), makeGen()
	for range 30 {
		x, err := a.Next()
		y, other := b.Next()
		if x != y || !errors.Is(err, other) || a.Stats() != b.Stats() || !reflect.DeepEqual(a.Coverage(), b.Coverage()) {
			t.Fatalf("same seed diverged: %q/%v versus %q/%v", x, err, y, other)
		}
		if err != nil {
			t.Fatal(err)
		}
		checkParse(t, p, []string{x})
	}
	for _, option := range []sample.Option{sample.WithMaxRepeat(0), sample.WithBudget(1)} {
		g, err := sample.New(p, option, sample.WithAttempts(2))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Next(); !errors.Is(err, sample.ErrNoInput) || g.Stats().Failed != 2 {
			t.Fatalf("bounded none search: %v; %+v", err, g.Stats())
		}
	}
}

// Compile is outside the timer. New measures constructor analysis separately from steady-state
// Next (which includes candidate search and mandatory Parse validation). A required-input workload
// cannot be compared as equivalent successful work on versions that return ErrNoInput.
func BenchmarkSamplePratt(b *testing.B) {
	for _, c := range []struct{ name, src string }{
		{"restricted-no-prefix", `def main = e(tight) $$
def e = pratt { operand "x" level { infix left "+" } level tight { infix left "*" } }`},
		{"unrestricted-prefix", "def main = e $$\n" + samplePrefixLevels},
		{"restricted-prefix", "def main = e(tight) $$\n" + samplePrefixLevels},
		{"required-prefix", `def main = &"~" e(tight) $$` + "\n" + samplePrefixLevels},
		{"none-simple", `def main = e $$
def e = pratt { operand "a" level { infix none "<" } }`},
		{"none-mixed", "def main = e $$\n" + sampleNoneLevels},
		{"none-wide", "def main = e $$\ndef e = pratt { operand \"a\" " + strings.Repeat("level {} ", 69) + `level { infix none "<" } }`},
		{"required-none-caller", `def main = &("a<a<a" $$) e "<" e $$
def e = pratt { operand "a" level { infix none "<" } }`},
	} {
		p, err := pego.CompileSource(c.src, "main")
		if err != nil {
			b.Fatal(err)
		}
		for _, coverage := range []bool{false, true} {
			opts := []sample.Option{sample.WithSeed(17)}
			if coverage {
				opts = append(opts, sample.WithCoverage())
			}
			b.Run(fmt.Sprintf("%s/coverage=%t/New", c.name, coverage), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					if _, err := sample.New(p, opts...); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(fmt.Sprintf("%s/coverage=%t/Next", c.name, coverage), func(b *testing.B) {
				g, err := sample.New(p, opts...)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				bytes := 0
				for range b.N {
					s, err := g.Next()
					if err != nil {
						b.Fatal(err)
					}
					bytes += len(s)
				}
				b.ReportMetric(float64(bytes)/float64(b.N), "input-B/op")
				b.ReportMetric(float64(g.Stats().Rejected)/float64(b.N), "rejected/op")
				b.ReportMetric(float64(g.Stats().Failed)/float64(b.N), "failed/op")
			})
		}
	}
}
