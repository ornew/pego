package generateddocumentbench

import (
	"strconv"
	"strings"
	"testing"

	generatedbatch "generateddocumentbench/generatedbatch"
	generateddocument "generateddocumentbench/generateddocument"
)

const benchmarkRecords = 4096 // 20 KiB UTF-8, 16 Ki code points

var batchWithoutDocumentResult *generatedbatch.Node
var batchWithDocumentResult *generateddocument.Node

// BenchmarkBatchGenerationFlag compares ordinary batch parsing of the same
// grammar and input, generated with Document support off/on. The off variant
// disables Document branches at compile time. Revision-to-revision measurements
// are still needed to assess the full cost of the generator change.
func BenchmarkBatchGenerationFlag(b *testing.B) {
	input := strings.Repeat("é:0;", benchmarkRecords)
	for _, unit := range []struct {
		name        string
		withoutUnit generatedbatch.PositionUnit
		withUnit    generateddocument.PositionUnit
	}{
		{"CodePoints", generatedbatch.CodePoints, generateddocument.CodePoints},
		{"Bytes", generatedbatch.Bytes, generateddocument.Bytes},
	} {
		b.Run(unit.name+"/without-document", func(b *testing.B) {
			option := generatedbatch.WithUnit(unit.withoutUnit)
			if _, err := generatedbatch.Parse(input, option); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				node, err := generatedbatch.Parse(input, option)
				if err != nil {
					b.Fatal(err)
				}
				batchWithoutDocumentResult = node
			}
		})
		b.Run(unit.name+"/with-document", func(b *testing.B) {
			option := generateddocument.WithUnit(unit.withUnit)
			if _, err := generateddocument.Parse(input, option); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				node, err := generateddocument.Parse(input, option)
				if err != nil {
					b.Fatal(err)
				}
				batchWithDocumentResult = node
			}
		})
	}
}

// BenchmarkNearEOFControl is the same-source disabled control of BenchmarkNearEOFEditAndParse:
// the batch Parse of the generated package compiled without Document support, applied to the
// two near-EOF inputs in the same order.
func BenchmarkNearEOFControl(b *testing.B) {
	for _, records := range []int{256, 4096} {
		base := strings.Repeat("é:0;", records)
		alternate := base[:len(base)-2] + "1;"
		for _, unit := range []struct {
			name string
			unit generatedbatch.PositionUnit
		}{
			{"CodePoints", generatedbatch.CodePoints},
			{"Bytes", generatedbatch.Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+unit.name, func(b *testing.B) {
				option := generatedbatch.WithUnit(unit.unit)
				if _, err := generatedbatch.Parse(alternate, option); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					input := alternate
					if i&1 != 0 {
						input = base
					}
					node, err := generatedbatch.Parse(input, option)
					if err != nil {
						b.Fatal(err)
					}
					batchWithoutDocumentResult = node
				}
			})
		}
	}
}
