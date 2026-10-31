//go:build !zp_budget_controls_reference

package main

import (
	"testing"

	"github.com/ornew/zeroparse"
)

func TestCompiledCorpusBudgetParity(t *testing.T) {
	for _, name := range []string{"json", "xml", "arithmetic", "minilang"} {
		t.Run(name, func(t *testing.T) {
			source, input, err := corpus("../..", name)
			if err != nil {
				t.Fatal(err)
			}
			p, err := zp.CompileSource(source, "main")
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
				var want string
				for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
					for _, active := range []bool{false, true} {
						opts := []zp.ParseOption{zp.WithBackend(backend), zp.WithUnit(unit)}
						if active {
							// The largest finite budget: ^uint64(0) is Unlimited, which runs without a control.
							opts = append(opts, zp.WithLimits(zp.Limits{Budget: ^uint64(0) - 1}))
						} else {
							opts = append(opts, zp.WithLimits(zp.Limits{Budget: zp.Unlimited}))
						}
						n, err := p.Parse(input, opts...)
						if err != nil {
							t.Fatalf("backend=%v unit=%v active=%v: %v", backend, unit, active, err)
						}
						got, err := fingerprint(n)
						if err != nil {
							t.Fatal(err)
						}
						if want == "" {
							want = got
						} else if got != want {
							t.Fatalf("backend=%v unit=%v active=%v: result differs", backend, unit, active)
						}
						n, err = p.Parse(input, append(opts, zp.RecognizeOnly())...)
						if n != nil || err != nil {
							t.Fatalf("backend=%v unit=%v active=%v: recognition node=%v error=%v", backend, unit, active, n, err)
						}
					}
				}
			}
		})
	}
}
