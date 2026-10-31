package sample_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/sample"
)

// The generator works on the rules the parser runs, where an inline action is an anonymous rule, but
// its reports describe the grammar as written: an inline action is "the inline action in rule R"
// (with its number when R holds several), and an alternative is the expression as written, never a
// reference to a generated rule.
func TestCoverageReportsInlineActionsAsWritten(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		rules     int // the rules that count: the declared ones and the inline actions
		alts      int
		want      []string // in the report of one input
	}{
		{"several in a choice", `rule main = (x:"a" => $x) / (y:"b" => $y) / (z:"c" => $z)`, 4, 3,
			[]string{"inline action 2 in rule main", `(y:"b" => $y)`}},
		{"one in a rule", `rule main = "a" (x:"b" "c" => $x) / "d"`, 2, 2,
			[]string{"the inline action in rule main", `"a" (x:"b" "c" => $x)`}},
		{"nested", `rule main = (x:(y:"a" => $y) "b" => $x) / "c"`, 3, 2,
			[]string{"inline action 1 in rule main", `(x:(y:"a" => $y) "b" => $x)`}},
		{"in a declared hidden rule", "rule main = _h / \"c\"\nrule _h = (x:\"a\" => $x) / \"b\"", 3, 4,
			[]string{"missed rule: _h", "the inline action in rule _h"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := sample.New(compile(t, tc.src), sample.WithSeed(1), sample.WithCoverage())
			if err != nil {
				t.Fatal(err)
			}
			// No input yet: everything is missed, and the report names it all.
			c := g.Coverage()
			if c.Rules != tc.rules || c.Alternatives != tc.alts {
				t.Errorf("%d rules and %d alternatives, want %d and %d: %s", c.Rules, c.Alternatives, tc.rules, tc.alts, c.Report())
			}
			if len(c.MissedRules) != tc.rules {
				t.Errorf("missed rules %q, want %d", c.MissedRules, tc.rules)
			}
			seen := map[string]bool{}
			for _, r := range c.MissedRules {
				if seen[r] {
					t.Errorf("rule %s is reported twice", r)
				}
				seen[r] = true
			}
			if _, err := g.Next(); err != nil {
				t.Fatal(err)
			}
			c = g.Coverage()
			if c.RulesCovered == 0 || c.RulesCovered > c.Rules {
				t.Errorf("rules covered %d of %d", c.RulesCovered, c.Rules)
			}
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			cuts, err := json.Marshal(g.Cuts())
			if err != nil {
				t.Fatal(err)
			}
			all := c.Report() + string(data) + string(cuts)
			if strings.Contains(strings.ReplaceAll(all, "_h", ""), "_main") {
				t.Errorf("a generated name in the report:\n%s", all)
			}
			g2, _ := sample.New(compile(t, tc.src), sample.WithSeed(1), sample.WithCoverage())
			empty := g2.Coverage()
			text := empty.Report()
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("the report lacks %q:\n%s", w, text)
				}
			}
		})
	}
}
