package complexitytest

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/starttest"
)

var differentialGrammars = flag.Int("differential", 600, "number of random grammars TestLinearClaims generates")

// differentialCap bounds the budget of one parse of TestLinearClaims. A parse of a linear grammar
// stays far below it on the inputs of the test.
const differentialCap = 4_000_000

// differentialSizes are n of the series n, 2n, 4n of TestLinearClaims.
var differentialSizes = double(512)

// perByteLimit is the largest growth of the cost per input byte from the larger of the sizes n
// and 2n to the size 4n for a grammar classified linear. Growth as n^k with k >= 1.27 exceeds it
// even against 2n.
const perByteLimit = 1.2

// measuresLinear reports whether the series stays within the bound of linear cost: no point is
// capped, and the cost per byte at the largest size grows by at most perByteLimit from the
// smaller sizes. The slopes of Classify do not serve here: whether a parse of a random grammar
// fails, which adds the cost of reporting the error, can change from one size to the next, and
// the series then zigzags inside a linear bound.
func measuresLinear(s Series) bool {
	if s.CappedPoints() {
		return false
	}
	perByte := func(i int) float64 { return float64(s.Costs[i].Steps) / float64(s.Sizes[i]+1) }
	return perByte(2) <= perByteLimit*max(perByte(0), perByte(1))
}

// differentialInputs are the families of inputs of TestLinearClaims, over the alphabet of the
// random grammars. The input of a size is a prefix of the input of a larger size: each family
// repeats a unit, except the last, which takes prefixes of randomInput.
var differentialInputs = []func(n int) string{
	func(n int) string { return rep("a", n) },
	func(n int) string { return rep("b", n) },
	func(n int) string { return rep("ab", n/2) },
	func(n int) string { return rep("aab", n/3) },
	func(n int) string { return rep("a", n) + "c" },
	func(n int) string { return rep("abc", n/3) },
	func(n int) string { return randomInput[:n] },
}

// randomInput is a pseudo-random string over the alphabet abc, long enough for the largest size.
var randomInput = func() string {
	rng := rand.New(rand.NewPCG(7, 11))
	var b strings.Builder
	for range 4 * differentialSizes[2] {
		b.WriteByte("abc"[rng.IntN(3)])
	}
	return b.String()
}()

// randomCostGrammar returns a random grammar over the alphabet abc with the start rule main.
// The shapes are those of the measured families: choices whose alternatives share prefixes,
// repetitions of them, scans, lookaheads, recursion in either direction, cuts and recovery.
func randomCostGrammar(rng *rand.Rand) string {
	atom := func() string {
		atoms := []string{`"a"`, `"b"`, `"c"`, `"ab"`, `"aa"`, `[a-b]`, `[^c]`, `.`,
			`"a"*`, `[ab]*`, `"b"+`, "r1", "r2", "r3", "r1", "r2", "r3"}
		return atoms[rng.IntN(len(atoms))]
	}
	var expr func(depth int) string
	expr = func(depth int) string {
		if depth <= 0 || rng.IntN(4) == 0 {
			return atom()
		}
		parts := func() []string {
			ps := make([]string, 2+rng.IntN(2))
			for i := range ps {
				ps[i] = expr(depth - 1)
			}
			return ps
		}
		switch rng.IntN(10) {
		case 0, 1, 2:
			ps := parts()
			if rng.IntN(8) == 0 {
				ps[rng.IntN(len(ps))] += " #cut"
			}
			return "(" + strings.Join(ps, " ") + ")"
		case 3, 4, 5:
			return "(" + strings.Join(parts(), " / ") + ")"
		case 6:
			return "(" + expr(depth-1) + ")" + []string{"*", "+", "?"}[rng.IntN(3)]
		case 7:
			return []string{"!", "&"}[rng.IntN(2)] + "(" + expr(depth-1) + ")"
		case 8:
			return "(" + expr(depth-1) + ") #recover(skip=" + []string{"[a-b]+", "[^c]", `"c"`}[rng.IntN(3)] + ")"
		}
		return "(" + expr(depth-1) + ") #atomic"
	}
	var b strings.Builder
	if rng.IntN(3) == 0 {
		fmt.Fprintf(&b, "rule main = %s $$\n", expr(2))
	} else {
		fmt.Fprintf(&b, "rule main = (%s / .)* $$\n", expr(2))
	}
	for _, r := range []string{"r1", "r2", "r3"} {
		fmt.Fprintf(&b, "rule %s = %s\n", r, expr(3))
	}
	return b.String()
}

