package gogen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/syntax"
)

// typedOnlyLonelyGrammar is the left-recursive typed-only grammar, which keeps its Node rules, plus a rule
// that nothing reaches.
func typedOnlyLonelyGrammar(t *testing.T) string {
	t.Helper()
	for _, c := range gentest.TypedOnlyConverted {
		if c.Name == "left recursion" {
			return strings.Replace(c.Src, "rule main", "@start\nrule main", 1) + "\nrule lonely = \"zz\" item \"q\"+\n"
		}
	}
	t.Fatal("the left recursion grammar is missing")
	return ""
}

// TestTypedOnlyUnreachedNodeRulesAreLeftOut checks that a typed-only parser that keeps Node rules prints
// the Node rules that its entry points reach, and neither a function nor a slot Body of the others.
func TestTypedOnlyUnreachedNodeRulesAreLeftOut(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testTypedOnlyUnreached)
}

func testTypedOnlyUnreached(t *testing.T) {
	src := typedOnlyLonelyGrammar(t)
	for _, controls := range []bool{false, true} {
		opts := gen.GenOptions{Types: true, TypedOnly: true, BudgetControls: controls}
		pruned := generateReach(t, src, opts)
		kept := opts
		kept.KeepUnreachable = true
		full := generateReach(t, src, kept)
		if !strings.Contains(string(full), `"zz"`) || strings.Contains(string(pruned), `"zz"`) {
			t.Errorf("controls=%v: the unreached rule is not left out of the typed-only output", controls)
		}
		if got := undefinedGenerated(t, pruned); len(got) > 0 {
			t.Errorf("controls=%v: the output calls undefined functions %v", controls, got)
		}
		if gentest.CountBodies(pruned) >= gentest.CountBodies(full) {
			t.Errorf("controls=%v: pruned output has %d Node rule bodies, the full output %d", controls, gentest.CountBodies(pruned), gentest.CountBodies(full))
		}
	}
}

// TestTypedOnlyUnreachedCalleeFailsGeneration is the negative control: with every Node rule but the first
// removed from the first pass, generation of the typed-only parser reports the calls and slots left out.
func TestTypedOnlyUnreachedCalleeFailsGeneration(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testTypedOnlyUnreachedCalleeFails)
}

func testTypedOnlyUnreachedCalleeFails(t *testing.T) {
	g, err := syntax.Parse(typedOnlyLonelyGrammar(t))
	if err != nil {
		t.Fatal(err)
	}
	gh := &gogen.Hooks{}
	gentest.SetGenHooks(t, gh)
	gh.Reach = func(l *gogen.LiveRules) {
		for k := range l.Reach {
			if k.Table == "rules" && k.ID != 0 {
				delete(l.Reach, k)
			}
		}
	}
	_, err = gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p", Types: true, TypedOnly: true})
	if err == nil || !strings.Contains(err.Error(), "left out") {
		t.Errorf("typed-only generation with the Node rules left out: err = %v, want an internal error", err)
	}
}

// TestTypedOnlyTypedCalleeFailsGeneration is the negative control for the typed table of a typed-only parser
// without Node rules: a typed rule that a printed call reaches and the first pass left out fails generation.
func TestTypedOnlyTypedCalleeFailsGeneration(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testTypedOnlyTypedCalleeFails)
}

func testTypedOnlyTypedCalleeFails(t *testing.T) {
	g, err := syntax.Parse(reachTypedGrammar)
	if err != nil {
		t.Fatal(err)
	}
	opts := gen.GenOptions{Start: "main", Package: "p", Types: true, TypedOnly: true}
	if _, err := gentest.GenerateCode(t, g, opts); err != nil {
		t.Fatalf("typed-only generation without the hook: %v", err)
	}
	gh := &gogen.Hooks{}
	gentest.SetGenHooks(t, gh)
	gh.Reach = func(l *gogen.LiveRules) {
		for k := range l.Reach {
			if k.Table == "trules" && !isRoot(l, k) {
				delete(l.Reach, k)
			}
		}
	}
	_, err = gentest.GenerateCode(t, g, opts)
	if err == nil || !strings.Contains(err.Error(), "left out") {
		t.Errorf("typed-only generation with a typed callee left out: err = %v, want an internal error", err)
	}
}
