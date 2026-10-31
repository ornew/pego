package schema_test

import (
	"fmt"
	"testing"

	"github.com/ornew/zeroparse/schema"
)

var schemaDiffBenchmarkSink *schema.Diff

func schemaDiffBenchmarkInput(count int, changed bool) *schema.Output {
	s := &schema.Output{Format: "zp-output-schema", Version: 2, Start: "parseItem000", PositionUnitIndependent: true}
	for i := range count {
		name := fmt.Sprintf("Item%03d", i)
		rule := fmt.Sprintf("parseItem%03d", i)
		scalar := "string"
		if changed && i == count-1 {
			scalar = "bool"
		}
		s.Types = append(s.Types,
			schema.Type{ID: "declared/" + name, Name: name, Origin: "declared", Shape: schema.Shape{Kind: "struct", Fields: []schema.Field{
				{Name: "Value", Shape: schema.Shape{Kind: "scalar", Scalar: scalar}},
			}}},
			schema.Type{ID: "inferred/" + rule, Name: rule, Origin: "inferred", Shape: schema.Shape{Kind: "ref", Ref: "declared/" + name}},
		)
		s.Rules = append(s.Rules, schema.Rule{Name: rule, Origin: "declared", Result: schema.Shape{Kind: "ref", Ref: "inferred/" + rule}})
	}
	return s
}

func BenchmarkCompare(b *testing.B) {
	for _, count := range []int{16, 128} {
		for _, changed := range []bool{false, true} {
			b.Run(fmt.Sprintf("Types%d/Changed%t", count, changed), func(b *testing.B) {
				before, after := schemaDiffBenchmarkInput(count, false), schemaDiffBenchmarkInput(count, changed)
				b.ReportAllocs()
				for b.Loop() {
					got, err := schema.Compare(before, after)
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
			makeInput := func() *schema.Output {
				s := schemaDiffBenchmarkInput(1, false)
				shape := &schema.Shape{Kind: "scalar", Scalar: "string"}
				for range depth {
					shape = &schema.Shape{Kind: "join", Alternatives: []schema.Shape{
						{Kind: "optional", Element: shape}, {Kind: "list", Element: shape},
					}}
				}
				s.Types[0].Shape.Fields[0].Shape = *shape
				return s
			}
			before, after := makeInput(), makeInput()
			b.ReportAllocs()
			for b.Loop() {
				got, err := schema.Compare(before, after)
				if err != nil {
					b.Fatal(err)
				}
				schemaDiffBenchmarkSink = got
			}
		})
	}
}
