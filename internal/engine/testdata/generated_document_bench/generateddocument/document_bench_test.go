package generateddocument

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

var documentBenchmarkRecordCounts = [...]int{256, 4096} // 1/20 KiB UTF-8

var documentBenchmarkResult *Node

func documentBenchmarkFixture(tb testing.TB, unit Unit, records int) (*documentState, parseOptions, int, string, string) {
	tb.Helper()
	base := strings.Repeat("é:0;", records)
	edit := len(base) - 2 // the last record's value; one ASCII byte/code point
	if unit == CodePoints {
		edit = utf8.RuneCountInString(base) - 2
	}
	alternate := base[:len(base)-2] + "1;"
	d := newDocumentState(rules[0], base, unit)
	o := parseOptions{unit: unit, maxDepth: defaultMaxDepth}
	if _, err := d.parse(o); err != nil {
		tb.Fatal(err)
	}
	if err := d.edit(edit, edit+1, "1"); err != nil {
		tb.Fatal(err)
	}
	node, err := d.parse(o)
	if err != nil || d.stats.Reused == 0 {
		tb.Fatalf("initial edited parse: %v, stats %+v", err, d.stats)
	}
	fresh, freshErr := Parse(alternate, WithUnit(unit))
	if freshErr != nil {
		tb.Fatal(freshErr)
	}
	got, _ := json.Marshal(node)
	want, _ := json.Marshal(fresh)
	if string(got) != string(want) {
		tb.Fatal("edited and fresh Node results differ")
	}
	if err := d.edit(edit, edit+1, "0"); err != nil {
		tb.Fatal(err)
	}
	if _, err := d.parse(o); err != nil {
		tb.Fatal(err)
	}
	return d, o, edit, base, alternate
}

func TestGeneratedDocumentBenchmarkPreflight(t *testing.T) {
	for _, records := range documentBenchmarkRecordCounts {
		for _, unit := range []Unit{CodePoints, Bytes} {
			_, _, _, _, _ = documentBenchmarkFixture(t, unit, records)
		}
	}
}

// BenchmarkNearEOFEditAndParse includes the edit, prefix-memo copying and
// reparse in each operation. The document is warm before timing; each edit
// alternates the last record's value so no operation is a no-op.
func BenchmarkNearEOFEditAndParse(b *testing.B) {
	for _, records := range documentBenchmarkRecordCounts {
		for _, item := range []struct {
			name string
			unit Unit
		}{
			{"CodePoints", CodePoints},
			{"Bytes", Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+item.name, func(b *testing.B) {
				d, options, pos, _, _ := documentBenchmarkFixture(b, item.unit, records)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					replacement := "1"
					if i&1 != 0 {
						replacement = "0"
					}
					if err := d.edit(pos, pos+1, replacement); err != nil {
						b.Fatal(err)
					}
					node, err := d.parse(options)
					if err != nil {
						b.Fatal(err)
					}
					documentBenchmarkResult = node
				}
			})
		}
	}
}

// BenchmarkFreshParseNearEOF parses the two prebuilt post-edit inputs in the
// same order, excluding input construction just as the Document benchmark does.
func BenchmarkFreshParseNearEOF(b *testing.B) {
	for _, records := range documentBenchmarkRecordCounts {
		for _, item := range []struct {
			name string
			unit Unit
		}{
			{"CodePoints", CodePoints},
			{"Bytes", Bytes},
		} {
			b.Run(strconv.Itoa(records)+"/"+item.name, func(b *testing.B) {
				_, _, _, base, alternate := documentBenchmarkFixture(b, item.unit, records)
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
