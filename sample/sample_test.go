package sample_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

// exampleParsers compiles every grammar in examples/.
func exampleParsers(t *testing.T) map[string]*pego.Parser {
	t.Helper()
	paths, err := filepath.Glob("../examples/*/*.pego")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no example grammars: %v", err)
	}
	ps := map[string]*pego.Parser{}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		p, err := pego.CompileSource(string(src), "main")
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		ps[path] = p
	}
	return ps
}

func compile(t *testing.T, src string) *pego.Parser {
	t.Helper()
	p, err := pego.CompileSource(src, "main")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// checkParse checks that every input parses without errors on every backend.
func checkParse(t *testing.T, p *pego.Parser, inputs []string) {
	t.Helper()
	for _, in := range inputs {
		for _, b := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
			if _, err := p.Parse(in, pego.WithBackend(b)); err != nil {
				t.Errorf("backend %v: generated input %q does not parse: %v", b, in, err)
			}
		}
	}
}

// TestExamplesParse generates inputs for every example grammar and checks that they all parse.
func TestExamplesParse(t *testing.T) {
	for path, p := range exampleParsers(t) {
		t.Run(path, func(t *testing.T) {
			const n = 20
			inputs, err := sample.Generate(p, n, sample.WithSeed(1))
			if err != nil {
				t.Fatal(err)
			}
			if len(inputs) != n {
				t.Errorf("got %d inputs, want %d", len(inputs), n)
			}
			checkParse(t, p, inputs)
		})
	}
}

// TestExamplesCoverage checks that coverage mode reaches most of each example grammar, and logs the
// coverage (go test -v ./sample -run Coverage).
func TestExamplesCoverage(t *testing.T) {
	// Minimum shares of rules and alternatives, in percent. The Go and Python grammars are large,
	// and their inputs reach the length limit before covering everything.
	want := map[string][2]int{"golang/go.pego": {60, 50}, "python/python.pego": {85, 80}}
	for path, p := range exampleParsers(t) {
		t.Run(path, func(t *testing.T) {
			g, err := sample.New(p, sample.WithSeed(1), sample.WithCoverage())
			if err != nil {
				t.Fatal(err)
			}
			inputs, err := g.Generate(50)
			if err != nil {
				t.Fatal(err)
			}
			checkParse(t, p, inputs)
			c := g.Coverage()
			t.Logf("%d inputs: %s", len(inputs), c)
			min := [2]int{100, 100}
			for suffix, m := range want {
				if strings.HasSuffix(filepath.ToSlash(path), suffix) {
					min = m
				}
			}
			if c.RulesCovered*100 < min[0]*c.Rules || c.AlternativesCovered*100 < min[1]*c.Alternatives {
				t.Errorf("coverage %s is below %d%% of rules and %d%% of alternatives\n%s", c, min[0], min[1], c.Report())
			}
		})
	}
}

// TestJSONAgreesWithEncodingJSON is a differential test: the JSON grammar of examples/json and
// encoding/json must accept the same inputs.
func TestJSONAgreesWithEncodingJSON(t *testing.T) {
	p := exampleParsers(t)[filepath.Join("..", "examples", "json", "json.pego")]
	g, err := sample.New(p, sample.WithSeed(1), sample.WithCoverage())
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := g.Generate(200)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if !json.Valid([]byte(in)) {
			t.Errorf("encoding/json rejects %q", in)
		}
	}
	invalid, err := g.GenerateInvalid(200)
	if err != nil {
		t.Fatal(err)
	}
	for _, inv := range invalid {
		if json.Valid([]byte(inv.Input)) {
			t.Errorf("encoding/json accepts %q (%s of %q)", inv.Input, inv.Mutation, inv.Base)
		}
	}
}

func TestDeterminism(t *testing.T) {
	ps := exampleParsers(t)
	p := ps[filepath.Join("..", "examples", "json", "json.pego")]
	gen := func(seed uint64) ([]string, []sample.Invalid) {
		g, err := sample.New(p, sample.WithSeed(seed), sample.WithCoverage())
		if err != nil {
			t.Fatal(err)
		}
		inputs, err := g.Generate(20)
		if err != nil {
			t.Fatal(err)
		}
		invalid, err := g.GenerateInvalid(10)
		if err != nil {
			t.Fatal(err)
		}
		return inputs, invalid
	}
	a, ai := gen(7)
	b, bi := gen(7)
	if !slices.Equal(a, b) {
		t.Errorf("the same seed gave different inputs:\n%q\n%q", a, b)
	}
	if len(ai) != len(bi) {
		t.Fatalf("the same seed gave %d and %d invalid inputs", len(ai), len(bi))
	}
	for i := range ai {
		if ai[i].Input != bi[i].Input || ai[i].Mutation != bi[i].Mutation {
			t.Errorf("the same seed gave different invalid inputs: %+v, %+v", ai[i], bi[i])
		}
	}
	if c, _ := gen(8); slices.Equal(a, c) {
		t.Errorf("different seeds gave the same inputs: %q", a)
	}
}

