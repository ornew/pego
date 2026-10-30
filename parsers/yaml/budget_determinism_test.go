package yaml_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

// TestBudgetErrorsAreDeterministic requires that a parse under a finite budget fails the same way
// every time for one input, whatever the pooled parsers hold from earlier parses. The grammar has
// more than 64 memoized rules and the input more than 1024 characters, so the first calls of a rule
// at a position are recorded in the sparse form. The first run of each limit, the reference, uses a
// parser constructed new.
func TestBudgetErrorsAreDeterministic(t *testing.T) {
	doc := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "---\nname: item-%d\nlabels: {a: b, c: [1, 2, 3]}\nitems:\n  - x: 1\n    y: \"two\"\n  - [p, q]\n", i)
		}
		return b.String()
	}
	small, large := doc(2), doc(40)
	if len([]rune(large)) <= 1024 {
		t.Fatal("the large input does not reach the sparse mode")
	}
	describe := func(ok bool, err error) string { return fmt.Sprintf("ok=%v err=%v", ok, err) }
	runs := map[string]func(in string, b uint64) string{
		"Parse": func(in string, b uint64) string {
			n, err := yaml.Parse(in, yaml.WithLimits(yaml.Limits{Budget: b}))
			return describe(n != nil, err)
		},
		"ParseAST": func(in string, b uint64) string {
			v, err := yaml.ParseAST(in, yaml.WithLimits(yaml.Limits{Budget: b}))
			return describe(v != nil, err)
		},
		"Recognize": func(in string, b uint64) string {
			return describe(true, yaml.Recognize(in, yaml.WithLimits(yaml.Limits{Budget: b})))
		},
	}
	for name, run := range runs {
		for _, in := range []string{small, large} {
			t.Run(fmt.Sprintf("%s/%d", name, len(in)), func(t *testing.T) {
				cost := uint64(1)
				for run(in, cost) != describe(true, nil) && cost < 1<<40 {
					cost *= 2
				}
				var limits []uint64
				for l := uint64(1); l <= cost; l += 1 + l/2 {
					limits = append(limits, l)
				}
				limits = append(limits, cost-1, cost, cost+1)
				other := small
				if in == small {
					other = large
				}
				for _, l := range limits {
					runtime.GC()
					runtime.GC()
					want := run(in, l)
					for i := range 4 {
						switch i % 2 {
						case 0:
							run(other, 1<<40)
						case 1:
							runtime.GC()
							runtime.GC()
						}
						if got := run(in, l); got != want {
							t.Fatalf("budget %d, repeat %d: %s, first %s", l, i, got, want)
						}
					}
				}
			})
		}
	}
}
