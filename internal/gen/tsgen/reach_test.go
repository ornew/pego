package tsgen_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// A rule that only value-free contexts reference has no original and, entered only through its plain
// function without a value, no constant; a rule that nothing references has none either, yet every
// rule name stays known to parseRule's errors.
func TestGeneratedTSLeavesOutUnreachableRules(t *testing.T) {
	t.Parallel()
	g := mustParse("@start\nrule main = !bad \"a\" kept\nrule bad = \"b\" kept\nrule kept = \"k\"\nrule dead = \"d\"")
	code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(code)
	for name, want := range map[string]int{"main": 1, "kept": 1, "bad": 0, "dead": 0} {
		if got := len(regexp.MustCompile(`mkRule\(\d+, "`+name+`", `).FindAllString(src, -1)); got != want {
			t.Errorf("rule %s has %d constants, want %d", name, got, want)
		}
	}
	if !strings.Contains(src, `const ruleNames = ["main", "bad", "kept", "dead"]`) {
		t.Errorf("rule names are not all listed")
	}
}
