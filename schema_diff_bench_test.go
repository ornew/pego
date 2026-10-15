package pego_test

import (
	"fmt"
	"testing"

	"github.com/ornew/pego"
)

var schemaDiffBenchmarkSink *pego.OutputSchemaDiff

func schemaDiffBenchmarkInput(count int, changed bool) *pego.OutputSchema {
	s := &pego.OutputSchema{Format: "pego-output-schema", Version: 1, Start: "parseItem000", PositionUnitIndependent: true}
	for i := range count {
		name := fmt.Sprintf("Item%03d", i)
		rule := fmt.Sprintf("parseItem%03d", i)
		scalar := "string"
		if changed && i == count-1 {
			scalar = "bool"
		}
		s.Types = append(s.Types,
			pego.SchemaType{ID: "declared/" + name, Name: name, Origin: "declared", Shape: pego.SchemaShape{Kind: "struct", Fields: []pego.SchemaField{
				{Name: "Value", Origin: "declared", Shape: pego.SchemaShape{Kind: "scalar", Scalar: scalar}},
			}}},
			pego.SchemaType{ID: "inferred/" + rule, Name: rule, Origin: "inferred", Shape: pego.SchemaShape{Kind: "ref", Ref: "declared/" + name}},
		)
		s.Rules = append(s.Rules, pego.SchemaRule{Name: rule, Origin: "declared", Result: pego.SchemaShape{Kind: "ref", Ref: "inferred/" + rule}})
	}
	return s
}

func BenchmarkDiffOutputSchemas(b *testing.B) {
	for _, count := range []int{16, 128} {
		for _, changed := range []bool{false, true} {
			b.Run(fmt.Sprintf("Types%d/Changed%t", count, changed), func(b *testing.B) {
				before, after := schemaDiffBenchmarkInput(count, false), schemaDiffBenchmarkInput(count, changed)
				b.ReportAllocs()
				for b.Loop() {
					got, err := pego.DiffOutputSchemas(before, after)
					if err != nil {
						b.Fatal(err)
					}
					schemaDiffBenchmarkSink = got
				}
			})
		}
	}
	for _, depth := range []int{4, 8} {
		b.Run(fmt.Sprintf("SharedDAG%d", depth), func(b *testing.B) {
			makeInput := func() *pego.OutputSchema {
				s := schemaDiffBenchmarkInput(1, false)
				shape := &pego.SchemaShape{Kind: "scalar", Scalar: "string"}
				for range depth {
					shape = &pego.SchemaShape{Kind: "union", Alternatives: []pego.SchemaShape{
						{Kind: "optional", Element: shape}, {Kind: "list", Element: shape},
					}}
				}
				s.Types[0].Shape = *shape
				return s
			}
			before, after := makeInput(), makeInput()
			b.ReportAllocs()
			for b.Loop() {
				got, err := pego.DiffOutputSchemas(before, after)
				if err != nil {
					b.Fatal(err)
				}
				schemaDiffBenchmarkSink = got
			}
		})
	}
}