func TestInvalid(t *testing.T) {
	for path, p := range exampleParsers(t) {
		t.Run(path, func(t *testing.T) {
			g, err := sample.New(p, sample.WithSeed(3))
			if err != nil {
				t.Fatal(err)
			}
			invs, err := g.GenerateInvalid(10)
			if err != nil {
				t.Fatal(err)
			}
			if len(invs) < 5 {
				t.Errorf("got %d invalid inputs", len(invs))
			}
			for _, inv := range invs {
				if _, err := p.Parse(inv.Input); err == nil {
					t.Errorf("invalid input %q (%s) parses", inv.Input, inv.Mutation)
				}
				if _, err := p.Parse(inv.Base); err != nil {
					t.Errorf("base %q does not parse: %v", inv.Base, err)
				}
				if inv.Input == inv.Base || inv.Mutation == "" || inv.Err == nil {
					t.Errorf("bad invalid input %+v", inv)
				}
			}
		})
	}
}

// TestPEGSemantics checks grammars where a naive walk of the grammar produces inputs that the parser
// rejects or reads differently.
func TestPEGSemantics(t *testing.T) {
	cases := []struct {
		name, src string
		// check, if set, is called with each input.
		check func(t *testing.T, in string)
	}{
		{"greedy repetition before an overlapping element", `def main = (?a-c)* (?b-z) (?0-9)`, func(t *testing.T, in string) {
			if c := in[len(in)-2]; c < 'd' {
				t.Errorf("got %q", in)
			}
		}},
		{"ordered choice shadows a longer alternative", `def main = ("a" / "ab") "c"`, func(t *testing.T, in string) {
			if in != "ac" {
				t.Errorf("got %q", in)
			}
		}},
		{"negative lookahead", `def main = ident (" " ident)*
def ident = !kw @(?a-z)+
def kw = ("if" / "in") !(?a-z)`, nil},
		{"positive lookahead with a capture and a predicate", `def main = &(n:@(?0-9)) [len($n) == 1] (?0-9)+ ";"`, nil},
		{"negated classes and multi-byte ranges", `def main = (?^a-z|\n)+ "|" (?\u{3040}-\u{309f}\u{1F600}-\u{1F64F})+ .`, nil},
		{"anchors", `def main = ^^ (^ (?a-z)+ $ "\n")* $$`, nil},
		{"captures, atomic and discard", `def main = k:@(?a-z)+ -"=" v:(?0-9)+ -(?;,)?`, nil},
		{"variables", `def main = [n = 2] item item?
def item = s:@" "* [len($s) == n] "x"`, func(t *testing.T, in string) {
			if !strings.HasPrefix(in, "  x") {
				t.Errorf("got %q", in)
			}
		}},
		{"matching texts", `def main = "<" a:name ">" "</" b:name ">" [text($a) == text($b)]
def name = @(?a-z)+`, nil},
		{"left recursion", `def main = e $$
def e = e "+" t / e "-" t / t
def t = t "*" f / f
def f = "(" e ")" / (?0-9)`, nil},
		{"Pratt levels, non-associative and level-restricted calls", `def main = e $$
def e = pratt {
    skip " "*
    operand (?0-9)+
    operand "(" e ")"
    operand "f(" e(add) ("," e(add))* ")"
    level { infix right "?" e ":" }
    level { infix none "==" / "<" }
    level add { infix left "+" / "-" }
    level { prefix "-" / "!" }
    level { postfix "!" / "[" e "]" }
}`, nil},
		{"unknown left side of ||, undefined variable on the right", `def main = e:num [text($e) != "" || z == 1]
def num = d:(?0-9) -> $d`, nil},
		{"empty iterations below the minimum", `def main = (_){3} "q"`, nil},
		// A repetition element sees only its own captures: outer ones are read through a variable.
		{"repetition elements read enclosing captures through variables", `def main = n:"a" [k = len($n)] ([k == 1] "x")* "y"`, nil},
		{"top, cut, attributes", `def main = (stmt -- ";")* _ $$
def stmt = (@"x" / "y" "z") #error(message="expected a statement") #recover(skip=(?^;)+)`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := compile(t, c.src)
			inputs, err := sample.Generate(p, 10, sample.WithSeed(1))
			if err != nil {
				t.Fatal(err)
			}
			checkParse(t, p, inputs)
			if c.check != nil {
				for _, in := range inputs {
					c.check(t, in)
				}
			}
		})
	}
}

