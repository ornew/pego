package parsers_test

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/gen"
)

// manifestFile is the checked-in manifest of the generated output of manifestGrammars in every
// manifestModes option set. Its lines are "grammar mode path sha256 bytes lines funcs", or
// "grammar mode error message" for an option set the generator rejects.
const manifestFile = "testdata/output_manifest.txt"

var outputTree = flag.String("output-tree", "", "with TestGeneratedOutputManifest: write the full text of every manifest output under this directory")

// manifestGrammars are the grammars of the output manifest: the repository parsers and the examples
// that the benchmarks generate. Paths are relative to this package.
var manifestGrammars = []struct{ name, path string }{
	{"cel", "cel/cel.zp"},
	{"csv", "csv/csv.zp"},
	{"cue", "cue/cue.zp"},
	{"duckdb", "duckdb/duckdb.zp"},
	{"golang", "golang/golang.zp"},
	{"json", "json/json.zp"},
	{"python", "python/python.zp"},
	{"typescript", "typescript/typescript.zp"},
	{"xml", "xml/xml.zp"},
	{"yaml", "yaml/yaml.zp"},
	{"calc", "../examples/calculator/calc.zp"},
	{"calclr", "../examples/calculator/calc_lr.zp"},
	{"minilang", "../examples/minilang/minilang.zp"},
	{"outline", "../examples/outline/outline.zp"},
}

// manifestModes are the configurations of the output manifest. Each is generated for every grammar.
var manifestModes = []struct {
	name   string
	config gen.Go
}{
	{"node", gen.Go{}},
	{"node-exp", gen.Go{Expectations: true}},
	{"typed-exp", gen.Go{Types: true, Expectations: true}},
	{"node-rec", gen.Go{Recognize: true}},
	{"node-wc", gen.Go{BudgetControls: true}},
	{"node-rec-wc", gen.Go{Recognize: true, BudgetControls: true}},
	{"node-doc", gen.Go{Document: true}},
	{"node-obs", gen.Go{Observability: true}},
	{"node-rec-obs", gen.Go{Recognize: true, Observability: true}},
	{"node-doc-wc", gen.Go{Document: true, BudgetControls: true}},
	{"node-stream", gen.Go{Stream: true}},
	{"typed", gen.Go{Types: true}},
	{"typed-only", gen.Go{Types: true, TypedOnly: true}},
	{"typed-rec-wc", gen.Go{Types: true, Recognize: true, BudgetControls: true}},
	{"typed-doc", gen.Go{Types: true, Document: true}},
	{"typed-stream", gen.Go{Types: true, Stream: true}},
	{"typed-only-wc", gen.Go{Types: true, TypedOnly: true, BudgetControls: true}},
	{"default-value", gen.Go{DefaultValue: true}},
	{"source-info", gen.Go{SourceInfo: true}},
}

// manifestOutputs generates every output of the manifest and returns its lines, sorted by grammar,
// mode and path, and the full text of each output keyed by "grammar/mode/path".
func manifestOutputs(t *testing.T) ([]string, map[string]string) {
	t.Helper()
	var lines []string
	texts := map[string]string{}
	for _, g := range manifestGrammars {
		src, err := os.ReadFile(g.path)
		if err != nil {
			t.Fatal(err)
		}
		gr, err := zeroparse.ParseGrammar(string(src))
		if err != nil {
			t.Fatalf("%s: %v", g.path, err)
		}
		for _, m := range manifestModes {
			c := m.config
			c.Module, c.Runtime = "gentest/"+g.name, gen.RuntimeImport
			files, err := c.Generate(gr, "")
			if err != nil {
				// A combination the generator rejects, such as -stream without a #stream repetition,
				// is a line of the manifest too, so that a change of rejection fails the test.
				lines = append(lines, fmt.Sprintf("%s %s error %s", g.name, m.name, strings.ReplaceAll(err.Error(), "\n", " ")))
				continue
			}
			slices.SortFunc(files, func(a, b gen.File) int { return strings.Compare(a.Path, b.Path) })
			for _, f := range files {
				sum := sha256.Sum256(f.Content)
				funcs := 0
				for _, l := range strings.Split(string(f.Content), "\n") {
					if strings.HasPrefix(l, "func ") {
						funcs++
					}
				}
				lines = append(lines, fmt.Sprintf("%s %s %s %s %d %d %d", g.name, m.name, f.Path, hex.EncodeToString(sum[:]), len(f.Content), strings.Count(string(f.Content), "\n"), funcs))
				texts[g.name+"/"+m.name+"/"+f.Path] = string(f.Content)
			}
		}
	}
	return lines, texts
}

// TestGeneratedOutputManifest checks the hash, size, line and function counts of every output of the
// generator's manifest matrix against testdata/output_manifest.txt. A change to the generated text
// of any output therefore fails until the manifest is regenerated with go test ./parsers -run
// TestGeneratedOutputManifest -update, which makes the change reviewable as a diff of the manifest.
// The full text of the outputs is written with -output-tree.
func TestGeneratedOutputManifest(t *testing.T) {
	lines, texts := manifestOutputs(t)
	if *outputTree != "" {
		for key, text := range texts {
			path := filepath.Join(append([]string{*outputTree}, strings.Split(key, "/")...)...)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := strings.Join(lines, "\n") + "\n"
	if *update {
		if err := os.MkdirAll(filepath.Dir(manifestFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) == got {
		return
	}
	wantLines := strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
	reported := 0
	for i := 0; i < max(len(lines), len(wantLines)) && reported < 20; i++ {
		var g, w string
		if i < len(lines) {
			g = lines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Errorf("manifest line %d:\n  manifest %s\n  generated %s", i+1, w, g)
			reported++
		}
	}
	t.Errorf("%s is out of date (%d lines generated, %d in the manifest); run go test ./parsers -run TestGeneratedOutputManifest -update", manifestFile, len(lines), len(wantLines))
}
