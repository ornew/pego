package gogen_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedTypedFramedCuts(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedFramedCuts)
}

func testGeneratedTypedFramedCuts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	cases := gentest.TypedFramedCutCases(t)
	for _, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts := gen.GenOptions{Package: "framedcuts", Start: "main", Types: true, Recognize: true}
		code, err := gentest.GenerateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		marker := "(typed OperatorsRule body inlined)"
		if c.Name == "typed/framed_cut_LR_elements" {
			marker = "(typed LR body inlined)"
		}
		if !strings.Contains(string(code), "TParse(trules") || !strings.Contains(string(code), marker) {
			t.Fatalf("%s: cut-bearing typed body must be exercised", c.Name)
		}
	}
	cases = append(cases, gentest.PrefixPartCutCorpus())
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true})
}
