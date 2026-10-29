package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
)

var documentDiagnosticBenchmarkError error

func BenchmarkDocumentDiagnosticPublication(b *testing.B) {
	prog, err := Compile(mustParse(`rule main = item* $$
rule item = "a;" @error(message="original") #recover(skip="?;")`), compile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, count := range []int{0, 1, 128} {
				b.Run(fmt.Sprintf("%s/%s/%d", backend, unit, count), func(b *testing.B) {
					text := strings.Repeat("?;", count)
					if count == 0 {
						text = strings.Repeat("a;", 128)
					}
					d, err := prog.NewDocumentWith("main", text, ParseOptions{Backend: backend, Unit: unit})
					if err != nil {
						b.Fatal(err)
					}
					check := func() {
						_, err := d.Parse()
						if count == 0 {
							if err != nil {
								b.Fatal(err)
							}
						} else if e, ok := err.(SyntaxErrors); !ok || len(e) != count {
							b.Fatalf("recovered diagnostics: %v", err)
						}
						documentDiagnosticBenchmarkError = err
					}
					check()
					b.ReportAllocs()
					for b.Loop() {
						check()
					}
				})
			}
		}
	}
}
