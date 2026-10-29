package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/internal/schema"
)

var structuralQueryCompileSink *StructuralQuery
var structuralQueryCompileErrorSink error
var querySchemaSink *QuerySchema

// BenchmarkCompileStructuralQuerySchema separates ordinary field checks from
// the shared-inline-DAG nonmatch that previously repeated the same work.
func BenchmarkCompileStructuralQuerySchema(b *testing.B) {
	for _, typeCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Types%d", typeCount), func(b *testing.B) {
			sch := outputSchemaValidationBenchmarkValue(typeCount)
			known := QueryPattern{Type: "Item000", Fields: []QueryFieldPattern{{Name: "Name", Pattern: QueryPattern{Category: "string"}}}}
			b.Run("KnownField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := CompileStructuralQuery(known, &sch)
					if err != nil {
						b.Fatal(err)
					}
					structuralQueryCompileSink = q
				}
			})
			prepared, err := PrepareQuerySchema(&sch)
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
					q, err := PrepareQuerySchema(&sch)
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
			absent := QueryPattern{Type: "Item000", Fields: []QueryFieldPattern{{Name: "Deep", Pattern: QueryPattern{Fields: []QueryFieldPattern{{Name: "Absent"}}}}}}
			b.Run("SharedDAGAbsentField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := CompileStructuralQuery(absent, &sharedSchema)
					if err == nil || q != nil {
						b.Fatalf("accepted impossible field: query=%+v, err=%v", q, err)
					}
					structuralQueryCompileErrorSink = err
				}
			})
			sharedPrepared, err := PrepareQuerySchema(&sharedSchema)
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
