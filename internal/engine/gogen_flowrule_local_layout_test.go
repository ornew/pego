package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedTypedLocalLayouts(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedLocalLayouts)
}

func testGeneratedTypedLocalLayouts(t *testing.T) {
	for _, c := range gentest.TypedLocalLayoutCases {
		ast, err := parseTest(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		opts := gen.GenOptions{Package: "localfixture", Start: "main", Types: true, Recognize: true}
		code, err := gentest.GenerateCode(t, ast, opts)
		if err != nil || !strings.Contains(string(code), "rt.TParse(trules") || !localBodyInlined(code) {
			t.Fatalf("%s: typed local body unavailable: %v", c.Name, err)
		}
	}
	if !testing.Short() {
		goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
		gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedLocalLayoutCases, gen.GenOptions{})
		gentest.GeneratedTypesCorpus(t, goBin, gentest.TypedLocalLayoutCases, gen.GenOptions{ConvertTypes: true})
	}
}

func localBodyInlined(code []byte) bool {
	lines := strings.Split(string(code), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "// body, ") && strings.Contains(line, "(body inlined)") &&
			i+1 < len(lines) && strings.HasPrefix(lines[i+1], "func i") && strings.Contains(lines[i+1], "(p *rt.TParser)") {
			return true
		}
	}
	return false
}

func TestGeneratedTypedLocalLayoutControls(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedLocalLayoutControls)
}

func testGeneratedTypedLocalLayoutControls(t *testing.T) {
	for _, path := range []string{"../../examples/calculator/calc.pego", "../../parsers/typescript/typescript.pego"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ast, err := parseTest(string(src))
		if err != nil {
			t.Fatal(err)
		}
		opts := gen.GenOptions{Package: "localfixture", Start: "main", Types: true, Recognize: true}
		if _, err := gentest.GenerateCode(t, ast, opts); err != nil {
			t.Fatal(err)
		}
	}
}
