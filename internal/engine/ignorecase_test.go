package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
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
			program := compile(t, c.src)
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
	folded := compile(t, `rule main = "select"i`)
	classes := compile(t, `rule main = [sS][eE][lL][eE][cC][tT]`)
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
			program := compile(t, c.src)
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
	src := `rule main = e:expr $$
rule expr = operators {
    operand [0-9]
    level { infix left "plus"i }
}
`
	program := compile(t, src)
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

// TestIgnoreCaseBackendsUnsupported checks that the backends that do not support case-insensitive
// literals yet refuse the program, and that a program without them keeps its backends.
func TestIgnoreCaseBackendsUnsupported(t *testing.T) {
	folded := compile(t, `rule main = "select"i`)
	if _, err := folded.ParseWith("main", "SELECT", ParseOptions{Backend: Bytecode}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("bytecode parse: %v", err)
	}
	if _, err := folded.Marshal("main", CompiledParse); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("compiled grammar: %v", err)
	}
	plain := compile(t, `rule main = "select"`)
	if n, err := plain.ParseWith("main", "select", ParseOptions{Backend: Bytecode}); err != nil || n.Text() != "select" {
		t.Fatalf("bytecode parse of a plain literal: %v", err)
	}
}

// TestIgnoreCaseMissingSpelling checks that #missing inserts the literal as written, without the
// suffix and without a change of case.
func TestIgnoreCaseMissingSpelling(t *testing.T) {
	prog := compile(t, `rule main = "Sel"i #missing $$`)
	n, errs := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 2}, 1)
	insertions := missingNodes(t, n)
	if len(insertions) != 1 {
		t.Fatalf("missing nodes: %v", insertions)
	}
	if expected, _ := insertions[0].fields.Get("expected"); expected != "Sel" {
		t.Fatalf("inserted spelling %q, want Sel (errors %v)", expected, errs)
	}
}

// TestIgnoreCasePragmaReachesArguments checks that the pragma folds the literals of a recovery
// skip expression written in the annotated expression, and that the skip is not a called rule.
func TestIgnoreCasePragmaReachesArguments(t *testing.T) {
	prog := compile(t, `rule main = ("ab" #recover(skip="x")) #ignorecase`)
	found := map[string]bool{}
	walkExpr(prog.byName["main"].def.Expr, func(e grammar.Expr) {
		if l, ok := e.(*grammar.Literal); ok {
			found[l.Value] = l.IgnoreCase
		}
	})
	if !found["ab"] || !found["x"] {
		t.Fatalf("literals under the pragma are not case-insensitive: %v", found)
	}
}

// TestIgnoreCaseDocumentAndStream checks the incremental Document and the stream parse with
// case-insensitive literals.
func TestIgnoreCaseDocumentAndStream(t *testing.T) {
	prog := compile(t, `rule main = "select"i " " "x" $$`)
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

	stream := compile(t, `rule main = ("ab"i #atomic)* #stream`)
	count := 0
	if err := stream.ParseStream("main", strings.NewReader("ABab"), func(*Node) error { count++; return nil }); err != nil {
		t.Fatal("stream: ", err)
	}
	if count != 2 {
		t.Fatalf("stream emitted %d elements, want 2", count)
	}
}
