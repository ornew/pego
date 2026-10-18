package engine

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// BenchmarkDocumentOwnerCreation excludes compilation and parsing. It tracks
// the cold allocation cost of document identity and input representation.
func BenchmarkDocumentOwnerCreation(b *testing.B) {
	p, err := Compile(mustParse(`rule main = .*`), Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{1 << 10, 4 << 10} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			b.Run(fmt.Sprintf("%d/%s", size, unit), func(b *testing.B) {
				text := strings.Repeat("x", size)
				b.ReportAllocs()
				for b.Loop() {
					d, err := p.NewDocumentWith("main", text, ParseOptions{Unit: unit})
					if err != nil {
						b.Fatal(err)
					}
					runtime.KeepAlive(d)
				}
			})
		}
	}
}

// BenchmarkDocumentEditTransaction includes edit validation, publication and
// reparsing. Three equal-length replacements keep sequential and batch
// coordinates identical; documents and initial parse are prepared once.
func BenchmarkDocumentEditTransaction(b *testing.B) {
	p, err := Compile(mustParse(`rule main = line* $$
rule line = [a-z]+ #atomic -"\n"`), Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{4 << 10, 64 << 10} {
		for _, back := range []Backend{Closure, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, mode := range []string{"batch", "sequential"} {
					b.Run(fmt.Sprintf("%d/%s/%s/%s", size, back, unit, mode), func(b *testing.B) {
						d, err := p.NewDocumentWith("main", strings.Repeat("abc\n", size/4), ParseOptions{Backend: back, Unit: unit})
						if err != nil {
							b.Fatal(err)
						}
						if _, err := d.Parse(); err != nil {
							b.Fatal(err)
						}
						edits := []TextEdit{{0, 1, "z"}, {size / 2, size/2 + 1, "z"}, {size - 4, size - 3, "z"}}
						b.ReportAllocs()
						i := 0
						for b.Loop() {
							text := "z"
							if i%2 != 0 {
								text = "a"
							}
							for j := range edits {
								edits[j].Text = text
							}
							if mode == "batch" {
								if err := d.EditBatch(d.Version(), edits...); err != nil {
									b.Fatal(err)
								}
							} else {
								for _, edit := range edits {
									if err := d.Edit(edit.Start, edit.End, edit.Text); err != nil {
										b.Fatal(err)
									}
								}
							}
							if _, err := d.Parse(); err != nil {
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
