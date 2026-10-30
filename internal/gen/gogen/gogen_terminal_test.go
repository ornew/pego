package gogen_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// terminalCases are grammars with value-free terminal expressions that lean typed parsers match
// through a TerminalProgram, with inputs that reach their alternatives, guards, options and
// lookaheads, and fail inside them so that syntax errors report their expectations.
func terminalCases() []gentest.Case {
	cases := []gentest.Case{
		// A keyword list spelled as a trie of case-insensitive classes, as in the SQL grammar,
		// in a negative lookahead and in a discard, whose failures are reported, and again
		// behind the farthest position; guards that overlap, one of a Unicode literal, and an
		// unguarded alternative after the guarded ones.
		{Name: "terminal keyword trie", Src: `rule main = (stmt ";")* $$
rule stmt = -kw "(" / -kw "[" / !kw id
rule kw = ([aA] ([bB] ([oO] [rR] [tT] / [sS] [oO] [lL] [uU] [tT] [eE]) / [cC] [tT] [iI] [oO] [nN] / [lL] [lL]) / [cC] ([aA] [lL] [lL] ([eE] [dD])? / [hH] [eE] [cC] [kK]) / [dD] [oO] / [a-e] [xX] / [eE] [nN] [dD] / [fF] [oO] [rR] / "é" [tT] / "q"? [zZ] [qQ]) ![a-z_]
rule id = [a-z_]+ #atomic
`, Inputs: []string{"abort(;", "ABSOLUTE(;", "abo;", "all(;allx;", "call(;called(;calle;", "check;", "do(;dox;", "action(;", "a;", "", "x;", "abortx;", "Call(;", "CALLED(;", "ch(;", "abs(;", "abo(;", "ab(;", "abso(;", "cal(;", "calle(;", "ac(;", "chec(;", "d(;", "\xff(;", "a\xffort(;", "abort\xc3;", "ax(;", "ex[;", "end[;", "en[;", "for[;", "fo(;", "ét(;", "ét[;", "é;", "ab[;", "callx[;", "do[;dox;", "zq;", "qzq;", "zq(;", "qzq[;", "z;", "qz(;"}},
		// A terminal expression in the typed body of a left-recursive rule.
		{Name: "terminal in left recursion", Src: `rule main = e $$
rule e = e -([aA] [bB] / [cC] [dD] / [eE] [fF] / [gG] [hH] / [iI] [jJ] / [kK] [lL] / [mM] [nN] / [oO] [pP]) [0-9] / [0-9]
`, Inputs: []string{"1", "1ab2", "1AB2cd3", "1ab", "1a2", "1x2", "", "1op9OP0", "1ab2\xff"}},
		// Literals of several lengths and an empty one, Unicode, options and lookaheads inside
		// the expression, and alternatives that no guard covers (they begin with an option or a
		// lookahead); the first alternative that matches wins even where a later one is longer.
		{Name: "terminal literals and lookaheads", Src: `rule main = (-op / w)* $$
rule op = ("<<=" / "<<" / "<=" / "<" / ">>>=" / ">>>" / ">>=" / ">>" / ">=" / ">" / "=" !"=" / "==" "="? / "é" "日本"? / !"!" "!" "=" / [α-ω] [^a-z]? / ("+" / "-")? "#" / "" "?" "?"?) !"~"
rule w = [a-z ]+ #atomic
`, Inputs: []string{"a<<=b", "<<", "<=<", ">>>=>>>>", "==", "===", "=", "é日本", "éé", "!=", "!", "α", "αa", "ωω", "+#", "#", "-#-", "??", "?", "<~", "~", "", "a b c", "<<~", "日", "\xff", "é\xff", "<\xe6\x97"}},
	}
	// A choice whose dispatch table would have more entries than it can index (1200 guards
	// that admit 127 ASCII characters each) tests its guards in turn.
	var huge strings.Builder
	huge.WriteString("rule main = (!kw [a-z0-9]+ \";\")* $$\nrule kw = ")
	for i := range 1200 {
		if i > 0 {
			huge.WriteString(" / ")
		}
		fmt.Fprintf(&huge, "[^X] \"k%04d\"", i)
	}
	huge.WriteString("\n")
	cases = append(cases, gentest.Case{Name: "terminal choice without a table", Src: huge.String(), Inputs: []string{"ab;", "ak0001;", "zk1199;", "ak1200;", "Xk0001;", "k0000;", "1k0500x;", "ak;", "", "ab", "\xffk0001;", "ék0002;"}})
	// Random inputs over the characters the grammars mention.
	alphabet := []rune("abcdeklorstuhnfACBOLEX_ ;([é日本<=>!α~+-#?xy")
	rng := rand.New(rand.NewSource(1))
	for i := range cases {
		for range 150 {
			var b strings.Builder
			for range rng.Intn(14) {
				b.WriteRune(alphabet[rng.Intn(len(alphabet))])
			}
			cases[i].Inputs = append(cases[i].Inputs, b.String())
		}
	}
	return cases
}

