package generateddocumentbench

import (
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	typedbatch "generateddocumentbench/typedbatch"
	typeddocument "generateddocumentbench/typeddocument"
)

var typedBatchBenchmarkResult []*typedbatch.Item
var typedDocumentBenchmarkResult []*typeddocument.Item

func typedBenchmarkInput(records int) string {
	return strings.Repeat("é😀=123;", records)
}

func TestGeneratedTypedDocumentBenchmarkPreflight(t *testing.T) {
	for _, records := range []int{256, 4096} {
		base := typedBenchmarkInput(records)
		alternate := base[:len(base)-2] + "4;"
		for _, unit := range []typeddocument.Unit{typeddocument.CodePoints, typeddocument.Bytes} {
			pos := len(base) - 2
			if unit == typeddocument.CodePoints {
				pos = utf8.RuneCountInString(base) - 2
			}
			d, err := typeddocument.NewDocument(base, typeddocument.WithUnit(unit))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.ParseAST(); err != nil {
				t.Fatal(err)
			}
			if err := d.Edit(pos, pos+1, "4"); err != nil {
				t.Fatal(err)
			}
			got, gotErr := d.ParseAST()
			want, wantErr := typeddocument.ParseAST(alternate, typeddocument.WithUnit(unit))
			if gotErr != nil || wantErr != nil || !reflect.DeepEqual(got, want) || d.Stats().Reused == 0 {
				t.Fatalf("%d/%v: edited typed result differs from fresh parse: %v / %v, stats %+v", records, unit, gotErr, wantErr, d.Stats())
			}
		}
	}
}

