package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/syntax"
)

// A hidden rule cannot be a start rule: @start on it is an error at the
// attribute, whether the program is compiled or generated, and the rules
// behind inline actions never become start rules.
func TestStartHiddenRule(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testStartHiddenRule)
}

func testStartHiddenRule(t *testing.T) {
	const hidden = "rule _x is hidden: a hidden rule cannot be a start rule"
	for _, tc := range []struct{ src, want string }{
		{"@start\nrule _x = \"x\"\n", "1:1: " + hidden},
		{"@start(default)\nrule _x = \"x\"\n", "1:1: " + hidden},
		{"@start(default)\nrule a = _x\n\n@start\nrule _x = \"x\"\n", "4:1: " + hidden},
	} {
		_, err := compileStarts(t, tc.src)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q\n got %v\nwant %s", tc.src, err, tc.want)
		}
		g, perr := syntax.Parse(tc.src)
		if perr != nil {
			t.Fatal(perr)
		}
		if _, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p"}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Generate %q\n got %v\nwant %s", tc.src, err, tc.want)
		}
		if _, err := gentest.GenerateTS(g, gen.GenOptions{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("GenerateTS %q\n got %v\nwant %s", tc.src, err, tc.want)
		}
	}
	// Negative control: the hidden rule compiles when it is not marked.
	prog, err := compileStarts(t, "@start\nrule a = _x\nrule _x = \"x\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(prog.Starts(), []string{"a"}) {
		t.Errorf("starts %v", prog.Starts())
	}
	_, err = prog.ParseWith("_x", "x", engine.ParseOptions{Expectations: true})
	if want := "rule _x is hidden: a rule whose name begins with an underscore is not an entry point"; err == nil || err.Error() != want {
		t.Errorf("Parse(_x): got %v, want %s", err, want)
	}
	// The hidden rules behind inline actions are not start rules.
	prog, err = compileStarts(t, "@start\nrule a = (\"x\" => 1) (\"y\" => 2)\n")
	if err != nil {
		t.Fatal(err)
	}
	var helpers []string
	for _, r := range prog.RuleList {
		if compile.HiddenRule(r.Name) {
			helpers = append(helpers, r.Name)
		}
	}
	if len(helpers) != 2 || !slices.Equal(prog.Starts(), []string{"a"}) || prog.DefaultStart() != "a" {
		t.Errorf("hidden rules %v starts %v default %q", helpers, prog.Starts(), prog.DefaultStart())
	}
}
