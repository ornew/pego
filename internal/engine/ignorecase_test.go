package engine

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/value"
)

// ignoreCaseCase is one parse of a grammar whose main rule has case-insensitive literals. The
// result is "ok" with the matched text, or "error" when the parse fails.
type ignoreCaseCase struct {
	name, src, input, want string
}

// ignoreCaseOutcome parses input with the closure backend and returns "error", or "ok" with the
// consumed input. A rule without an action has no text of its own, so the span is read from the input.
func ignoreCaseOutcome(t *testing.T, program *Program, input string, unit PositionUnit) string {
	t.Helper()
	n, err := program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit})
	if err != nil {
		return "error"
	}
	if unit == Bytes {
		return "ok " + input[n.Start():n.End()]
	}
	runes := []rune(input)
	return "ok " + string(runes[n.Start():n.End()])
}

func TestIgnoreCaseLiterals(t *testing.T) {
	t.Parallel()
	cases := []ignoreCaseCase{
		{"suffix matches any case", `rule main = "select"i`, "SELECT", "ok SELECT"},
		{"suffix matches mixed case", `rule main = "select"i`, "sElEcT", "ok sElEcT"},
		{"suffix matches the written case", `rule main = "select"i`, "select", "ok select"},
		{"suffix rejects a shorter input", `rule main = "select"i`, "selec", "error"},
		{"suffix rejects a different letter", `rule main = "select"i`, "SELECX", "error"},
		{"suffix rejects extra input", `rule main = "select"i`, "SELECTS", "error"},
		{"first-character guard of a choice folds", `rule main = ("ab"i "c" / "AB" "d")`, "ABc", "ok ABc"},
		{"first-character guard of a choice, other alternative", `rule main = ("ab"i "c" / "AB" "d")`, "ABd", "ok ABd"},
		{"non-ASCII letters match themselves", `rule main = "é"i`, "é", "ok é"},
		{"non-ASCII letters are not folded", `rule main = "é"i`, "É", "error"},
		{"Kelvin sign is not folded to k", `rule main = "k"i`, "K", "error"},
		{"ASCII letters of a non-ASCII literal fold", `rule main = "café"i`, "CAFé", "ok CAFé"},
		{"pragma annotates a group", "rule main = (\"ab\" \"cd\") #ignorecase", "ABcd", "ok ABcd"},
		{"pragma annotates a single literal", `rule main = "ab" #ignorecase "cd"`, "ABcd", "ok ABcd"},
		{"pragma leaves the next literal exact", `rule main = "ab" #ignorecase "cd"`, "ABCD", "error"},
		{"pragma does not reach a called rule", "rule main = c #ignorecase\nrule c = \"ab\"", "AB", "error"},
		{"pragma does not reach a called rule when it matches", "rule main = c #ignorecase\nrule c = \"ab\"", "ab", "ok ab"},
		{"pragma does not change a character class", `rule main = [a] #ignorecase`, "A", "error"},
		{"pragma with a redundant suffix", `rule main = "select"i #ignorecase`, "SeLeCt", "ok SeLeCt"},
		{"pragma under negative lookahead", "rule main = (\"x\" !\"ab\" .*) #ignorecase", "XAB", "error"},
		{"pragma under negative lookahead, other input", "rule main = (\"x\" !\"ab\" .*) #ignorecase", "XAC", "ok XAC"},
		{"no pragma keeps negative lookahead exact", "rule main = \"x\" !\"ab\" .*", "xAB", "ok xAB"},
		{"pragma under positive lookahead", "rule main = (&\"ab\" \"a\") #ignorecase \"B\"", "AB", "ok AB"},
		{"suffix needs no space before the next item", `rule main = "a"i"b"`, "Ab", "ok Ab"},
		{"suffix leaves the next literal exact", `rule main = "a"i"b"`, "AB", "error"},
		{"space before i makes a reference", "rule main = \"a\" i\nrule i = \"x\"", "AX", "error"},
		{"space before i makes a reference, matching", "rule main = \"a\" i\nrule i = \"x\"", "ax", "ok ax"},
		{"identifier starting with i is not a suffix", "rule main = \"a\"ia\nrule ia = \"b\"", "AB", "error"},
		{"identifier starting with i is not a suffix, matching", "rule main = \"a\"ia\nrule ia = \"b\"", "aB", "error"},
		{"identifier starting with i is not a suffix, exact", "rule main = \"a\"ia\nrule ia = \"b\"", "ab", "ok ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			program := mustCompile(t, c.src)
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				if got := ignoreCaseOutcome(t, program, c.input, unit); got != c.want {
					t.Fatalf("%s input %q: got %s, want %s", unit, c.input, got, c.want)
				}
			}
		})
	}
}