// TestReviewRegressions checks inputs that the generator used to prune although the parser accepts them.
func TestReviewRegressions(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`def main = ("a"?){2} "b"`, "b"},
		{`def main = n:"a" [k = len($n)] ([k == 1] "x")* "y"`, "axy"},
	} {
		inputs, err := sample.Generate(compile(t, c.src), 20, sample.WithSeed(1))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(inputs, c.want) {
			t.Errorf("%s: %q does not include %q", c.src, inputs, c.want)
		}
	}
}

// TestBudgetBoundsTime checks that an attempt stays within its budget when checks, not generation
// steps, do the work: every character written re-evaluates the pending checks.
func TestBudgetBoundsTime(t *testing.T) {
	// Many pending checks over a growing text (one attempt took 16 s when only generation steps
	// counted).
	p := compile(t, `def main = ("c" r ";")* ";"
def r = "a" (";" r / ";" "b")*`)
	g, err := sample.New(p, sample.WithSeed(2), sample.WithAttempts(1))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	g.Next()
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("one attempt took %v", d)
	}
	// A lookahead per character over the whole text: cubic work in the length.
	p = compile(t, `def main = (!(x* "b") "a"){200} "c" $$
def x = "a" / "c"`)
	if _, err := sample.Generate(p, 1); err != nil {
		t.Error(err)
	}
}

// TestCoverageDeadAlternative checks that coverage mode stops pursuing an alternative that the parser
// never takes when the search fails on it late, without a candidate to reject: the last alternative
// below is dead, since the first one matches whatever it starts with.
func TestCoverageDeadAlternative(t *testing.T) {
	p := compile(t, `def main = s $$
def s = "(" s ")" / "x" / "y" / "(" s ")" "!"`)
	plain, err := sample.Generate(p, 20, sample.WithSeed(1))
	if err != nil {
		t.Fatal(err)
	}
	covered, err := sample.Generate(p, 20, sample.WithSeed(1), sample.WithCoverage())
	if err != nil {
		t.Fatal(err)
	}
	if len(covered) < len(plain)-2 {
		t.Errorf("coverage mode found %d inputs, plain mode %d", len(covered), len(plain))
	}
}

