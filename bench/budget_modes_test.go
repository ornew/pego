//go:build !pego_budget_controls_reference

package bench

import (
	"context"
	"testing"

	"github.com/ornew/pego"
)

// BenchmarkBudgetControlModes separates warm context polling from finite counters.
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
							opts := []pego.ParseOption{pego.WithBackend(backend.b), pego.WithUnit(unit.u)}
							if mode == "context" || mode == "combined" {
								ctx, cancel := context.WithCancel(context.Background())
								defer cancel()
								opts = append(opts, pego.WithContext(ctx))
							}
							if mode == "budget" || mode == "combined" {
								opts = append(opts, pego.WithLimits(pego.Limits{Budget: ^uint64(0)}))
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
