package engine

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/limits"
	"github.com/ornew/zeroparse/internal/value"
)

// expectationCases are grammars with inputs that fail, recover or succeed.
var expectationCases = []struct {
	name, grammar string
	inputs        []string
}{
	{"literals", `rule main = "a" ("b" "c")?`, []string{"abc", "a", "abd", "ad", ""}},
	{"classes and lookahead", `rule main = [a-c]+ !"d" [0-9]*  $$`, []string{"abc", "abd", "ab9x", "x"}},
	{"labels", `rule main = item+ $$
rule item = "x" @error(message="expected an x") / "y"`, []string{"xy", "xz", "z"}},
	{"recover", `rule main = stmt* $$
rule stmt = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")`, []string{"ab;cd;", "ab;1x;cd;9;", "1x;ab"}},
	{"cut", `rule main = "(" ^ [a-z]+ ")" / [0-9]+`, []string{"(ab)", "(a", "12", "(1)"}},
	{"memo", `rule main = e $$
rule e = t ("+" t)*
rule t = "(" e ")" / [0-9]+`, []string{"1+(2+3)", "1+(2+", "1+", "(("}},
}

var expectationBackends = []Backend{Closure, Bytecode, BytecodeIterative}

// A parse without expectations succeeds with the same tree as one with them, and fails at the
// same position with no expectations and no messages.
func TestExpectationsOffKeepsResultsAndPositions(t *testing.T) {
	t.Parallel()
	for _, c := range expectationCases {
		prog := mustCompile(t, c.grammar)
		for _, in := range c.inputs {
			for _, b := range expectationBackends {
				on, onErr := prog.ParseWith("main", in, ParseOptions{Backend: b, Expectations: true})
				off, offErr := prog.ParseWith("main", in, ParseOptions{Backend: b})
				if (onErr == nil) != (offErr == nil) {
					t.Fatalf("%s %q %v: on %v, off %v", c.name, in, b, onErr, offErr)
				}
				if onErr == nil {
					if a, w := dumpNode(off), dumpNode(on); a != w {
						t.Errorf("%s %q %v: tree differs\n off %s\n on  %s", c.name, in, b, a, w)
					}
					continue
				}
				var onList, offList SyntaxErrors
				if errors.As(onErr, &onList) && errors.As(offErr, &offList) {
					// Recovered errors: the tree, and the position of each error, are the same.
					if a, w := dumpNode(off), dumpNode(on); a != w || len(onList) != len(offList) {
						t.Errorf("%s %q %v: recovered tree differs\n off %s\n on  %s", c.name, in, b, a, w)
					}
					for i := range min(len(onList), len(offList)) {
						if onList[i].Pos != offList[i].Pos || len(offList[i].Expected) != 0 || len(offList[i].Messages) != 0 {
							t.Errorf("%s %q %v: recovered error %d: %+v off, %+v on", c.name, in, b, i, offList[i], onList[i])
						}
					}
					continue
				}
				var onSE, offSE *SyntaxError
				if !errors.As(onErr, &onSE) || !errors.As(offErr, &offSE) {
					t.Fatalf("%s %q %v: not syntax errors: %v, %v", c.name, in, b, onErr, offErr)
				}
				if offSE.Pos != onSE.Pos || offSE.Line != onSE.Line || offSE.Col != onSE.Col {
					t.Errorf("%s %q %v: position %d:%d (%d) off, %d:%d (%d) on", c.name, in, b, offSE.Line, offSE.Col, offSE.Pos, onSE.Line, onSE.Col, onSE.Pos)
				}
				if len(offSE.Expected) != 0 || len(offSE.Messages) != 0 || offSE.Message() != "syntax error" {
					t.Errorf("%s %q %v: off error records expectations: %+v", c.name, in, b, offSE)
				}
				if len(onSE.Expected)+len(onSE.Messages) == 0 {
					t.Errorf("%s %q %v: on error lists no expectation: %+v", c.name, in, b, onSE)
				}
			}
		}
	}
}

var expectedText = regexp.MustCompile(`syntax error: expected [^"\\]*(\\"[^"\\]*)*`)

// dumpNode renders a result without the expectations in the text of recovered errors, whose
// positions and spans stay.
func dumpNode(n *value.Node) string {
	return expectedText.ReplaceAllString(gentest.ResultJSON(n, nil), "syntax error")
}

// The text of recovered errors differs between modes, the tree shape and spans do not.
func TestExpectationsOffRecoveredErrorText(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, expectationCases[3].grammar)
	for _, b := range expectationBackends {
		on, _ := prog.ParseWith("main", "ab;1x;cd;", ParseOptions{Backend: b, Expectations: true})
		off, _ := prog.ParseWith("main", "ab;1x;cd;", ParseOptions{Backend: b})
		if got := gentest.ResultJSON(on, nil); !strings.Contains(got, "expected") {
			t.Errorf("%v: on: %s", b, got)
		}
		if got := gentest.ResultJSON(off, nil); strings.Contains(got, "expected") || !strings.Contains(got, "syntax error") {
			t.Errorf("%v: off: %s", b, got)
		}
	}
}