// linearClaims generates count random grammars, keeps those that compile, and checks every one
// that claimsLinear classifies as linear against the measured cost on the input families on the
// closure backend. It returns the number of grammars checked and the failures; it stops at the
// first failure if firstOnly is set.
func linearClaims(t *testing.T, count int, claimsLinear func(*grammar.Grammar) bool, firstOnly bool) (int, []string) {
	t.Helper()
	rng := rand.New(rand.NewPCG(3, 5))
	checked := 0
	var failures []string
	for n := range count {
		src := randomCostGrammar(rng)
		g, err := pego.ParseGrammar(src)
		if err != nil {
			t.Fatalf("generated grammar %d does not parse: %v\n%s", n, err, src)
		}
		g = starttest.MarkAll(g)
		p, err := pego.Compile(g, "main")
		if err != nil {
			continue // left recursion without a base case, a repetition of nothing and the like
		}
		if !claimsLinear(g) {
			continue
		}
		checked++
		for i, build := range differentialInputs {
			s := Series{Sizes: differentialSizes}
			for _, size := range differentialSizes {
				c, err := StepsLenient(p, build(size), pego.Closure, differentialCap)
				if err != nil {
					t.Fatalf("grammar %d, input %d, size %d: %v\n%s", n, i, size, err, src)
				}
				s.Costs = append(s.Costs, c)
			}
			if !measuresLinear(s) {
				failures = append(failures, fmt.Sprintf("grammar %d, input %d: steps %v, slopes %.2f\n%s",
					n, i, s.Costs, s.Slopes(), src))
				if firstOnly {
					return checked, failures
				}
				break
			}
		}
	}
	return checked, failures
}

// TestLinearClaims checks the claims of linear cost of the classifier against measurement: every
// random grammar that the classifier classifies as linear, with or without an environment bound,
// measures linear on every input family. Run with -differential=5000 for a longer search.
func TestLinearClaims(t *testing.T) {
	t.Parallel()
	count := *differentialGrammars
	if testing.Short() {
		count /= 4
	}
	checked, failures := linearClaims(t, count, func(g *grammar.Grammar) bool {
		c, err := engine.ClassifyComplexity(context.Background(), g, "main")
		if err != nil {
			t.Fatal(err)
		}
		return c.Class == ClassLinear || c.Class == ClassLinearEnv
	}, false)
	for _, f := range failures {
		t.Errorf("classified linear, measured otherwise: %s", f)
	}
	// The check is only as strong as the claims it sees.
	if min := count / 10; checked < min {
		t.Errorf("%d grammars classified linear, want at least %d", checked, min)
	}
	t.Logf("%d of %d grammars classified linear", checked, count)
}

// TestLinearClaimsNegativeControl checks that TestLinearClaims can fail: with a classifier that
// claims linear cost for the grammars that the classifier classifies as polynomial or exponential,
// some random grammar measures otherwise.
func TestLinearClaimsNegativeControl(t *testing.T) {
	t.Parallel()
	count := *differentialGrammars
	if testing.Short() {
		count /= 4
	}
	_, failures := linearClaims(t, count, func(g *grammar.Grammar) bool {
		c, err := engine.ClassifyComplexity(context.Background(), g, "main")
		if err != nil {
			t.Fatal(err)
		}
		return c.Class == ClassPoly || c.Class == ClassExp
	}, true)
	if len(failures) == 0 {
		t.Error("no grammar classified polynomial or exponential measures otherwise than linear: the differential test cannot fail")
	} else {
		t.Logf("the first grammar that measures otherwise: %s", failures[0])
	}
}
