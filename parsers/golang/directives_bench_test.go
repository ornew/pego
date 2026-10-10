package golang_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

// BenchmarkLineDirectives exercises ordinary directive fields and the native
// unsigned-to-signed wrap accepted by go/scanner on each target width.
func BenchmarkLineDirectives(b *testing.B) {
	wrapped := "9223372036854775808"
	if strconv.IntSize == 32 {
		wrapped = "2147483648"
	}
	for name, number := range map[string]string{"ordinary": "123", "wrapped": wrapped} {
		var src strings.Builder
		src.WriteString("package p\n")
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(&src, "//line generated.go:%s:7\nvar x%d int\n", number, i)
		}
		input := src.String()
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := golang.ParseAST(input, golang.Bytes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