func TestTerminalProgramGeneration(t *testing.T) {
	t.Parallel()
	// Terminal programs are for parsers without budget controls.
	gentest.PinOrdinary(t)
	for _, c := range terminalCases() {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "terminal", Start: "main", Types: true, TypedOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		// The frame body of a left-recursive rule keeps straight-line code.
		if got, want := strings.Contains(string(code), "p.SkipTerminal(term"), !strings.Contains(c.Name, "left recursion"); got != want {
			t.Errorf("%s: terminal program %v, want %v", c.Name, got, want)
		}
		reference, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "terminal", Start: "main", Types: true, TypedOnly: true, DisableTerminalPrograms: true})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(reference), "SkipTerminal") {
			t.Errorf("%s: the reference has a terminal program", c.Name)
		}
		// Parsers with budget controls or Documents keep the straight-line code, which charges
		// budget for each expression.
		for _, opts := range []gen.GenOptions{{BudgetControls: true}, {Document: true}} {
			opts.Package, opts.Module, opts.Start, opts.Types, opts.Runtime = "terminal", "example.com/generated", "main", true, gen.RuntimeImport
			files, err := gentest.Generate(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(gentest.MergeGenerated(files)), "SkipTerminal") {
				t.Errorf("%s: terminal program with %+v", c.Name, opts)
			}
		}
	}
}

// TestTerminalProgramDisabledParity compares lean typed parsers that match terminal expressions
// through programs with the same parsers generated without them, and their errors with the
// engine's, in both position units.
func TestTerminalProgramDisabledParity(t *testing.T) {
	t.Parallel()
	gentest.PinOrdinary(t)
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), terminalCases(), gen.GenOptions{DisableTerminalPrograms: true}, 0)
}

