package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// TestGeneratedDocumentBenchmarkFixture exports source for a focused benchmark.
// It never runs the benchmarks. Keep the generated packages separate so the
// batch control compiles the same grammar with and without Document generation.
func TestGeneratedDocumentBenchmarkFixture(t *testing.T) {
	dir := os.Getenv("PEGO_GENERATED_DOCUMENT_BENCH_DIR")
	if dir == "" {
		t.Skip("set PEGO_GENERATED_DOCUMENT_BENCH_DIR to export the benchmark fixture")
	}
	observability := os.Getenv("PEGO_GENERATED_DOCUMENT_OBSERVABILITY") == "1"
	g, err := syntax.Parse(`rule main = record+ $$
rule record = k:"é" ":" v:("0" / "1") ";"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct {
		name     string
		document bool
	}{
		{"generatedbatch", false},
		{"generateddocument", true},
	} {
		code, err := Generate(g, GenOptions{Package: variant.name, Start: "main", Document: variant.document, Observability: observability})
		if err != nil {
			t.Fatal(err)
		}
		if err := writeGeneratedDocumentBenchmarkFile(dir, filepath.Join(variant.name, "parser.go"), code); err != nil {
			t.Fatal(err)
		}
	}
	typedGrammar, err := syntax.Parse(`type Item = { Key node[string]; Value node[string]; N int; S string; B bool }
rule main []Item = item*
rule item Item = k:[a-zé😀]+ #atomic "=" v:[0-9]+ #atomic ";" => Item{Key: $k, Value: $v, N: len($v), S: text($k), B: true}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []struct {
		name     string
		document bool
	}{
		{"typedbatch", false},
		{"typeddocument", true},
	} {
		code, err := Generate(typedGrammar, GenOptions{Package: variant.name, Start: "main", Types: true, Document: variant.document, Observability: observability, convertTypes: os.Getenv("PEGO_GENERATED_DOCUMENT_CONVERSION_REFERENCE") == "1"})
		if err != nil {
			t.Fatal(err)
		}
		if err := writeGeneratedDocumentBenchmarkFile(dir, filepath.Join(variant.name, "parser.go"), code); err != nil {
			t.Fatal(err)
		}
	}

	for _, driver := range []struct{ name, parser, value, input, method, alternate, original, valid string }{
		{"Node", "generateddocument", "*p.Node", `strings.Repeat("é:0;", records)`, "Parse", "1", "0", "value != nil"},
		{"Typed", "typeddocument", "[]*p.Item", "typedBenchmarkInput(records)", "ParseAST", "4", "3", "len(value) == records"},
	} {
		modes := `"none"`
		setup := ""
		check := ""
		modeCheck := `if mode!="none"{b.Fatal("observation mode requires observation-enabled source")}`
		if observability {
			modes = `"none,trace,profile"`
			modeCheck = `if mode!="none"&&mode!="trace"&&mode!="profile"{b.Fatal("unknown observation mode")}`
			setup = `var profile p.Profile
   traceScalar,traceCalls:=0,0
   switch mode {
   case "trace": opts=append(opts,p.WithTrace(func(e p.TraceEvent){traceCalls++;traceScalar=e.Pos^e.End^e.Evals}))
   case "profile": opts=append(opts,p.WithProfile(&profile))
   }
   defer func(){if mode=="trace"&&(traceCalls==0||traceScalar<0){b.Fatal("invalid event")}}()`
			check = `if mode=="profile" && profile.Snapshot().Parses==0 {b.Fatal("profile was inactive")}`
		}
		code := strings.NewReplacer("FUNCTION", driver.name, "PARSER", driver.parser, "VALUE", driver.value, "INPUT", driver.input, "METHOD", driver.method, "ALTERNATE", driver.alternate, "ORIGINAL", driver.original, "VALID", driver.valid, "DEFAULTMODES", modes, "MODECHECK", modeCheck, "SETUP", setup, "CHECK", check).Replace(generatedDocumentObservationBenchmark)
		if err := writeGeneratedDocumentBenchmarkFile(dir, "observe_"+strings.ToLower(driver.name)+"_bench_test.go", []byte(code)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"batch_bench_test.go", "typed_bench_test.go", "generateddocument/document_bench_test.go"} {
		source, err := os.ReadFile(filepath.Join("testdata", "generated_document_bench", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := writeGeneratedDocumentBenchmarkFile(dir, name, source); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeGeneratedDocumentBenchmarkFile(dir, "go.mod", []byte("module generateddocumentbench\n\ngo 1.27.1\n")); err != nil {
		t.Fatal(err)
	}
}

func writeGeneratedDocumentBenchmarkFile(dir, name string, contents []byte) error {
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o644)
}
