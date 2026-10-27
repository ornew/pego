package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// typedOnlyLonelyGrammar is the left-recursive typed-only grammar, which keeps its Node rules, plus a rule
// that nothing reaches.
func typedOnlyLonelyGrammar(t *testing.T) string {
	t.Helper()
	for _, c := range typedOnlyConverted {
		if c.name == "left recursion" {
			return strings.Replace(c.src, "rule main", "@start\nrule main", 1) + "\nrule lonely = \"zz\" item \"q\"+\n"
		}
	}
	t.Fatal("the left recursion grammar is missing")
	return ""
}

// TestTypedOnlyUnreachedNodeRulesAreLeftOut checks that a typed-only parser that keeps Node rules prints
// the Node rules that its entry points reach, and neither a function nor a slot Body of the others.
func TestTypedOnlyUnreachedNodeRulesAreLeftOut(t *testing.T) { eachMode(t, testTypedOnlyUnreached) }

func testTypedOnlyUnreached(t *testing.T) {
	src := typedOnlyLonelyGrammar(t)
	for _, controls := range []bool{false, true} {
		opts := GenOptions{Types: true, TypedOnly: true, WorkControls: controls}
		pruned := generateReach(t, src, opts)
		kept := opts
		kept.keepUnreachable = true
		full := generateReach(t, src, kept)
		if !strings.Contains(string(full), `"zz"`) || strings.Contains(string(pruned), `"zz"`) {
			t.Errorf("controls=%v: the unreached rule is not left out of the typed-only output", controls)
		}
		if got := undefinedGenerated(t, pruned); len(got) > 0 {
			t.Errorf("controls=%v: the output calls undefined functions %v", controls, got)
		}
		if countBodies(pruned) >= countBodies(full) {
			t.Errorf("controls=%v: pruned output has %d Node rule bodies, the full output %d", controls, countBodies(pruned), countBodies(full))
		}
	}
}

// TestTypedOnlyUnreachedCalleeFailsGeneration is the negative control: with every Node rule but the first
// removed from the first pass, generation of the typed-only parser reports the calls and slots left out.
func TestTypedOnlyUnreachedCalleeFailsGeneration(t *testing.T) {
	eachMode(t, testTypedOnlyUnreachedCalleeFails)
}

func testTypedOnlyUnreachedCalleeFails(t *testing.T) {
	g, err := syntax.Parse(typedOnlyLonelyGrammar(t))
	if err != nil {
		t.Fatal(err)
	}
	reachHook = func(l *liveRules) {
		for k := range l.reach {
			if k.table == "rules" && k.id != 0 {
				delete(l.reach, k)
			}
		}
	}
	defer func() { reachHook = nil }()
	_, err = generateCode(g, GenOptions{Start: "main", Package: "p", Types: true, TypedOnly: true})
	if err == nil || !strings.Contains(err.Error(), "left out") {
		t.Errorf("typed-only generation with the Node rules left out: err = %v, want an internal error", err)
	}
}

// TestTypedOnlyTypedCalleeFailsGeneration is the negative control for the typed table of a typed-only parser
// without Node rules: a typed rule that a printed call reaches and the first pass left out fails generation.
func TestTypedOnlyTypedCalleeFailsGeneration(t *testing.T) {
	eachMode(t, testTypedOnlyTypedCalleeFails)
}

func testTypedOnlyTypedCalleeFails(t *testing.T) {
	g, err := syntax.Parse(reachTypedGrammar)
	if err != nil {
		t.Fatal(err)
	}
	opts := GenOptions{Start: "main", Package: "p", Types: true, TypedOnly: true}
	if _, err := generateCode(g, opts); err != nil {
		t.Fatalf("typed-only generation without the hook: %v", err)
	}
	reachHook = func(l *liveRules) {
		for k := range l.reach {
			if k.table == "trules" && !l.isRoot(k) {
				delete(l.reach, k)
			}
		}
	}
	defer func() { reachHook = nil }()
	_, err = generateCode(g, opts)
	if err == nil || !strings.Contains(err.Error(), "left out") {
		t.Errorf("typed-only generation with a typed callee left out: err = %v, want an internal error", err)
	}
}