func TestNoInput(t *testing.T) {
	// No input parses: the repetition takes every "a", and the second "a" never matches.
	p := compile(t, `def main = "a"* "a"`)
	g, err := sample.New(p, sample.WithAttempts(5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Generate(3); !errors.Is(err, sample.ErrNoInput) {
		t.Errorf("got %v", err)
	}
	if s := g.Stats(); s.Candidates != 0 && s.Rejected != s.Candidates {
		t.Errorf("stats %+v", s)
	}
	// A start rule that can never match.
	if _, err := sample.New(compile(t, `def main = "a" _|_`)); err == nil {
		t.Error("expected an error for a start rule that can never match")
	}
	// A parser without the grammar AST.
	data, err := compile(t, `def main = "a"`).Marshal(pego.WithoutAST())
	if err != nil {
		t.Fatal(err)
	}
	q, err := pego.LoadParser(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sample.New(q); err == nil {
		t.Error("expected an error for a parser without the AST")
	}
}

func TestFewInputs(t *testing.T) {
	// A grammar that accepts three inputs.
	p := compile(t, `def main = "a" / "b" / "c"`)
	inputs, err := sample.Generate(p, 10, sample.WithAttempts(20))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(inputs)
	if !slices.Equal(inputs, []string{"a", "b", "c"}) {
		t.Errorf("got %q", inputs)
	}
}

func TestLimits(t *testing.T) {
	p := compile(t, `def main = n $$
def n = "(" n ")" / "[" n* "]" / "x"`)
	depth := func(s string) int {
		d, m := 0, 0
		for _, r := range s {
			switch r {
			case '(', '[':
				d++
				m = max(m, d)
			case ')', ']':
				d--
			}
		}
		return m
	}
	shallow, err := sample.Generate(p, 30, sample.WithSeed(1), sample.WithMaxDepth(2))
	if err != nil {
		t.Fatal(err)
	}
	deep, err := sample.Generate(p, 30, sample.WithSeed(1), sample.WithMaxDepth(8))
	if err != nil {
		t.Fatal(err)
	}
	checkParse(t, p, shallow)
	checkParse(t, p, deep)
	maxDepth := func(ins []string) (m int) {
		for _, in := range ins {
			m = max(m, depth(in))
		}
		return m
	}
	// Past the depth, the generator takes the shortest way out ("x"), so nesting stays within one
	// level of it.
	if m := maxDepth(shallow); m > 3 {
		t.Errorf("max depth 2: nesting %d", m)
	}
	if m := maxDepth(deep); m <= 3 {
		t.Errorf("max depth 8: nesting only %d", m)
	}
	short, err := sample.Generate(p, 30, sample.WithSeed(1), sample.WithMaxDepth(100), sample.WithMaxLen(10))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range short {
		// The length limit is soft: the input is closed the shortest way after it.
		if len(in) > 40 {
			t.Errorf("max len 10: %d bytes: %q", len(in), in)
		}
	}
}

func TestCoverageReport(t *testing.T) {
	p := compile(t, `def main = a / b / "c" _|_
def a = "a" !kw
def b = "b" #recover(skip=skipped)
def kw = "k"
def skipped = (?^;)+
def unused = "u"
def never = "n" _|_`)
	g, err := sample.New(p, sample.WithCoverage(), sample.WithSeed(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Generate(5); err != nil {
		t.Fatal(err)
	}
	c := g.Coverage()
	// main, a and b; alternatives a and b of main (the third can never match).
	if c.Rules != 3 || c.RulesCovered != 3 || c.Alternatives != 2 || c.AlternativesCovered != 2 {
		t.Errorf("got %+v", c)
	}
	if !slices.Equal(c.Unreachable, []string{"kw", "skipped", "unused", "never"}) {
		t.Errorf("unreachable: %q", c.Unreachable)
	}
	if len(c.Impossible) != 0 {
		t.Errorf("impossible: %q", c.Impossible)
	}
	if got := c.String(); got != "rules 3/3 (100%), alternatives 2/2 (100%)" {
		t.Errorf("got %q", got)
	}

	// Targets inside expressions that can never match are not counted, and rules called only there are
	// unreachable.
	p = compile(t, `def main = ("(" ("a" / "b") _|_) / r _|_ / "c"
def r = "x" / "y"`)
	g, err = sample.New(p, sample.WithSeed(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Generate(1); err != nil {
		t.Fatal(err)
	}
	c = g.Coverage()
	if c.Rules != 1 || c.Alternatives != 1 || c.AlternativesCovered != 1 || !slices.Equal(c.Unreachable, []string{"r"}) {
		t.Errorf("got %+v", c)
	}

	// Without generating anything, everything reachable is missed.
	p = compile(t, `def main = x / y
def x = "x" z?
def y = "y"
def z = "z" _|_ / "zz"`)
	g, err = sample.New(p)
	if err != nil {
		t.Fatal(err)
	}
	c = g.Coverage()
	if !slices.Equal(c.MissedRules, []string{"main", "x", "y", "z"}) || len(c.MissedAlternatives) != 3 {
		t.Errorf("got %+v", c)
	}
	if a := c.MissedAlternatives[2]; a.Rule != "z" || a.Kind != "choice" || a.Index != 1 || a.Expr != `"zz"` {
		t.Errorf("got %+v", a)
	}
}

// fakeF records what Seed does with a *testing.F.
type fakeF struct {
	added  []string
	failed string
}

func (f *fakeF) Helper() {}
func (f *fakeF) Add(args ...any) {
	f.added = append(f.added, args[0].(string))
}
func (f *fakeF) Fatalf(format string, args ...any) { f.failed = format }

func TestSeed(t *testing.T) {
	p := compile(t, `def main = (?a-z)+ ("," (?a-z)+)*`)
	f := &fakeF{}
	got := sample.Seed(f, p, 8, sample.WithSeed(2))
	if f.failed != "" || len(got) != 8 || !slices.Equal(got, f.added) {
		t.Errorf("got %q, added %q, failed %q", got, f.added, f.failed)
	}
	checkParse(t, p, got)
	f = &fakeF{}
	sample.Seed(f, compile(t, `def main = "a"* "a"`), 3, sample.WithAttempts(2))
	if f.failed == "" || len(f.added) != 0 {
		t.Errorf("expected a failure, got %+v", f)
	}
}
