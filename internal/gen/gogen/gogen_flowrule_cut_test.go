package gogen_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedTypedDirectCuts(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedDirectCuts)
}

func testGeneratedTypedDirectCuts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	for _, c := range gentest.TypedDirectCutCases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "cuts", Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !strings.Contains(string(code), "TParse(trules") {
			t.Fatalf("%s: typed runtime must be exercised", c.Name)
		}
		if !strings.Contains(string(code), "// main, invoked as by invoke (body inlined)") &&
			!strings.Contains(string(code), "// main, called as by invokePlain (body inlined)") {
			t.Fatalf("%s: main must use the direct typed route", c.Name)
		}
		if c.Name == "typed/direct_cut_callee" && !strings.Contains(string(code), "// part, called as by invokePlain (body inlined)") {
			t.Fatal("callee containing cuts must use the direct typed route")
		}
		for _, r := range mustCompile(t, c.Src).RuleList {
			if !gentest.DirectCuts(r.Def.Expr) {
				continue
			}
			inlined := false
			for _, route := range []string{"invoked as by invoke", "called as by invokePlain"} {
				marker := "// " + r.Name + ", " + route + " (body inlined)"
				inlined = inlined || strings.Contains(string(code), marker)
			}
			if !inlined {
				t.Fatalf("%s: cut-bearing %s must use the direct typed route", c.Name, r.Name)
			}
		}
	}
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), gentest.TypedDirectCutCases, gen.GenOptions{ConvertTypes: true})
}
