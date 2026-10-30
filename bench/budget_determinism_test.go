//go:build !pego_budget_controls_reference

package bench

import (
	"fmt"
	"runtime"
	"testing"

	gcsv "github.com/ornew/pego/internal/bench/gen/csv"
	gjson "github.com/ornew/pego/internal/bench/gen/json"
	gxml "github.com/ornew/pego/internal/bench/gen/xml"
	"github.com/ornew/pego/internal/bench/inputs"
)

// TestGeneratedBudgetErrorsAreDeterministic requires that a parse under a finite budget fails the
// same way every time for one input and one set of options, whatever the pooled parsers hold from
// earlier parses: the reported Used and Requested depend on the input alone.
func TestGeneratedBudgetErrorsAreDeterministic(t *testing.T) {
	type entry struct {
		name string
		// run parses in with the budget and describes the outcome.
		run map[string]func(in string, budget uint64) string
	}
	describe := func(ok bool, err error) string { return fmt.Sprintf("ok=%v err=%v", ok, err) }
	entries := []entry{
		{"JSON", map[string]func(string, uint64) string{
			"Parse": func(in string, b uint64) string {
				n, err := gjson.Parse(in, gjson.WithLimits(gjson.Limits{Budget: b}))
				return describe(n != nil, err)
			},
			"ParseAST": func(in string, b uint64) string {
				v, err := gjson.ParseAST(in, gjson.WithLimits(gjson.Limits{Budget: b}))
				return describe(v != nil, err)
			},
			"Recognize": func(in string, b uint64) string {
				return describe(true, gjson.Recognize(in, gjson.WithLimits(gjson.Limits{Budget: b})))
			},
		}},
		{"CSV", map[string]func(string, uint64) string{
			"Parse": func(in string, b uint64) string {
				n, err := gcsv.Parse(in, gcsv.WithLimits(gcsv.Limits{Budget: b}))
				return describe(n != nil, err)
			},
			"ParseAST": func(in string, b uint64) string {
				v, err := gcsv.ParseAST(in, gcsv.WithLimits(gcsv.Limits{Budget: b}))
				return describe(v != nil, err)
			},
		}},
		{"XML", map[string]func(string, uint64) string{
			"Parse": func(in string, b uint64) string {
				n, err := gxml.Parse(in, gxml.WithLimits(gxml.Limits{Budget: b}))
				return describe(n != nil, err)
			},
			"ParseAST": func(in string, b uint64) string {
				v, err := gxml.ParseAST(in, gxml.WithLimits(gxml.Limits{Budget: b}))
				return describe(v != nil, err)
			},
		}},
	}
	inputsOf := map[string][]string{
		"JSON": {inputs.JSONInput(600), inputs.JSONInput(24 << 10)},
		"CSV":  {inputs.CSVInput(8), inputs.CSVInput(400)},
		"XML":  {inputs.XMLInput(600), inputs.XMLInput(24 << 10)},
	}
	for _, e := range entries {
		for entry, run := range e.run {
			for _, in := range inputsOf[e.name] {
				t.Run(fmt.Sprintf("%s/%s/%d", e.name, entry, len(in)), func(t *testing.T) {
					// The cost of the parse, found by doubling, then limits around it and below it.
					cost := uint64(1)
					for run(in, cost) != describe(true, nil) && cost < 1<<40 {
						cost *= 2
					}
					var limits []uint64
					for l := uint64(1); l <= cost; l += 1 + l/3 {
						limits = append(limits, l)
					}
					limits = append(limits, cost-1, cost, cost+1)
					for _, l := range limits {
						// Two collections empty the pool of parsers, so that the parser of this
						// first run is constructed new, as it is in a fresh process.
						runtime.GC()
						runtime.GC()
						want := run(in, l)
						for i := range 6 {
							switch i % 3 {
							case 1:
								// Dirty the pooled parsers with a parse of another size.
								other := inputsOf[e.name][(i/3+1)%2]
								run(other, 1<<40)
							case 2:
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
}
