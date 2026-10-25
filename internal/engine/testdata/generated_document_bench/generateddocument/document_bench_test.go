package generateddocument

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	rt "github.com/ornew/pego/runtime"
)

var documentBenchmarkRecordCounts = [...]int{256, 4096} // 1/20 KiB UTF-8

var documentBenchmarkResult *Node

// documentBenchmarkFixture returns a warm Document over base, edited once at its last value and
// restored to base, so that every benchmark operation starts from the same warm state. The edited
// result is checked against a fresh batch parse of the edited text before timing.
func documentBenchmarkFixture(tb testing.TB, unit PositionUnit, records int) (*rt.Document, int, string, string) {
	tb.Helper()
	base := strings.Repeat("é:0;", records)
	edit := len(base) - 2 // the last record's value; one ASCII byte or code point
	if unit == CodePoints {
		edit = utf8.RuneCountInString(base) - 2
	}
	alternate := base[:len(base)-2] + "1;"
	d, err := NewDocument(base, WithUnit(unit))
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := d.Parse(); err != nil {
		tb.Fatal(err)
	}
	if err := d.Edit(edit, edit+1, "1"); err != nil {
		tb.Fatal(err)
	}
	node, err := d.Parse()
	if err != nil {
		tb.Fatalf("initial edited parse: %v", err)
	}
	if d.Stats().Reused == 0 {
		tb.Fatalf("edited parse reused no memo entries: %+v", d.Stats())
	}
	fresh, err := Parse(alternate, WithUnit(unit))
	if err != nil {
		tb.Fatal(err)
	}
	got, _ := json.Marshal(node)
	want, _ := json.Marshal(fresh)
	if string(got) != string(want) {
		tb.Fatal("edited and fresh Node results differ")
	}
	if err := d.Edit(edit, edit+1, "0"); err != nil {
		tb.Fatal(err)
	}
	if _, err := d.Parse(); err != nil {
		tb.Fatal(err)
	}
	return d, edit, base, alternate
}

func TestGeneratedDocumentBenchmarkPreflight(t *testing.T) {
	for _, records := range documentBenchmarkRecordCounts {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			documentBenchmarkFixture(t, unit, records)
		}
	}
}

// BenchmarkNearEOFEditAndParse includes the edit, memo reuse and reparse in each operation. The
// document is warm before timing; each edit alternates the last record's value so that no
// operation is a no-op.
func BenchmarkNearEOFEditAndParse(b *testing.B) {
	for _, records := range documentBenchmarkRecordCounts {
		for _, item := range []struct {
			name string
			unit PositionUnit
		}{
			{"CodePoints", CodePoints},
			{"Bytes", Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+item.name, func(b *testing.B) {
				d, pos, _, _ := documentBenchmarkFixture(b, item.unit, records)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					replacement := "1"
					if i&1 != 0 {
						replacement = "0"
					}
					if err := d.Edit(pos, pos+1, replacement); err != nil {
						b.Fatal(err)
					}
					node, err := d.Parse()
					if err != nil {
						b.Fatal(err)
					}
					documentBenchmarkResult = node
				}
			})
		}
	}
}

// BenchmarkWarmParseNearEOF parses the unchanged warm Document in each operation.
func BenchmarkWarmParseNearEOF(b *testing.B) {
	for _, records := range documentBenchmarkRecordCounts {
		for _, item := range []struct {
			name string
			unit PositionUnit
		}{
			{"CodePoints", CodePoints},
			{"Bytes", Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+item.name, func(b *testing.B) {
				d, _, _, _ := documentBenchmarkFixture(b, item.unit, records)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					node, err := d.Parse()
					if err != nil {
						b.Fatal(err)
					}
					documentBenchmarkResult = node
				}
			})
		}
	}
}

// BenchmarkFreshParseNearEOF parses the two prebuilt post-edit inputs in the same order, with the
// batch Parse of this generated package, excluding input construction.
func BenchmarkFreshParseNearEOF(b *testing.B) {
	for _, records := range documentBenchmarkRecordCounts {
		for _, item := range []struct {
			name string
			unit PositionUnit
		}{
			{"CodePoints", CodePoints},
			{"Bytes", Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+item.name, func(b *testing.B) {
				_, _, base, alternate := documentBenchmarkFixture(b, item.unit, records)
				option := WithUnit(item.unit)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					input := alternate
					if i&1 != 0 {
						input = base
					}
					node, err := Parse(input, option)
					if err != nil {
						b.Fatal(err)
					}
					documentBenchmarkResult = node
				}
			})
		}
	}
}
