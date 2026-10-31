package complexitytest

import (
	"errors"
	"math"
	"testing"

	"github.com/ornew/zeroparse"
)

// series builds a series of three doubling sizes from f.
func series(f func(n float64) float64, capAt float64) Series {
	s := Series{Sizes: []int{10, 20, 40}}
	for _, n := range s.Sizes {
		v := f(float64(n))
		if capAt > 0 && v >= capAt {
			s.Costs = append(s.Costs, Cost{Steps: uint64(capAt), Capped: true})
		} else {
			s.Costs = append(s.Costs, Cost{Steps: uint64(v)})
		}
	}
	return s
}

func TestClassifySynthetic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    Series
		want Order
	}{
		{"linear", series(func(n float64) float64 { return 1500 * n }, 0), Linear},
		{"linear with offset", series(func(n float64) float64 { return 900 + 1500*n }, 0), Linear},
		{"quadratic", series(func(n float64) float64 { return 100 * n * n }, 0), Poly(2)},
		{"cubic", series(func(n float64) float64 { return 100 * n * n * n }, 0), Poly(3)},
		{"quadratic with linear part", series(func(n float64) float64 { return 100*n*n + 1000*n }, 0), Poly(2)},
		{"exponential", series(func(n float64) float64 { return 100 * math.Pow(2, n/2) }, 0), Exp},
		{"exponential capped at 4n", series(func(n float64) float64 { return 100 * math.Pow(2, n/2) }, 1e7), Exp},
		{"too steep for a polynomial band", series(func(n float64) float64 { return math.Pow(n, 3.5) }, 0), Unknown},
	}
	for _, c := range cases {
		if got := Classify(c.s); got != c.want {
			t.Errorf("%s: classified %v, want %v (slopes %v)", c.name, got, c.want, c.s.Slopes())
		}
	}
}

// A capped point is a lower bound: it never makes a series Linear or Poly.
func TestClassifyCappedIsNeverLinearOrPoly(t *testing.T) {
	t.Parallel()
	// A linear series cut by a small cap flattens: its true cost is unknown.
	s := Series{Sizes: []int{10, 20, 40}, Costs: []Cost{{Steps: 1000}, {Steps: 2000}, {Steps: 2000, Capped: true}}}
	if got := Classify(s); got != Unknown {
		t.Errorf("capped flat tail classified %v, want Unknown", got)
	}
	// A capped quadratic series.
	s = Series{Sizes: []int{10, 20, 40}, Costs: []Cost{{Steps: 1000}, {Steps: 4000}, {Steps: 9000, Capped: true}}}
	if got := Classify(s); got == Linear || got.Kind == KindPoly {
		t.Errorf("capped quadratic tail classified %v", got)
	}
	// A capped earlier point.
	s = Series{Sizes: []int{10, 20, 40}, Costs: []Cost{{Steps: 1000}, {Steps: 9000, Capped: true}, {Steps: 9000, Capped: true}}}
	if got := Classify(s); got != Unknown {
		t.Errorf("series capped from 2n classified %v, want Unknown", got)
	}
}

func TestClassifyShortSeries(t *testing.T) {
	t.Parallel()
	if got := Classify(Series{Sizes: []int{1, 2}, Costs: []Cost{{Steps: 1}, {Steps: 2}}}); got != Unknown {
		t.Errorf("two points classified %v, want Unknown", got)
	}
}

func TestOrderString(t *testing.T) {
	t.Parallel()
	for o, want := range map[Order]string{Linear: "Linear", Poly(3): "Poly(3)", Exp: "Exp", Unknown: "Unknown"} {
		if got := o.String(); got != want {
			t.Errorf("%#v = %q, want %q", o, got, want)
		}
	}
}

