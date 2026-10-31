package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/syntax"
)

// complexityMemoSources are grammars with the shapes that decide memoization: left-recursion
// cycles with leaders and other members, transient rules, rules called once, #missing in a rule
// and in a callee, and inline actions, which are lowered to hidden rules.
var complexityMemoSources = []string{
	"rule main = e $$\nrule e = e \"+\" t / t\nrule t = t \"*\" f / f\nrule f = \"x\" / \"(\" e \")\"\n",
	"rule main = c $$\nrule a = c \"+\" / \"n\"\nrule c = \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n",
	"rule main = x y y z\nrule x = \"x\"\nrule y = \"y\" x\nrule z = (\"z\" #missing) / w\nrule w = z \"w\" / \"w\"\n",
	"rule main = p p r r\nrule p = q \"p\"\nrule q = \"q\" #missing\nrule r = (\";\" #missing) p\n",
	"rule main = a:(\"a\" => 1) b:(\"b\" => 2) => $a + $b\n",
	"rule main = (e \"q\" / \"x\")* $$\nrule e = #operators {\n    operand \"x\"\n    level add { infix left \"+\" }\n}\n",
}

// TestComplexityMemoPolicy checks that the rules the classifier assumes the memo answers are the
// rules that every parse memoizes: a leader, or a rule that is memoized, not transient and not
// subject to missing tokens (Parser.Memoizes under every option).
func TestComplexityMemoPolicy(t *testing.T) {
	srcs := map[string]string{}
	for i, src := range complexityMemoSources {
		srcs[string(rune('a'+i))] = src
	}
	for _, pattern := range []string{"../../parsers/*/*.pego", "../../examples/*/*.pego"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			srcs[p] = string(b)
		}
	}
	for name, src := range srcs {
		t.Run(name, func(t *testing.T) {
			g, err := syntax.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			g = starttest.MarkAll(g)
			prog, err := compileStatic(g, Options{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			c, err := ClassifyComplexity(g, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range prog.RuleList {
				always := r.Leader || r.Memo && !r.Transient && !r.Missing
				if c.Memoized[r.Name] != always {
					t.Errorf("rule %s: classifier assumes memoized=%v, compiled rule memoizes in every parse=%v (Leader %v Memo %v Transient %v Missing %v)",
						r.Name, c.Memoized[r.Name], always, r.Leader, r.Memo, r.Transient, r.Missing)
				}
			}
			for n := range c.Memoized {
				if prog.ByName[n] == nil && !strings.HasPrefix(n, "_") {
					t.Errorf("classifier rule %s is not a compiled rule", n)
				}
			}
		})
	}
}

func classifySource(t *testing.T, src string) *Complexity {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ClassifyComplexity(starttest.MarkAll(g), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestClassifyComplexity checks the class of grammars that each exercise one part of the
// conditions for linear cost.
func TestClassifyComplexity(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		class     ComplexityClass
		degree    int
	}{
		{"committing alternative", "rule main = (a / \"a\")* $$\nrule a = \"a\" \"c\"\n", ClassLinear, 0},
		{"restarted scan", "rule main = (a / \"a\")* $$\nrule a = \"a\"* \"c\"\n", ClassPoly, 2},
		{"nested restarted scans", "rule main = (a / \"a\")* $$\nrule a = (b / \"a\")* \"c\"\nrule b = \"a\"* \"d\"\n", ClassPoly, 3},
		// The iteration fails only at "\"\"", before its scan, so it need not be committing.
		{"unit failing before its source", "rule main = (s \";\")* $$\nrule s = \"\\\"\" [^\"]* (\"\\\"\\\"\" [^\"]*)* \"\\\"\"\n", ClassLinear, 0},
		{"unit failing after its source", "rule main = (s \";\")* $$\nrule s = \"\\\"\" [^\"]* (\"\\\"\" [^\"]* \"!\")* \"\\\"\"\n", ClassPoly, 2},
		// The operands overlap, but their only source is the shared skip ws.
		{"source inside a shared call", "rule main = e $$\nrule e = #operators {\n    skip ws\n    operand \"true\"\n    operand [a-z]+\n    level { infix left \"+\" }\n}\nrule ws = [ \\t]*\n", ClassLinear, 0},
		// A list built from the elements of a repetition costs its number of elements.
		{"list of a repetition", "rule main = l $$\nrule l = \"[\" ms:m? \"]\" => concat($ms)\nrule m = f:v rs:(\",\" x:v)* => concat(list($f), map($rs, (r) => r.$x))\nrule v = \"x\" / l\n", ClassLinear, 0},
		{"list built by right recursion", "rule main = l $$\nrule l = x:\"a\" t:l? => concat(list($x), $t)\n", ClassPoly, 2},
		// Comparing two strings examines the shorter one: against a literal it is bounded, between
		// two values of the recursion it is not.
		{"comparison with a literal", "rule main = l $$\nrule l string = (\"(\" x:l \")\" {$x != \"\"} => $x) / a\nrule a string = \"a\" => \"a\"\n", ClassLinear, 0},
		{"comparison of recursive values", "rule main = l $$\nrule l string = (\"(\" x:l y:l \")\" {$x == $y} => $x) / a\nrule a string = \"a\" => \"a\"\n", ClassPoly, 2},
		// An element of a list holds anything the list can, so indexing keeps the size.
		{"element of a recursive list", "rule main = r $$\nrule r []string = (\"a\" x:r => list($x[0] + \"a\")) / (\"\" => list(\"\"))\n", ClassPoly, 2},
		{"list of literal elements", "rule main = r $$\nrule r []string = (\"a\" x:r => list(\"a\")) / (\"\" => list(\"\"))\n", ClassLinear, 0},
		{"comparison of recursive elements", "rule main = l $$\nrule l []string = (\"(\" x:l y:l \")\" {$x[0] == $y[0]} => $x) / (t:[a-z]+ => list(text($t)))\n", ClassPoly, 2},
		// A comparison examines no more than one of its operands.
		{"comparison of a recursive element with itself", "rule main = l $$\nrule l []string = (\"(\" x:l \")\" {$x[0] == $x[0]} => $x) / (t:[a-z]+ => list(text($t)))\n", ClassPoly, 2},
		// len examines all of a string or a list.
		{"len of a failing unit", "rule main = (v \"q\" / \"x\")* $$\nrule v = x:e #atomic {len($x.Value) > 0}\nrule e = \"x\" e / \"x\"\n", ClassPoly, 2},
		{"len of a recursive capture", "rule main = r $$\nrule r = x:(\"a\" r / \"\") #atomic {len($x.Value) >= 0}\n", ClassPoly, 2},
		// The length of a node is its range, which len reads in constant time.
		{"len of the node of a failing unit", "rule main = (x:e #atomic {len($x) > 0} \"q\" / \"x\")* $$\nrule e = \"x\" e / \"x\"\n", ClassLinear, 0},
		{"len of the node of a recursive capture", "rule main = r $$\nrule r = x:(\"a\" r / \"\") #atomic {len($x) >= 0}\n", ClassLinear, 0},
		{"len of a list node of a recursive capture", "rule main = r $$\nrule r = x:(\"a\" r)? {len($x) >= 0}\n", ClassLinear, 0},
		{"nil test of a recursive capture", "rule main = r $$\nrule r = x:(\"a\" r / \"\") #atomic {$x != nil}\n", ClassLinear, 0},
		// A repetition ends at its first empty iteration, so it can fail after scanning even
		// when its expression always succeeds.
		{"repetition below its minimum", "rule main = (p / \"a\")* $$\nrule p = (\"a\"*) #repeat(3)\n", ClassPoly, 2},
		// With a minimum of two, "a"* fails only when its first iteration is empty, after a
		// constant examination; the classifier does not tell this from a body that can be empty
		// after scanning, as &"a"* can, and stays pessimistic.
		{"repetition of two below its minimum", "rule main = (p / \"a\")* $$\nrule p = (\"a\"*) #repeat(2)\n", ClassPoly, 2},
		{"repetition below its minimum without a retry", "rule main = (\"a\"*) #repeat(3) $$\n", ClassLinear, 0},
		{"self-growing variable", "var s string = \"a\"\nrule main = r $$\nrule r = \"x\" {s := s + s} r / {len(s) > 0}\n", ClassExp, 0},
		// Only a polynomial class has a degree.
		{"exponential with a polynomial cause", "var s string = \"a\"\nrule main = r (a / \"a\")* $$\nrule r = \"x\" {s := s + s} r / {len(s) > 0}\nrule a = \"a\"* \"c\"\n", ClassExp, 0},
		{"rebinding after the left call", "var k int\nrule main = e $$\nrule e = e \"x\" {k := k + 1} / \"x\" {k >= 0}\n", ClassLinearEnv, 0},
		{"non-leader cycle members", "rule main = c $$\nrule a = c \"+\" / \"n\"\nrule c = \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n", ClassUnknown, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := classifySource(t, tc.src)
			if c.Class != tc.class || c.Degree != tc.degree {
				t.Errorf("class %v degree %d, want %v degree %d\ncauses %v\nreasons %v", c.Class, c.Degree, tc.class, tc.degree, c.Causes, c.Reasons)
			}
			if tc.class == ClassPoly || tc.class == ClassExp {
				if len(c.Causes) == 0 {
					t.Error("no witness")
				}
			}
			if tc.class == ClassUnknown && len(c.Reasons) == 0 {
				t.Error("no reason")
			}
		})
	}
}

// TestClassifyComplexityDeterministic checks that classifying a grammar again gives the same
// result, on a grammar whose units have several callees with sources.
func TestClassifyComplexityDeterministic(t *testing.T) {
	src := "rule main = (a b c d \"q\" / \"x\" / \"y\" / \"z\" / \"w\")* $$\nrule a = \"x\"*\nrule b = \"y\"*\nrule c = \"z\"*\nrule d = \"w\"*\n"
	want := fmt.Sprintf("%+v", *classifySource(t, src))
	for range 50 {
		if got := fmt.Sprintf("%+v", *classifySource(t, src)); got != want {
			t.Fatalf("classification\n%s\nthen\n%s", want, got)
		}
	}
}

// TestComplexityGrowingBindings checks the bindings that the growing-variable check reports.
func TestComplexityGrowingBindings(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"doubling before the call", "var s string = \"a\"\nrule main = r $$\nrule r = \"x\" {s := s + s} r / \"\"\n", 1},
		{"bounded extension", "var indent string\nrule main = r $$\nrule r = \"x\" {indent := indent + \"  \"} r / \"\"\n", 0},
		{"after the call", "var s string = \"a\"\nrule main = r $$\nrule r = \"x\" r {s := s + s} / \"\"\n", 0},
		{"different alternative", "var s string = \"a\"\nrule main = r $$\nrule r = {s := s + s} \"q\" / \"x\" r / \"\"\n", 0},
		{"empty variable", "var s string\nrule main = r $$\nrule r = \"x\" {s := s + s} r / \"\"\n", 0},
		{"in a repetition", "var s string = \"a\"\nrule main = (\"x\" {s := s + s})* $$\n", 1},
		{"int variable", "var n int = 1\nrule main = r $$\nrule r = \"x\" {n := n + n} r / \"\"\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := classifySource(t, tc.src)
			if len(c.Growing) != tc.want {
				t.Errorf("growing %v, want %d", c.Growing, tc.want)
			}
		})
	}
}

