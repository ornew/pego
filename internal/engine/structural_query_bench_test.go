package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/internal/schema"
)

var structuralQueryCompileSink *schema.StructuralQuery
var structuralQueryCompileErrorSink error
var querySchemaSink *schema.QuerySchema

// BenchmarkCompileStructuralQuerySchema separates ordinary field checks from
// the shared-inline-DAG nonmatch that previously repeated the same work.
func BenchmarkCompileStructuralQuerySchema(b *testing.B) {
	for _, typeCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Types%d", typeCount), func(b *testing.B) {
			sch := outputSchemaValidationBenchmarkValue(typeCount)
			known := schema.QueryPattern{Type: "Item000", Fields: []schema.QueryFieldPattern{{Name: "Name", Pattern: schema.QueryPattern{Category: "string"}}}}
			b.Run("KnownField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := schema.CompileStructuralQuery(known, &sch)
					if err != nil {
						b.Fatal(err)
					}
					structuralQueryCompileSink = q
				}
			})
			prepared, err := schema.PrepareQuerySchema(&sch)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("KnownFieldPrepared", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := prepared.CompileStructuralQuery(known)
					if err != nil {
						b.Fatal(err)
					}
					structuralQueryCompileSink = q
				}
			})
			b.Run("Prepare", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := schema.PrepareQuerySchema(&sch)
					if err != nil {
						b.Fatal(err)
					}
					querySchemaSink = q
				}
			})

			sharedSchema := outputSchemaValidationBenchmarkValue(typeCount)
			shared := &schema.Shape{Kind: "scalar", Scalar: "string"}
			for range 16 {
				shared = &schema.Shape{Kind: "join", Alternatives: []schema.Shape{
					{Kind: "optional", Element: shared},
					{Kind: "optional", Element: shared},
				}}
			}
			sharedSchema.Types[0].Shape.Fields = append(sharedSchema.Types[0].Shape.Fields, schema.Field{Name: "Deep", Shape: *shared})
			absent := schema.QueryPattern{Type: "Item000", Fields: []schema.QueryFieldPattern{{Name: "Deep", Pattern: schema.QueryPattern{Fields: []schema.QueryFieldPattern{{Name: "Absent"}}}}}}
			b.Run("SharedDAGAbsentField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := schema.CompileStructuralQuery(absent, &sharedSchema)
					if err == nil || q != nil {
						b.Fatalf("accepted impossible field: query=%+v, err=%v", q, err)
					}
					structuralQueryCompileErrorSink = err
				}
			})
			sharedPrepared, err := schema.PrepareQuerySchema(&sharedSchema)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("SharedDAGAbsentFieldPrepared", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := sharedPrepared.CompileStructuralQuery(absent)
					if err == nil || q != nil {
						b.Fatalf("accepted impossible field: query=%+v, err=%v", q, err)
					}
					structuralQueryCompileErrorSink = err
				}
			})
		})
	}
}
