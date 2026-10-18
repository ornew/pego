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

// Export isolated typed tracing controls, keeping generation, compilation and
// raw measurements outside the timed parse loop and outside the repository.
func TestExportGeneratedTypedTraceBenchmarks(t *testing.T) {
	dir := os.Getenv("PEGO_TYPED_TRACE_BENCH_DIR")
	if dir == "" {
		t.Skip("set PEGO_TYPED_TRACE_BENCH_DIR to export benchmark controls")
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
	write("go.mod", []byte("module typedtracebench\n\ngo 1.27.1\n"))
	manifest := map[string]struct {
		Bytes       int
		DirectStart bool
	}{}
	for _, fixture := range []struct{ name, path, input string }{
		{"json", "parsers/json/json.pego", "[" + strings.TrimSuffix(strings.Repeat(`{"name":"éclair","n":123,"ok":true},`, 64), ",") + "]"},
		{"xml", "parsers/xml/xml.pego", "<root>" + strings.Repeat(`<item name="éclair">123</item>`, 64) + "</root>"},
		{"calc", "examples/calculator/calc.pego", strings.TrimSuffix(strings.Repeat("1+2*3+", 32), "+")},
	} {
		data, err := os.ReadFile(filepath.Join("../..", fixture.path))
		if err != nil {
			t.Fatal(err)
		}
		g, err := syntax.Parse(string(data))
		if err != nil {
			t.Fatal(err)
		}
		for _, convert := range []bool{false, true} {
			route := "auto"
			if convert {
				route = "converted"
			}
			for _, enabled := range []bool{false, true} {
				pkg := "ordinary"
				if enabled {
					pkg = "observed"
				}
				code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, Observability: enabled, convertTypes: convert})
				if err != nil {
					t.Fatal(err)
				}
				path := fixture.name + "/" + route + "/" + pkg
				manifest[path] = struct {
					Bytes       int
					DirectStart bool
				}{len(code), strings.Contains(string(code), "return tparse(trules[")}
				write(path+"/parser.go", code)
				driver := strings.NewReplacer("PACKAGE", pkg, "INPUT", fmt.Sprintf("%q", fixture.input)).Replace(generatedTypedTraceBenchmark)
				if enabled {
					driver += generatedTypedTraceEnabledBenchmark
				}
				write(path+"/bench_test.go", []byte(driver))
			}
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write("manifest.json", data)
}

const generatedTypedTraceBenchmark = `package PACKAGE
import("testing")
var typedSink any
var errorSink error
const input=INPUT
func BenchmarkAST(b *testing.B){for _,named:=range []bool{false,true}{route:="ParseAST";if named{route="ParseRuleAST"};b.Run(route,func(b *testing.B){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=[]ParseOption{WithUnit(unit)};if _,err:=ParseAST(input,opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){if named{typedSink,errorSink=ParseRuleAST("main",input,opts...)}else{typedSink,errorSink=ParseAST(input,opts...)};if errorSink!=nil{b.Fatal(errorSink)}}})}})}}
`
const generatedTypedTraceEnabledBenchmark = `
var eventSink TraceEvent
func BenchmarkASTTrace(b *testing.B){for _,mode:=range []string{"full","root"}{b.Run(mode,func(b *testing.B){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=[]ParseOption{WithUnit(unit),WithTrace(func(e TraceEvent){eventSink=e})};if mode=="root"{opts=append(opts,WithTraceMaxDepth(1))};if _,err:=ParseAST(input,opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){typedSink,errorSink=ParseAST(input,opts...);if errorSink!=nil{b.Fatal(errorSink)}}})}})}}
`