// TestGrowingBindingsAlone checks that GrowingBindings returns the Growing member of the full
// classification, on the grammars of the repository and on bindings in inline actions, whose rule
// is the rule that holds them.
func TestGrowingBindingsAlone(t *testing.T) {
	srcs := map[string]string{
		"recursion":     "var s string = \"a\"\nrule main = r $$\nrule r = \"x\" {s := s + s} r / \"\"\n",
		"inline action": "var s string = \"a\"\nrule main = r $$\nrule r = ((\"x\" {s := s + s})* => 1) \"y\"\n",
		"variables":     "var s string = \"a\"\nvar t string\nrule main = (\"x\" {t := t + t} {s := s + s + \"b\"})* $$\n",
	}
	for _, pattern := range []string{"../../parsers/*/*.pego", "../../examples/*/*.pego"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			srcs[p] = string(b)
		}
	}
	for name, src := range srcs {
		t.Run(name, func(t *testing.T) {
			c := classifySource(t, src)
			g, err := syntax.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			alone, err := GrowingBindings(starttest.MarkAll(g), "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(alone, c.Growing) {
				t.Errorf("GrowingBindings %v, classification %v", alone, c.Growing)
			}
			if strings.HasPrefix(name, "inline") && (len(alone) != 1 || alone[0].Rule != "r") {
				t.Errorf("inline action: %v, want one binding in r", alone)
			}
			if name == "variables" && len(alone) != 1 {
				t.Errorf("variables: %v, want the binding of s", alone)
			}
		})
	}
}
