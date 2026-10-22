package engine

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestRuleTableLimits(t *testing.T) {
	for _, c := range []struct {
		strs, seen int
		ok         bool
	}{
		{1, 0, true},
		{math.MaxUint16 + 1, math.MaxInt32, true},
		{math.MaxUint16 + 2, 0, false},
		{1, math.MaxInt32 + 1, false},
	} {
		if err := ruleTableLimits(c.strs, c.seen); (err == nil) != c.ok {
			t.Errorf("ruleTableLimits(%d, %d) = %v, want ok=%v", c.strs, c.seen, err, c.ok)
		}
	}
}

// Generate itself reports a grammar whose rule table does not fit the compact form.
func TestGenerateRejectsRuleTableOverLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("generates a grammar with 65537 distinct capture lists")
	}
	const n = math.MaxUint16 + 2
	var b strings.Builder
	b.WriteString("rule main = r0")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, " / r%d", i)
	}
	b.WriteString("\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "rule r%d = c%d:\"a\"\n", i, i)
	}
	g, err := parseTest(b.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Generate(g, GenOptions{Package: "p", Start: "main", Runtime: RuntimeImport})
	if err == nil || !strings.Contains(err.Error(), "distinct capture or variable lists") {
		t.Fatalf("Generate = %v, want the rule table limit error", err)
	}
}
