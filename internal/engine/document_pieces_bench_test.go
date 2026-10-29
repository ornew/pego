package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
)

// BenchmarkDocumentInputEdit isolates storage and memo-splice costs from
// parsing and full-text materialization. The two variants keep length stable.
func BenchmarkDocumentInputEdit(b *testing.B) {
	program, err := Compile(mustParse(`rule main = .*`), compile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []struct {
		name string
		n    int
	}{
		{"1KiB", 1 << 10}, {"4KiB", 4 << 10}, {"8KiB", 8 << 10},
		{"16KiB", 16 << 10}, {"32KiB", 32 << 10}, {"64KiB", 64 << 10},
		{"100KiB", 100 << 10}, {"1MiB", 1 << 20}, {"4MiB", 4 << 20},
	} {
		for _, pos := range []struct {
			name string
			at   int
		}{{"start", 0}, {"middle", size.n / 2}, {"end", size.n - 1}} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, mode := range []string{"insert-delete", "replace"} {
					b.Run(fmt.Sprintf("%s/%s/%s/%s", size.name, pos.name, unit, mode), func(b *testing.B) {
						doc, err := program.NewDocumentWith("main", strings.Repeat("a", size.n), ParseOptions{Unit: unit})
						if err != nil {
							b.Fatal(err)
						}
						b.ReportAllocs()
						i := 0
						for b.Loop() {
							if mode == "replace" {
								text := "z"
								if i%2 != 0 {
									text = "a"
								}
								if err := doc.Edit(pos.at, pos.at+1, text); err != nil {
									b.Fatal(err)
								}
							} else if i%2 == 0 {
								if err := doc.Edit(pos.at, pos.at, "z"); err != nil {
									b.Fatal(err)
								}
							} else if err := doc.Edit(pos.at, pos.at+1, ""); err != nil {
								b.Fatal(err)
							}
							i++
						}
					})
				}
			}
		}
	}
}