func mustCompile(t *testing.T, src string) *zeroparse.Parser {
	t.Helper()
	p, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A traced parse spends slightly more budget than an untraced one, so the suites measure the
// untraced parse by bisection. The tolerant search stays within 0.4% of the exact threshold, and
// the counter of a traced parse is an upper estimate within 15%.
func TestStepsBisectionAndTrace(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, in string }{
		{"scan", "rule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\"\n", rep("a", 60)},
		{"left recursion", "rule main = e $$\nrule e = e \"x\" / \"x\"\n", rep("x", 40)},
		{"environment", "var v string\nrule main = r $$\nrule r = \"(\" {v := \"0\"} r \")\" \"y\" / \"n\"\n", "(((n)y)y)y"},
		{"recovery", "rule main = (x / \"a\")* $$\nrule x = (y #recover(skip=[a]+)) \"c\"\nrule y = \"b\"\n", rep("a", 30)},
	}
	for _, c := range cases {
		p := mustCompile(t, c.src)
		for _, b := range Backends {
			exact, err := ExactSteps(p, c.in, b, 10_000_000)
			if err != nil {
				t.Fatalf("%s %s: %v", c.name, BackendName(b), err)
			}
			traced, err := StepsTraced(p, c.in, b, 10_000_000)
			if err != nil {
				t.Fatalf("%s %s: %v", c.name, BackendName(b), err)
			}
			if traced.Steps < exact.Steps || traced.Steps > exact.Steps+exact.Steps*15/100+16 {
				t.Errorf("%s %s: bisection %v, traced %v", c.name, BackendName(b), exact, traced)
			}
			approx, err := Steps(p, c.in, b, 10_000_000)
			if err != nil || approx.Steps < exact.Steps || approx.Steps > exact.Steps+exact.Steps/256+1 {
				t.Errorf("%s %s: tolerant search %v (%v), exact %v", c.name, BackendName(b), approx, err, exact)
			}
		}
	}
}

func TestStepsCapAndErrors(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, "rule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\"\n")
	for _, b := range Backends {
		c, err := Steps(p, rep("a", 200), b, 1000)
		if err != nil || !c.Capped || c.Steps != 1000 {
			t.Errorf("%s: cap: %v, %v", BackendName(b), c, err)
		}
		c, err = StepsTraced(p, rep("a", 200), b, 1000)
		if err != nil || !c.Capped || c.Steps != 1000 {
			t.Errorf("%s: traced cap: %v, %v", BackendName(b), c, err)
		}
		if _, err := Steps(p, "b", b, 1000); err == nil {
			t.Errorf("%s: a syntax error was not returned", BackendName(b))
		}
	}
}

func TestStepsLenient(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, "rule main = \"a\"* $$\n")
	for _, b := range Backends {
		c, err := StepsLenient(p, "aaab", b, 100_000)
		if err != nil || c.ParseErr == nil || c.Capped || c.Steps == 0 {
			t.Errorf("%s: lenient cost %+v, %v", BackendName(b), c, err)
		}
		var se *zeroparse.SyntaxError
		var ses zeroparse.SyntaxErrors
		if !errors.As(c.ParseErr, &se) && !errors.As(c.ParseErr, &ses) {
			t.Errorf("%s: ParseErr %T is no syntax error", BackendName(b), c.ParseErr)
		}
		if _, err := Steps(p, "aaab", b, 100_000); err == nil {
			t.Errorf("%s: strict Steps accepted a syntax error", BackendName(b))
		}
	}
}

// The tolerant search stops only when its interval is within 1/256 of the result: the result
// exceeds the exact threshold by less than result/256. Inputs of many sizes vary where the
// bisection ends, so a looser stopping rule shows on some of them.
func TestStepsTolerance(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, "rule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\"\n")
	for _, b := range Backends {
		for n := 40; n <= 160; n += 4 {
			in := rep("a", n)
			exact, err := ExactSteps(p, in, b, 10_000_000)
			if err != nil {
				t.Fatalf("%s n=%d: %v", BackendName(b), n, err)
			}
			approx, err := Steps(p, in, b, 10_000_000)
			if err != nil || approx.Steps < exact.Steps || approx.Steps-exact.Steps > approx.Steps/256 {
				t.Errorf("%s n=%d: tolerant search %v (%v), exact %v", BackendName(b), n, approx, err, exact)
			}
		}
	}
}

// An error of a finished parse that is not a syntax error, such as a run-time error of an action,
// is a failure of the measurement, also for StepsLenient.
func TestStepsLenientRejectsRuntimeError(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, "rule main = xs:\"a\"* $$ => $xs[10]\n")
	for _, b := range Backends {
		c, err := StepsLenient(p, "aaa", b, 100_000)
		if err == nil {
			t.Errorf("%s: run-time error accepted as %T in %+v", BackendName(b), c.ParseErr, c)
			continue
		}
		var se *zeroparse.SyntaxError
		var ses zeroparse.SyntaxErrors
		if errors.As(err, &se) || errors.As(err, &ses) {
			t.Errorf("%s: error %v is a syntax error, want a run-time error", BackendName(b), err)
		}
	}
}
