package python_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/parsers/python"
)

// benchFiles are two of the largest modules of the standard library, the inputs of the benchmarks
// (CPython 3.14.0's Lib, vendored in testdata/bench; PEGO_BENCH_DIR names another directory with
// files of these names). There is no Python parser in the Go standard library to compare with:
// CPython's own, ast.parse, is measured on the same files with the command in the README.
var benchFiles = []string{"_pydecimal.py", "typing.py"}

func benchmark(b *testing.B, parse func(string) error) {
	dir := os.Getenv("PEGO_BENCH_DIR")
	if dir == "" {
		dir = filepath.Join("testdata", "bench")
	}
	for _, name := range benchFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			b.Fatal(err)
		}
		src := string(data)
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				if err := parse(src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParseAST parses to the typed AST, without the checks of ParseModule.
func BenchmarkParseAST(b *testing.B) {
	benchmark(b, func(src string) error { _, err := python.ParseAST(src); return err })
}

// BenchmarkParseASTBytes measures the same typed inputs with byte positions.
func BenchmarkParseASTBytes(b *testing.B) {
	benchmark(b, func(src string) error { _, err := python.ParseAST(src, python.WithUnit(python.Bytes)); return err })
}

// BenchmarkParseModule is ParseAST and the checks that ast.parse makes outside its grammar.
func BenchmarkParseModule(b *testing.B) {
	benchmark(b, func(src string) error { _, err := python.ParseModule(src); return err })
}

// BenchmarkParse builds the generic tree of *Node.
func BenchmarkParse(b *testing.B) {
	benchmark(b, func(src string) error { _, err := python.Parse(src); return err })
}

// BenchmarkRecognize only checks the syntax.
func BenchmarkRecognize(b *testing.B) {
	benchmark(b, func(src string) error { return python.Recognize(src) })
}

// BenchmarkDump prints the tree as ast.dump does (ParseAST is not part of the measurement).
func BenchmarkDump(b *testing.B) {
	dir := os.Getenv("PEGO_BENCH_DIR")
	if dir == "" {
		dir = filepath.Join("testdata", "bench")
	}
	data, err := os.ReadFile(filepath.Join(dir, "typing.py"))
	if err != nil {
		b.Fatal(err)
	}
	m, err := python.ParseModule(string(data))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		python.Dump(m)
	}
}
