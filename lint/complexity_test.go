package lint

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/syntax"
)

func TestGrowingVariable(t *testing.T) {
	runCases(t, []lintCase{
		{name: "doubling on recursion", src: "var s string = \"a\"\nrule main = \"x\" {s := s + s} main / \"\"",
			want: []string{"2:18 warning growing-variable"}, msg: "s := s + s can at least double the size of s"},
		{name: "doubling in a repetition", src: "var s string = \"a\"\nrule main = (\"x\" {s := s + s})* {len(s) > 0}",
			want: []string{"2:19 warning growing-variable"}},
		{name: "bounded growth", src: "var s string = \"a\"\nrule main = \"x\" {s := s + \"  \"} main / \"\""},
		{name: "binding after the call", src: "var s string = \"a\"\nrule main = \"x\" main {s := s + s} / \"\""},
		{name: "binding in another alternative", src: "var s string = \"a\"\nrule main = {s := s + s} \"q\" / \"x\" main / \"\""},
		{name: "empty variable", src: "var s string\nrule main = \"x\" {s := s + s} main / \"\""},
	})
	// The check can be disabled.
	got, _ := lintSource(t, "var s string = \"a\"\nrule main = \"x\" {s := s + s} main / \"\"",
		options{Disable: map[string]bool{CheckGrowingVariable: true}})
	if len(got) != 0 {
		t.Errorf("disabled: got %q", got)
	}
}

func TestSuperLinear(t *testing.T) {
	const quadratic = "rule main = (a / \"a\")* $$\n\nrule a = \"a\"* \"c\"\n"
	// Without the classification requested, the check reports nothing.
	if got, _ := lintSource(t, quadratic, options{}); len(got) != 0 {
		t.Errorf("not requested: got %q", got)
	}
	got, fs := lintSource(t, quadratic, options{Classify: true})
	if want := []string{"3:10 hint super-linear"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if f := fs[0]; f.Rule != "a" || !strings.Contains(f.Message, "n^2") {
		t.Errorf("finding %+v", f)
	}
	if got, _ := lintSource(t, quadratic, options{Classify: true, Disable: map[string]bool{CheckSuperLinear: true}}); len(got) != 0 {
		t.Errorf("disabled: got %q", got)
	}
	// A linear grammar, and a grammar the classifier cannot bound, have no finding: the class
	// unknown has no witness.
	for _, src := range []string{
		"rule main = (a / \"a\")* $$\nrule a = \"a\" \"c\"\n",
		"rule main = c $$\nrule a = c \"+\" / \"n\"\nrule c = \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n",
	} {
		if got, _ := lintSource(t, src, options{Classify: true}); len(got) != 0 {
			t.Errorf("%q: got %q", src, got)
		}
	}
	// A suppression directive applies to it as to any check.
	if got, _ := lintSource(t, "rule main = (a / \"a\")* $$\n\n// lint:ignore super-linear the input is short\nrule a = \"a\"* \"c\"\n", options{Classify: true}); len(got) != 0 {
		t.Errorf("suppressed: got %q", got)
	}
}

// TestExampleClassifications pins the classes of the example grammars from their default start
// rules, which zp lint -classify shows.
func TestExampleClassifications(t *testing.T) {
	for _, tc := range []struct {
		file   string
		class  engine.ComplexityClass
		degree int
	}{
		{"calculator/calc.zp", engine.ClassLinear, 0},
		{"calculator/calc_lr.zp", engine.ClassLinear, 0},
		{"minilang/minilang.zp", engine.ClassUnknown, 0},
		{"outline/outline.zp", engine.ClassPoly, 3},
	} {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile("../examples/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			g, err := syntax.Parse(string(src))
			if err != nil {
				t.Fatal(err)
			}
			c, err := engine.ClassifyComplexity(context.Background(), g, "")
			if err != nil {
				t.Fatal(err)
			}
			if c.Class != tc.class || c.Degree != tc.degree {
				t.Errorf("class %v degree %d, want %v degree %d\ncauses %v\nreasons %v", c.Class, c.Degree, tc.class, tc.degree, c.Causes, c.Reasons)
			}
		})
	}
}