// BenchmarkTypedBatchGenerationFlag measures the direct typed batch path with
// Document generation off and on. It isolates the opt-in generator flag from
// the incremental API; revision comparisons are needed for absolute cost.
func BenchmarkTypedBatchGenerationFlag(b *testing.B) {
	for _, records := range []int{256, 4096} {
		input := typedBenchmarkInput(records)
		for _, unit := range []struct {
			name         string
			batchUnit    typedbatch.Unit
			documentUnit typeddocument.Unit
		}{
			{"CodePoints", typedbatch.CodePoints, typeddocument.CodePoints},
			{"Bytes", typedbatch.Bytes, typeddocument.Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+unit.name+"/without-document", func(b *testing.B) {
				opt := typedbatch.WithUnit(unit.batchUnit)
				if got, err := typedbatch.ParseAST(input, opt); err != nil || len(got) != records {
					b.Fatalf("preflight: %d items, %v", len(got), err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, err := typedbatch.ParseAST(input, opt)
					if err != nil {
						b.Fatal(err)
					}
					typedBatchBenchmarkResult = got
				}
			})
			b.Run(strconv.Itoa(records)+"/"+unit.name+"/with-document", func(b *testing.B) {
				opt := typeddocument.WithUnit(unit.documentUnit)
				if got, err := typeddocument.ParseAST(input, opt); err != nil || len(got) != records {
					b.Fatalf("preflight: %d items, %v", len(got), err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, err := typeddocument.ParseAST(input, opt)
					if err != nil {
						b.Fatal(err)
					}
					typedDocumentBenchmarkResult = got
				}
			})
		}
	}
}

// BenchmarkTypedDocumentNearEOF includes a one-character edit, memo reuse,
// matching and final typed publication. The source is warm before timing.
func BenchmarkTypedDocumentNearEOF(b *testing.B) {
	for _, records := range []int{256, 4096} {
		base := typedBenchmarkInput(records)
		for _, unit := range []struct {
			name string
			unit typeddocument.Unit
		}{
			{"CodePoints", typeddocument.CodePoints},
			{"Bytes", typeddocument.Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+unit.name, func(b *testing.B) {
				pos := len(base) - 2
				if unit.unit == typeddocument.CodePoints {
					pos = utf8.RuneCountInString(base) - 2
				}
				d, err := typeddocument.NewDocument(base, typeddocument.WithUnit(unit.unit))
				if err != nil {
					b.Fatal(err)
				}
				if got, err := d.ParseAST(); err != nil || len(got) != records {
					b.Fatalf("preflight: %d items, %v", len(got), err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					replacement := "4"
					if i&1 != 0 {
						replacement = "3"
					}
					if err := d.Edit(pos, pos+1, replacement); err != nil {
						b.Fatal(err)
					}
					got, err := d.ParseAST()
					if err != nil {
						b.Fatal(err)
					}
					typedDocumentBenchmarkResult = got
				}
			})
		}
	}
}

// BenchmarkTypedDocumentNoEdit isolates the cost of converting a warm,
// memoized Node result on each call without charging for an Edit.
func BenchmarkTypedDocumentNoEdit(b *testing.B) {
	for _, records := range []int{256, 4096} {
		base := typedBenchmarkInput(records)
		for _, unit := range []struct {
			name string
			unit typeddocument.Unit
		}{
			{"CodePoints", typeddocument.CodePoints},
			{"Bytes", typeddocument.Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+unit.name, func(b *testing.B) {
				d, err := typeddocument.NewDocument(base, typeddocument.WithUnit(unit.unit))
				if err != nil {
					b.Fatal(err)
				}
				if got, err := d.ParseAST(); err != nil || len(got) != records {
					b.Fatalf("preflight: %d items, %v", len(got), err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, err := d.ParseAST()
					if err != nil {
						b.Fatal(err)
					}
					typedDocumentBenchmarkResult = got
				}
			})
		}
	}
}

// BenchmarkTypedDocumentFresh includes construction, matching and publication.
func BenchmarkTypedDocumentFresh(b *testing.B) {
	for _, unit := range []struct {
		name string
		unit typeddocument.Unit
	}{{"CodePoints", typeddocument.CodePoints}, {"Bytes", typeddocument.Bytes}} {
		b.Run(unit.name, func(b *testing.B) {
			input := typedBenchmarkInput(4096)
			b.ReportAllocs()
			for b.Loop() {
				d, err := typeddocument.NewDocument(input, typeddocument.WithUnit(unit.unit))
				if err != nil {
					b.Fatal(err)
				}
				result, err := d.ParseAST()
				if err != nil {
					b.Fatal(err)
				}
				typedDocumentBenchmarkResult = result
			}
			b.ReportMetric(float64(len(input)), "B/input")
		})
	}
}

// TestGeneratedTypedDocumentRetained is an opt-in retained-heap probe. Run it
// separately from timing, in a fresh test process, with no other tests selected.
func TestGeneratedTypedDocumentRetained(t *testing.T) {
	if os.Getenv("PEGO_GENERATED_DOCUMENT_RETAINED") == "" {
		t.Skip("set PEGO_GENERATED_DOCUMENT_RETAINED for the isolated heap probe")
	}
	for _, unit := range []struct {
		name string
		unit typeddocument.Unit
	}{{"CodePoints", typeddocument.CodePoints}, {"Bytes", typeddocument.Bytes}} {
		for _, edits := range []int{0, 32} {
			func() {
				input := typedBenchmarkInput(4096)
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				d, err := typeddocument.NewDocument(input, typeddocument.WithUnit(unit.unit))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := d.ParseAST(); err != nil {
					t.Fatal(err)
				}
				pos := len(input) - 2
				if unit.unit == typeddocument.CodePoints {
					pos = utf8.RuneCountInString(input) - 2
				}
				for i := 0; i < edits; i++ {
					text := "4"
					if i%2 == 1 {
						text = "3"
					}
					if err := d.Edit(pos, pos+1, text); err != nil {
						t.Fatal(err)
					}
					if _, err := d.ParseAST(); err != nil {
						t.Fatal(err)
					}
				}
				runtime.GC()
				runtime.ReadMemStats(&after)
				t.Logf("%s/%d-edits: %d retained bytes, %d objects", unit.name, edits, int64(after.HeapAlloc)-int64(before.HeapAlloc), int64(after.HeapObjects)-int64(before.HeapObjects))
				runtime.KeepAlive(d)
			}()
		}
	}
}
