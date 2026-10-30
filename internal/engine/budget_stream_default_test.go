//go:build !pego_budget_controls_reference

package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

// streamScanGrammar has a record whose cost grows with the cube of its length.
const streamScanGrammar = `rule main = ^^ header items:record* #stream $$
rule header = "#records\n"
rule record = ^ ([a-z] &mid)* "\n"
rule mid = ([a-z] &[a-z]*)*`

// The default budget of a stream follows the furthest position the parser demands, which does not
// depend on how the reader chunks the input: the same input gives the same result, and the same
// limit, spend and request when the budget is exceeded, whatever the chunk size. The furthest
// position of the third case is the 3010 units of the header and the record.
func TestBudgetStreamDefaultIgnoresChunking(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, source, text string
		limits             limits.Limits
		exceeds            bool
		limit              uint64
	}{
		{"completes", gentest.Records, "#records\na=1\nbc=22\n" + strings.Repeat("k=7\n", 2000), limits.Limits{}, false, 0},
		{"exceeds the floor", streamScanGrammar, "#records\n" + strings.Repeat("a", 300) + "\n", limits.Limits{}, true, 1 << 20},
		{"exceeds a grown default", streamScanGrammar, "#records\n" + strings.Repeat("a", 3000) + "\n", limits.Limits{}, true, 3010 * 1024},
		{"exceeds an explicit limit", gentest.Records, "#records\na=1\nbc=22\n", limits.Limits{Budget: 20}, true, 20},
	}
	for _, c := range cases {
		prog := mustCompile(t, c.source)
		budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
			o.Limits = c.limits
			var want string
			for _, size := range []int{1, 7, len(c.text)} {
				var out []string
				r := &budgetStreamReader{chunks: chunks(c.text, size)}
				err := prog.ParseStreamWith("main", r, func(n *value.Node) error { out = append(out, gentest.ResultJSON(n, nil)); return nil }, o)
				var le *limits.LimitError
				if c.exceeds != errors.As(err, &le) {
					t.Fatalf("%s chunk size %d: error %v", c.name, size, err)
				}
				// A LimitError prints the limit, the spend and the request.
				got := fmt.Sprint(out, " ", err)
				if want == "" {
					want = got
				} else if got != want {
					t.Errorf("%s: chunk size %d gives %q, chunk size 1 gives %q", c.name, size, got, want)
				}
				if le != nil && c.limit != 0 && le.Limit != c.limit {
					t.Errorf("%s: limit %d, want %d", c.name, le.Limit, c.limit)
				}
			}
		})
	}
}

func chunks(s string, size int) []string {
	var out []string
	for len(s) > 0 {
		n := min(size, len(s))
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}