// TestIgnoreCaseMatchesClassForm checks the suffix against the character-class spelling of the
// same keyword on the same inputs, including inputs that differ from the keyword in one letter.
func TestIgnoreCaseMatchesClassForm(t *testing.T) {
	t.Parallel()
	folded := mustCompile(t, `rule main = "select"i`)
	classes := mustCompile(t, `rule main = [sS][eE][lL][eE][cC][tT]`)
	inputs := []string{"select", "SELECT", "SeLeCt", "selec", "selects", "SELECX", "sElEcT", "xelect", "", "s", "seleé", "SELEK"}
	for _, input := range inputs {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			a := ignoreCaseOutcome(t, folded, input, unit)
			b := ignoreCaseOutcome(t, classes, input, unit)
			if a != b {
				t.Fatalf("%s input %q: suffix %s, class form %s", unit, input, a, b)
			}
		}
	}
}

// TestIgnoreCaseExpectations checks that an expectation shows a case-insensitive literal with its
// suffix, including the literal that the pragma makes case-insensitive, and that the other literals
// keep their spelling.
func TestIgnoreCaseExpectations(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, src, input string
		contains         []string
		absent           []string
	}{
		{"suffix", `rule main = "select"i`, "x", []string{`"select"i`}, nil},
		{"pragma", `rule main = ("select") #ignorecase`, "x", []string{`"select"i`}, []string{`"select" `}},
		{"plain literal", `rule main = "select"`, "x", []string{`"select"`}, []string{`"select"i`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			program := mustCompile(t, c.src)
			_, err := program.ParseWith("main", c.input, ParseOptions{Backend: Closure})
			if err == nil {
				t.Fatal("the parse succeeded")
			}
			for _, s := range c.contains {
				if !strings.Contains(err.Error(), s) {
					t.Fatalf("error %q does not contain %s", err, s)
				}
			}
			for _, s := range c.absent {
				if strings.Contains(err.Error(), s) {
					t.Fatalf("error %q contains %s", err, s)
				}
			}
		})
	}
}

// TestIgnoreCaseOperatorsRule checks that the pragma reaches the operator literals written in the
// annotated expression of an Operators Rule.
func TestIgnoreCaseOperatorsRule(t *testing.T) {
	t.Parallel()
	src := `rule main = e:expr $$
rule expr = #operators {
    operand [0-9]
    level { infix left "plus"i }
}
`
	program := mustCompile(t, src)
	for _, c := range []struct{ input, want string }{
		{"1PLUS2", "ok 1PLUS2"},
		{"1plus2", "ok 1plus2"},
		{"1PlUs2", "ok 1PlUs2"},
		{"1+2", "error"},
	} {
		if got := ignoreCaseOutcome(t, program, c.input, CodePoints); got != c.want {
			t.Fatalf("input %q: got %s, want %s", c.input, got, c.want)
		}
	}
}

// TestIgnoreCaseOperatorsRulePragma checks that #ignorecase after the block of an Operators Rule
// makes the literals of its operands, skip and operators case-insensitive, and that it is
// accepted only as that pragma.
func TestIgnoreCaseOperatorsRulePragma(t *testing.T) {
	t.Parallel()
	src := `rule main = e:expr $$
rule expr = #operators {
    skip " "*
    operand "one" / [0-9]
    level { infix left "plus" }
} #ignorecase
`
	program := mustCompile(t, src)
	for _, c := range []struct{ input, want string }{
		{"ONE PLUS 1", "ok ONE PLUS 1"},
		{"one plus one", "ok one plus one"},
		{"1PlUs2", "ok 1PlUs2"},
		{"1+2", "error"},
	} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			if got := ignoreCaseOutcome(t, program, c.input, unit); got != c.want {
				t.Fatalf("%s input %q: got %s, want %s", unit, c.input, got, c.want)
			}
		}
		checkBackends(t, program, "main", c.input)
	}
	// Without the pragma the same rule is exact.
	exact := mustCompile(t, strings.Replace(src, "} #ignorecase", "}", 1))
	if got := ignoreCaseOutcome(t, exact, "1PLUS2", CodePoints); got != "error" {
		t.Fatalf("without the pragma: got %s, want error", got)
	}
	for _, bad := range []string{
		"rule expr = #operators { operand [0-9] } #atomic\n",
		"rule expr = #operators { operand [0-9] } #missing\n",
	} {
		if _, err := parseTest("rule main = expr $$\n" + bad); err == nil || !strings.Contains(err.Error(), "only #ignorecase may follow an Operators Rule") {
			t.Fatalf("%q: got error %v", bad, err)
		}
	}
}

