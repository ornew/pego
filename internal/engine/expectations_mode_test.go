package engine

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
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
