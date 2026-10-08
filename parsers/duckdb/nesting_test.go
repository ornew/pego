package duckdb_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/duckdb"
)

// nestings are the constructs that nest, and some that repeat: a text with n levels or terms of each. Chains of
// prefix operators (NOT, -) are read by recursion on the Go stack that the depth limit of the generated parser does
// not count (an engine limitation, see the README): they are tested at moderate depth only.
var nestings = []struct {
	name      string
	recursive bool // nests, so its depth is limited by the parser (the others are loops, or not limited)
	make      func(n int) string
}{
	{"parentheses", true, func(n int) string { return "SELECT " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) }},
	{"subqueries", true, func(n int) string { return "SELECT " + strings.Repeat("(SELECT ", n) + "1" + strings.Repeat(")", n) }},
	{"calls", true, func(n int) string { return "SELECT " + strings.Repeat("f(", n) + "1" + strings.Repeat(")", n) }},
	{"lists", true, func(n int) string { return "SELECT " + strings.Repeat("[", n) + "1" + strings.Repeat("]", n) }},
	{"case", true, func(n int) string {
		return "SELECT " + strings.Repeat("CASE WHEN a THEN ", n) + "1" + strings.Repeat(" END", n)
	}},
	{"not", false, func(n int) string { return "SELECT " + strings.Repeat("NOT ", n) + "a" }},
	{"minus", false, func(n int) string { return "SELECT " + strings.Repeat("- ", n) + "a" }},
	{"derived tables", true, func(n int) string {
		return "SELECT 1 FROM " + strings.Repeat("(SELECT 1 FROM ", n) + "t" + strings.Repeat(")", n)
	}},
	{"sum", false, func(n int) string { return "SELECT " + strings.Repeat("a + ", n) + "a" }},
	{"unions", false, func(n int) string { return "SELECT 1" + strings.Repeat(" UNION ALL SELECT 1", n) }},
	{"joins", false, func(n int) string { return "SELECT 1 FROM t" + strings.Repeat(" JOIN u ON true", n) }},
}

// TestNesting checks that a text of moderate depth parses, that a text of absurd depth gives an error and does
// not exhaust the stack, and that the constructs that repeat have no limit that matters. NESTING_LIMITS=1 logs
// the depth at which each construct fails.
func TestNesting(t *testing.T) {
	for _, tc := range nestings {
		t.Run(tc.name, func(t *testing.T) {
			if err := duckdb.Recognize(tc.make(200)); err != nil {
				t.Fatalf("200 levels: %v", err)
			}
			if _, err := duckdb.ParseAST(tc.make(200)); err != nil {
				t.Fatalf("200 levels: %v", err)
			}
			if os.Getenv("NESTING_LIMITS") != "" && tc.recursive {
				lo, hi := 200, 1<<20
				for lo+1 < hi {
					mid := (lo + hi) / 2
					if _, err := duckdb.ParseAST(tc.make(mid)); err == nil {
						lo = mid
					} else {
						hi = mid
					}
				}
				t.Logf("%s: parses %d levels", tc.name, lo)
				return
			}
			if !tc.recursive {
				if err := duckdb.Recognize(tc.make(20000)); err != nil {
					t.Errorf("20,000 terms: %v", err)
				}
				return
			}
			if _, err := duckdb.ParseAST(tc.make(300000)); err == nil {
				t.Errorf("300,000 levels were accepted")
			}
		})
	}
}
