package engine

import (
	"math/rand"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// guardedClassCases are grammars whose choices have alternatives that begin with character
// classes, including classes of characters that take several bytes, whose values are built,
// captured or discarded.
func guardedClassCases() []genCase {
	invalidUTF8 := []string{"", " ", "\xffa", "\xff", "\xffb", "\xffc", "\xe6\x97", "\xe6\x97a", "\xe6\x97c", "日\xe6\x97", "日\xe6", "日本", "a\xff", "a\xe6\x97", "a", "\xc3", "\xc3b", "\xef\xbf\xbda", "\xef\xbf\xbd", "\xed\xa0\x80c", "\xf4\x90\x80\x80b", "\x80\x80c", "~c", "~"}
	cases := []genCase{
		{"guarded classes with values", `rule main = (item / -" ")* $$
rule item = [a-zé] [0-9]* / [α-ω] "!" / x:[日本] y:"語"? / [^a-z0-9 !] / "q"
`, []string{"a1 é22 α! 日語 本 ~", "é", "α", "α?", "日本語", "", "1", "!", "\xff", "a\xff", "ω!日", "  "}},
		{"guarded classes without values", `rule main = (-word / -" ")* $$
rule word = [é] "x" / [日] "y" [α-ω]? / [a-c] ([d-f] / [é] "!") / [^ ]
`, []string{"éx 日y 日yα ad aé! b", "é", "日", "aé", "a", "\xffx", "日\xff", "", "zz"}},
		// Every guarded alternative begins with a run of classes, which ClassesSkip matches after
		// the guarded class.
		{"guarded class runs", `rule main = (-([a-c] [d-f] [g-h] / [é-ë] [0-9] [0-9] / [x-z] [é]) / -" ")* $$
`, []string{"adg é12 xé", "ad", "é1", "é1x", "x", "xe", "bfh  cdg", "", "\xff", "é\xff"}},
		// Guards that decode invalid UTF-8 and truncated sequences, each of which is one U+FFFD:
		// a class that contains U+FFFD, a negated class that admits it, and classes of multibyte
		// characters at the end of the input.
		{"guarded classes of invalid UTF-8", `rule main = (item / -" ")* $$
rule item = [\uFFFD] "a" / [\uFFFD-\u{10FFFF}] [é]? "b" / [^a-z ] "c" / x:[日] y:[本]? / [a-z] [\uFFFD]
`, invalidUTF8},
		{"guarded classes of invalid UTF-8 without values", `rule main = (-item / -" ")* $$
rule item = [\uFFFD] "a" / [\uFFFD-\u{10FFFF}] [é]? "b" / [^a-z ] "c" / [日] [本]? / [a-z] [\uFFFD]
`, invalidUTF8},
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
			cases[i].inputs = append(cases[i].inputs, b.String())
		}
	}
	return cases
}

func TestGuardedClassGeneration(t *testing.T) {
	pinOrdinary(t)
	for _, c := range guardedClassCases() {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, disabled := range []bool{false, true} {
			code, err := generateCode(t, g, gen.GenOptions{Package: "guarded", Start: "main", Types: true, TypedOnly: true, DisableGuardedClasses: disabled})
			if err != nil {
				t.Fatal(err)
			}
			// A class that the guard has matched consumes the character by the size the guard kept.
			if got := strings.Contains(string(code), "p.Pos += x"); got == disabled {
				t.Errorf("%s (disabled %v): a class consumes the guard's character: %v", c.name, disabled, got)
			}
		}
	}
}

// TestGuardedClassDisabledParity compares parsers in which a guard's character is consumed by
// the first class of the alternative with parsers in which the class reads and tests it again,
// in both position units, and their errors with the engine's.
func TestGuardedClassDisabledParity(t *testing.T) {
	pinOrdinary(t)
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), guardedClassCases(), gen.GenOptions{DisableGuardedClasses: true}, 0)
}

// TestGuardedClassEligibility checks that parsers that charge work for reading (work controls or
// Documents) keep the class reading and testing the character again.
func TestGuardedClassEligibility(t *testing.T) {
	for _, c := range guardedClassCases() {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []gen.GenOptions{{TypedOnly: true}, {TypedOnly: true, WorkControls: true}, {Types: true, WorkControls: true}, {Types: true, Document: true}} {
			want := !opts.WorkControls && !opts.Document
			opts.Package, opts.Module, opts.Start, opts.Types, opts.Runtime = "guarded", "example.com/generated", "main", true, gen.RuntimeImport
			files, err := Generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(mergeGenerated(files)), "p.Pos += x"); got != want {
				t.Errorf("%s (work controls %v, document %v): a class consumes the guard's character: %v, want %v", c.name, opts.WorkControls, opts.Document, got, want)
			}
		}
	}
}
