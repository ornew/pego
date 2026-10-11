package golang_test

import (
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

// benchFiles are large files of the standard library, read from GOROOT/src: a parser, an HTTP
// server and the scheduler (457 KB together).
var benchFiles = []string{"go/parser/parser.go", "net/http/server.go", "runtime/proc.go"}

func benchInputs(b *testing.B) []string {
	var srcs []string
	for _, name := range benchFiles {
		data, err := os.ReadFile(filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(name)))
		if err != nil {
			b.Skipf("GOROOT sources are not available: %v", err)
		}
		srcs = append(srcs, string(data))
	}
	return srcs
}

func bench(b *testing.B, parse func(src string) error) {
	srcs := benchInputs(b)
	n := 0
	for _, s := range srcs {
		n += len(s)
	}
	b.SetBytes(int64(n))
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range srcs {
			if err := parse(s); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkParseAST(b *testing.B) {
	bench(b, func(src string) error {
		_, err := golang.ParseAST(src, golang.WithUnit(golang.Bytes))
		return err
	})
}

func BenchmarkParse(b *testing.B) {
	bench(b, func(src string) error {
		_, err := golang.Parse(src, golang.WithUnit(golang.Bytes))
		return err
	})
}

func BenchmarkRecognize(b *testing.B) {
	bench(b, func(src string) error { return golang.Recognize(src, golang.WithUnit(golang.Bytes)) })
}

// BenchmarkParseFile parses into go/ast: ParseAST and ToGoAST.
func BenchmarkParseFile(b *testing.B) {
	bench(b, func(src string) error {
		_, err := golang.ParseFile(token.NewFileSet(), "x.go", []byte(src), 0)
		return err
	})
}

// The standard library, for comparison.

func BenchmarkGoParser(b *testing.B) {
	bench(b, func(src string) error {
		_, err := goparser.ParseFile(token.NewFileSet(), "x.go", src, goparser.SkipObjectResolution)
		return err
	})
}
