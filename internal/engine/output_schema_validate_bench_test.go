package engine

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// outputSchemaUnvalidated has OutputSchema's fields and JSON tags but no
// UnmarshalJSON method, isolating encoding/json's decoding cost from graph
// validation.
type outputSchemaUnvalidated OutputSchema

var outputSchemaValidatedSink OutputSchema
var outputSchemaUnvalidatedSink outputSchemaUnvalidated

func outputSchemaValidationBenchmarkValue(typeCount int) OutputSchema {
	types := make([]SchemaType, typeCount)
	rules := make([]SchemaRule, typeCount)
	for i := 0; i < typeCount; i++ {
		id := fmt.Sprintf("declared/Item%03d", i)
		next := fmt.Sprintf("declared/Item%03d", (i+1)%typeCount)
		types[i] = SchemaType{
			ID:     id,
			Name:   fmt.Sprintf("Item%03d", i),
			Origin: "declared",
			Shape: SchemaShape{Kind: "struct", Fields: []SchemaField{
				{Name: "Name", Origin: "declared", Shape: SchemaShape{Kind: "scalar", Scalar: "string"}},
				{Name: "Next", Origin: "declared", Shape: SchemaShape{Kind: "optional", Element: &SchemaShape{Kind: "ref", Ref: next}}},
			}},
		}
		rules[i] = SchemaRule{
			Name:   fmt.Sprintf("parseItem%03d", i),
			Origin: "declared",
			Result: SchemaShape{Kind: "ref", Ref: id},
		}
	}
	return OutputSchema{
		Format:                  "pego-output-schema",
		Version:                 1,
		Start:                   "parseItem000",
		PositionUnitIndependent: true,
		Types:                   types,
		Rules:                   rules,
	}
}

func BenchmarkOutputSchemaUnmarshalJSON(b *testing.B) {
	for _, typeCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Types%d", typeCount), func(b *testing.B) {
			data, err := json.Marshal(outputSchemaValidationBenchmarkValue(typeCount))
			if err != nil {
				b.Fatal(err)
			}

			var validated OutputSchema
			if err := json.Unmarshal(data, &validated); err != nil {
				b.Fatalf("validate benchmark fixture: %v", err)
			}
			var plain outputSchemaUnvalidated
			if err := json.Unmarshal(data, &plain); err != nil {
				b.Fatalf("decode benchmark fixture: %v", err)
			}
			if !reflect.DeepEqual(validated, OutputSchema(plain)) {
				b.Fatal("validated and unvalidated fixture decodes differ")
			}

			b.Run("UnmarshalValidated", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				b.ReportMetric(float64(len(data)), "B/input")
				for b.Loop() {
					var got OutputSchema
					if err := json.Unmarshal(data, &got); err != nil {
						b.Fatal(err)
					}
					outputSchemaValidatedSink = got
				}
			})
			b.Run("UnmarshalPlainDTO", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				b.ReportMetric(float64(len(data)), "B/input")
				for b.Loop() {
					var got outputSchemaUnvalidated
					if err := json.Unmarshal(data, &got); err != nil {
						b.Fatal(err)
					}
					outputSchemaUnvalidatedSink = got
				}
			})
		})
	}
}
