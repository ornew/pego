package gogen_test

import (
	"math/rand"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// guardedClassCases are grammars whose choices have alternatives that begin with character
// classes, including classes of characters that take several bytes, whose values are built,
// captured or discarded.
func guardedClassCases() []gentest.Case {
	invalidUTF8 := []string{"", " ", "\xffa", "\xff", "\xffb", "\xffc", "\xe6\x97", "\xe6\x97a", "\xe6\x97c", "日\xe6\x97", "日\xe6", "日本", "a\xff", "a\xe6\x97", "a", "\xc3", "\xc3b", "\xef\xbf\xbda", "\xef\xbf\xbd", "\xed\xa0\x80c", "\xf4\x90\x80\x80b", "\x80\x80c", "~c", "~"}
	cases := []gentest.Case{
		{Name: "guarded classes with values", Src: `rule main = (item / -" ")* $$
rule item = [a-zé] [0-9]* / [α-ω] "!" / x:[日本] y:"語"? / [^a-z0-9 !] / "q"
`, Inputs: []string{"a1 é22 α! 日語 本 ~", "é", "α", "α?", "日本語", "", "1", "!", "\xff", "a\xff", "ω!日", "  "}},
		{Name: "guarded classes without values", Src: `rule main = (-word / -" ")* $$
rule word = [é] "x" / [日] "y" [α-ω]? / [a-c] ([d-f] / [é] "!") / [^ ]
`, Inputs: []string{"éx 日y 日yα ad aé! b", "é", "日", "aé", "a", "\xffx", "日\xff", "", "zz"}},
		// Every guarded alternative begins with a run of classes, which ClassesSkip matches after
		// the guarded class.
		{Name: "guarded class runs", Src: `rule main = (-([a-c] [d-f] [g-h] / [é-ë] [0-9] [0-9] / [x-z] [é]) / -" ")* $$
`, Inputs: []string{"adg é12 xé", "ad", "é1", "é1x", "x", "xe", "bfh  cdg", "", "\xff", "é\xff"}},
		// Guards that decode invalid UTF-8 and truncated sequences, each of which is one U+FFFD:
		// a class that contains U+FFFD, a negated class that admits it, and classes of multibyte
		// characters at the end of the input.
		{Name: "guarded classes of invalid UTF-8", Src: `rule main = (item / -" ")* $$
rule item = [\uFFFD] "a" / [\uFFFD-\u{10FFFF}] [é]? "b" / [^a-z ] "c" / x:[日] y:[本]? / [a-z] [\uFFFD]
`, Inputs: invalidUTF8},
		{Name: "guarded classes of invalid UTF-8 without values", Src: `rule main = (-item / -" ")* $$
rule item = [\uFFFD] "a" / [\uFFFD-\u{10FFFF}] [é]? "b" / [^a-z ] "c" / [日] [本]? / [a-z] [\uFFFD]
`, Inputs: invalidUTF8},
	}
	// The pieces of random inputs: characters, invalid bytes and truncated sequences.
	alphabet := append(strings.Split("abcdefghqxyz019 !~éëα日本語\uFFFD", ""), "\xff", "\x80", "\xe6\x97", "\xc3")
	rng := rand.New(rand.NewSource(2))
	for i := range cases {
		for range 150 {
			var b strings.Builder
			for range rng.Intn(10) {
				b.WriteString(alphabet[rng.Intn(len(alphabet))])
			}
			cases[i].Inputs = append(cases[i].Inputs, b.String())
		}
	}
	return cases
}

func TestGuardedClassGeneration(t *testing.T) {
	t.Parallel()
	gentest.PinOrdinary(t)
	for _, c := range guardedClassCases() {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		for _, disabled := range []bool{false, true} {
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "guarded", Start: "main", Types: true, TypedOnly: true, DisableGuardedClasses: disabled})
			if err != nil {
				t.Fatal(err)
			}
			// A class that the guard has matched consumes the character by the size the guard kept.
			if got := strings.Contains(string(code), "p.Pos += x"); got == disabled {
				t.Errorf("%s (disabled %v): a class consumes the guard's character: %v", c.Name, disabled, got)
			}
		}
	}
}

// TestGuardedClassDisabledParity compares parsers in which a guard's character is consumed by
// the first class of the alternative with parsers in which the class reads and tests it again,
// in both position units, and their errors with the engine's.
func TestGuardedClassDisabledParity(t *testing.T) {
	t.Parallel()
	gentest.PinOrdinary(t)
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), guardedClassCases(), gen.GenOptions{DisableGuardedClasses: true}, 0)
}

// TestGuardedClassEligibility checks that parsers that spend budget for reading (budget controls or
// Documents) keep the class reading and testing the character again.
func TestGuardedClassEligibility(t *testing.T) {
	t.Parallel()
	for _, c := range guardedClassCases() {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []gen.GenOptions{{TypedOnly: true}, {TypedOnly: true, BudgetControls: true}, {Types: true, BudgetControls: true}, {Types: true, Document: true}} {
			want := !opts.BudgetControls && !opts.Document
			opts.Package, opts.Module, opts.Start, opts.Types, opts.Runtime = "guarded", "example.com/generated", "main", true, gen.RuntimeImport
			files, err := gentest.Generate(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(gentest.MergeGenerated(files)), "p.Pos += x"); got != want {
				t.Errorf("%s (budget controls %v, document %v): a class consumes the guard's character: %v, want %v", c.Name, opts.BudgetControls, opts.Document, got, want)
			}
		}
	}
}