// A Document fixes the mode at construction.
func TestExpectationsDocumentFixesMode(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, expectationCases[0].grammar)
	for _, on := range []bool{false, true} {
		d, err := prog.NewDocumentWith("main", "ab", ParseOptions{Expectations: on})
		if err != nil {
			t.Fatal(err)
		}
		_, err = d.ParseWith(ParseOptions{Expectations: !on})
		if err == nil || !strings.Contains(err.Error(), "expectations") {
			t.Errorf("expectations %v: changing the mode gives %v", on, err)
		}
		_, err = d.Parse()
		var se *SyntaxError
		if !errors.As(err, &se) || (len(se.Expected) > 0) != on {
			t.Errorf("expectations %v: Document error %v", on, err)
		}
	}
}

// #missing inside @error reports the message only when expectations are recorded: the message is
// an expectation, so a parse without them reports a plain syntax error. Only the closure backend
// supports missing-token recovery; the bytecode backends reject it.
func TestExpectationsOffMissingTokenMessage(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = ("a" #missing "b") @error("m1")`)
	for _, recognize := range []bool{false, true} {
		for _, on := range []bool{false, true} {
			_, err := prog.ParseWith("main", "b", ParseOptions{Expectations: on, MissingTokens: 5, Recognize: recognize})
			if err == nil {
				t.Fatalf("recognize=%v expectations=%v: no error", recognize, on)
			}
			got := err.Error()
			if on != strings.Contains(got, "m1") {
				t.Errorf("recognize=%v expectations=%v: %q", recognize, on, got)
			}
			if !on && strings.Contains(got, "expected") {
				t.Errorf("recognize=%v expectations=%v: %q lists an expectation", recognize, on, got)
			}
		}
	}
}

// longExpectedGrammar fails after its first character with ten expected entries ("t"), eleven
// ("e") and fifteen ("w"), of every kind, and recovers from eleven ("r").
const longExpectedGrammar = `rule main = "t" ten $$ / "e" eleven $$ / "w" wide $$ / "r" (eleven ";") #recover(skip=[^;]* ";") $$
rule ten = "a" / "b" / "c" / "d" / "e" / "f" / "g" / [0-9] / [x-z] / $
rule eleven = ten / "h"
rule wide = eleven / ^ "q" / "é" / "😀" / [A-Z]`

// A syntax error lists its expected entries as names, then literals, then classes, and its
// message shows the first ten of them; Expected keeps all of them.
func TestLongExpectedLists(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, longExpectedGrammar)
	ten := []string{"end of line", `"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`, `"g"`, "[0-9]", "[x-z]"}
	eleven := []string{"end of line", `"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`, `"g"`, `"h"`, "[0-9]", "[x-z]"}
	wide := []string{"beginning of line", "end of line", `"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`, `"g"`, `"h"`, `"é"`, `"😀"`, "[0-9]", "[A-Z]", "[x-z]"}
	for _, c := range []struct {
		input    string
		expected []string
		message  string
	}{
		{"t!", ten, `1:2: syntax error: expected end of line, "a", "b", "c", "d", "e", "f", "g", [0-9], [x-z]`},
		{"e!", eleven, `1:2: syntax error: expected end of line, "a", "b", "c", "d", "e", "f", "g", "h", [0-9], ... and 1 more`},
		{"w!", wide, `1:2: syntax error: expected beginning of line, end of line, "a", "b", "c", "d", "e", "f", "g", "h", ... and 5 more`},
		{"r!;", eleven, `1:2: syntax error: expected end of line, "a", "b", "c", "d", "e", "f", "g", "h", [0-9], ... and 1 more`},
	} {
		for _, b := range expectationBackends {
			// A budget limit sorts and formats under budget controls, with the same result.
			for _, l := range []limits.Limits{{}, {Budget: 1 << 50}} {
				n, err := prog.ParseWith("main", c.input, ParseOptions{Backend: b, Expectations: true, Limits: l})
				var se *SyntaxError
				var list SyntaxErrors
				if errors.As(err, &list) && len(list) == 1 {
					se = list[0]
					if text := gentest.ResultJSON(n, nil); !strings.Contains(text, strconv.Quote(c.message)) {
						t.Errorf("%q %v %v: recovered tree lacks the message: %s", c.input, b, l, text)
					}
				} else if !errors.As(err, &se) {
					t.Fatalf("%q %v %v: %v", c.input, b, l, err)
				}
				if se.Error() != c.message {
					t.Errorf("%q %v %v: message\n got %s\nwant %s", c.input, b, l, se.Error(), c.message)
				}
				if strings.Join(se.Expected, "|") != strings.Join(c.expected, "|") {
					t.Errorf("%q %v %v: Expected %q, want %q", c.input, b, l, se.Expected, c.expected)
				}
			}
		}
	}
}
