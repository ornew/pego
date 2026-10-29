package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// The exporter keeps timing and generated artifacts outside the repository.
// Run its standalone benchmarks serially with fixed toolchain and GC settings.
func TestExportGeneratedTraceBenchmarks(t *testing.T) {
	dir := os.Getenv("PEGO_TRACE_BENCH_DIR")
	if dir == "" {
		t.Skip("set PEGO_TRACE_BENCH_DIR to export standalone benchmarks")
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
	write("go.mod", []byte(testGoMod("tracebench")))
	cases := []struct{ name, path, input string }{
		{"json", "parsers/json/json.pego", "[" + strings.TrimSuffix(strings.Repeat(`{"name":"éclair","n":123,"ok":true},`, 64), ",") + "]"},
		{"xml", "parsers/xml/xml.pego", "<root>" + strings.Repeat(`<item name="éclair">123</item>`, 64) + "</root>"},
		{"calc", "examples/calculator/calc.pego", strings.TrimSuffix(strings.Repeat("1+2*3+", 32), "+")},
	}
	sizes := map[string]int{}
	for _, c := range cases {
		src, err := os.ReadFile(filepath.Join("../..", c.path))
		if err != nil {
			t.Fatal(err)
		}
		g, err := parseTest(string(src))
		if err != nil {
			t.Fatal(err)
		}
		for _, enabled := range []bool{false, true} {
			variant := "ordinary"
			if enabled {
				variant = "observed"
			}
			code, err := generateCode(t, g, gen.GenOptions{Package: variant, Start: "main", Recognize: true, Observability: enabled})
			if err != nil {
				t.Fatal(err)
			}
			sizes[c.name+"/"+variant] = len(code)
			write(c.name+"/"+variant+"/parser.go", code)
			driver := strings.NewReplacer("PACKAGE", variant, "INPUT", fmt.Sprintf("%q", c.input)).Replace(generatedTraceBenchmark)
			if enabled {
				driver += generatedTraceEnabledBenchmark
			}
			write(c.name+"/"+variant+"/bench_test.go", []byte(driver))
		}
	}
	data, err := json.MarshalIndent(sizes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write("source-sizes.json", data)
}

const generatedTraceBenchmark = `package PACKAGE
import("testing")
var nodeSink *Node
var errorSink error
const input=INPUT
func BenchmarkParse(b *testing.B){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=[]ParseOption{WithUnit(unit)};if _,err:=Parse(input,opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){nodeSink,errorSink=Parse(input,opts...);if errorSink!=nil{b.Fatal(errorSink)}}})}}
func BenchmarkRecognize(b *testing.B){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=[]ParseOption{WithUnit(unit)};if err:=Recognize(input,opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){errorSink=Recognize(input,opts...);if errorSink!=nil{b.Fatal(errorSink)}}})}}
`
const generatedTraceEnabledBenchmark = `
var eventSink TraceEvent
func BenchmarkTrace(b *testing.B){for _,mode:=range []string{"full","root","unknown"}{b.Run(mode,func(b *testing.B){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=[]ParseOption{WithUnit(unit),WithTrace(func(e TraceEvent){eventSink=e})};if mode=="root"{opts=append(opts,WithTraceMaxDepth(1))};if mode=="unknown"{opts=append(opts,WithTraceFilter("not-defined"))};if _,err:=Parse(input,opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){nodeSink,errorSink=Parse(input,opts...);if errorSink!=nil{b.Fatal(errorSink)}}})}})}}
`

var generatedTraceBenchmarkSource []byte

func BenchmarkGeneratedObservabilityGeneration(b *testing.B) {
	for _, fixture := range []struct{ name, path string }{{"json", "parsers/json/json.pego"}, {"xml", "parsers/xml/xml.pego"}, {"calc", "examples/calculator/calc.pego"}} {
		source, err := os.ReadFile(filepath.Join("../..", fixture.path))
		if err != nil {
			b.Fatal(err)
		}
		g, err := parseTest(string(source))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fixture.name, func(b *testing.B) {
			for _, enabled := range []bool{false, true} {
				name := "ordinary"
				if enabled {
					name = "observed"
				}
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						var err error
						generatedTraceBenchmarkSource, err = generateCode(b, g, gen.GenOptions{Package: "fixture", Start: "main", Recognize: true, Observability: enabled})
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
