package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/gen"
)

func TestRuntimeMode(t *testing.T) {
	if gen.RuntimeEmbed.String() != "embed" || gen.RuntimeImport.String() != "import" || gen.RuntimeMode(2).String() != "RuntimeMode(2)" {
		t.Errorf("names: %v %v %v", gen.RuntimeEmbed, gen.RuntimeImport, gen.RuntimeMode(2))
	}
	parser, err := compileTestSource(`rule main = "x"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []gen.Generator{
		gen.Go{Module: "example.com/m", Runtime: gen.RuntimeMode(2)},
		gen.TypeScript{Runtime: gen.RuntimeMode(-1)},
	} {
		if _, err := g.Generate(parser.Grammar(), "main"); err == nil || !strings.Contains(err.Error(), "unknown runtime mode") {
			t.Errorf("%T: %v", g, err)
		}
	}
	// GenerateRuntime writes only the runtime, whatever the mode.
	files, err := gen.Go{Module: "example.com/m", Runtime: gen.RuntimeImport}.GenerateRuntime()
	if err != nil || len(files) == 0 {
		t.Fatalf("Go runtime: %d files, %v", len(files), err)
	}
	for _, f := range files {
		if !strings.HasPrefix(f.Path, "internal/runtime/") {
			t.Errorf("Go runtime file %s", f.Path)
		}
	}
	files, err = gen.TypeScript{}.GenerateRuntime()
	if err != nil || len(files) != 1 || files[0].Path != "runtime.ts" {
		t.Errorf("TypeScript runtime: %v, %v", files, err)
	}
}