// TestIgnoreCaseBackendsAgree checks that the bytecode backends produce the closure backend's results
// for case-insensitive literals, and that a serialized program loads with the same results in every
// mode. The plain literal keeps working in the same programs.
func TestIgnoreCaseBackendsAgree(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		src    string
		inputs []string
	}{
		{`rule main = "select"i`, []string{"SELECT", "sElEcT", "selec", "SELECTS", ""}},
		{`rule main = ("ab"i "c" / "AB" "d")`, []string{"ABc", "abd", "ABd", "Abx"}},
		{`rule main = "é"i`, []string{"é", "É"}},
		{`rule main = "café"i`, []string{"CAFé", "CAFÉ", "cafe"}},
		{"rule main = (\"x\" !\"ab\" .*) #ignorecase", []string{"XAB", "XAC", "xab"}},
		{`rule main = "a"i"b"`, []string{"Ab", "AB"}},
		{`rule main = "k"i`, []string{"K", "k", "\u212a"}},
		{`rule main = "plain" "Mixed"i`, []string{"plainmixed", "plainMIXED", "PLAINMIXED"}},
	} {
		prog := mustCompile(t, c.src)
		folded := 0
		for _, in := range prog.Module().Code {
			if in.Op == bytecode.OpStrFold {
				folded++
			}
		}
		if folded == 0 {
			t.Fatalf("%s: no folded string instruction", c.src)
		}
		for _, in := range c.inputs {
			checkBackends(t, prog, "main", in)
		}
		checkCompiledRoundTrip(t, prog, c.inputs)
	}
}

// TestIgnoreCaseRandomAgreement compares the closure and bytecode backends on seeded random inputs
// over letters of both cases, non-ASCII letters, and the Kelvin sign, which is not ASCII.
func TestIgnoreCaseRandomAgreement(t *testing.T) {
	t.Parallel()
	alphabet := []rune("aAbBkK\u00e9\u00c9\u212ac")
	rng := rand.New(rand.NewSource(79))
	programs := []*Program{
		mustCompile(t, `rule main = ("ab"i "k" / "AK"i "c")+ "é"i`),
		mustCompile(t, "rule main = (\"Ab\" \"Kk\"i) #ignorecase"),
		mustCompile(t, `rule main = ("kk"i !"c"i .) / "AB"`),
	}
	for _, prog := range programs {
		for i := 0; i < 300; i++ {
			n := rng.Intn(9)
			in := make([]rune, n)
			for j := range in {
				in[j] = alphabet[rng.Intn(len(alphabet))]
			}
			checkBackends(t, prog, "main", string(in))
		}
	}
}

// TestIgnoreCaseMissingSpelling checks that #missing inserts the literal as written, without the
// suffix and without a change of case.
func TestIgnoreCaseMissingSpelling(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "Sel"i #missing $$`)
	n, errs := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 2}, 1)
	insertions := missingNodes(t, n)
	if len(insertions) != 1 {
		t.Fatalf("missing nodes: %v", insertions)
	}
	if expected, _ := value.D(insertions[0]).Fields.Get("expected"); expected != "Sel" {
		t.Fatalf("inserted spelling %q, want Sel (errors %v)", expected, errs)
	}
}

// TestIgnoreCaseMissingSpellingBackends checks that the closure backend, the only one that recovers
// from missing tokens, inserts the spelling written in a case-insensitive literal for #missing, from
// the suffix and from the pragma, and that the expectation keeps the suffix. The bytecode backends
// reject missing-token recovery.
func TestIgnoreCaseMissingSpellingBackends(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = "Sel"i #missing $$`,
		`rule main = ("Sel" #missing $$) #ignorecase`,
		`rule main = "Sel" #missing #ignorecase $$`,
	} {
		prog := mustCompile(t, src)
		for _, backend := range []Backend{Closure, Default} {
			n, errs := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 2, Backend: backend}, 1)
			insertions := missingNodes(t, n)
			if len(insertions) != 1 {
				t.Fatalf("%s: backend %v: missing nodes: %v", src, backend, insertions)
			}
			if expected, _ := value.D(insertions[0]).Fields.Get("expected"); expected != "Sel" {
				t.Fatalf("%s: backend %v: inserted spelling %q, want Sel (errors %v)", src, backend, expected, errs)
			}
			if !strings.Contains(fmt.Sprint(errs), `"Sel"i`) {
				t.Fatalf("%s: backend %v: expectation lacks the suffix: %v", src, backend, errs)
			}
		}
	}
}

