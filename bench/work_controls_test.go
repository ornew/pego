//go:build !pego_work_controls_reference

package bench

import (
	"encoding/json"
	"testing"

	"github.com/ornew/pego"
	gjson "github.com/ornew/pego/bench/gen/json"
	gxml "github.com/ornew/pego/bench/gen/xml"
)

// BenchmarkControlledParse uses finite but effectively inexhaustible counters
// to measure the active instrumentation path without cancellation callbacks.
// Ordinary BenchmarkParse remains the inactive-control measurement.
func BenchmarkControlledParse(b *testing.B) {
	for i := range workloads {
		w := &workloads[i]
		if w.name != "JSON" && w.name != "XML" {
			continue
		}
		b.Run(w.name, func(b *testing.B) {
			p := load(b, w.grammar)
			input := w.input(1)
			limits := pego.WorkLimits{MaxInputWork: ^uint64(0), MaxSteps: ^uint64(0)}
			for _, backend := range backends {
				for _, unit := range units {
					b.Run(backend.name+"/"+unit.name, func(b *testing.B) {
						// Initialize lazy instrumentation before measuring repeated calls.
						if err := parseOK(p, w, input, pego.WithBackend(backend.b), pego.WithUnit(unit.u), pego.WithWorkLimits(limits)); err != nil {
							b.Fatal(err)
						}
						b.SetBytes(int64(len(input)))
						b.ReportAllocs()
						for b.Loop() {
							if err := parseOK(p, w, input, pego.WithBackend(backend.b), pego.WithUnit(unit.u), pego.WithWorkLimits(limits)); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
			for _, unit := range units {
				b.Run("generated/"+unit.name, func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						var err error
						if w.name == "JSON" {
							selected := gjson.CodePoints
							if unit.u == pego.Bytes {
								selected = gjson.Bytes
							}
							_, err = gjson.Parse(input, gjson.WithUnit(selected), gjson.WithWorkLimits(gjson.WorkLimits(limits)))
						} else {
							selected := gxml.CodePoints
							if unit.u == pego.Bytes {
								selected = gxml.Bytes
							}
							_, err = gxml.Parse(input, gxml.WithUnit(selected), gxml.WithWorkLimits(gxml.WorkLimits(limits)))
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				})
				b.Run("generated_ast/"+unit.name, func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						var err error
						if w.name == "JSON" {
							selected := gjson.CodePoints
							if unit.u == pego.Bytes {
								selected = gjson.Bytes
							}
							_, err = gjson.ParseAST(input, gjson.WithUnit(selected), gjson.WithWorkLimits(gjson.WorkLimits(limits)))
						} else {
							selected := gxml.CodePoints
							if unit.u == pego.Bytes {
								selected = gxml.Bytes
							}
							_, err = gxml.ParseAST(input, gxml.WithUnit(selected), gxml.WithWorkLimits(gxml.WorkLimits(limits)))
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

// TestControlledWorkloads compares results on the representative active-counter
// workloads before using their benchmarks to assess instrumentation cost.
func TestControlledWorkloads(t *testing.T) {
	for i := range workloads {
		w := &workloads[i]
		if w.name != "JSON" && w.name != "XML" {
			continue
		}
		t.Run(w.name, func(t *testing.T) {
			input := w.input(10)
			p := load(t, w.grammar)
			limits := pego.WorkLimits{MaxInputWork: ^uint64(0), MaxSteps: ^uint64(0)}
			equal := func(plain, controlled any) {
				t.Helper()
				a, err := json.Marshal(plain)
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(controlled)
				if err != nil {
					t.Fatal(err)
				}
				if string(a) != string(b) {
					t.Fatal("active work counters changed parse result")
				}
			}
			for _, backend := range backends {
				for _, unit := range units {
					opts := []pego.ParseOption{pego.WithBackend(backend.b), pego.WithUnit(unit.u)}
					plain, err := p.Parse(input, opts...)
					if err != nil {
						t.Fatal(err)
					}
					controlled, err := p.Parse(input, append(opts, pego.WithWorkLimits(limits))...)
					if err != nil {
						t.Fatal(err)
					}
					equal(plain, controlled)
				}
			}
			for _, unit := range units {
				if w.name == "JSON" {
					selected := gjson.CodePoints
					if unit.u == pego.Bytes {
						selected = gjson.Bytes
					}
					plain, err := gjson.Parse(input, gjson.WithUnit(selected))
					if err != nil {
						t.Fatal(err)
					}
					controlled, err := gjson.Parse(input, gjson.WithUnit(selected), gjson.WithWorkLimits(gjson.WorkLimits(limits)))
					if err != nil {
						t.Fatal(err)
					}
					equal(plain, controlled)
					plainAST, err := gjson.ParseAST(input, gjson.WithUnit(selected))
					if err != nil {
						t.Fatal(err)
					}
					controlledAST, err := gjson.ParseAST(input, gjson.WithUnit(selected), gjson.WithWorkLimits(gjson.WorkLimits(limits)))
					if err != nil {
						t.Fatal(err)
					}
					equal(plainAST, controlledAST)
				} else {
					selected := gxml.CodePoints
					if unit.u == pego.Bytes {
						selected = gxml.Bytes
					}
					plain, err := gxml.Parse(input, gxml.WithUnit(selected))
					if err != nil {
						t.Fatal(err)
					}
					controlled, err := gxml.Parse(input, gxml.WithUnit(selected), gxml.WithWorkLimits(gxml.WorkLimits(limits)))
					if err != nil {
						t.Fatal(err)
					}
					equal(plain, controlled)
					plainAST, err := gxml.ParseAST(input, gxml.WithUnit(selected))
					if err != nil {
						t.Fatal(err)
					}
					controlledAST, err := gxml.ParseAST(input, gxml.WithUnit(selected), gxml.WithWorkLimits(gxml.WorkLimits(limits)))
					if err != nil {
						t.Fatal(err)
					}
					equal(plainAST, controlledAST)
				}
			}
		})
	}
}
