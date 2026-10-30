//go:build !pego_budget_controls_reference

package main

import (
	"testing"

	"github.com/ornew/pego"
)

func TestCompiledCorpusBudgetParity(t *testing.T) {
	for _, name := range []string{"json", "xml", "arithmetic", "minilang"} {
		t.Run(name, func(t *testing.T) {
			source, input, err := corpus("../..", name)
			if err != nil {
				t.Fatal(err)
			}
			p, err := pego.CompileSource(source, "main")
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
				var want string
				for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
					for _, active := range []bool{false, true} {
						opts := []pego.ParseOption{pego.WithBackend(backend), pego.WithUnit(unit)}
						if active {
							opts = append(opts, pego.WithLimits(pego.Limits{Budget: ^uint64(0)}))
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
						n, err = p.Parse(input, append(opts, pego.RecognizeOnly())...)
						if n != nil || err != nil {
							t.Fatalf("backend=%v unit=%v active=%v: recognition node=%v error=%v", backend, unit, active, n, err)
						}
					}
				}
			}
		})
	}
}
