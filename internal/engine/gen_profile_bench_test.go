package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// Export standalone controls; keep generated fixtures and results outside Git.
func TestExportGeneratedGoProfileBenchmarks(t *testing.T) {
	dir := os.Getenv("PEGO_PROFILE_BENCH_DIR")
	if dir == "" {
		t.Skip("set PEGO_PROFILE_BENCH_DIR to export profile benchmarks")
	}
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(testGoMod("profilebench")))
	sizes := map[string]int{}
	for _, f := range []struct{ name, path, input string }{
		{"json", "parsers/json/json.pego", "[" + strings.TrimSuffix(strings.Repeat(`{"name":"éclair","n":123,"ok":true},`, 64), ",") + "]"},
		{"xml", "parsers/xml/xml.pego", "<root>" + strings.Repeat(`<item name="éclair">123</item>`, 64) + "</root>"},
		{"calc", "examples/calculator/calc.pego", strings.TrimSuffix(strings.Repeat("1+2*3+", 32), "+")},
	} {
		src, err := os.ReadFile(filepath.Join("../..", f.path))
		if err != nil {
			t.Fatal(err)
		}
		g, err := syntax.Parse(string(src))
		if err != nil {
			t.Fatal(err)
		}
		for _, typed := range []bool{false, true} {
			route := "node"
			if typed {
				route = "typed"
			}
			for _, observed := range []bool{false, true} {
				variant := "ordinary"
				if observed {
					variant = "observed"
				}
				code, err := generateCode(g, GenOptions{Package: variant, Start: "main", Types: typed, Recognize: true, Observability: observed})
				if err != nil {
					t.Fatal(err)
				}
				path := f.name + "/" + route + "/" + variant
				write(path+"/parser.go", code)
				sizes[path] = len(code)
				driver := strings.NewReplacer("PACKAGE", variant, "INPUT", fmt.Sprintf("%q", f.input)).Replace(generatedGoProfileBenchmark)
				if typed {
					driver += generatedGoProfileTypedBenchmark
				}
				if observed {
					driver += generatedGoProfileTraceBenchmark
				}
				if observed && os.Getenv("PEGO_PROFILE_BASE") == "" {
					driver += generatedGoProfileEnabledBenchmark
					if typed {
						driver += generatedGoProfileTypedEnabledBenchmark
					}
				}
				write(path+"/bench_test.go", []byte(driver))
			}
		}
	}
	data, err := json.MarshalIndent(sizes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write("source-sizes.json", data)
}

const generatedGoProfileBenchmark = `package PACKAGE
import("testing")
const input=INPUT
var nodeSink *Node
var errorSink error
var valueSink any
func benchOperation(b *testing.B,op func(...ParseOption)error, extra ...ParseOption){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=append([]ParseOption{WithUnit(unit)},extra...);if err:=op(opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){errorSink=op(opts...);if errorSink!=nil{b.Fatal(errorSink)}}})}}
func benchmarkParseOp(opts ...ParseOption)error{var err error;nodeSink,err=Parse(input,opts...);return err}
func benchmarkRecognizeOp(opts ...ParseOption)error{return Recognize(input,opts...)}
func BenchmarkParse(b *testing.B){benchOperation(b,benchmarkParseOp)}
func BenchmarkRecognize(b *testing.B){benchOperation(b,benchmarkRecognizeOp)}
`
const generatedGoProfileTypedBenchmark = `
func benchmarkASTOp(opts ...ParseOption)error{var err error;valueSink,err=ParseAST(input,opts...);return err}
func BenchmarkParseAST(b *testing.B){benchOperation(b,benchmarkASTOp)}
`
const generatedGoProfileEnabledBenchmark = `
func BenchmarkProfileParse(b *testing.B){var pr Profile;benchOperation(b,benchmarkParseOp,WithProfile(&pr))}
func BenchmarkProfileRecognize(b *testing.B){var pr Profile;benchOperation(b,benchmarkRecognizeOp,WithProfile(&pr))}
`
const generatedGoProfileTypedEnabledBenchmark = `
func BenchmarkProfileParseAST(b *testing.B){var pr Profile;benchOperation(b,benchmarkASTOp,WithProfile(&pr))}
`

const generatedGoProfileTraceBenchmark = `
var traceSink TraceEvent
func BenchmarkTraceParse(b *testing.B){benchOperation(b,benchmarkParseOp,WithTrace(func(e TraceEvent){traceSink=e}))}
`
