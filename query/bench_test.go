package query_test

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/query"
	"github.com/ornew/pego/schema"
)

var compileSink *query.Query
var compileErrorSink error
var schemaSink *query.Schema

// benchmarkSchema returns a valid schema of typeCount struct types, each with a string field Name
// and an optional field Next that refers to the next type, and a rule for each type.
func benchmarkSchema(typeCount int) schema.Output {
	types := make([]schema.Type, typeCount)
	rules := make([]schema.Rule, typeCount)
	for i := 0; i < typeCount; i++ {
		id := fmt.Sprintf("declared/Item%03d", i)
		next := fmt.Sprintf("declared/Item%03d", (i+1)%typeCount)
		types[i] = schema.Type{
			ID:     id,
			Name:   fmt.Sprintf("Item%03d", i),
			Origin: "declared",
			Shape: schema.Shape{Kind: "struct", Fields: []schema.Field{
				{Name: "Name", Shape: schema.Shape{Kind: "scalar", Scalar: "string"}},
				{Name: "Next", Shape: schema.Shape{Kind: "optional", Element: &schema.Shape{Kind: "ref", Ref: next}}},
			}},
		}
		rules[i] = schema.Rule{
			Name:   fmt.Sprintf("parseItem%03d", i),
			Origin: "declared",
			Result: schema.Shape{Kind: "ref", Ref: fmt.Sprintf("inferred/parseItem%03d", i)},
		}
	}
	for i := range rules {
		types = append(types, schema.Type{
			ID: rules[i].Result.Ref, Name: rules[i].Name, Origin: "inferred",
			Shape: schema.Shape{Kind: "ref", Ref: types[i].ID},
		})
	}
	return schema.Output{
		Format:                  schema.Format,
		Version:                 schema.Version,
		Start:                   "parseItem000",
		PositionUnitIndependent: true,
		Types:                   types,
		Rules:                   rules,
	}
}

// BenchmarkCompile separates ordinary field checks from the shared-inline-DAG nonmatch, whose
// shapes must each be checked once.
func BenchmarkCompile(b *testing.B) {
	for _, typeCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Types%d", typeCount), func(b *testing.B) {
			sch := benchmarkSchema(typeCount)
			known := query.Pattern{Type: "Item000", Fields: []query.FieldPattern{{Name: "Name", Pattern: query.Pattern{Category: "string"}}}}
			b.Run("KnownField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := query.Compile(known, &sch)
					if err != nil {
						b.Fatal(err)
					}
					compileSink = q
				}
			})
			prepared, err := query.Prepare(&sch)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("KnownFieldPrepared", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := prepared.Compile(known)
					if err != nil {
						b.Fatal(err)
					}
					compileSink = q
				}
			})
			b.Run("Prepare", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := query.Prepare(&sch)
					if err != nil {
						b.Fatal(err)
					}
					schemaSink = q
				}
			})

			sharedSchema := benchmarkSchema(typeCount)
			shared := &schema.Shape{Kind: "scalar", Scalar: "string"}
			for range 16 {
				shared = &schema.Shape{Kind: "join", Alternatives: []schema.Shape{
					{Kind: "optional", Element: shared},
					{Kind: "optional", Element: shared},
				}}
			}
			sharedSchema.Types[0].Shape.Fields = append(sharedSchema.Types[0].Shape.Fields, schema.Field{Name: "Deep", Shape: *shared})
			absent := query.Pattern{Type: "Item000", Fields: []query.FieldPattern{{Name: "Deep", Pattern: query.Pattern{Fields: []query.FieldPattern{{Name: "Absent"}}}}}}
			b.Run("SharedDAGAbsentField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := query.Compile(absent, &sharedSchema)
					if err == nil || q != nil {
						b.Fatalf("accepted impossible field: query=%+v, err=%v", q, err)
					}
					compileErrorSink = err
				}
			})
			sharedPrepared, err := query.Prepare(&sharedSchema)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("SharedDAGAbsentFieldPrepared", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := sharedPrepared.Compile(absent)
					if err == nil || q != nil {
						b.Fatalf("accepted impossible field: query=%+v, err=%v", q, err)
					}
					compileErrorSink = err
				}
			})
		})
	}
}
