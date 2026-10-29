package engine

import (
	"fmt"
	"testing"

	ischema "github.com/ornew/pego/internal/schema"
	"github.com/ornew/pego/schema"
)

var structuralQueryCompileSink *ischema.StructuralQuery
var structuralQueryCompileErrorSink error
var querySchemaSink *ischema.QuerySchema

// BenchmarkCompileStructuralQuerySchema separates ordinary field checks from
// the shared-inline-DAG nonmatch that previously repeated the same work.
func BenchmarkCompileStructuralQuerySchema(b *testing.B) {
	for _, typeCount := range []int{16, 128} {
		b.Run(fmt.Sprintf("Types%d", typeCount), func(b *testing.B) {
			sch := outputSchemaValidationBenchmarkValue(typeCount)
			known := ischema.QueryPattern{Type: "Item000", Fields: []ischema.QueryFieldPattern{{Name: "Name", Pattern: ischema.QueryPattern{Category: "string"}}}}
			b.Run("KnownField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := ischema.CompileStructuralQuery(known, &sch)
					if err != nil {
						b.Fatal(err)
					}
					structuralQueryCompileSink = q
				}
			})
			prepared, err := ischema.PrepareQuerySchema(&sch)
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
					q, err := ischema.PrepareQuerySchema(&sch)
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
			absent := ischema.QueryPattern{Type: "Item000", Fields: []ischema.QueryFieldPattern{{Name: "Deep", Pattern: ischema.QueryPattern{Fields: []ischema.QueryFieldPattern{{Name: "Absent"}}}}}}
			b.Run("SharedDAGAbsentField", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					q, err := ischema.CompileStructuralQuery(absent, &sharedSchema)
					if err == nil || q != nil {
						b.Fatalf("accepted impossible field: query=%+v, err=%v", q, err)
					}
					structuralQueryCompileErrorSink = err
				}
			})
			sharedPrepared, err := ischema.PrepareQuerySchema(&sharedSchema)
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
