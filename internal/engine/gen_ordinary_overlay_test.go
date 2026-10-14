package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// This opt-in artifact writer makes latest-generator benchmark controls
// reproducible without patching templates or adding a parse-time option.
func TestGeneratedOrdinaryWorkBenchmarkOverlay(t *testing.T) {
	destination := os.Getenv("PEGO_ORDINARY_WORK_OVERLAY")
	if destination == "" {
		t.Skip("set PEGO_ORDINARY_WORK_OVERLAY to write benchmark artifacts")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(destination, 0755); err != nil {
		t.Fatal(err)
	}
	workloads := []struct{ pkg, path string }{
		{"json", "parsers/json/json.pego"}, {"csv", "parsers/csv/csv.pego"}, {"xml", "parsers/xml/xml.pego"},
		{"calc", "examples/calculator/calc.pego"}, {"calclr", "examples/calculator/calc_lr.pego"},
		{"minilang", "examples/minilang/minilang.pego"}, {"outline", "examples/outline/outline.pego"},
	}
	overlays := map[string]map[string]string{"enabled": {}, "disabled": {}}
	for _, workload := range workloads {
		source, err := os.ReadFile(filepath.Join(root, workload.path))
		if err != nil {
			t.Fatal(err)
		}
		g, err := syntax.Parse(string(source))
		if err != nil {
			t.Fatal(err)
		}
		original := filepath.Join(root, "bench", "gen", workload.pkg, "parser.go")
		checked, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		for _, variant := range []string{"enabled", "disabled"} {
			opts := GenOptions{Package: workload.pkg, Start: "main", Types: true, Recognize: true, disableOrdinaryWorkSpecialization: variant == "disabled"}
			code, err := Generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "enabled" && !bytes.Equal(code, checked) {
				t.Fatalf("%s: generated benchmark source is stale", workload.pkg)
			}
			path := filepath.Join(destination, variant, workload.pkg, "parser.go")
			if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, code, 0644); err != nil {
				t.Fatal(err)
			}
			overlays[variant][original] = path
		}
	}
	for _, variant := range []string{"enabled", "disabled"} {
		data, err := json.MarshalIndent(struct{ Replace map[string]string }{overlays[variant]}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(destination, variant+".json"), append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
