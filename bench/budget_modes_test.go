//go:build !zp_budget_controls_reference

package bench

import (
	"context"
	"testing"

	"github.com/ornew/zeroparse"
)

// BenchmarkBudgetControlModes separates warm context polling from finite counters. The inactive and
// context modes set Unlimited, so that they measure no counter.
// Cold controlled-program construction is measured separately.
func BenchmarkBudgetControlModes(b *testing.B) {
	for _, w := range workloads {
		if w.name != "JSON" && w.name != "XML" {
			continue
		}
		b.Run(w.name, func(b *testing.B) {
			p := load(b, w.grammar)
			input := w.input(1)
			for _, backend := range backends {
				for _, unit := range units {
					for _, mode := range []string{"inactive", "context", "budget", "combined"} {
						b.Run(backend.name+"/"+unit.name+"/"+mode, func(b *testing.B) {
							opts := []zp.ParseOption{zp.WithBackend(backend.b), zp.WithUnit(unit.u)}
							if mode == "context" || mode == "combined" {
								ctx, cancel := context.WithCancel(context.Background())
								defer cancel()
								opts = append(opts, zp.WithContext(ctx))
							}
							if mode == "inactive" || mode == "context" {
								// A zero budget selects the default, which is a counter; Unlimited leaves only the context.
								opts = append(opts, zp.WithLimits(zp.Limits{Budget: zp.Unlimited}))
							}
							if mode == "budget" || mode == "combined" {
								// The largest finite budget: ^uint64(0) is Unlimited, which runs without a control.
								opts = append(opts, zp.WithLimits(zp.Limits{Budget: ^uint64(0) - 1}))
							}
							if err := parseOK(p, &w, input, opts...); err != nil {
								b.Fatal(err)
							}
							b.SetBytes(int64(len(input)))
							b.ReportAllocs()
							for b.Loop() {
								if err := parseOK(p, &w, input, opts...); err != nil {
									b.Fatal(err)
								}
							}
						})
					}
				}
			}
		})
	}
}
