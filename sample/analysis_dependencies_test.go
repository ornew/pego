package sample

import (
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

func analysisChain(n int, leafFirst bool) string {
	lines := make([]string, n+1)
	lines[0] = "def main = r0 $$"
	for i := range n {
		body := `"é"`
		if i+1 < n {
			body = fmt.Sprintf("r%d", i+1)
		}
		lines[i+1] = fmt.Sprintf("def r%d = %s", i, body)
	}
	if leafFirst {
		slices.Reverse(lines)
	}
	return strings.Join(lines, "\n")
}

// sweepReference retains the whole-grammar fixed point as an independent
// scheduling oracle. Expression semantics and coverage numbering are shared.
func sweepReference(got *info) *info {
	ref := *got
	ref.rules = make(map[string]*ruleInfo)
	ref.order = nil
	for _, ri := range got.order {
		r := *ri
		r.height, r.length = inf, inf
		r.reach = newBitset(len(got.targets))
		r.reach.set(r.index)
		ref.localTargets(r.def.Expr, r.reach)
		ref.rules[r.def.Name] = &r
		ref.order = append(ref.order, &r)
	}
	for changed := true; changed; {
		changed = false
		for _, r := range ref.order {
			h, l := ref.height(r.def.Expr), ref.length(r.def.Expr)
			if h < r.height || l < r.length {
				r.height, r.length = min(h, r.height), min(l, r.length)
				changed = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, r := range ref.order {
			for _, name := range r.calls {
				if c := ref.rules[name]; c != nil && r.reach.union(c.reach) {
					changed = true
				}
			}
		}
	}
	return &ref
}

func checkAnalysisSweeps(t *testing.T, g *grammar.Grammar) *info {
	t.Helper()
	got := analyze(g, "main")
	ref := sweepReference(got)
	for _, r := range got.order {
		want := ref.rules[r.def.Name]
		if r.height != want.height || r.length != want.length || !slices.Equal(r.reach, want.reach) {
			t.Fatalf("%s: height/length/reach = %d/%d/%v; sweep = %d/%d/%v", r.def.Name, r.height, r.length, r.reach, want.height, want.length, want.reach)
		}
	}
	return got
}

func TestAnalysisDependencyChains(t *testing.T) {
	for _, n := range []int{128, 2048} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/leaf-first=%t", n, reverse), func(t *testing.T) {
				g, err := pego.ParseGrammar(analysisChain(n, reverse))
				if err != nil {
					t.Fatal(err)
				}
				in := checkAnalysisSweeps(t, g)
				if in.start.height != n || in.start.length != 2 {
					t.Fatalf("main height/length = %d/%d", in.start.height, in.start.length)
				}
				for i := range n {
					r := in.rules[fmt.Sprintf("r%d", i)]
					if r.height != n-1-i || r.length != 2 || !in.reachable[r.index] || !in.start.reach.has(r.index) {
						t.Fatalf("incorrect analysis for r%d", i)
					}
				}
				if n == 128 {
					p, err := pego.Compile(g, "main")
					if err != nil {
						t.Fatal(err)
					}
					gen, err := New(p, WithSeed(17), WithCoverage())
					if err != nil {
						t.Fatal(err)
					}
					for range 3 {
						s, err := gen.Next()
						if err != nil || s != "é" {
							t.Fatalf("Next = %q, %v", s, err)
						}
					}
					if c := gen.Coverage(); c.Rules != n+1 || c.RulesCovered != n+1 {
						t.Fatalf("coverage: %+v", c)
					}
				}
			})
		}
	}
}

const analysisMixed = `def main = (left / right / cycle / expr(mul))* $$
def left = shared shared
def right = shared? "r"
def shared = leaf / "xyz"
def leaf = "é"
def cycle = other "c" / "z"
def other = cycle "o"
def impossible = pure
 def pure = impossible
def hidden = "hidden"
def guarded = !hidden leaf
def recovered = leaf #recover(skip=hidden)
def missing = absent
def independent = "independent"
def expr = pratt { operand leaf level { infix left "+" } level mul { infix left "*" } }`

func TestAnalysisDependencyCyclesAndCoverage(t *testing.T) {
	g, err := pego.ParseGrammar(analysisMixed)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(3, 7))
	for round := range 30 {
		rng.Shuffle(len(g.Statements), func(i, j int) { g.Statements[i], g.Statements[j] = g.Statements[j], g.Statements[i] })
		in := checkAnalysisSweeps(t, g)
		for name, want := range map[string][2]int{"leaf": {0, 2}, "shared": {0, 2}, "left": {1, 4}, "cycle": {0, 1}, "other": {1, 2}, "pure": {inf, inf}, "missing": {inf, inf}} {
			r := in.rules[name]
			if r.height != want[0] || r.length != want[1] {
				t.Fatalf("round %d %s: %d/%d; want %v", round, name, r.height, r.length, want)
			}
		}
		for _, name := range []string{"guarded", "recovered"} {
			if in.rules[name].reach.has(in.rules["hidden"].index) {
				t.Fatalf("%s includes a non-generated call", name)
			}
		}
		for _, name := range []string{"hidden", "independent", "pure", "missing"} {
			if in.reachable[in.rules[name].index] {
				t.Fatalf("%s unexpectedly reachable", name)
			}
		}
	}
}

func BenchmarkSampleAnalysis(b *testing.B) {
	for _, n := range []int{128, 512, 1024, 2048} {
		for _, reverse := range []bool{false, true} {
			b.Run(fmt.Sprintf("chain/%d/leaf-first=%t", n, reverse), func(b *testing.B) {
				g, err := pego.ParseGrammar(analysisChain(n, reverse))
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					analyze(g, "main")
				}
			})
		}
	}
	for name, src := range map[string]string{"mixed": analysisMixed, "small": `def main = ("a" / "b")* $$`} {
		b.Run(name, func(b *testing.B) {
			g, err := pego.ParseGrammar(src)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				analyze(g, "main")
			}
		})
	}
	for _, name := range []string{"calculator", "minilang", "outline"} {
		b.Run(name, func(b *testing.B) {
			file := "../examples/" + name + "/" + name + ".pego"
			if name == "calculator" {
				file = "../examples/calculator/calc.pego"
			}
			src, err := os.ReadFile(file)
			if err != nil {
				b.Fatal(err)
			}
			g, err := pego.ParseGrammar(string(src))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				analyze(g, "main")
			}
		})
	}
}
