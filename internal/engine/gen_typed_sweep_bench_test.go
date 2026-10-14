package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// TestExportOrdinaryTypedSweepBenchmarks writes same-generator reference,
// legacy-allocation and sweep variants for the seven benchmark parsers. The
// output root is disposable and separate from committed bench/gen sources.
// PEGO_TYPED_OUTPUT_SWEEP_EXPORT_DIR preserves both variants for later tests
// and measurements; an empty setting uses t.TempDir.
func TestExportOrdinaryTypedSweepBenchmarks(t *testing.T) {
	if testing.Short() {
		t.Skip("generates standalone benchmark controls")
	}
	root := os.Getenv("PEGO_TYPED_OUTPUT_SWEEP_EXPORT_DIR")
	if root == "" {
		root = t.TempDir()
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	type benchmarkGrammar struct {
		name, path string
	}
	grammars := []benchmarkGrammar{
		{"json", "parsers/json/json.pego"},
		{"csv", "parsers/csv/csv.pego"},
		{"xml", "parsers/xml/xml.pego"},
		{"calc", "examples/calculator/calc.pego"},
		{"calclr", "examples/calculator/calc_lr.pego"},
		{"minilang", "examples/minilang/minilang.pego"},
		{"outline", "examples/outline/outline.pego"},
	}
	for _, spec := range grammars {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			grammarPath := filepath.Join(repoRoot, filepath.FromSlash(spec.path))
			source, err := os.ReadFile(grammarPath)
			if err != nil {
				t.Fatal(err)
			}
			g, err := syntax.Parse(string(source))
			if err != nil {
				t.Fatal(err)
			}
			var reference []byte
			for _, variant := range []struct {
				name                   string
				sweep, bounded, legacy bool
			}{{"ref", false, false, false}, {"legacy", false, false, true}, {"sweep", true, false, false}, {"bounded", false, true, false}} {
				code, err := Generate(g, GenOptions{
					Package: spec.name, Start: "main", Types: true, Recognize: true,
					enableTypedOutputSweep: variant.sweep, enableBoundedTypedOutputSweep: variant.bounded,
					disableTypedOutputBootstrap: variant.legacy,
				})
				if err != nil {
					t.Fatalf("generate %s: %v", variant.name, err)
				}
				if variant.name == "ref" {
					reference = code
					committedPath := filepath.Join(repoRoot, "bench", "gen", spec.name, "parser.go")
					committed, err := os.ReadFile(committedPath)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(code, committed) {
						t.Fatalf("reference output differs from preserved %s", committedPath)
					}
				} else if bytes.Equal(reference, code) {
					t.Fatalf("%s sweep output is byte-identical to the reference", spec.name)
				}
				dir := filepath.Join(root, variant.name, "bench", "gen", spec.name)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "parser.go"), code, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	var packages []string
	for _, spec := range grammars {
		packages = append(packages, spec.name)
	}
	for _, variant := range []string{"ref", "legacy", "sweep", "bounded"} {
		writeOrdinaryOverlay(t, root, repoRoot, variant, packages)
	}
}