// TestIgnoreCaseDocumentAndStream checks the incremental Document and the stream parse with
// case-insensitive literals.
func TestIgnoreCaseDocumentAndStream(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "select"i " " "x" $$`)
	doc, err := prog.NewDocument("main", "SELECT x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal("document: ", err)
	}
	if err := doc.EditBatch(doc.Version(), TextEdit{Start: 0, End: 1, Text: "s"}); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal("edited document: ", err)
	}
	if err := doc.EditBatch(doc.Version(), TextEdit{Start: 7, End: 8, Text: "y"}); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err == nil {
		t.Fatal("edited document with a wrong letter parsed")
	}

	stream := mustCompile(t, `rule main = ("ab"i #atomic)* #stream`)
	count := 0
	if err := stream.ParseStream("main", strings.NewReader("ABab"), func(*value.Node) error { count++; return nil }); err != nil {
		t.Fatal("stream: ", err)
	}
	if count != 2 {
		t.Fatalf("stream emitted %d elements, want 2", count)
	}
}

// TestIgnoreCaseMissingInPragmaScope checks that #missing inside the annotated expression inserts
// the literal as written, while the expectation of the same literal keeps the suffix.
func TestIgnoreCaseMissingInPragmaScope(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = ("Sel" #missing $$) #ignorecase`)
	n, errs := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 2}, 1)
	insertions := missingNodes(t, n)
	if len(insertions) != 1 {
		t.Fatalf("missing nodes: %v", insertions)
	}
	if expected, _ := value.D(insertions[0]).Fields.Get("expected"); expected != "Sel" {
		t.Fatalf("inserted spelling %q, want Sel (errors %v)", expected, errs)
	}
	if !strings.Contains(fmt.Sprint(errs), `"Sel"i`) {
		t.Fatalf("expectation lacks the suffix: %v", errs)
	}
}

// TestIgnoreCaseDocumentAndStreamBackends runs the incremental Document and the stream parse on
// every backend that supports them, with case-insensitive literals.
func TestIgnoreCaseDocumentAndStreamBackends(t *testing.T) {
	t.Parallel()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		prog := mustCompile(t, `rule main = "select"i " " "x" $$`)
		doc, err := prog.NewDocumentWith("main", "SELECT x", ParseOptions{Backend: b})
		if err != nil {
			t.Fatal(b, err)
		}
		if _, err := doc.Parse(); err != nil {
			t.Fatal(b, "document: ", err)
		}
		if err := doc.EditBatch(doc.Version(), TextEdit{Start: 0, End: 1, Text: "s"}); err != nil {
			t.Fatal(b, err)
		}
		if _, err := doc.Parse(); err != nil {
			t.Fatal(b, "edited document: ", err)
		}
		if err := doc.EditBatch(doc.Version(), TextEdit{Start: 7, End: 8, Text: "y"}); err != nil {
			t.Fatal(b, err)
		}
		if _, err := doc.Parse(); err == nil {
			t.Fatal(b, "edited document with a wrong letter parsed")
		}

		stream := mustCompile(t, `rule main = ("ab"i #atomic)* #stream`)
		count := 0
		if err := stream.ParseStreamWith("main", strings.NewReader("ABab"), func(*value.Node) error { count++; return nil }, ParseOptions{Backend: b}); err != nil {
			t.Fatal(b, "stream: ", err)
		}
		if count != 2 {
			t.Fatalf("%v: stream emitted %d elements, want 2", b, count)
		}
	}
}

// TestIgnoreCaseExpectationBackends checks that the expectation of a case-insensitive literal keeps
// its suffix on every backend that parses such a literal.
func TestIgnoreCaseExpectationBackends(t *testing.T) {
	t.Parallel()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, c := range []struct {
			src, want string
		}{
			{`rule main = "select"i`, `"select"i`},
			{`rule main = ("select") #ignorecase`, `"select"i`},
			{`rule main = "select"`, `"select"`},
		} {
			prog := mustCompile(t, c.src)
			_, err := prog.ParseWith("main", "x", ParseOptions{Backend: b})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%v %q: error %v, want the expectation %s", b, c.src, err, c.want)
			}
		}
	}
}