// TestTerminalProgramEligibility pins where the generator lowers a terminal expression: only
// value-free ones (not where its text is a value), with a choice and at least minTerminalLeaves
// literals and classes, in lean typed parsers without observability; a program is static data
// prepared once at initialization.
func TestTerminalProgramEligibility(t *testing.T) {
	t.Parallel()
	gentest.PinOrdinary(t)
	// 16 leaves: eight two-class alternatives.
	choice16 := `([aA] [bB] / [cC] [dD] / [eE] [fF] / [gG] [hH] / [iI] [jJ] / [kK] [lL] / [mM] [nN] / [oO] [pP])`
	choice15 := `([aA] [bB] / [cC] [dD] / [eE] [fF] / [gG] [hH] / [iI] [jJ] / [kK] [lL] / [mM] [nN] / [oO])`
	seq16 := `[aA] [bB] [cC] [dD] [eE] [fF] [gG] [hH] [iI] [jJ] [kK] [lL] [mM] [nN] [oO] [pP]`
	for _, c := range []struct {
		name, src string
		opts      gen.GenOptions
		want      bool
	}{
		{"discarded", "rule main = -" + choice16 + " $$\n", gen.GenOptions{}, true},
		{"lookahead", "rule main = !" + choice16 + " [a-z]* $$\n", gen.GenOptions{}, true},
		{"text is the value", "rule main = " + choice16 + " $$\n", gen.GenOptions{}, false},
		{"captured", "rule main = x:" + choice16 + " $$ => $x\n", gen.GenOptions{}, false},
		{"too few leaves", "rule main = -" + choice15 + " $$\n", gen.GenOptions{}, false},
		{"no choice", "rule main = -(" + seq16 + ") $$\n", gen.GenOptions{}, false},
		{"observability", "rule main = -" + choice16 + " $$\n", gen.GenOptions{Observability: true}, false},
		{"budget controls", "rule main = -" + choice16 + " $$\n", gen.GenOptions{BudgetControls: true}, false},
		{"document", "rule main = -" + choice16 + " $$\n", gen.GenOptions{Document: true}, false},
		{"disabled", "rule main = -" + choice16 + " $$\n", gen.GenOptions{DisableTerminalPrograms: true}, false},
		// The typed body of a left-recursive rule is a frame body, which keeps straight-line code.
		{"left recursion", "rule main = e $$\nrule e = e -" + choice16 + " [0-9] / [0-9]\n", gen.GenOptions{}, false},
		// Streaming parses run the Node bodies; only the typed bodies of ParseAST hold programs.
		{"stream", "rule main = (-" + choice16 + " w:[0-9]+ \";\")* #stream $$\n", gen.GenOptions{Stream: true}, true},
	} {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		opts := c.opts
		opts.Package, opts.Module, opts.Start, opts.Types, opts.TypedOnly, opts.Runtime = "terminal", "example.com/generated", "main", true, true, gen.RuntimeImport
		if opts.Document || opts.Stream {
			opts.TypedOnly = false
		}
		files, err := gentest.Generate(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code := string(gentest.MergeGenerated(files))
		if got := strings.Contains(code, "SkipTerminal("); got != c.want {
			t.Errorf("%s: lowered %v, want %v", c.name, got, c.want)
		}
		if c.want && !regexp.MustCompile(`(?m)^var (term\d+)Data = \w+\.TerminalProgram\{Code: .*\n^var (term\d+) = (term\d+)Data\.Prepare\(\)$`).MatchString(code) {
			t.Errorf("%s: the program is not static data prepared at initialization", c.name)
		}
		fn := ""
		for i, line := range strings.Split(code, "\n") {
			if strings.HasPrefix(line, "func ") {
				fn = line
			}
			if strings.Contains(line, "SkipTerminal(") && !strings.Contains(fn, "(p *rt.TParser)") {
				t.Errorf("%s:%d: a program outside a typed body: %s", c.name, i+1, fn)
			}
		}
	}
}

// TestDepthChecks pins what the guards of terminal programs rely on when they leave out the
// depth limit of the straight-line guards (p.Depth <= p.MaxDepth): every increment of p.Depth,
// in the runtime and in generated code, is followed by the check that panics past p.MaxDepth,
// and nothing else raises p.Depth or changes p.MaxDepth during a parse.
func TestDepthChecks(t *testing.T) {
	t.Parallel()
	gentest.PinOrdinary(t)
	sources := map[string]string{}
	files, err := filepath.Glob("../../../runtime/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("runtime sources: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sources[f] = string(b)
	}
	for _, c := range terminalCases() {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []gen.GenOptions{{TypedOnly: true}, {BudgetControls: true}} {
			opts.Package, opts.Module, opts.Start, opts.Types, opts.Runtime = "terminal", "example.com/generated", "main", true, gen.RuntimeImport
			files, err := gentest.Generate(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			sources[fmt.Sprintf("%s %+v", c.Name, opts.BudgetControls)] = string(gentest.MergeGenerated(files))
		}
	}
	other := regexp.MustCompile(`\.Depth\s*(\+=|=[^=])|\.MaxDepth\s*(\+\+|--|\+=|-=|=[^=])`)
	increments := 0
	for name, src := range sources {
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			if strings.Contains(line, "Depth++") {
				increments++
				if strings.TrimSpace(line) != "p.Depth++" || i+2 >= len(lines) || strings.TrimSpace(lines[i+1]) != "if p.Depth > p.MaxDepth {" ||
					!(strings.TrimSpace(lines[i+2]) == "p.TooDeep()" || strings.HasPrefix(strings.TrimSpace(lines[i+2]), "panic(fatal{")) {
					t.Errorf("%s:%d: p.Depth++ without the depth check", name, i+1)
				}
				continue
			}
			if m := other.FindString(line); m != "" && strings.TrimSpace(line) != "p.MaxDepth = o.maxDepth" {
				t.Errorf("%s:%d: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
	if increments == 0 {
		t.Errorf("no depth increments found")
	}
}
